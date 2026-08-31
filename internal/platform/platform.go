// Package platform mendeteksi OS, kernel, dan jenis platform tempat proses ini
// berjalan. Urutan pengecekan di DetectPlatform() disengaja: yang paling
// spesifik dulu, jangan diubah tanpa menelusuri seluruh pemanggilnya.
package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

type Info struct {
	OS            string `json:"os"`             // mis. "Ubuntu"
	OSVersion     string `json:"os_version"`     // mis. "24.04.4 LTS"
	PrettyName    string `json:"pretty_name"`    // dari PRETTY_NAME
	KernelVersion string `json:"kernel_version"` // uname -r
	// KernelMode: "own" (kernel sendiri), "shared" (share kernel host),
	// atau "none" (bukan kernel Linux — WSL1).
	KernelMode string `json:"kernel_mode"`
	// PlatformType: bare_metal, hypervisor_host, lxc, vm, docker, wsl2, wsl1,
	// sbc, cloud, chroot, live, proot.
	PlatformType string `json:"platform_type"`
	Hypervisor   string `json:"hypervisor"` // qemu/kvm/proxmox/aws/gcp/...
	Arch         string `json:"arch"`
	// Display adalah string siap tampil: "[OS] [Versi] · kernel [versi]".
	Display string `json:"display"`
	// KernelNote menjelaskan asal kernel untuk platform yang share/tanpa kernel.
	KernelNote string `json:"kernel_note,omitempty"`
}

var (
	once   sync.Once
	cached Info
)

// Detect mengembalikan info platform. Hasil di-cache: platform tidak berubah
// selama proses hidup, jadi tidak perlu dihitung ulang tiap request.
func Detect() Info {
	once.Do(func() { cached = detect() })
	return cached
}

func detect() Info {
	info := Info{Arch: machineArch()}
	osRelease := parseOSRelease("/etc/os-release")
	info.PrettyName = osRelease["PRETTY_NAME"]
	info.OS = firstNonEmpty(osRelease["NAME"], "Linux")
	info.OSVersion = firstNonEmpty(osRelease["VERSION"], osRelease["VERSION_ID"])
	info.KernelVersion = kernelRelease()

	info.PlatformType, info.Hypervisor, info.KernelMode, info.KernelNote = classify(info.KernelVersion)
	info.Display = buildDisplay(info)
	return info
}

func buildDisplay(i Info) string {
	name := firstNonEmpty(i.PrettyName, strings.TrimSpace(i.OS+" "+i.OSVersion))
	return name + " · " + labelPlatform(i)
}

// labelPlatform mengubah hasil deteksi jadi satu frasa siap tampil —
// "VM (KVM)", "LXC container", "Bare metal (Proxmox VE)". Versi kernel tetap
// ada di field KernelVersion untuk yang butuh detailnya.
func labelPlatform(i Info) string {
	hv := namaHypervisor(i.Hypervisor)
	switch i.PlatformType {
	case "vm":
		return imbuh("VM", hv)
	case "cloud":
		return imbuh("Cloud VM", hv)
	case "hypervisor_host":
		return imbuh("Bare metal", hv)
	case "lxc":
		if i.Hypervisor == "systemd-nspawn" {
			return "systemd-nspawn container"
		}
		return "LXC container"
	case "docker":
		return "Docker container"
	case "wsl2":
		return "WSL2"
	case "wsl1":
		return "WSL1"
	case "proot":
		return "PRoot (Android)"
	case "chroot":
		return "chroot"
	case "live":
		return "Live boot"
	case "sbc":
		// Untuk SBC, Hypervisor diisi model device-tree (mis. "Raspberry Pi 5").
		return firstNonEmpty(i.Hypervisor, "SBC")
	default:
		return "Bare metal"
	}
}

func imbuh(dasar, hv string) string {
	if hv == "" {
		return dasar
	}
	return dasar + " (" + hv + ")"
}

// namaHypervisor menerjemahkan nilai mentah systemd-detect-virt / DMI ke nama
// yang dikenal user. Nilai yang tidak ada di daftar dipakai apa adanya.
var namaHV = map[string]string{
	"kvm": "KVM", "qemu": "QEMU", "xen": "Xen", "vmware": "VMware",
	"oracle": "VirtualBox", "microsoft": "Hyper-V", "hyper-v": "Hyper-V",
	"bochs": "Bochs", "parallels": "Parallels", "amazon": "AWS",
	"proxmox-ve": "Proxmox VE", "aws": "AWS", "gcp": "GCP",
	"digitalocean": "DigitalOcean", "vultr": "Vultr", "hetzner": "Hetzner",
	"none": "",
}

func namaHypervisor(v string) string {
	if nama, ok := namaHV[v]; ok {
		return nama
	}
	return v
}

// classify menjalankan urutan deteksi: layer terdekat ke proses dulu
// (container), baru naik ke WSL, live boot, dan terakhir VM/hardware.
func classify(kernel string) (platformType, hypervisor, kernelMode, note string) {
	procVersion := readFile("/proc/version")

	// 1. Container layer.
	if fileExists("/.dockerenv") || strings.Contains(readFile("/proc/1/cgroup"), "docker") {
		return "docker", detectVirt(), "shared", "share kernel host"
	}
	if os.Getenv("PROOT_TMP_DIR") != "" || os.Getenv("TERMUX_VERSION") != "" ||
		strings.Contains(procVersion, "-android") {
		return "proot", "android", "shared", "host, via PRoot"
	}
	if v := detectVirt(); v == "lxc" || v == "lxc-libvirt" || v == "systemd-nspawn" {
		return "lxc", v, "shared", "share kernel host"
	}
	if strings.Contains(readFile("/proc/1/environ"), "container=lxc") {
		return "lxc", "lxc", "shared", "share kernel host"
	}

	// 2. WSL.
	if strings.Contains(procVersion, "microsoft-standard-WSL2") || os.Getenv("WSL_INTEROP") != "" {
		return "wsl2", "hyper-v", "own", ""
	}
	if strings.Contains(procVersion, "Microsoft") {
		return "wsl1", "nt", "none", ""
	}

	// 3. Live boot.
	cmdline := readFile("/proc/cmdline")
	if strings.Contains(cmdline, "boot=casper") || strings.Contains(cmdline, "boot=live") {
		return "live", "", "own", ""
	}

	// 4. Chroot — tidak ada sinyal tunggal yang reliable; heuristiknya
	// membandingkan root proses ini dengan root PID 1.
	if isChroot() {
		return "chroot", "", "shared", "share kernel host"
	}

	// 5. Layer VM / hardware.
	virt := detectVirt()
	vendor := strings.TrimSpace(readFile("/sys/class/dmi/id/sys_vendor"))
	product := strings.TrimSpace(readFile("/sys/class/dmi/id/product_name"))

	switch {
	case strings.Contains(vendor, "Amazon EC2"):
		return "cloud", "aws", "own", ""
	case strings.Contains(product, "Google Compute Engine"), strings.Contains(vendor, "Google"):
		return "cloud", "gcp", "own", ""
	case strings.Contains(vendor, "DigitalOcean"):
		return "cloud", "digitalocean", "own", ""
	case strings.Contains(vendor, "Vultr"):
		return "cloud", "vultr", "own", ""
	case strings.Contains(vendor, "Hetzner"):
		return "cloud", "hetzner", "own", ""
	}

	if virt != "" && virt != "none" {
		return "vm", virt, "own", ""
	}

	// Bare metal — cek apakah mesin ini justru host hypervisor.
	if fileExists("/etc/pve") || lookPath("pveversion") {
		return "hypervisor_host", "proxmox-ve", "own", ""
	}
	if model := deviceTreeModel(); model != "" {
		return "sbc", model, "own", ""
	}
	return "bare_metal", "", "own", ""
}

// detectVirt memakai systemd-detect-virt kalau tersedia (paling akurat),
// dengan fallback ke DMI vendor string untuk sistem tanpa systemd.
func detectVirt() string {
	if path, err := exec.LookPath("systemd-detect-virt"); err == nil {
		out, _ := exec.Command(path).Output()
		v := strings.TrimSpace(string(out))
		if v != "" && v != "none" {
			return v
		}
		if v == "none" {
			return "none"
		}
	}
	vendor := strings.ToLower(readFile("/sys/class/dmi/id/sys_vendor"))
	switch {
	case strings.Contains(vendor, "qemu"):
		return "qemu"
	case strings.Contains(vendor, "vmware"):
		return "vmware"
	case strings.Contains(vendor, "innotek"), strings.Contains(vendor, "virtualbox"):
		return "oracle"
	case strings.Contains(vendor, "xen"):
		return "xen"
	case strings.Contains(vendor, "microsoft"):
		return "microsoft"
	}
	return ""
}

func deviceTreeModel() string {
	for _, p := range []string{"/proc/device-tree/model", "/sys/firmware/devicetree/base/model"} {
		if v := strings.TrimRight(readFile(p), "\x00\n"); v != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// isChroot membandingkan device+inode "/" milik proses ini dengan milik PID 1.
// Kalau berbeda, root filesystem kita sudah diganti.
func isChroot() bool {
	var mine, init syscall.Stat_t
	if err := syscall.Stat("/", &mine); err != nil {
		return false
	}
	if err := syscall.Stat("/proc/1/root", &init); err != nil {
		// Tanpa akses /proc/1/root (hardening, atau bukan root) heuristik ini
		// tidak bisa dipakai — lebih baik bilang "bukan chroot" daripada salah.
		return false
	}
	return mine.Dev != init.Dev || mine.Ino != init.Ino
}

func parseOSRelease(path string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(readFile(path), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		out[key] = strings.Trim(val, `"'`)
	}
	return out
}

// machineArch memakai uname -m (aarch64/armv7l/x86_64), bukan runtime.GOARCH —
// nilai uname yang dilihat user, dan bisa berbeda dari arsitektur binary
// (mis. binary armhf di atas kernel arm64).
func machineArch() string {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return runtime.GOARCH
	}
	return charsToString(u.Machine[:])
}

func kernelRelease() string {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return strings.TrimSpace(readFile("/proc/sys/kernel/osrelease"))
	}
	return charsToString(u.Release[:])
}

func charsToString(ca []int8) string {
	b := make([]byte, 0, len(ca))
	for _, c := range ca {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	if err == nil {
		return true
	}
	// pveversion tinggal di /usr/bin tapi PATH systemd bisa minim.
	_, err = os.Stat(filepath.Join("/usr/bin", name))
	return err == nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
