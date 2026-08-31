package metrics

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GPUInfo struct {
	Vendor         string  `json:"vendor"`
	Name           string  `json:"name"`
	UtilizationPct float64 `json:"utilization_pct"`
	MemUsedMB      uint64  `json:"mem_used_mb"`
	MemTotalMB     uint64  `json:"mem_total_mb"`
	TempC          float64 `json:"temp_c,omitempty"`
}

// GPUDetector menyimpan hasil deteksi vendor yang dilakukan sekali saat
// startup. Bisa lebih dari satu vendor aktif kalau mesinnya hybrid.
type GPUDetector struct {
	winDicek   bool
	rocmSMI    string
	nvidiaSMI  string
	intelTool  string
	sysfsCards []sysfsCard
}

type sysfsCard struct {
	vendor   string
	name     string
	busyPath string
	memUsed  string
	memTotal string
	tempPath string
}

// DetectGPUs mengecek tooling tiap vendor sesuai urutan prioritas:
// AMD dulu (target utama), lalu NVIDIA, lalu Intel. Sysfs dipakai sebagai
// fallback karena banyak sistem AMD/Intel tanpa stack vendor penuh tetap
// mengekspos gpu_busy_percent.
func DetectGPUs() *GPUDetector {
	d := &GPUDetector{
		rocmSMI:   cariAlat("rocm-smi"),
		nvidiaSMI: cariAlat("nvidia-smi"),
		intelTool: cariAlat("intel_gpu_top"),
	}
	d.sysfsCards = scanSysfsCards()
	return d
}

// gpuPATH adalah tempat alat vendor dicari, dan sengaja tidak memakai PATH
// milik proses: service berjalan lewat systemd yang PATH-nya minimal.
//
//	/opt/rocm/bin     — ROCm dari installer AMD tidak menaruh symlink di /usr/bin.
//	/usr/lib/wsl/lib  — di WSL, nvidia-smi datang dari driver Windows lewat
//	                    direktori ini, dan hanya masuk PATH shell interaktif.
const gpuPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/rocm/bin:/usr/lib/wsl/lib"

// cariAlat mengembalikan path lengkap alat vendor, atau "" kalau tidak ada.
// Path lengkap penting karena exec.Command menyelesaikan nama lewat PATH
// proses saat perintah dibuat — mengisi cmd.Env belakangan tidak mengubah
// pencarian itu, jadi alat di /opt/rocm/bin atau /usr/lib/wsl/lib tidak akan
// pernah ketemu kalau hanya diandalkan pada nama.
func cariAlat(nama string) string {
	for _, dir := range strings.Split(gpuPATH, ":") {
		p := filepath.Join(dir, nama)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func (d *GPUDetector) Collect() []GPUInfo {
	out := []GPUInfo{}
	if d.nvidiaSMI != "" {
		out = append(out, collectNvidia(d.nvidiaSMI)...)
	}
	if d.rocmSMI != "" {
		if amd := collectROCm(d.rocmSMI); len(amd) > 0 {
			out = append(out, amd...)
		}
	}
	// Sysfs melengkapi vendor yang belum terwakili tool khususnya.
	haveVendor := map[string]bool{}
	for _, g := range out {
		haveVendor[g.Vendor] = true
	}
	for _, c := range d.sysfsCards {
		if haveVendor[c.vendor] {
			continue
		}
		if g, ok := readSysfsCard(c); ok {
			out = append(out, g)
		}
	}
	// Di WSL, semua jalur di atas selalu kosong: GPU-nya tidak pernah muncul
	// di /sys/class/drm dan rocm-smi dari repo distro membaca sysfs yang sama.
	if len(out) == 0 {
		out = append(out, d.gpuWindows()...)
	}
	// Fallback terakhir: tidak ada /sys/class/drm (WSL, lxc tanpa passthrough)
	// tapi driver dimuat oleh kernel. Laporkan satu entri generik dengan
	// nama driver — lebih berguna daripada "tidak terdeteksi" padahal GPU
	// jelas aktif.
	if len(out) == 0 {
		if drv := driverAktif(); drv != "" {
			out = append(out, GPUInfo{
				Vendor: vendorDriver(drv), Name: "Driver " + drv,
			})
		}
	}
	return out
}

// vendorDriver menerjemahkan nama driver kernel ke label vendor yang dipakai
// di UI. Driver Nouveau milik NVIDIA, amdgpu milik AMD, i915 milik Intel.
func vendorDriver(drv string) string {
	switch drv {
	case "amdgpu", "radeon":
		return "AMD"
	case "nvidia", "nouveau":
		return "NVIDIA"
	case "i915", "i965":
		return "Intel"
	}
	return "GPU"
}

// driverAktif mengembalikan nama driver GPU yang dimuat kernel, dicek dari
// /sys/module/<drv>. Dipakai untuk WSL/lxc yang tidak punya /sys/class/drm
// (zero device file) tetapi tetap memuat modul driver.
func driverAktif() string {
	for _, drv := range []string{"amdgpu", "nvidia", "nouveau", "i915", "radeon"} {
		if _, err := os.Stat("/sys/module/" + drv); err == nil {
			return drv
		}
	}
	return ""
}

// pciVendors memetakan ID vendor PCI ke nama yang ditampilkan.
var pciVendors = map[string]string{
	"0x1002": "AMD",
	"0x1022": "AMD",
	"0x10de": "NVIDIA",
	"0x8086": "Intel",
}

func scanSysfsCards() []sysfsCard {
	matches, err := filepath.Glob("/sys/class/drm/card[0-9]*/device")
	if err != nil {
		return nil
	}
	var out []sysfsCard
	for _, dev := range matches {
		// Lewati connector (card0-DP-1) — hanya device utama yang punya vendor.
		vendorID := strings.TrimSpace(readSysfs(filepath.Join(dev, "vendor")))
		vendor, ok := pciVendors[vendorID]
		if !ok {
			continue
		}
		// Tiap metrik dipasang sendiri-sendiri kalau file sysfs-nya ada. Driver
		// lawas (mis. amdgpu di APU Kabini) sering hanya mengekspos suhu tanpa
		// gpu_busy_percent atau mem_info_vram_* — sebelumnya kartu seperti itu
		// dilaporkan tanpa metrik apa pun, termasuk suhu yang sebenarnya terbaca.
		// Kartu TETAP dilaporkan walau tidak ada satu pun metrik, supaya UI tidak
		// menulis "tidak terdeteksi" untuk GPU yang jelas ada di PCIe.
		c := sysfsCard{vendor: vendor, name: gpuNameFromSysfs(dev, vendor)}
		if busy := filepath.Join(dev, "gpu_busy_percent"); fileExists(busy) {
			c.busyPath = busy
		}
		memUsed := filepath.Join(dev, "mem_info_vram_used")
		memTotal := filepath.Join(dev, "mem_info_vram_total")
		if fileExists(memUsed) && fileExists(memTotal) {
			c.memUsed, c.memTotal = memUsed, memTotal
		}
		if hw, err := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*", "temp1_input")); err == nil && len(hw) > 0 {
			c.tempPath = hw[0]
		}
		out = append(out, c)
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func gpuNameFromSysfs(dev, vendor string) string {
	// Nama produk ada di database pci.ids, bukan di sysfs — sysfs hanya
	// menyimpan kode device (mis. 0x9837). lspci membacakan database itu,
	// sumber yang sama dipakai neofetch, jadi namanya cocok dengan yang
	// biasa dilihat user di terminal.
	if name := lspciName(dev); name != "" {
		return name
	}
	// pciutils tidak terpasang: jatuh ke kode device supaya tetap informatif.
	device := strings.TrimSpace(readSysfs(filepath.Join(dev, "device")))
	if device == "" {
		return vendor + " GPU"
	}
	return vendor + " GPU (" + device + ")"
}

// lspciNames dihitung sekali: daftar perangkat PCI tidak berubah selama
// proses hidup, dan memanggil lspci tiap tick collector (1 detik) sia-sia.
var (
	lspciOnce  sync.Once
	lspciNames map[string]string
)

// lspciName menerjemahkan direktori device sysfs jadi nama produk.
// /sys/class/drm/card0/device adalah symlink ke ".../0000:00:01.0", dan
// alamat itulah kunci yang dipakai lspci.
func lspciName(dev string) string {
	lspciOnce.Do(func() { lspciNames = scanLspci() })
	target, err := filepath.EvalSymlinks(dev)
	if err != nil {
		return ""
	}
	return lspciNames[filepath.Base(target)]
}

func scanLspci() map[string]string {
	// -mm = keluaran mesin (field ber-tanda kutip), -D = alamat lengkap
	// dengan domain supaya cocok dengan nama direktori sysfs.
	out := runGPUCmd(cariAlat("lspci"), "-mmD")
	names := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		// Format: <alamat> "<kelas>" "<vendor>" "<device>" ...
		f := strings.Split(line, `"`)
		if len(f) < 6 {
			continue
		}
		class := f[1]
		if !strings.Contains(class, "VGA") && !strings.Contains(class, "3D") &&
			!strings.Contains(class, "Display") {
			continue
		}
		if name := gpuDisplayName(f[3], f[5]); name != "" {
			names[strings.TrimSpace(f[0])] = name
		}
	}
	return names
}

// gpuDisplayName menyusun nama tampil dari field vendor & device milik lspci.
// Keduanya sering memuat nama pemasaran di dalam kurung siku, sementara di
// luar kurung adalah nama korporat atau kode arsitektur:
//
//	vendor "Advanced Micro Devices, Inc. [AMD/ATI]" → "AMD ATI"
//	device "Kabini [Radeon HD 8280E]"               → "Radeon HD 8280E"
//	hasil                                           → "AMD ATI Radeon HD 8280E"
func gpuDisplayName(vendor, device string) string {
	v := bracketOrTrim(vendor, true)
	d := bracketOrTrim(device, false)
	switch {
	case v == "" && d == "":
		return ""
	case v == "":
		return d
	case d == "":
		return v
	case strings.HasPrefix(strings.ToLower(d), strings.ToLower(v)):
		// Hindari "Intel Intel HD Graphics".
		return d
	}
	return v + " " + d
}

// corporateSuffix adalah kata yang tidak menambah informasi pada nama produk.
var corporateSuffix = []string{"corporation", "corp.", "corp", "inc.", "inc", "ltd.", "ltd", "co.", "company"}

// bracketOrTrim mengambil isi kurung siku kalau ada — di situlah nama
// pemasaran berada. Kalau tidak ada, buang kata korporat dari nama polos.
// Vendor memakai "/" sebagai pemisah merek ("AMD/ATI"), device tidak.
func bracketOrTrim(s string, isVendor bool) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.Index(s[i:], "]"); j > 0 {
			inner := strings.TrimSpace(s[i+1 : i+j])
			if isVendor {
				inner = strings.ReplaceAll(inner, "/", " ")
			}
			return strings.Join(strings.Fields(inner), " ")
		}
	}
	var kept []string
	for _, w := range strings.Fields(strings.ReplaceAll(s, ",", " ")) {
		if slices.Contains(corporateSuffix, strings.ToLower(w)) {
			continue
		}
		kept = append(kept, w)
	}
	return strings.Join(kept, " ")
}

func readSysfsCard(c sysfsCard) (GPUInfo, bool) {
	g := GPUInfo{Vendor: c.vendor, Name: c.name}
	if c.busyPath != "" {
		busy := strings.TrimSpace(readSysfs(c.busyPath))
		if busy != "" {
			if pct, err := strconv.ParseFloat(busy, 64); err == nil {
				g.UtilizationPct = pct
			}
		}
	}
	if c.memUsed != "" && c.memTotal != "" {
		if v, err := strconv.ParseUint(strings.TrimSpace(readSysfs(c.memUsed)), 10, 64); err == nil {
			g.MemUsedMB = v / (1 << 20)
		}
		if v, err := strconv.ParseUint(strings.TrimSpace(readSysfs(c.memTotal)), 10, 64); err == nil {
			g.MemTotalMB = v / (1 << 20)
		}
	}
	if c.tempPath != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(readSysfs(c.tempPath)), 64); err == nil {
			g.TempC = v / 1000
		}
	}
	// Selalu return true kalau sysfs card diikutsertakan; util/mem 0 = driver
	// belum expose metric tersebut (umum pada amdgpu lawas atau LXC passthrough).
	return g, true
}

func readSysfs(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// gpuCmdTimeout menjaga tool vendor yang menggantung tidak menahan tick
// collector (interval default hanya 1 detik).
const gpuCmdTimeout = 2 * time.Second

func runGPUCmd(bin string, args ...string) string {
	return runGPUCmdT(gpuCmdTimeout, bin, args...)
}

func runGPUCmdT(timeout time.Duration, bin string, args ...string) string {
	if bin == "" {
		return ""
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = []string{"PATH=" + gpuPATH, "LC_ALL=C"}
	if v := interopWSL(); v != "" {
		cmd.Env = append(cmd.Env, "WSL_INTEROP="+v)
	}
	done := make(chan struct{})
	var out []byte
	go func() {
		out, _ = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
		return string(out)
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return ""
	}
}

func collectNvidia(bin string) []GPUInfo {
	out := runGPUCmd(bin,
		"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits")
	var res []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 4 {
			continue
		}
		g := GPUInfo{Vendor: "NVIDIA", Name: strings.TrimSpace(f[0])}
		g.UtilizationPct, _ = strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
		g.MemUsedMB, _ = strconv.ParseUint(strings.TrimSpace(f[2]), 10, 64)
		g.MemTotalMB, _ = strconv.ParseUint(strings.TrimSpace(f[3]), 10, 64)
		if len(f) >= 5 {
			g.TempC, _ = strconv.ParseFloat(strings.TrimSpace(f[4]), 64)
		}
		res = append(res, g)
	}
	return res
}

// collectROCm memakai output CSV rocm-smi. Format kolomnya berubah antar versi
// ROCm, jadi kolom dicari lewat nama header, bukan indeks tetap.
func collectROCm(bin string) []GPUInfo {
	out := runGPUCmd(bin, "--showuse", "--showmemuse", "--showtemp", "--csv")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return nil
	}
	header := strings.Split(lines[0], ",")
	idx := func(substr string) int {
		for i, h := range header {
			if strings.Contains(strings.ToLower(h), substr) {
				return i
			}
		}
		return -1
	}
	useIdx := idx("gpu use")
	memIdx := idx("memory use")
	tempIdx := idx("temperature")

	var res []GPUInfo
	for _, line := range lines[1:] {
		f := strings.Split(line, ",")
		if len(f) == 0 || !strings.HasPrefix(strings.TrimSpace(f[0]), "card") {
			continue
		}
		g := GPUInfo{Vendor: "AMD", Name: "AMD " + strings.TrimSpace(f[0])}
		if useIdx >= 0 && useIdx < len(f) {
			g.UtilizationPct, _ = strconv.ParseFloat(strings.TrimSpace(f[useIdx]), 64)
		}
		if memIdx >= 0 && memIdx < len(f) {
			if v, err := strconv.ParseFloat(strings.TrimSpace(f[memIdx]), 64); err == nil {
				g.MemUsedMB = uint64(v)
			}
		}
		if tempIdx >= 0 && tempIdx < len(f) {
			g.TempC, _ = strconv.ParseFloat(strings.TrimSpace(f[tempIdx]), 64)
		}
		res = append(res, g)
	}
	return res
}

// gpuWindows membaca daftar GPU dari sisi Windows lewat interop WSL.
//
// Di WSL, GPU disalurkan lewat /dev/dxg, bukan sebagai perangkat PCI: sysfs
// tidak punya kartunya dan rocm-smi/nvidia-smi dari repo distro membaca sysfs
// yang sama, jadi tidak ada satu pun sumber Linux yang tahu kartu apa yang
// terpasang. Windows tahu, dan WSL bisa menjalankan binary Windows.
//
// Yang diambil hanya nama. AdapterRAM milik WMI adalah uint32 dan melaporkan
// 4095 MB untuk semua kartu di atas 4 GB — angka yang salah lebih buruk
// daripada tidak ada angka, dan UI sudah menampilkan "—" untuk GPU tanpa
// metrik.
//
// ponytail: nama saja. Utilisasi bisa didapat dari Get-Counter '\GPU
// Engine(*)\Utilization Percentage', tapi sampling-nya ~1 detik per
// pembacaan — perlu goroutine sendiri, bukan tick collector.
func (d *GPUDetector) gpuWindows() []GPUInfo {
	// Hasil yang sudah didapat berlaku seumur proses: daftar kartu tidak
	// berubah, dan tiap pembacaan berarti satu proses powershell.exe.
	if len(wslGPUs) > 0 || d.winDicek {
		return wslGPUs
	}
	// Kegagalan TIDAK di-cache seumur proses, hanya seumur detektor (semenit).
	// Socket /run/WSL baru ada setelah ada sesi WSL yang hidup, sedangkan
	// service ini menyala saat boot — probe pertama sering jatuh di celah itu,
	// dan meng-cache kegagalannya berarti panel tidak pernah menemukan GPU
	// sampai service di-restart.
	d.winDicek = true
	// powershell.exe perlu waktu start yang jauh melebihi alat Linux.
	out := runGPUCmdT(20*time.Second, cariPowerShell(), "-NoProfile", "-NonInteractive",
		"-Command", "(Get-CimInstance Win32_VideoController).Name")
	for _, line := range strings.Split(out, "\n") {
		nama := strings.TrimSpace(line)
		// Adapter virtual RDP/Hyper-V bukan GPU yang menarik dilaporkan.
		if nama == "" || strings.HasPrefix(nama, "Microsoft") {
			continue
		}
		wslGPUs = append(wslGPUs, GPUInfo{Vendor: vendorNama(nama), Name: nama})
	}
	return wslGPUs
}

var wslGPUs []GPUInfo

// cariPowerShell mengembalikan powershell.exe milik Windows, sekaligus jadi
// penanda bahwa kita ada di WSL dengan interop aktif — di luar WSL glob ini
// tidak pernah cocok, jadi tidak perlu deteksi platform terpisah.
func cariPowerShell() string {
	m, _ := filepath.Glob("/mnt/?/Windows/System32/WindowsPowerShell/v1.0/powershell.exe")
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// vendorNama menebak vendor dari nama produk Windows, mis. "NVIDIA GeForce
// RTX 4060 Laptop GPU" atau "AMD Radeon(TM) Graphics".
func vendorNama(nama string) string {
	n := strings.ToLower(nama)
	switch {
	case strings.Contains(n, "nvidia") || strings.Contains(n, "geforce"):
		return "NVIDIA"
	case strings.Contains(n, "amd") || strings.Contains(n, "radeon"):
		return "AMD"
	case strings.Contains(n, "intel"):
		return "Intel"
	}
	return "GPU"
}

// interopWSL mencari socket interop WSL, yang wajib ada di environment supaya
// /init mau menjalankan binary Windows. Kita menyusun cmd.Env sendiri, jadi
// tanpa ini variabelnya ikut terhapus dan tiap panggilan powershell.exe gagal
// tanpa pesan. Service systemd tidak pernah mewarisinya dari sesi mana pun,
// jadi socket sesi yang sedang hidup dipakai sebagai gantinya.
func interopWSL() string {
	if v := os.Getenv("WSL_INTEROP"); v != "" {
		return v
	}
	m, _ := filepath.Glob("/run/WSL/*_interop")
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}
