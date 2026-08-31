// Package metrics mengumpulkan metrik sistem dan menyediakannya sebagai
// snapshot yang siap di-broadcast lewat WebSocket.
package metrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Semua angka ukuran dikirim mentah dalam byte. Penskalaan
// B → KB → MB → GB → TB dilakukan di frontend, supaya tampilan bisa berubah
// tanpa perlu request ulang ke backend.

type CPUInfo struct {
	TotalPct float64   `json:"total_pct"`
	PerCore  []float64 `json:"per_core"`
	Cores    int       `json:"cores"`
	Model    string    `json:"model"`
	Load1    float64   `json:"load1"`
	Load5    float64   `json:"load5"`
	Load15   float64   `json:"load15"`
}

type MemoryInfo struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Cached    uint64  `json:"cached"`
	UsedPct   float64 `json:"used_pct"`
	SwapTotal uint64  `json:"swap_total"`
	SwapUsed  uint64  `json:"swap_used"`
	SwapPct   float64 `json:"swap_pct"`
}

type DiskInfo struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	FSType  string  `json:"fstype"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	UsedPct float64 `json:"used_pct"`
}

// UnusedDisk adalah disk fisik yang terpasang tapi belum dipakai: tanpa
// partisi, tanpa holder (LVM/RAID), dan tidak ter-mount. Tanpa ini, disk yang
// baru ditambahkan di hypervisor tidak muncul sama sekali di panel — statfs
// hanya melihat filesystem yang sudah ter-mount.
type UnusedDisk struct {
	Name  string `json:"name"` // sdb
	Path  string `json:"path"` // /dev/sdb
	Size  uint64 `json:"size"` // byte
	Model string `json:"model,omitempty"`
}

type NetInfo struct {
	Name    string `json:"name"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	// Rate dalam byte per detik, dihitung dari selisih antar tick.
	RxRate uint64 `json:"rx_rate"`
	TxRate uint64 `json:"tx_rate"`
}

type Snapshot struct {
	Timestamp int64      `json:"timestamp"`
	Uptime    uint64     `json:"uptime"`
	CPU       CPUInfo    `json:"cpu"`
	Memory    MemoryInfo `json:"memory"`
	Disks     []DiskInfo `json:"disks"`
	// UnusedDisks tidak ikut dijumlahkan ke total storage — kapasitasnya
	// belum bisa dipakai sampai disknya diformat dan di-mount.
	UnusedDisks []UnusedDisk `json:"unused_disks"`
	GPUs        []GPUInfo    `json:"gpus"`
	Network     []NetInfo    `json:"network"`
	Processes   int          `json:"processes"`
}

// Frame membawa snapshot beserta payload JSON-nya yang sudah di-serialize
// SEKALI per tick. Dengan belasan client terhubung, marshal per-client jadi
// beban CPU yang tidak perlu di mesin 2 core.
type Frame struct {
	Snapshot Snapshot
	JSON     []byte
}

type Collector struct {
	mu       sync.RWMutex
	last     Snapshot
	lastNet  map[string]net.IOCountersStat
	lastTime time.Time

	cpuModel string
	gpu      *GPUDetector
	// gpuDicekPada menandai kapan tooling GPU terakhir dipindai ulang.
	gpuDicekPada time.Time

	subs   map[chan Frame]struct{}
	subsMu sync.Mutex

	// interval adalah tick tercepat yang diminta client mana pun.
	intervalMu sync.Mutex
	interval   time.Duration
	wake       chan struct{}
}

func NewCollector() *Collector {
	c := &Collector{
		lastNet:      map[string]net.IOCountersStat{},
		subs:         map[chan Frame]struct{}{},
		interval:     time.Second,
		wake:         make(chan struct{}, 1),
		gpu:          DetectGPUs(),
		gpuDicekPada: time.Now(),
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		c.cpuModel = infos[0].ModelName
	}
	return c
}

// SetInterval memasang interval tick tercepat yang diminta client.
// Broadcaster yang menyaring per-client, jadi client dengan interval lambat
// tidak memaksa yang lain ikut lambat, dan sebaliknya.
func (c *Collector) SetInterval(d time.Duration) {
	if d < 250*time.Millisecond {
		d = 250 * time.Millisecond
	}
	c.intervalMu.Lock()
	changed := c.interval != d
	c.interval = d
	c.intervalMu.Unlock()
	if changed {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

func (c *Collector) currentInterval() time.Duration {
	c.intervalMu.Lock()
	defer c.intervalMu.Unlock()
	return c.interval
}

func (c *Collector) Subscribe() chan Frame {
	ch := make(chan Frame, 1)
	c.subsMu.Lock()
	c.subs[ch] = struct{}{}
	c.subsMu.Unlock()
	return ch
}

func (c *Collector) Unsubscribe(ch chan Frame) {
	c.subsMu.Lock()
	delete(c.subs, ch)
	c.subsMu.Unlock()
}

func (c *Collector) Last() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

func (c *Collector) Run(ctx context.Context) {
	// Panggilan pertama cpu.Percent menetapkan baseline; hasilnya dibuang.
	_, _ = cpu.Percent(0, false)
	c.collect()

	for {
		timer := time.NewTimer(c.currentInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.wake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		snap := c.collect()
		payload, err := json.Marshal(snap)
		if err != nil {
			continue
		}
		frame := Frame{Snapshot: snap, JSON: payload}
		c.subsMu.Lock()
		for ch := range c.subs {
			select {
			case ch <- frame:
			default:
				// Client lambat: lewati tick ini daripada memblok collector.
			}
		}
		c.subsMu.Unlock()
	}
}

func (c *Collector) collect() Snapshot {
	now := time.Now()
	snap := Snapshot{Timestamp: now.UnixMilli()}

	if total, err := cpu.Percent(0, false); err == nil && len(total) > 0 {
		snap.CPU.TotalPct = round1(total[0])
	}
	if per, err := cpu.Percent(0, true); err == nil {
		snap.CPU.PerCore = make([]float64, len(per))
		for i, v := range per {
			snap.CPU.PerCore[i] = round1(v)
		}
		snap.CPU.Cores = len(per)
	}
	snap.CPU.Model = c.cpuModel
	if avg, err := load.Avg(); err == nil {
		snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15 = avg.Load1, avg.Load5, avg.Load15
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		snap.Memory = MemoryInfo{
			Total: vm.Total, Used: vm.Used, Available: vm.Available,
			Cached: vm.Cached, UsedPct: round1(vm.UsedPercent),
		}
	}
	if sw, err := mem.SwapMemory(); err == nil {
		snap.Memory.SwapTotal, snap.Memory.SwapUsed = sw.Total, sw.Used
		snap.Memory.SwapPct = round1(sw.UsedPercent)
	}

	snap.Disks = collectDisks()
	snap.UnusedDisks = UnusedDisks()
	snap.GPUs = c.detektorGPU().Collect()
	snap.Network = c.collectNetwork(now)

	if up, err := host.Uptime(); err == nil {
		snap.Uptime = up
	}
	if pids, err := process.Pids(); err == nil {
		snap.Processes = len(pids)
	}

	c.mu.Lock()
	c.last = snap
	c.mu.Unlock()
	return snap
}

// virtualFS adalah filesystem semu yang tidak berguna ditampilkan sebagai
// storage (tidak punya kapasitas nyata milik user).
var virtualFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"devpts": true, "ramfs": true, "efivarfs": true, "autofs": true,
	"binfmt_misc": true, "fusectl": true, "configfs": true, "tracefs": true,
	"debugfs": true, "pstore": true, "bpf": true, "securityfs": true,
	"mqueue": true, "hugetlbfs": true, "nsfs": true,
}

func collectDisks() []DiskInfo {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []DiskInfo{}
	for _, p := range parts {
		if virtualFS[p.Fstype] || seen[p.Mountpoint] {
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		seen[p.Mountpoint] = true
		out = append(out, DiskInfo{
			Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype,
			Total: usage.Total, Used: usage.Used, Free: usage.Free,
			UsedPct: round1(usage.UsedPercent),
		})
	}
	return out
}

// UnusedDisks membaca /sys/block langsung, bukan lsblk: daftar yang dicari
// (nama, ukuran, holder, partisi) semuanya ada di sysfs, jadi tidak perlu
// proses anak tiap tick.
//
// Diekspor karena helper memakainya sebagai guard sebelum memformat disk:
// yang boleh diformat hanya disk yang daftar ini akui kosong, sehingga UI dan
// helper tidak pernah memakai dua definisi "belum dipakai" yang berbeda.
func UnusedDisks() []UnusedDisk {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	mounted := map[string]bool{}
	if parts, err := disk.Partitions(true); err == nil {
		for _, p := range parts {
			mounted[p.Device] = true
		}
	}

	out := []UnusedDisk{}
	for _, e := range entries {
		name := e.Name()
		if virtualBlock(name) {
			continue
		}
		dir := filepath.Join("/sys/block", name)
		// Punya partisi atau holder (LVM/RAID/dm-crypt) = sudah dipakai,
		// walau whole-disk-nya sendiri tidak pernah ter-mount.
		if adaIsi(filepath.Join(dir, "holders")) || punyaPartisi(dir, name) {
			continue
		}
		if mounted["/dev/"+name] || sysfsUint(filepath.Join(dir, "size")) == 0 {
			continue
		}
		out = append(out, UnusedDisk{
			Name:  name,
			Path:  "/dev/" + name,
			Size:  sysfsUint(filepath.Join(dir, "size")) * 512, // sektor sysfs selalu 512 B
			Model: bacaSysfs(filepath.Join(dir, "device", "model")),
		})
	}
	return out
}

// virtualBlock menyaring block device yang bukan disk fisik — tidak ada
// gunanya menawarkan loop/ram/dm sebagai "disk kosong yang bisa dipakai".
func virtualBlock(name string) bool {
	for _, p := range []string{"loop", "ram", "zram", "dm-", "md", "sr", "fd", "nbd"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func punyaPartisi(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		// Partisi muncul sebagai subdirektori berawalan nama disknya
		// (sda1, nvme0n1p1) dan punya file "partition".
		if strings.HasPrefix(e.Name(), name) && fileAda(filepath.Join(dir, e.Name(), "partition")) {
			return true
		}
	}
	return false
}

func adaIsi(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func fileAda(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func bacaSysfs(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func sysfsUint(path string) uint64 {
	v, err := strconv.ParseUint(bacaSysfs(path), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// detektorGPU mengembalikan detektor yang masih relevan, memindai ulang
// tooling vendor secara berkala.
//
// DetectGPUs() semula hanya dipanggil sekali saat Collector dibuat. Akibatnya
// rocm-smi/nvidia-smi/intel_gpu_top yang dipasang SETELAH panel hidup tidak
// pernah terpakai: dashboard terus melaporkan "Tidak terdeteksi" sampai
// service di-restart, dan tidak ada apa pun di UI yang memberi tahu bahwa
// restart itu syaratnya. Hal yang sama berlaku untuk GPU yang baru
// di-passthrough ke container.
//
// Pemindaiannya murah — tiga LookPath dan satu pembacaan direktori — jadi
// mengulanginya semenit sekali tidak terasa, sementara menunggu restart
// terasa seperti panel yang rusak.
func (c *Collector) detektorGPU() *GPUDetector {
	const jedaPindai = time.Minute
	if c.gpu == nil || time.Since(c.gpuDicekPada) > jedaPindai {
		c.gpu = DetectGPUs()
		c.gpuDicekPada = time.Now()
	}
	return c.gpu
}

func (c *Collector) collectNetwork(now time.Time) []NetInfo {
	counters, err := net.IOCounters(true)
	if err != nil {
		return nil
	}
	elapsed := now.Sub(c.lastTime).Seconds()
	out := []NetInfo{}
	for _, ct := range counters {
		if ct.Name == "lo" || ct.BytesRecv+ct.BytesSent == 0 {
			continue
		}
		ni := NetInfo{Name: ct.Name, RxBytes: ct.BytesRecv, TxBytes: ct.BytesSent}
		if prev, ok := c.lastNet[ct.Name]; ok && elapsed > 0 {
			// Counter kernel bisa reset (interface down/up) — hasil negatif
			// dibuang, bukan dibiarkan wrap jadi angka raksasa.
			if ct.BytesRecv >= prev.BytesRecv {
				ni.RxRate = uint64(float64(ct.BytesRecv-prev.BytesRecv) / elapsed)
			}
			if ct.BytesSent >= prev.BytesSent {
				ni.TxRate = uint64(float64(ct.BytesSent-prev.BytesSent) / elapsed)
			}
		}
		c.lastNet[ct.Name] = ct
		out = append(out, ni)
	}
	c.lastTime = now
	return out
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
