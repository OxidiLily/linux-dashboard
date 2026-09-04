package helper

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// unit9RouterTertanam adalah salinan deploy/9router.service yang
// disatukan ke binary lewat //go:embed — tersedia di semua deployment
// tanpa butuh folder deploy/ di samping binary. Sumber kebenaran ada di
// deploy/9router.service; embed/9router.service adalah salinannya yang
// ikut compile. Untuk sinkron: `cp deploy/9router.service
// internal/helper/embed/9router.service`.
//
//go:embed embed/9router.service
var unit9RouterTertanam []byte

// component menggambarkan satu software opsional yang dikelola panel.
type component struct {
	Name        string
	Binary      string
	Service     string
	Category    string
	Description string
	// RequiredFor = halaman panel yang butuh komponen ini.
	RequiredFor string
	// KelolaDi = halaman panel yang memegang kendali service komponen ini.
	// Diisi kalau menjalankan service-nya tanpa konfigurasi dari halaman itu
	// tidak masuk akal: cloudflared tanpa token tunnel cuma daemon yang hidup
	// lalu mati, dan tombol "Jalankan" di sini justru menghidupkan kembali
	// tunnel lama yang tokennya masih tertinggal di unit systemd.
	KelolaDi string
	// install/uninstall dijalankan sebagai langkah-langkah command array.
	install   func() error
	uninstall func() error
	// installUser menggantikan install untuk komponen yang HARUS dipasang
	// dengan identitas user panel, bukan root — seluruh CLI agent AI, yang
	// installer resminya memasang ke dalam $HOME dan mensyaratkan pemakainya
	// bisa menulis ulang binernya sendiri agar pembaruan otomatis bekerja.
	// Lihat aiagent.go.
	installUser func(*userInfo) error
	// terpasangUser menjawab "apakah komponen ini tersedia untuk user INI".
	// Berbeda dari terpasang, yang menjawab untuk mesin secara keseluruhan:
	// agent yang dipasang user lain ada di mesin ini tapi tidak terjangkau
	// dari HOME user yang sekarang menekan Pasang.
	terpasangUser func(*userInfo) bool
	// purge menghapus data milik komponen yang tidak ikut terbawa uninstall
	// paketnya. Hanya diisi komponen yang benar-benar menyimpan sesuatu, dan
	// hanya dijalankan kalau user memintanya secara eksplisit.
	purge   func() error
	version func() string
	// terpasang menggantikan pencarian Binary di PATH untuk komponen yang
	// bukan binary — ponytail misalnya berupa plugin/harness directive per
	// CLI agent, jadi tidak ada nama yang bisa dicari lewat PATH.
	terpasang func() bool
	// ports = port masuk yang harus diizinkan firewall agar komponen ini bisa
	// dipakai dari LAN. Didaftarkan ke ufw saat komponen dipasang dan sekali
	// lagi sebelum ufw dinyalakan — lihat portkomponen.go.
	ports []portKomponen
}

// aptComponent membangun entri untuk paket yang tersedia langsung di repo
// Debian/Ubuntu — mayoritas software opsional tidak butuh installer vendor.
// versiNpmGlobal membaca versi paket npm global dari package.json-nya.
func versiNpmGlobal(paket string) func() string {
	return func() string {
		for _, root := range []string{"/usr/lib/node_modules", "/usr/local/lib/node_modules"} {
			b, err := os.ReadFile(root + "/" + paket + "/package.json")
			if err != nil {
				continue
			}
			var p struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(b, &p) == nil && p.Version != "" {
				return p.Version
			}
		}
		return ""
	}
}

// wajib menandai komponen yang menjadi syarat sebuah halaman panel.
func wajib(c *component, halaman string) *component {
	c.RequiredFor = halaman
	return c
}

func aptComponent(name, binary, service, category, desc string, pkgs ...string) *component {
	if len(pkgs) == 0 {
		pkgs = []string{name}
	}
	return &component{
		Name: name, Binary: binary, Service: service, Category: category, Description: desc,
		install:   func() error { return aptInstall(pkgs...) },
		uninstall: func() error { return aptRemove(pkgs...) },
		version:   func() string { return firstLine(tryRun(binary, "--version")) },
	}
}

// komponenAgen membangun entri katalog untuk satu CLI agent AI. Bentuknya seragam
// untuk kelimanya, dan keseragaman itu yang penting: perbedaan cara pasang
// antar-agent adalah bagaimana rilis sebelumnya berakhir dengan satu agent
// yang dipasang lewat skrip resmi dan empat lewat npm.
func komponenAgen(nama, binary, deskripsi string) *component {
	return &component{
		Name: nama, Binary: binary,
		Category: katAI, RequiredFor: "AI → AI Agent",
		Description:   deskripsi,
		installUser:   func(u *userInfo) error { return installAgenResmi(nama, binary, u) },
		uninstall:     func() error { return uninstallAgen(nama, binary) },
		terpasang:     func() bool { _, ok := agenTerpasangDiMesin(binary); return ok },
		terpasangUser: func(u *userInfo) bool { return agenSehatUntuk(binary, u) },
		version:       versiAgen(binary),
	}
}

const (
	katRuntime = "Runtime & tunnel"
	katAI      = "AI & Agent"
	katBerbagi = "Berbagi file & jaringan"
	katAman    = "Keamanan"
	katPantau  = "Monitoring & disk"
	katUtil    = "Utilitas"
	katData    = "Database & backend"
)

var components = map[string]*component{
	"docker": {
		Name: "docker", Binary: "docker", Service: "docker",
		Category: katRuntime, RequiredFor: "System → Docker",
		Description: "Container runtime + Compose v2. Dipakai halaman System → Docker.",
		install:     installDocker,
		uninstall:   uninstallDocker,
		version:     func() string { return firstLine(tryRun("docker", "--version")) },
	},
	// Supabase bukan paket dan bukan service systemd — ia stack docker compose
	// yang dipasang setup.sh resmi ke /opt/supabase. Karena itu Service kosong
	// dan KelolaDi menunjuk halaman yang benar-benar memegang kendalinya;
	// tombol Jalankan/Hentikan untuk sepuluh container ada di sana, bukan di
	// kartu komponen. Detail lengkapnya di supabase.go.
	//
	// installUser, bukan install: pemasangannya bisa menyeret Docker ikut
	// terpasang, dan user yang menekan Pasang harus masuk grup docker supaya
	// halaman System → Docker bisa mengelola stack-nya.
	//
	// Hanya port gateway yang didaftarkan ke firewall. Postgres (5432) dan
	// pooler (6543) juga terbuka di compose bawaan, tapi mengizinkannya ke
	// seluruh LAN adalah keputusan admin — bukan efek samping menekan Pasang.
	"supabase": denganPort(&component{
		Name: "supabase", Category: katData,
		Description: "Backend self-hosted lengkap (Postgres, Auth, Storage, Realtime, Edge Functions, Studio) di atas Docker Compose, dipasang lewat setup.sh resmi Supabase ke /opt/supabase.",
		KelolaDi:    "System → Docker",
		installUser: installSupabase,
		uninstall:   uninstallSupabase,
		purge:       purgeSupabase,
		terpasang:   supabaseTerpasang,
		version:     versiSupabase,
	}, portKomponen{portGatewaySupabase, "tcp", "API gateway & Studio"}),
	"wireguard": {
		Name: "wireguard", Binary: "wg", Service: "wg-quick@wg0",
		Category: katRuntime, Description: "VPN peer-to-peer, dikonfigurasi di Settings → Network.",
		install:   func() error { return aptInstall("wireguard", "wireguard-tools") },
		uninstall: func() error { return aptRemove("wireguard", "wireguard-tools") },
		version:   func() string { return firstLine(tryRun("wg", "--version")) },
	},
	"tailscale": {
		Name: "tailscale", Binary: "tailscale", Service: "tailscaled",
		Category: katRuntime, Description: "Mesh VPN berbasis WireGuard, akses remote tanpa buka port.",
		install:   installTailscale,
		uninstall: uninstallTailscale,
		version:   func() string { return firstLine(tryRun("tailscale", "version")) },
	},
	"cloudflared": {
		Name: "cloudflared", Binary: "cloudflared", Service: "cloudflared",
		Category: katRuntime, Description: "Cloudflare Tunnel — ekspos service tanpa port forwarding.",
		KelolaDi:  "Settings → Network",
		install:   installCloudflared,
		uninstall: uninstallCloudflared,
		purge:     purgeCloudflared,
		version:   func() string { return firstLine(tryRun("cloudflared", "--version")) },
	},
	"9router": {
		Name: "9router", Binary: "9router", Service: "9router",
		Category: katAI, Description: "Gateway API AI lokal (butuh Node.js).",
		// installUser, bukan install: 9router memindai CLI tool yang terpasang
		// (Claude Code, Hermes, Codex, …) di dalam $HOME proses-nya sendiri,
		// jadi ia harus berjalan dengan identitas user panel — lihat
		// pastikanUser9Router.
		installUser: install9Router,
		uninstall:   uninstall9Router,
		purge:       purge9Router,
		// Versi dibaca dari package.json, bukan `9router --version`: CLI Node itu
		// butuh belasan detik untuk hidup, sementara file ini dibaca dalam
		// hitungan milidetik dan isinya sama.
		version: versiNpmGlobal("9router"),
		// 20128 = default 9router, sama dengan yang dipakai unit service-nya
		// (deploy/9router.service tidak menimpa host/port).
		ports: []portKomponen{{"20128", "tcp", "gateway API"}},
	},
	// Kelima CLI agent memakai bentuk yang sama: installer resmi vendor,
	// dijalankan sebagai user panel. Alasan lengkapnya di aiagent.go —
	// ringkasnya, keduanya wajib: perintah resmi karena itu satu-satunya
	// jalur yang diuji vendor, dan sebagai user karena binernya harus bisa
	// ditulis ulang pemakainya agar pembaruan otomatis agent bekerja.
	"hermes":      komponenAgen("hermes", "hermes", "Hermes Agent CLI oleh Nous Research (Autonomous AI Agent)."),
	"claude-code": komponenAgen("claude-code", "claude", "Claude Code CLI oleh Anthropic (Agentic Coding CLI)."),
	"codex":       komponenAgen("codex", "codex", "OpenAI Codex CLI (AI coding assistant)."),
	"opencode":    komponenAgen("opencode", "opencode", "OpenCode CLI (Open-source autonomous coding agent)."),
	"openclaw":    komponenAgen("openclaw", "openclaw", "OpenClaw CLI (Multi-channel autonomous AI Agent gateway)."),

	// ---- alat & skill wajib yang dipakai SEMUA AI Agent ----
	//
	// Keempatnya dipasang otomatis saat agent mana pun dipasang (lihat
	// pastikanToolchainAI), tapi tetap muncul terpisah di katalog supaya
	// statusnya terlihat dan bisa dipasang ulang sendiri kalau salah satu
	// gagal — bukan tersembunyi di dalam installer agent.
	"rtk": {
		Name: "rtk", Binary: "rtk",
		Category: katAI, RequiredFor: "AI → AI Agent",
		Description: "Rust Token Killer — proxy CLI yang memangkas keluaran perintah sebelum masuk konteks AI Agent.",
		install:     installRTK,
		uninstall:   uninstallRTK,
		version:     func() string { return firstLine(tryRun("rtk", "--version")) },
	},
	"graphify": {
		Name: "graphify", Binary: "graphify",
		Category: katAI, RequiredFor: "AI → AI Agent",
		Description: "Knowledge graph kode lewat parsing AST lokal — dipakai AI Agent untuk memetakan repo tanpa membaca berkas satu per satu.",
		install:     installGraphify,
		uninstall:   uninstallGraphify,
		version:     func() string { return firstLine(tryRun("graphify", "--version")) },
	},
	"ponytail": {
		Name:     "ponytail",
		Category: katAI, RequiredFor: "AI → AI Agent",
		Description: "Harness \"lazy senior dev\" level ultra + bundle skill ponytail-audit/review/debt.",
		install:     installPonytail,
		uninstall:   uninstallPonytail,
		terpasang:   ponytailTerpasang,
	},
	"browser-use": {
		Name: "browser-use", Binary: "browser-use",
		Category: katAI, RequiredFor: "AI → AI Agent",
		Description: "Browser Use — kendali browser lewat CDP untuk AI Agent (buka halaman, klik, isi form, ambil data dari halaman ber-JavaScript). Skill-nya didaftarkan ke tiap agent saat sesinya dibuka; butuh Chrome/Chromium di mesin yang dipakai.",
		install:     installBrowserUse,
		uninstall:   uninstallBrowserUse,
		version:     versiPipx("browser-use"),
	},

	// ---- paket repo Debian/Ubuntu yang TIDAK ikut di instalasi dasar ----

	// 137:138/udp ikut didaftarkan meski `disable netbios = Yes` (bawaan Samba
	// 4.22+) membuatnya tidak dipakai hari ini: aturannya menganggur tanpa
	// biaya, dan sudah siap kalau nmbd dinyalakan untuk klien lama.
	"samba": wajib(denganPort(aptComponent("samba", "smbd", "smbd", katBerbagi,
		"Server file sharing SMB/CIFS. Halaman File manager → Samba butuh ini.", "samba"),
		portKomponen{"445", "tcp", "SMB"},
		portKomponen{"139", "tcp", "sesi NetBIOS"},
		portKomponen{"137:138", "udp", "nama & datagram NetBIOS"}),
		"File manager → Samba"),
	"nfs-server": denganPort(aptComponent("nfs-server", "exportfs", "nfs-kernel-server", katBerbagi,
		"Server NFS untuk klien Linux/Unix.", "nfs-kernel-server"),
		portKomponen{"2049", "tcp", "NFSv4"},
		portKomponen{"111", "tcp", "rpcbind"},
		portKomponen{"111", "udp", "rpcbind"}),
	"cifs-utils": aptComponent("cifs-utils", "mount.cifs", "", katBerbagi,
		"Klien untuk me-mount share SMB dari server lain.", "cifs-utils"),
	"avahi": denganPort(aptComponent("avahi", "avahi-daemon", "avahi-daemon", katBerbagi,
		"mDNS/Bonjour — server dikenali sebagai <hostname>.local di LAN.", "avahi-daemon"),
		portKomponen{"5353", "udp", "mDNS"}),

	// Technitium dipasang lewat skrip resmi vendor: tidak ada paket .deb-nya,
	// dan yang diunduh skrip itu bukan cuma servernya — runtime ASP.NET Core
	// ikut dipasang ke /opt/dotnet.
	"technitium-dns": denganPort(&component{
		Name: "technitium-dns", Service: "dns", Category: katBerbagi,
		Description: "Server DNS lengkap (blocklist, DoH/DoT, cache) — web console di port 5380, login awal admin/admin. Pemasangannya mematikan systemd-resolved.",
		install:     installTechnitium,
		uninstall:   uninstallTechnitium,
		purge:       purgeTechnitium,
		terpasang:   technitiumTerpasang,
		version:     versiTechnitium,
	},
		portKomponen{"53", "tcp", "DNS"},
		portKomponen{"53", "udp", "DNS"},
		portKomponen{"5380", "tcp", "web console"}),

	// Binary yang dicek adalah cupsd, bukan lpstat: lpstat ikut paket
	// cups-client yang bisa terpasang sendirian di mesin yang justru MENCETAK
	// ke server lain. Yang dikelola halaman ini adalah servernya.
	// printer-driver-gutenprint ikut dipasang, bukan opsional: printer USB
	// rumahan (Canon PIXMA, Epson, banyak HP) tidak mendukung IPP Everywhere,
	// dan tanpa driver CUPS mendaftarkan antreannya dengan senang hati lalu
	// setiap cetakan berakhir sebagai halaman kosong atau job yang menggantung.
	// Gejalanya terbaca sebagai "panel rusak", bukan sebagai driver yang hilang.
	"print-server": wajib(denganPort(aptComponent("print-server", "cupsd", "cups", katBerbagi,
		"Print server CUPS + driver Gutenprint. Halaman Settings → Print server dan menu Print di file manager butuh ini.",
		"cups", "printer-driver-gutenprint"),
		portKomponen{"631", "tcp", "IPP"}),
		"Settings → Print server"),

	"ufw": wajib(aptComponent("ufw", "ufw", "ufw", katAman,
		"Firewall sederhana di atas iptables. Halaman Settings → Firewall butuh ini.", "ufw"),
		"Settings → Firewall"),
	"fail2ban": aptComponent("fail2ban", "fail2ban-client", "fail2ban", katAman,
		"Blokir IP otomatis setelah percobaan login gagal berulang.", "fail2ban"),

	"lm-sensors": aptComponent("lm-sensors", "sensors", "", katPantau,
		"Baca sensor suhu/kipas motherboard dan CPU.", "lm-sensors"),
	"smartmontools": aptComponent("smartmontools", "smartctl", "smartd", katPantau,
		"Kesehatan disk lewat S.M.A.R.T.", "smartmontools"),
	"nvme-cli": aptComponent("nvme-cli", "nvme", "", katPantau,
		"Info dan kesehatan SSD NVMe.", "nvme-cli"),
	"qemu-guest-agent": aptComponent("qemu-guest-agent", "qemu-ga", "qemu-guest-agent", katPantau,
		"Integrasi VM dengan host Proxmox/QEMU (shutdown rapi, info IP).", "qemu-guest-agent"),

	"htop": aptComponent("htop", "htop", "", katUtil,
		"Monitor proses interaktif di terminal.", "htop"),
	"ncdu": aptComponent("ncdu", "ncdu", "", katUtil,
		"Analisis pemakaian disk per folder.", "ncdu"),
	// fastfetch adalah penerus resmi neofetch di Ubuntu modern — neofetch
	// dihapus dari repo Debian/Ubuntu sejak 24.10 (diganti fastfetch). Cek
	// neofetch dulu untuk backward compat dengan user lama yang sudah
	// pernah install sebelum rilis ini.
	"fastfetch": aptComponent("fastfetch", "fastfetch", "", katUtil,
		"Ringkasan sistem bergaya ASCII art untuk halaman Terminal (penerus neofetch).", "fastfetch"),
	// Node.js tidak ada di instalasi dasar Ubuntu/Debian server, dan 9router
	// menolak jalan tanpanya — dimunculkan supaya statusnya terlihat, bukan
	// tersembunyi di dalam installer 9router.
	"nodejs": {
		Name: "nodejs", Binary: "node",
		Category: katRuntime, Description: "Runtime JavaScript + npm. Dibutuhkan komponen 9router.",
		install:   installNode,
		uninstall: uninstallNode,
		version:   func() string { return firstLine(tryRun("node", "--version")) },
	},
	"mergerfs": aptComponent("mergerfs", "mergerfs", "", katBerbagi,
		"Gabungkan beberapa disk jadi satu mount point (union filesystem).", "mergerfs"),

	"restic": aptComponent("restic", "restic", "", katUtil,
		"Backup terenkripsi dan ter-deduplikasi.", "restic"),
}

// ComponentNames menentukan urutan tampil di halaman Components.
func ComponentNames() []string {
	return []string{
		"docker", "nodejs", "tailscale", "cloudflared", "wireguard", "9router",
		"hermes", "claude-code", "codex", "opencode", "openclaw",
		"rtk", "graphify", "ponytail", "browser-use",
		"supabase",
		"samba", "nfs-server", "cifs-utils", "avahi", "technitium-dns", "print-server", "mergerfs",
		"ufw", "fail2ban",
		"lm-sensors", "smartmontools", "nvme-cli", "qemu-guest-agent",
		"htop", "ncdu", "fastfetch", "restic",
	}
}

func componentStatus(name string) helperproto.ComponentStatus {
	c, ok := components[name]
	if !ok {
		return helperproto.ComponentStatus{Name: name}
	}
	st := helperproto.ComponentStatus{
		Name: name, Service: c.Service, Category: c.Category,
		Description: c.Description, RequiredFor: c.RequiredFor, KelolaDi: c.KelolaDi,
	}
	if c.terpasang != nil {
		st.Installed = c.terpasang()
		// Tanpa binary di PATH tidak ada yang bisa diprobe `--version`;
		// komponen yang punya versi membacanya dari berkasnya sendiri, jadi
		// murah dan tidak perlu ikut cache probe.
		if st.Installed && c.version != nil {
			st.Version = c.version()
		}
	} else if path, ok := lookBinary(c.Binary); ok {
		st.Installed = true
		// Satu kali probe saja: fallback `--version` kedua berarti komponen
		// lambat menunggu batas waktu DUA kali.
		st.Version = versiTersimpan(path, c.version)
	}
	if st.Installed && c.Service != "" {
		if _, err := run("systemctl", "is-active", "--quiet", c.Service); err == nil {
			st.Running = true
		}
	}
	st.PunyaData = c.purge != nil
	if st.Installed && name == "9router" {
		st.Note = catatan9Router()
	}
	if st.Installed && name == "docker" {
		st.Note = catatanGrupDocker
	}
	if st.Installed && komponenAgenAI(name) {
		st.Note = catatanAgenLama(c.Binary)
	}
	return st
}

// lookBinary mencari binary di PATH sekaligus direktori sbin. Banyak paket
// server (samba, ufw, nfs, smartmontools) hanya memasang binary ke /usr/sbin,
// yang TIDAK selalu ada di PATH proses daemon — tanpa ini paket yang jelas
// terpasang tetap dilaporkan "belum terpasang".
func lookBinary(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	for _, dir := range []string{"/usr/local/sbin", "/usr/sbin", "/sbin", "/usr/local/bin", "/usr/bin", "/bin"} {
		p := dir + "/" + name
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// Versi disimpan per-binary, dikunci ke identitas file itu sendiri (path,
// ukuran, waktu ubah). Versi hanya berubah kalau binary-nya berubah, jadi
// probe cukup sekali seumur binary — dan probe yang kebetulan melewati batas
// waktu (cloudflared 1,2–2,1 detik, tepat di batas) tetap memakai nilai
// terakhir yang berhasil, bukan menampilkan versi kosong sesekali.
var (
	versiMu    sync.Mutex
	versiCache = map[string]string{}
)

func versiTersimpan(path string, probe func() string) string {
	kunci := path
	if fi, err := os.Stat(path); err == nil {
		kunci = fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
	}
	versiMu.Lock()
	if v, ok := versiCache[kunci]; ok {
		versiMu.Unlock()
		return v
	}
	versiMu.Unlock()

	// Hasil kosong ikut disimpan: kalau probe sebuah binary memang lambat
	// (9router butuh belasan detik), mengulanginya tiap refresh cuma menahan
	// halaman lagi dengan hasil yang sama. Kunci memuat ukuran & waktu ubah
	// file, jadi binary yang diperbarui otomatis diprobe ulang.
	v := probe()
	versiMu.Lock()
	versiCache[kunci] = v
	versiMu.Unlock()
	return v
}

// Status komponen di-cache sebentar: satu kali buka halaman memicu ~19 probe
// proses eksternal, dan halaman ini sering dibuka-tutup. Cache dibuang setiap
// kali panel memasang/menghapus/menyalakan komponen supaya tidak pernah
// menampilkan keadaan basi setelah aksi user.
var (
	cacheMu     sync.Mutex
	cacheStatus []helperproto.ComponentStatus
	cacheWaktu  time.Time
)

const umurCacheStatus = 30 * time.Second

func lupakanCacheKomponen() {
	cacheMu.Lock()
	cacheStatus = nil
	cacheMu.Unlock()
}

// AllComponentStatus mengumpulkan status secara paralel. Berurutan, satu
// komponen lambat menahan 18 lainnya — daftar ini seluruhnya berupa probe
// proses eksternal yang saling lepas.
func AllComponentStatus() []helperproto.ComponentStatus {
	cacheMu.Lock()
	if cacheStatus != nil && time.Since(cacheWaktu) < umurCacheStatus {
		hasil := cacheStatus
		cacheMu.Unlock()
		return hasil
	}
	cacheMu.Unlock()

	names := ComponentNames()
	out := make([]helperproto.ComponentStatus, len(names))
	var wg sync.WaitGroup
	// Paralel, tapi dibatasi: 19 proses sekaligus membuat probe yang tadinya
	// 1,3 detik (cloudflared) melewati batas waktu gara-gara rebutan CPU, dan
	// versinya hilang dari tampilan padahal komponennya terpasang.
	slot := make(chan struct{}, 4)
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			slot <- struct{}{}
			defer func() { <-slot }()
			out[i] = componentStatus(n)
		}(i, n)
	}
	wg.Wait()

	cacheMu.Lock()
	cacheStatus, cacheWaktu = out, time.Now()
	cacheMu.Unlock()
	return out
}

// installComponent memasang satu komponen atas nama user panel yang menekan
// tombolnya. Identitas itu bukan sekadar untuk log: CLI agent AI dipasang KE
// DALAM home user tersebut (lihat aiagent.go), dan keanggotaan grup docker
// juga diberikan ke akun itu, bukan ke root.
func installComponent(name string, u *userInfo) (helperproto.ComponentStatus, error) {
	username := ""
	if u != nil {
		username = u.Name
	}
	defer lupakanCacheKomponen()
	c, ok := components[name]
	if !ok {
		return helperproto.ComponentStatus{}, errKode(helperproto.ErrKomponenTidakAda, "component %q tidak dikenal", name)
	}
	// Kemajuan dilaporkan selama pemasangan berjalan supaya UI bisa menampilkan
	// bar yang benar-benar bergerak, bukan penghitung detik yang tidak tahu
	// apa-apa soal isi pekerjaannya. Laporan ini juga satu-satunya jejak yang
	// ditinggalkan pemasangan untuk halaman yang dimuat ulang di tengah jalan.
	if err := mulaiProgres(name, "install"); err != nil {
		return helperproto.ComponentStatus{}, err
	}
	defer selesaiProgres()
	// Software yang sudah ada di sistem — dipasang manual, lewat repo lain, atau
	// oleh panel sebelumnya — tidak dipasang ulang. Menjalankan installer di
	// atas instalasi yang ada bukan cuma buang waktu: installer vendor
	// (cloudflared, tailscale) menolak dan melempar error yang terlihat seperti
	// kegagalan, padahal software-nya justru sudah siap dipakai.
	//
	// Untuk komponen per-user, "sudah terpasang" HARUS dijawab untuk user INI,
	// bukan untuk mesin. Dua keadaan yang sebelumnya tidak punya jalan keluar
	// sama sekali bergantung pada pembedaan itu:
	//
	//   - Agent yang dipasang admin lain ada di mesin ini tapi tidak
	//     terjangkau dari home user yang sekarang menekan Pasang. Panel
	//     menjawab "sudah terpasang" sementara halaman AI Agent tetap berkata
	//     "belum terpasang", dan tidak ada tombol yang bisa menyelesaikannya.
	//
	//   - Agent warisan yang dipasang panel lama sebagai paket npm global
	//     milik root. Ia berjalan, jadi terlihat terpasang, tapi pembaruan
	//     otomatisnya gagal selamanya untuk user biasa. Lihat agenSehatUntuk.
	if sudahAda(c, name, u) {
		// Agent yang sudah ada tetap ditarik ke toolchain wajib: mesin yang
		// memasang agent sebelum rilis ini punya agent tanpa satu pun alat.
		if komponenAgenAI(name) {
			pastikanToolchainAI()
		}
		// Alasan yang sama untuk grup docker: mesin yang memasang Docker
		// sebelum rilis ini punya Docker tanpa satu pun user di grupnya, dan
		// menekan "Pasang" lagi adalah satu-satunya jalan yang tersedia di
		// panel untuk memperbaikinya.
		if name == "docker" {
			tambahkanKeGrupDocker(username)
		}
		// Alasan yang sama sekali lagi: 9router yang dipasang rilis panel
		// sebelumnya berjalan sebagai root, sehingga daftar CLI Tools-nya
		// selalu kosong. Menekan "Pasang" lagi adalah satu-satunya jalan dari
		// dalam panel untuk memindahkannya ke identitas user.
		if name == "9router" {
			pastikanUser9Router(u)
		}
		return componentStatus(name), nil
	}
	if err := jalankanInstall(c, u); err != nil {
		return helperproto.ComponentStatus{}, err
	}
	// Port didaftarkan ke firewall begitu komponennya ada, bukan menunggu
	// firewall dinyalakan — lihat portkomponen.go. Untuk ufw sendiri kebalikannya:
	// firewall yang baru dipasang harus menyusul semua komponen yang sudah ada,
	// karena mereka belum pernah punya tempat mendaftar.
	switch name {
	case "ufw":
		daftarkanPortSemuaKomponen()
	case "fail2ban":
		pastikanJailBawaan()
	case "samba":
		// Samba butuh dua-duanya. Kalau fail2ban belum ada, jailnya menyusul
		// sendiri saat fail2ban dipasang — pastikanJailBawaan memeriksa smbd.
		daftarkanPortKomponen(c)
		pastikanJailBawaan()
	default:
		daftarkanPortKomponen(c)
	}
	// Alat & skill wajib menyusul agent-nya — bukan langkah manual terpisah
	// yang gampang terlewat. Kegagalannya tidak membatalkan instalasi agent
	// (lihat pastikanToolchainAI), hanya tercatat di log helper.
	if komponenAgenAI(name) {
		pastikanToolchainAI()
	}
	if name == "docker" {
		tambahkanKeGrupDocker(username)
	}
	return componentStatus(name), nil
}

// sudahAda menjawab "apakah pemasangan ini bisa dilewati".
func sudahAda(c *component, name string, u *userInfo) bool {
	if c.terpasangUser != nil && u != nil {
		return c.terpasangUser(u)
	}
	return componentStatus(name).Installed
}

// jalankanInstall memilih antara installer sistem dan installer per-user.
func jalankanInstall(c *component, u *userInfo) error {
	if c.installUser != nil {
		if u == nil {
			return errInvalid("komponen %s harus dipasang atas nama user panel", c.Name)
		}
		return c.installUser(u)
	}
	if c.install == nil {
		return errInvalid("komponen %s tidak punya cara pemasangan", c.Name)
	}
	return c.install()
}

func uninstallComponent(name string, purge bool) (helperproto.ComponentStatus, error) {
	defer lupakanCacheKomponen()
	c, ok := components[name]
	if !ok {
		return helperproto.ComponentStatus{}, errKode(helperproto.ErrKomponenTidakAda, "component %q tidak dikenal", name)
	}
	// Penghapusan ikut dilaporkan: ia memakai apt/dpkg yang sama, dan halaman
	// yang dimuat ulang di tengahnya harus bisa menemukannya lagi persis
	// seperti pemasangan.
	if err := mulaiProgres(name, "uninstall"); err != nil {
		return helperproto.ComponentStatus{}, err
	}
	defer selesaiProgres()
	if err := c.uninstall(); err != nil {
		return helperproto.ComponentStatus{}, err
	}
	// Izin firewall dicabut setelah paketnya hilang: membiarkannya berarti
	// menyisakan port terbuka untuk layanan yang sudah tidak ada.
	hapusPortKomponen(c)
	// Data dihapus SETELAH paketnya dicopot, dan kegagalannya tidak
	// membatalkan uninstall: paketnya sudah hilang, melaporkan komponen
	// "gagal dihapus" hanya akan membuat user mencoba lagi tanpa hasil.
	if purge && c.purge != nil {
		if err := c.purge(); err != nil {
			log.Printf("uninstall %s: hapus data: %v", name, err)
		}
	}
	return componentStatus(name), nil
}

// CopotComponentsArg adalah argv[1] yang menyuruh helper mencopot seluruh
// komponen katalog lalu keluar. Dipanggil uninstall.sh mode "total".
const CopotComponentsArg = "copot-components"

// CopotSemuaKomponen mencopot setiap komponen katalog yang terpasang, berikut
// datanya.
//
// Ini dijalankan oleh uninstall.sh, bukan ditulis ulang sebagai daftar paket
// di dalam skrip bash itu: daftar bash tidak pernah ikut bertambah waktu
// katalog di berkas ini bertambah, dan yang terlewat baru ketahuan setelah
// user memilih "hapus total" lalu menemukan komponennya masih terpasang.
// Uninstaller di sini juga tahu hal yang tidak diketahui `apt remove` —
// repo & keyring vendor, unit systemd cloudflared beserta tokennya, paket npm
// global, dan pipx.
//
// Urutannya dibalik dari urutan tampil: runtime (docker, nodejs) ada di awal
// katalog, sementara yang berdiri di atasnya (agent npm, 9router) ada di
// belakang. Mencopot dari belakang berarti npm masih hidup waktu paket npm
// dicabut.
//
// Kegagalan satu komponen tidak menghentikan sisanya — uninstall yang berhenti
// di tengah meninggalkan mesin dalam keadaan yang lebih membingungkan daripada
// uninstall yang melewatkan satu paket dan mengatakannya.
func CopotSemuaKomponen() int {
	nama := ComponentNames()
	for i := len(nama) - 1; i >= 0; i-- {
		n := nama[i]
		if !componentStatus(n).Installed {
			continue
		}
		fmt.Printf("[i] Mencopot component %s…\n", n)
		if _, err := uninstallComponent(n, true); err != nil {
			fmt.Fprintf(os.Stderr, "[⚠] %s gagal dicopot: %v\n", n, err)
			continue
		}
		fmt.Printf("[✓] %s dicopot\n", n)
	}
	return 0
}

func componentService(name, action string, u *userInfo) error {
	defer lupakanCacheKomponen()
	c, ok := components[name]
	if !ok {
		return errInvalid("component %q tidak dikenal", name)
	}
	if c.Service == "" {
		return errInvalid("component %s tidak punya service", name)
	}
	// Komponen yang service-nya dikelola halaman lain tidak bisa dijalankan
	// dari sini — penolakannya di helper, bukan cuma tombol yang disembunyikan
	// UI, karena endpoint-nya tetap bisa dipanggil langsung.
	if c.KelolaDi != "" {
		return errInvalid(
			"service %s dijalankan dan dihentikan dari %s, bukan dari halaman Components",
			c.Service, c.KelolaDi)
	}
	// WSL/lxc tanpa systemd init penuh: systemctl selalu gagal. Cek
	// sekali di sini, sebelum perintah pertama dikirim, supaya semua
	// service mendapat pesan jelas yang sama — sebelumnya smartd
	// diam-diam gagal lalu UI menampilkan "Nonaktif" tanpa alasan.
	if hasNoSystemd() {
		return errInvalid(
			"mesin ini tidak menjalankan systemd — service %s tidak bisa "+
				"dikontrol lewat systemctl. Jalankan daemon secara manual atau "+
				"aktifkan systemd (WSL: `wsl --update` lalu restart dengan init)",
			c.Service)
	}
	// Beberapa service (smartd, qemu-guest-agent) belum enabled di mesin
	// baru — start tanpa enable membuat unit kembali mati setelah reboot,
	// dan user mengira tombol "Jalankan" no-op. Untuk start, otomatis
	// enable juga; untuk stop biarkan apa adanya, karena disable bisa
	// membatalkan pilihan admin yang sengaja mematikan unit.
	if action == "start" {
		if _, err := run("systemctl", "enable", c.Service); err != nil {
			log.Printf("peringatan: enable %s gagal: %v", c.Service, err)
		}
		// 9router diinstal lewat `npm install -g` tanpa unit systemd; kalau
		// user menekan "Jalankan" sebelum sempat klik "Pasang" (mis. panel
		// di-deploy ulang, file unit hilang), systemctl start selalu
		// gagal "Unit 9router.service not found". Buat unit di sini kalau
		// belum ada — paket npm diasumsikan sudah terpasang (kalau belum,
		// 9router akan crash cepat dan systemctl menandai failed dengan
		// pesan yang jelas).
		if name == "9router" {
			lama, err := os.ReadFile(unitDst9Router)
			// Unit lama yang mengunci seluruh filesystem tanpa StateDirectory
			// ikut diganti di sini: kalau tidak, tombol Jalankan menyalakan
			// service yang pasti mati lagi beberapa detik kemudian.
			if err != nil || unit9RouterRusak(string(lama)) {
				if src, e := bacaUnit9Router(); e == nil && len(src) > 0 {
					_ = os.WriteFile(unitDst9Router, src, 0o644)
					_, _ = run("systemctl", "daemon-reload")
				}
			}
			// Password awal dipastikan sebelum start pertama — 9router
			// membaca INITIAL_PASSWORD hanya saat proses hidup.
			pastikanPassword9Router()
			pastikanUser9Router(u)
		}
		// wg-quick@<iface> butuh /etc/wireguard/<iface>.conf. Tanpa config,
		// systemctl start selalu gagal dengan pesan systemd yang menyesatkan
		// (Job for wg-quick@wg0.service failed because the control process
		// exited with error code) — user mengira WireGuard rusak, padahal
		// config-nya memang belum dibuat lewat Settings → Network.
		if strings.HasPrefix(c.Service, "wg-quick@") {
			iface := strings.TrimPrefix(c.Service, "wg-quick@")
			confPath := filepath.Join("/etc/wireguard", iface+".conf")
			if _, err := os.Stat(confPath); err != nil {
				return errInvalid(
					"file %s belum ada — buat konfigurasi WireGuard lewat Settings → Network dulu",
					confPath)
			}
		}
		// qemu-guest-agent hanya berguna di dalam VM QEMU/KVM: unit-nya
		// BindsTo perangkat virtio-serial yang dipasang hypervisor. Di mesin
		// fisik, WSL, atau container perangkat itu tidak ada, dan systemd
		// menolak dengan "A dependency job for qemu-guest-agent.service
		// failed" — pesan yang tidak menyebut sama sekali bahwa masalahnya
		// mesin ini memang bukan guest QEMU.
		if name == "qemu-guest-agent" && !guestQEMU() {
			return errInvalid(
				"mesin ini bukan guest QEMU/KVM — perangkat %s tidak ada, jadi "+
					"qemu-guest-agent tidak bisa dijalankan. Agent ini hanya "+
					"berguna di VM Proxmox/QEMU", portVirtioQEMU)
		}
	}
	return serviceAction(helperproto.ServiceArgs{Name: c.Service, Action: action})
}

// portVirtioQEMU adalah perangkat virtio-serial yang dipasang QEMU untuk
// kanal guest agent. Unit qemu-guest-agent.service memakainya lewat
// BindsTo/After, jadi keberadaannya adalah syarat mutlak unit itu bisa start.
const portVirtioQEMU = "/dev/virtio-ports/org.qemu.guest_agent.0"

func guestQEMU() bool {
	_, err := os.Stat(portVirtioQEMU)
	return err == nil
}

func aptInstall(pkgs ...string) error {
	setProgres(2, "indeks", "memperbarui daftar paket")
	if _, err := run("apt-get", "update"); err != nil {
		return err
	}
	setProgres(batasIndeks, "unduh", "")
	args := append([]string{"install", "-y", "--no-install-recommends"}, pkgs...)
	err := aptDenganProgres(args, batasIndeks, batasPasang)
	if err == nil {
		return nil
	}
	// "E: Unable to locate package ..." paling sering muncul di image WSL
	// Ubuntu minimal: komponen main aktif tapi universe (arsip community)
	// belum di-enable, dan paket seperti neofetch hanya ada di sana. Saat
	// install gagal, aktifkan universe, ulangi update, lalu coba sekali lagi
	// — ini untuk paket yang memang seharusnya tersedia di repo Ubuntu resmi.
	// Kegagalan mengunduh tidak akan membaik dengan mengaktifkan universe:
	// repositorinya terbaca (metadata sampai), yang gagal justru pengambilan
	// berkas paketnya. Satu putaran add-apt-repository + update di sini hanya
	// menunda pesan yang sama sambil membuat user menunggu lebih lama.
	if gagalUnduhAPT(err) {
		// Mirror sistem tidak bisa mengirim berkasnya — ulangi lewat mirror
		// resmi distro. Kalau itu berhasil, instalasinya selesai dan user tidak
		// perlu tahu mirrornya sempat bermasalah.
		if e := aptInstallCadangan(args); e == nil {
			return nil
		}
		return terjemahkanErrAPT(err)
	}
	distro, _, _ := distroAPT()
	if distro != "ubuntu" {
		return err
	}
	if _, e := run("add-apt-repository", "-y", "universe"); e != nil {
		return err
	}
	if _, e := run("apt-get", "update"); e != nil {
		return err
	}
	err = aptDenganProgres(args, batasIndeks, batasPasang)
	return terjemahkanErrAPT(err)
}

// mirrorCadangan adalah titik distribusi resmi tiap distro — bukan daftar
// panjang mirror pihak ketiga. Satu cadangan yang pasti ada dan pasti sinkron
// lebih berguna daripada sepuluh cermin yang sama-sama bisa basi, dan tidak
// menuntut panel memelihara daftar mirror yang menua sendiri.
var mirrorCadangan = map[string]struct{ arsip, keamanan, komponen, keyring string }{
	"ubuntu": {
		"http://archive.ubuntu.com/ubuntu/", "http://security.ubuntu.com/ubuntu/",
		"main restricted universe multiverse",
		"/usr/share/keyrings/ubuntu-archive-keyring.gpg",
	},
	"debian": {
		"http://deb.debian.org/debian/", "http://security.debian.org/debian-security/",
		"main contrib non-free non-free-firmware",
		"/usr/share/keyrings/debian-archive-keyring.gpg",
	},
}

// aptInstallCadangan mengulang instalasi lewat mirror resmi distro saat mirror
// milik sistem gagal mengirim berkas paket.
//
// Konfigurasi APT milik sistem TIDAK disentuh sama sekali: daftar sumber,
// direktori lists, dan cache-nya semua diarahkan ke folder sementara lewat
// opsi -o. Mirror yang dipilih admin adalah keputusannya, dan satu instalasi
// yang kebetulan gagal bukan alasan panel menulis ulang /etc/apt di
// belakangnya — apalagi diam-diam. Yang dilakukan di sini hanya "ambil paket
// ini dari sumber resmi untuk kali ini saja"; begitu selesai, tidak ada jejak
// yang tertinggal.
func aptInstallCadangan(args []string) error {
	id, codename, err := distroAPT()
	if err != nil {
		return err
	}
	m, ok := mirrorCadangan[id]
	if !ok || codename == "" {
		return errInvalid("tidak ada mirror cadangan yang dikenal untuk distro ini")
	}
	dir, err := os.MkdirTemp("", "lindash-apt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	lists := filepath.Join(dir, "lists")
	// apt menulis unduhan setengah jadi ke subfolder "partial" dan menolak
	// jalan kalau folder itu belum ada.
	if err := os.MkdirAll(filepath.Join(lists, "partial"), 0o755); err != nil {
		return err
	}

	// signed-by hanya dipasang kalau keyringnya benar-benar ada. Menunjuk
	// berkas yang tidak ada membuat apt menolak seluruh repositori dengan
	// pesan verifikasi yang jauh lebih membingungkan daripada masalah aslinya.
	tanda := ""
	if _, e := os.Stat(m.keyring); e == nil {
		tanda = "[signed-by=" + m.keyring + "] "
	}
	var b strings.Builder
	fmt.Fprintf(&b, "deb %s%s %s %s\n", tanda, m.arsip, codename, m.komponen)
	fmt.Fprintf(&b, "deb %s%s %s-updates %s\n", tanda, m.arsip, codename, m.komponen)
	fmt.Fprintf(&b, "deb %s%s %s-security %s\n", tanda, m.keamanan, codename, m.komponen)
	srcFile := filepath.Join(dir, "sources.list")
	if err := os.WriteFile(srcFile, []byte(b.String()), 0o644); err != nil {
		return err
	}

	opsi := []string{
		"-o", "Dir::Etc::SourceList=" + srcFile,
		// SourceParts diarahkan ke folder kosong, bukan ke
		// /etc/apt/sources.list.d — kalau tidak, mirror yang bermasalah ikut
		// terbaca lagi dan kegagalannya terulang.
		"-o", "Dir::Etc::SourceParts=" + dir + "/kosong",
		"-o", "Dir::State::Lists=" + lists,
	}
	if err := os.MkdirAll(filepath.Join(dir, "kosong"), 0o755); err != nil {
		return err
	}
	if _, err := run("apt-get", append(append([]string{}, opsi...), "update")...); err != nil {
		return err
	}
	_, err = run("apt-get", append(append([]string{}, opsi...), args...)...)
	return err
}

// gagalUnduhAPT: apt berhasil membaca repositori tapi gagal mengambil berkas.
func gagalUnduhAPT(err error) bool {
	if err == nil {
		return false
	}
	pesan := err.Error()
	return strings.Contains(pesan, "Failed to fetch") ||
		strings.Contains(pesan, "Unable to fetch some archives") ||
		strings.Contains(pesan, "gagal mengambil")
}

// terjemahkanErrAPT membedakan "mirror tidak bisa mengirim berkasnya" dari
// "paketnya memang tidak ada".
//
// Keduanya keluar dari apt sebagai kegagalan instalasi, tapi tindakan yang
// benar sama sekali berbeda: yang pertama diperbaiki dengan mengganti mirror,
// yang kedua dengan mencari nama paket yang tepat. Tanpa pembedaan ini user
// hanya melihat cetakan mentah apt di toast — termasuk baris "403 Forbidden"
// yang mudah dikira sebagai panel yang tidak punya izin, padahal yang menolak
// adalah server mirror-nya.
func terjemahkanErrAPT(err error) error {
	if err == nil {
		return nil
	}
	if !gagalUnduhAPT(err) {
		return err
	}
	return errKode(helperproto.ErrMirrorGagal,
		"mirror APT menolak mengirim berkas paket, dan mirror resmi distro juga tidak berhasil dipakai. "+
			"Periksa koneksi internet server, atau ganti mirror di /etc/apt/sources.list.d/ke mirror lain. "+
			"Pesan asli apt: %s", ringkasBarisAPT(err.Error()))
}

// ringkasBarisAPT menyisakan baris error apt yang benar-benar menjelaskan
// kegagalannya, supaya pesan di UI tidak berisi seluruh log unduhan.
func ringkasBarisAPT(pesan string) string {
	var penting []string
	for _, b := range strings.Split(pesan, "\n") {
		b = strings.TrimSpace(b)
		if strings.HasPrefix(b, "E:") || strings.Contains(b, "Failed to fetch") {
			penting = append(penting, b)
		}
		if len(penting) >= 3 {
			break
		}
	}
	if len(penting) == 0 {
		return strings.TrimSpace(pesan)
	}
	return strings.Join(penting, " | ")
}

func aptRemove(pkgs ...string) error {
	args := append([]string{"remove", "-y"}, pkgs...)
	_, err := run("apt-get", args...)
	return err
}

// Probe versi tidak boleh menahan halaman: `9router --version` di mesin ini
// butuh 16 detik (CLI Node), cloudflared 1,4 detik. Halaman Components hanya
// perlu tahu terpasang atau tidak — versi yang lambat lebih baik kosong
// daripada membuat seluruh daftar menunggu.
// Batasnya longgar karena hasilnya disimpan per-binary: probe lambat hanya
// dibayar sekali seumur binary, bukan tiap kali halaman dibuka.
const batasProbeVersi = 4 * time.Second

func tryRun(name string, args ...string) string {
	ctx, batal := context.WithTimeout(context.Background(), batasProbeVersi)
	defer batal()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LC_ALL=C",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 && stderr.Len() == 0 {
		return ""
	}
	return firstNonEmpty(stdout.String(), stderr.String())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// download mengunduh file ke path tujuan. Dipakai untuk installer resmi vendor
// yang tidak tersedia di repo Debian/Ubuntu.
func download(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unduh %s gagal: HTTP %d", url, resp.StatusCode)
	}
	// O_NOFOLLOW: kalau tujuannya ternyata symlink, gagalkan — jangan menulis
	// ke berkas yang ditunjuknya. Root yang menulis lewat symlink orang lain
	// adalah cara paling murah menimpa berkas sistem mana pun.
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o700)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// ---- installer resmi vendor -------------------------------------------

// distroAPT membaca ID dan codename dari /etc/os-release. Repo Docker dan
// NodeSource dipisah per distro dan per rilis, jadi baris sources.list-nya
// tidak bisa hardcode: menaruh baris "noble" di Debian membuat `apt update`
// gagal untuk SELURUH sistem, bukan cuma paket yang mau dipasang.
//
// Turunan Ubuntu (Mint, Pop!_OS) memakai UBUNTU_CODENAME; docs Docker memang
// menyuruh memakai variabel itu lebih dulu, baru VERSION_CODENAME.
func distroAPT() (id, codename string, err error) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", "", err
	}
	v := map[string]string{}
	for _, baris := range strings.Split(string(b), "\n") {
		k, nilai, ada := strings.Cut(strings.TrimSpace(baris), "=")
		if !ada {
			continue
		}
		v[k] = strings.Trim(nilai, `"`)
	}
	id = v["ID"]
	if id != "ubuntu" && id != "debian" {
		// ID_LIKE menampung induknya untuk turunan (linuxmint → ubuntu).
		for _, induk := range strings.Fields(v["ID_LIKE"]) {
			if induk == "ubuntu" || induk == "debian" {
				id = induk
				break
			}
		}
	}
	codename = firstNonEmpty(v["UBUNTU_CODENAME"], v["VERSION_CODENAME"])
	if id == "" || codename == "" {
		return "", "", errInvalid("os-release tidak menyebut distro/codename yang dikenal")
	}
	return id, codename, nil
}

// paketTerpasang memakai dpkg-query, bukan `command -v`: paket seperti
// containerd.io tidak punya binary dengan nama yang sama.
func paketTerpasang(nama string) bool {
	res, err := run("dpkg-query", "-W", "-f=${Status}", nama)
	return err == nil && strings.Contains(res.Stdout, "ok installed")
}

// aptRemoveTerpasang hanya membuang paket yang benar-benar ada. `apt-get
// remove` untuk nama paket yang tidak dikenal repo (mis. docker-ce di mesin
// yang dulu memasang docker.io) gagal dan membatalkan seluruh pencopotan.
func aptRemoveTerpasang(pkgs ...string) error {
	var ada []string
	for _, p := range pkgs {
		if paketTerpasang(p) {
			ada = append(ada, p)
		}
	}
	if len(ada) == 0 {
		return nil
	}
	return aptRemove(ada...)
}

// tambahRepoAPT menulis keyring + satu baris sources.list.d. Kunci diunduh ke
// /etc/apt/keyrings (bukan apt-key yang sudah usang) dan barisnya memakai
// signed-by, persis seperti dokumentasi resmi vendor.
func tambahRepoAPT(urlKunci, pathKunci, baris, pathList string) error {
	if err := os.MkdirAll(filepath.Dir(pathKunci), 0o755); err != nil {
		return err
	}
	if err := download(urlKunci, pathKunci); err != nil {
		return err
	}
	// 0644: apt berjalan sebagai _apt setelah drop privilege dan harus bisa
	// membaca keyring-nya.
	if err := os.Chmod(pathKunci, 0o644); err != nil {
		return err
	}
	return os.WriteFile(pathList, []byte(baris), 0o644)
}

// hapusRepoAPT membuang baris sources.list milik panel, lalu keyring-nya
// HANYA kalau tidak ada berkas sumber lain yang masih menunjuk ke sana.
//
// Ini bukan kehati-hatian teoretis: mesin yang sudah lebih dulu memasang
// vendor-nya sendiri sering memakai format deb822 (`docker.sources`,
// `nodesource.sources`) yang menunjuk keyring dengan path sama. Menghapus
// keyring itu diam-diam membuat `apt update` gagal untuk SELURUH sistem —
// pencopotan satu komponen tidak boleh melumpuhkan apt.
func hapusRepoAPT(pathList, pathKunci string) {
	for _, p := range strings.Fields(pathList) {
		_ = os.Remove(p)
	}
	if pathKunci == "" || masihDirujuk(pathKunci) {
		return
	}
	_ = os.Remove(pathKunci)
}

func masihDirujuk(pathKunci string) bool {
	berkas, err := filepath.Glob("/etc/apt/sources.list.d/*")
	if err != nil {
		// Ragu = jangan hapus. Keyring nyasar cuma sampah beberapa KB;
		// keyring hilang membuat apt berhenti bekerja.
		return true
	}
	berkas = append(berkas, "/etc/apt/sources.list")
	for _, f := range berkas {
		b, err := os.ReadFile(f)
		if err == nil && strings.Contains(string(b), pathKunci) {
			return true
		}
	}
	return false
}

const (
	dockerKeyring = "/etc/apt/keyrings/docker.asc"
	dockerList    = "/etc/apt/sources.list.d/docker.list"
)

var dockerPaket = []string{
	"docker-ce", "docker-ce-cli", "containerd.io",
	"docker-buildx-plugin", "docker-compose-plugin",
}

// installDocker mengikuti docs.docker.com/engine/install (Ubuntu & Debian):
// repo resmi Docker + paket docker-ce, BUKAN paket docker.io milik distro.
// docker.io tertinggal beberapa rilis dan tidak membawa plugin compose v2,
// padahal halaman System → Docker memanggil `docker compose`.
func installDocker() error {
	// Langkah pertama docs.docker.com/engine/install: buang paket lama.
	// Bukan formalitas — `docker-ce` dan `docker-ce-cli` menyatakan
	// `Conflicts: docker.io` TANPA `Replaces`, jadi apt tidak bisa
	// menyelesaikannya sendiri dan install gagal total di mesin yang pernah
	// memasang docker.io (perintah yang disebut hampir semua tutorial).
	// `podman-docker` memasang /usr/bin/docker sendiri sehingga bentrok
	// berkas dengan docker-ce-cli.
	//
	// Daftarnya lebih pendek daripada di dokumentasi: containerd dan runc
	// sengaja tidak dibuang karena `containerd.io` punya `Replaces` untuk
	// keduanya, jadi apt menggantinya sendiri — sementara membuang paksa bisa
	// menjatuhkan runtime lain (k3s, podman) yang memakainya.
	if err := aptRemoveTerpasang("docker.io", "docker-cli", "podman-docker"); err != nil {
		return err
	}
	id, codename, err := distroAPT()
	if err != nil {
		return err
	}
	arch, err := run("dpkg", "--print-architecture")
	if err != nil {
		return err
	}
	baris := "deb [arch=" + firstLine(arch.Stdout) + " signed-by=" + dockerKeyring + "] " +
		"https://download.docker.com/linux/" + id + " " + codename + " stable\n"
	if err := tambahRepoAPT(
		"https://download.docker.com/linux/"+id+"/gpg", dockerKeyring, baris, dockerList,
	); err != nil {
		return err
	}
	return aptInstall(dockerPaket...)
}

// uninstallDocker membuang paket + repo, TAPI tidak menyentuh /var/lib/docker.
// Dokumentasi Docker menyebut penghapusan folder itu sebagai langkah terpisah
// karena isinya image, volume, dan data container milik user — panel tidak
// boleh menghapusnya diam-diam lewat satu tombol.
func uninstallDocker() error {
	if err := aptRemoveTerpasang(append(dockerPaket, "docker.io", "docker-compose-v2")...); err != nil {
		return err
	}
	hapusRepoAPT(dockerList, dockerKeyring)
	return nil
}

// catatanGrupDocker ditampilkan halaman Components selama Docker terpasang.
// Kalimat "logout dulu" adalah bagian terpentingnya: usermod TIDAK menyentuh
// sesi yang sedang berjalan, jadi tanpa keterangan ini user mencoba
// `docker ps` di shell yang sama, tetap ditolak, dan menyimpulkan panel gagal
// memasukkannya ke grup.
const catatanGrupDocker = "Akun sudoer yang memasang Docker dimasukkan ke grup `docker` " +
	"agar bisa memakai perintah docker dari shell sendiri. Keanggotaan grup baru berlaku " +
	"pada sesi login BERIKUTNYA — logout lalu login lagi (atau jalankan `newgrp docker`) " +
	"sebelum mencoba `docker ps`."

// tambahkanKeGrupDocker memasukkan user yang memasang Docker ke grup `docker`.
// Tanpa ini `docker ps` dari shell user sendiri berhenti di "permission denied
// while trying to connect to the docker API at unix:///var/run/docker.sock":
// soket itu milik root:docker, dan panel bisa memakainya hanya karena helper-nya
// berjalan sebagai root.
//
// Yang ditambahkan HANYA akun yang menekan tombol pasang, bukan semua sudoer.
// Keanggotaan grup docker setara akses root — siapa pun di dalamnya bisa
// menjalankan container yang mem-bind mount `/` — jadi memberikannya ke akun
// yang tidak memintanya bukan keputusan yang boleh diambil panel diam-diam.
// Akun ini sendiri sudah sudoer (syarat seluruh menu Docker), jadi tidak ada
// wewenang baru yang sebenarnya diberikan, hanya jalan pintas yang lebih pendek.
//
// Kegagalannya TIDAK membatalkan instalasi: Docker-nya sudah terpasang dan
// panel tetap bisa memakainya lewat helper. Yang hilang cuma kenyamanan
// memakai docker dari shell, dan itu bisa disusulkan kapan saja dengan satu
// perintah — membatalkan instalasi yang sudah berhasil justru lebih merugikan.
func tambahkanKeGrupDocker(username string) {
	// root memakai soket docker lewat kepemilikan berkas, bukan lewat grup.
	if username == "" || username == "root" {
		return
	}
	// Grup `docker` dibuat oleh paket docker-ce. Kalau belum ada, instalasinya
	// belum benar-benar selesai dan usermod hanya akan menghasilkan error yang
	// membingungkan di log.
	if err := validGroups([]string{"docker"}); err != nil {
		log.Printf("docker: %v — usermod dilewati", err)
		return
	}
	// -aG, bukan -G: tanpa -a seluruh keanggotaan grup sekunder user DIGANTI,
	// yang berarti mencabut sudo dari akun yang barusan memakainya.
	if _, err := run("usermod", "-aG", "docker", username); err != nil {
		log.Printf("docker: gagal menambahkan %q ke grup docker: %v", username, err)
	}
}

const (
	// Node 24 = LTS aktif, dan versi yang diminta dokumentasi 9Router.
	nodeMajor    = "24"
	nodeSetupURL = "https://deb.nodesource.com/setup_" + nodeMajor + ".x"
)

// installNode memasang Node.js dari NodeSource, sumber resmi paket .deb Node.
//
// Bukan nvm: nvm memasang Node ke dalam $HOME satu user dan hanya aktif di
// shell interaktif yang mem-source nvm.sh. Helper daemon berjalan dari systemd
// dengan PATH tetap, jadi node hasil nvm tidak akan pernah terlihat olehnya —
// panel akan melaporkan "belum terpasang" untuk Node yang sebenarnya ada.
// Paket nodejs menaruh binary di /usr/bin dan modul global di
// /usr/lib/node_modules, yang justru dibaca panel.
//
// Bukan paket nodejs milik distro: Ubuntu 24.04 masih mengirim Node 18.
func installNode() error {
	script, bersihkan, err := unduhSkrip(nodeSetupURL, "nodesource-setup.sh")
	if err != nil {
		return err
	}
	defer bersihkan()
	if _, err := run("/bin/bash", script); err != nil {
		return err
	}
	return aptInstall("nodejs")
}

// unduhSkrip menaruh skrip installer vendor di direktori sementara milik root
// dengan nama yang tidak bisa ditebak.
//
// Nama tetap di /tmp (dulu "/tmp/lindash-<vendor>.sh") tidak aman: /tmp bisa
// ditulis semua user, dan setiap user panel punya akses terminal. Siapa pun
// bisa membuat symlink dengan nama itu lebih dulu, lalu menunggu admin menekan
// Install — daemon root akan menulis lewat symlink itu ke berkas mana pun yang
// ditunjuk penyerang. os.MkdirTemp membuat direktori 0700 milik root dengan
// nama acak, jadi tidak ada nama yang bisa didahului.
func unduhSkrip(url, nama string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "lindash-")
	if err != nil {
		return "", nil, err
	}
	bersihkan := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, nama)
	if err := download(url, path); err != nil {
		bersihkan()
		return "", nil, err
	}
	return path, bersihkan, nil
}

func uninstallNode() error {
	if err := aptRemoveTerpasang("nodejs", "npm"); err != nil {
		return err
	}
	// Skrip NodeSource menulis deb822 (`nodesource.sources`) di versi baru dan
	// satu baris `.list` di versi lama — mesin bisa punya salah satunya.
	hapusRepoAPT(
		"/etc/apt/sources.list.d/nodesource.sources /etc/apt/sources.list.d/nodesource.list",
		"/usr/share/keyrings/nodesource.gpg",
	)
	return nil
}

// installTailscale memakai skrip resmi Tailscale. Skrip diunduh ke file lalu
// dieksekusi dengan argumen array — bukan `curl | sh`, supaya isinya ada di
// disk dan bisa diaudit kalau instalasi bermasalah.
func installTailscale() error {
	script, bersihkan, err := unduhSkrip("https://tailscale.com/install.sh", "tailscale-install.sh")
	if err != nil {
		return err
	}
	defer bersihkan()
	// Seluruh pemasangan Tailscale terjadi di dalam skrip ini — tanpa membaca
	// apt yang dipanggilnya, bar berhenti di 0% sampai instalasinya tiba-tiba
	// selesai.
	tahapBaru("menjalankan skrip resmi Tailscale")
	return skripDenganProgres("/bin/sh", script, 2, batasPasang)
}

// uninstallTailscale ikut membuang repo yang ditulis skrip resmi Tailscale.
// Tanpa ini `apt update` terus menarik daftar paket vendor yang sudah dicopot,
// dan sisa repo itu diam-diam memasang Tailscale lagi pada `apt install`
// berikutnya yang menyentuh namanya.
func uninstallTailscale() error {
	if err := aptRemoveTerpasang("tailscale"); err != nil {
		return err
	}
	hapusRepoAPT(
		"/etc/apt/sources.list.d/tailscale.list /etc/apt/sources.list.d/tailscale.sources",
		"/usr/share/keyrings/tailscale-archive-keyring.gpg",
	)
	return nil
}

const (
	cloudflaredKeyring = "/usr/share/keyrings/cloudflare-public-v2.gpg"
	cloudflaredList    = "/etc/apt/sources.list.d/cloudflared.list"
)

// installCloudflared memakai repo apt resmi Cloudflare, bukan unduh .deb
// lepas dari GitHub: dengan repo terdaftar, cloudflared ikut `apt upgrade`
// seperti paket lain — .deb manual tidak pernah dapat update.
func installCloudflared() error {
	baris := "deb [signed-by=" + cloudflaredKeyring + "] https://pkg.cloudflare.com/cloudflared any main\n"
	if err := tambahRepoAPT(
		"https://pkg.cloudflare.com/cloudflare-public-v2.gpg", cloudflaredKeyring, baris, cloudflaredList,
	); err != nil {
		return err
	}
	return aptInstall("cloudflared")
}

// uninstallCloudflared membuang unit systemd-nya lebih dulu, baru paket, repo,
// dan keyring.
//
// Unit /etc/systemd/system/cloudflared.service ditulis oleh
// `cloudflared service install <token>` — bukan oleh paket .deb — dan TOKEN
// TUNNEL-nya ada di dalam ExecStart unit itu. `apt remove` tidak menyentuhnya,
// jadi sebelum ini uninstall meninggalkan kunci tunnel utuh di mesin: pasang
// ulang cloudflared lalu tekan "Jalankan" dan tunnel lama yang dikira sudah
// dihapus langsung hidup lagi.
func uninstallCloudflared() error {
	if cloudflaredServiceInstalled() {
		// `cloudflared service uninstall` adalah kebalikan resmi dari
		// perintah yang memasangnya. Versi lawas tidak punya subcommand itu —
		// untuk itu unitnya dibuang manual.
		if _, err := run("cloudflared", "service", "uninstall"); err != nil {
			_, _ = run("systemctl", "disable", "--now", "cloudflared")
			_ = os.Remove(cloudflaredUnit)
		}
		_, _ = run("systemctl", "daemon-reload")
	}
	if err := aptRemoveTerpasang("cloudflared"); err != nil {
		return err
	}
	hapusRepoAPT(cloudflaredList, cloudflaredKeyring)
	return nil
}

// purgeCloudflared menghapus kredensial tunnel yang ditulis cloudflared di
// luar paketnya: cert.pem akun dan berkas <tunnel-id>.json. Keduanya kunci,
// bukan konfigurasi, jadi hanya dihapus kalau user mencentang "hapus data
// juga" saat uninstall.
func purgeCloudflared() error {
	for _, dir := range []string{"/etc/cloudflared", "/root/.cloudflared"} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

// install9Router memasang Node.js LTS dulu kalau npm belum ada — 9Router
// adalah satu-satunya component yang butuh runtime Node.js.
//
// 9router adalah CLI Node, bukan binary yang dieksekusi langsung. Tanpa unit
// systemd, tombol "Jalankan" di halaman Components selalu gagal dengan
// "Unit 9router.service not found" — user mengira 9router rusak padahal
// sebenarnya daemon tidak pernah di-buat. Unit standar dipasang di sini
// (ExecStart = /usr/bin/9router, bin npm-nya) supaya
// systemctl start/stop/enable langsung bekerja di semua distro.
//
// Sumber unit = deploy/9router.service (sama dengan yang dipakai install.sh),
// supaya patch apa pun di ExecStart atau hardening cukup diedit di satu
// tempat. Saat deploy/ tidak tersedia (build lokal dari `go build` tanpa
// menyertakan folder deploy) daemon mencari unit dengan path relatif ke
// binary — terakhir, fallback ke string internal sebagai jaring pengaman.
func install9Router(u *userInfo) error {
	// Lewat npmInstallGlobal, bukan `npm install -g` telanjang: postinstall
	// 9router-lah yang memasang runtime-nya (sql.js, better-sqlite3, systray2)
	// ke $HOME/.9router/runtime, dan npm 12 memblokir script itu secara bawaan.
	if err := npmInstallGlobal("9router"); err != nil {
		return err
	}
	// Pasang unit systemd hanya jika belum ada — admin yang sudah menulis
	// ExecStart/env khusus tidak boleh ditimpa diam-diam tiap install ulang.
	if lama, err := os.ReadFile(unitDst9Router); err == nil {
		// Kecuali satu bentuk: unit tulisan panel versi lama yang memakai
		// ProtectSystem=strict tanpa StateDirectory. Kombinasi itu membuat
		// 9router tidak punya lokasi yang bisa ditulis sama sekali, jadi
		// service-nya mati berulang — membiarkannya "demi menghormati
		// kustomisasi admin" berarti membiarkan komponen yang pasti rusak.
		if !unit9RouterRusak(string(lama)) {
			pastikanPassword9Router()
			pastikanUser9Router(u)
			return nil
		}
		log.Printf("9router: unit lama yang tidak bisa start diganti dengan versi yang benar")
	}
	sumber, err := bacaUnit9Router()
	if err != nil || len(sumber) == 0 {
		return nil
	}
	if err := os.WriteFile(unitDst9Router, sumber, 0o644); err != nil {
		return nil
	}
	if _, err := run("systemctl", "daemon-reload"); err != nil {
		return nil
	}
	// Identitas user dipasang SEBELUM start pertama: kalau 9router sempat
	// hidup sebagai root, ia membuat ~/.9router milik root di HOME lama dan
	// pemindahannya jadi pekerjaan tambahan yang bisa gagal.
	pastikanUser9Router(u)
	if _, err := run("systemctl", "enable", "--now", "9router.service"); err != nil {
		// WSL tidak punya systemd init yang utuh — systemctl start selalu
		// gagal di sana, tapi unit file-nya sudah terpasang dan bisa dijalankan
		// manual lewat `9router --no-browser --skip-update`.
	}
	pastikanPassword9Router()
	return nil
}

// uninstall9Router membersihkan SEMUA yang dipasang panel, bukan cuma paket
// npm-nya.
//
// Sebelumnya uninstall hanya menjalankan `npm uninstall -g`, meninggalkan unit
// systemd yang menunjuk binary yang sudah tidak ada, dan — yang paling terasa
// — drop-in berisi INITIAL_PASSWORD lama. Pemasangan ulang menemukan drop-in
// itu masih ada lalu membiarkannya, jadi password "awal" yang ditampilkan
// panel sama persis dengan sebelum uninstall. Padahal seluruh gunanya
// uninstall adalah memulai dari nol.
//
// Data 9router sendiri (~/.9router: koneksi provider, API key, riwayat)
// sengaja TIDAK dihapus — itu milik user, bukan buatan panel, dan uninstall
// komponen lain di panel ini juga tidak memusnahkan datanya. Konsekuensinya
// satu dan perlu diketahui: kalau user sudah menyetel password sendiri dari
// UI 9router, password itulah yang berlaku dan INITIAL_PASSWORD diabaikan —
// 9router hanya memakai INITIAL_PASSWORD selama belum ada password tersimpan.
func uninstall9Router() error {
	// Hentikan lebih dulu: service yang masih hidup sementara binary-nya
	// dihapus akan gagal berulang dan tercatat sebagai failed di systemd.
	_, _ = run("systemctl", "disable", "--now", "9router.service")

	if _, err := run("npm", "uninstall", "-g", "9router"); err != nil {
		return err
	}

	_ = os.RemoveAll(dropDir9Router)
	_ = os.Remove(passFile9Router)
	_ = os.Remove(unitDst9Router)
	_, _ = run("systemctl", "daemon-reload")
	return nil
}

// purge9Router menghapus data 9router: database (koneksi provider, API key,
// riwayat pemakaian), config, dan runtime yang dipasang postinstall-nya.
//
// Dipisah dari uninstall9Router dan hanya berjalan kalau user memintanya:
// isinya bukan buatan panel, dan sebagiannya tidak bisa dibuat ulang tanpa
// login ke tiap provider lagi. Yang dijamin ikut terhapus adalah password
// tersimpan di settings — itulah yang membuat INITIAL_PASSWORD baru dari
// pemasangan berikutnya benar-benar berlaku; selama settings masih menyimpan
// password, 9router mengabaikan INITIAL_PASSWORD sepenuhnya.
//
// Beberapa lokasi dicoba karena HOME service-nya berbeda antar generasi unit:
// /var/lib/9router (StateDirectory, generasi pertama), /root (generasi kedua),
// dan sejak rilis ini home user panel — yang hanya diketahui dari drop-in,
// karena purge tidak menerima identitas user.
func purge9Router() error {
	lokasi := []string{homeRoot9Router, "/var/lib/9router"}
	if h := homeUser9Router(dropUser9Router); h != "" {
		lokasi = append(lokasi, h)
	}
	for _, home := range lokasi {
		if err := os.RemoveAll(filepath.Join(home, ".9router")); err != nil {
			return err
		}
	}
	// StateDirectory milik unit panel generasi lama.
	return os.RemoveAll("/var/lib/9router")
}

const unitDst9Router = "/etc/systemd/system/9router.service"

// homeRoot9Router = HOME yang dipakai unit 9router generasi kedua, sebelum
// service-nya dipindahkan ke identitas user panel (lihat pastikanUser9Router).
const homeRoot9Router = "/root"

// unit9RouterRusak mengenali unit tulisan panel versi lama yang tidak akan
// pernah bisa start. Bentuk pertama: ia mengunci seluruh filesystem
// (ProtectSystem=strict) tanpa menyediakan satu pun direktori yang bisa
// ditulis, sehingga 9router gagal membuat config dan database-nya. Unit
// buatan admin sendiri hampir pasti menyertakan
// StateDirectory, ReadWritePaths, atau melonggarkan ProtectSystem — jadi
// pola ini tidak akan menyentuh kustomisasi orang lain.
func unit9RouterRusak(isi string) bool {
	// Baris komentar dibuang dulu. Unit tulisan panel yang sekarang MENYEBUT
	// kedua pola di bawah dalam komentarnya sendiri — untuk menerangkan kenapa
	// ia tidak memakainya — jadi tanpa penyaringan ini panel menganggap unit
	// terbarunya sendiri rusak dan menulis ulang tiap kali dipasang.
	var arahan []string
	for _, baris := range strings.Split(isi, "\n") {
		t := strings.TrimSpace(baris)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		arahan = append(arahan, t)
	}
	isi = strings.Join(arahan, "\n")

	// Bentuk kedua, dan yang paling parah: ExecStart menunjuk
	// dist/index.js — berkas yang tidak pernah ada di paket npm 9router
	// (bin-nya cli.js). Service-nya gagal start seketika, jadi port 20128
	// tidak pernah terbuka dan browser cuma bilang "This site can't be
	// reached". Tidak ada admin yang menulis path itu sendiri.
	if strings.Contains(isi, "dist/index.js") {
		return true
	}
	if !strings.Contains(isi, "ProtectSystem=strict") {
		return false
	}
	for _, kunci := range []string{"StateDirectory=", "ReadWritePaths=", "RuntimeDirectory="} {
		if strings.Contains(isi, kunci) {
			return false
		}
	}
	return true
}

const (
	dropDir9Router  = "/etc/systemd/system/9router.service.d"
	dropFile9Router = dropDir9Router + "/10-initial-password.conf"
	// dropUser9Router menyimpan identitas user panel untuk service 9router.
	// Drop-in terpisah dari 10-initial-password.conf supaya keduanya bisa
	// ditulis ulang sendiri-sendiri: password hanya dibuat sekali, identitas
	// user berubah kalau panel dipakai admin yang berbeda.
	dropUser9Router = dropDir9Router + "/20-user.conf"
	// passFile9Router menyimpan password yang dibuat panel supaya bisa
	// ditampilkan lagi di halaman Components — tanpa ini password acak
	// hanya ada di drop-in systemd yang tidak pernah dilihat user.
	passFile9Router = "/var/lib/linux-dashboard/9router-password"
	// passDefault9Router adalah nilai bawaan 9router. Rilis panel
	// sebelumnya menuliskannya ke drop-in, yang justru TIDAK menyelesaikan
	// apa pun (lihat pastikanPassword9Router) — nilai ini dipertahankan
	// untuk mengenali drop-in lama dan menggantinya.
	passDefault9Router = "123456"
)

// pastikanPassword9Router menulis drop-in systemd yang menyetel env var
// INITIAL_PASSWORD ke password acak.
//
// 9router menolak login remote selama password akun masih nilai bawaannya:
// "Default password must be changed before remote access. Change it from the
// local machine (or set INITIAL_PASSWORD)". Rilis panel sebelumnya menyetel
// env itu ke 123456 — yang persis nilai bawaannya, jadi 9router tetap
// menolak. Password acak per mesin memenuhi syaratnya sekaligus tidak
// menaruh kredensial yang sama di semua instalasi.
//
// Drop-in di /etc/systemd/system/<unit>.d/ ditimpa systemd di atas unit
// utama — jadi ExecStart/env kustom admin tetap aman, hanya env var ini
// yang ditambah. Path drop-in khusus (bukan unit utama) sehingga unit
// bisa di-update tanpa menimpa setelan ini.
func pastikanPassword9Router() {
	if b, err := os.ReadFile(dropFile9Router); err == nil {
		// Drop-in sudah ada. Yang berisi password bawaan adalah peninggalan
		// rilis lama yang membuat 9router tetap menolak login remote —
		// itu diganti. Selain itu jangan disentuh: admin mungkin sudah
		// menyetel password sendiri lewat `systemctl edit`.
		if !strings.Contains(string(b), "INITIAL_PASSWORD="+passDefault9Router) {
			return
		}
	}
	pass, err := sandiAcak(18)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dropDir9Router, 0o755); err != nil {
		return
	}
	konten := "[Service]\nEnvironment=INITIAL_PASSWORD=" + pass + "\n"
	if err := os.WriteFile(dropFile9Router, []byte(konten), 0o600); err != nil {
		return
	}
	// 0600 milik root: password ini hanya dibaca daemon helper (root) untuk
	// ditampilkan ke admin yang sudah login ke panel.
	if err := os.MkdirAll(filepath.Dir(passFile9Router), 0o755); err == nil {
		_ = os.WriteFile(passFile9Router, []byte(pass+"\n"), 0o600)
	}
	_, _ = run("systemctl", "daemon-reload")
	// Restart hanya kalau service sudah jalan — kalau belum, daemon-reload
	// saja cukup; systemd akan membaca env baru saat start pertama.
	if _, err := run("systemctl", "is-active", "--quiet", "9router.service"); err == nil {
		_, _ = run("systemctl", "restart", "9router.service")
	}
}

// konfigUser9Router menyusun isi drop-in yang membuat 9router berjalan sebagai
// user panel. Dipisah dari pastikanUser9Router supaya bisa diuji tanpa systemd.
//
// Kenapa ini perlu: halaman CLI Tools di 9router memeriksa keberadaan tiap
// agent dengan `which <biner>` lalu — kalau itu gagal — dengan membaca berkas
// konfigurasinya di `os.homedir()/.<tool>`. Keduanya membaca lingkungan
// PROSES 9ROUTER, bukan mesin. Selama unit-nya berjalan sebagai root dengan
// HOME=/root dan PATH sistem, seluruh CLI agent yang dipasang panel (yang
// memang dipasang per-user ke dalam $HOME, lihat aiagent.go) tidak akan pernah
// terlihat: setiap kartu berbunyi "Not installed" di mesin yang jelas-jelas
// memilikinya. Bukan cuma labelnya yang salah — tombol Quick Setup di halaman
// itu menulis konfigurasi ke `os.homedir()`, jadi sebagai root ia menaruh
// setelan model di /root/.<tool> yang tidak pernah dibaca siapa pun.
//
// Nilai HOME dan PATH sengaja diambil dari sumber yang sama dengan pemasangan
// agent (pathAgen di aiagent.go): kalau keduanya berbeda, panel dan 9router
// akan berbeda pendapat soal agent mana yang terpasang.
func konfigUser9Router(u *userInfo) string {
	return "[Service]\n" +
		"User=" + u.Name + "\n" +
		"Group=" + strconv.Itoa(u.GID) + "\n" +
		"Environment=HOME=" + u.Home + "\n" +
		"Environment=PATH=" + pathAgen(u) + "\n"
}

// pastikanUser9Router memindahkan service 9router dari root ke user panel.
//
// Data 9router (~/.9router: provider, API key, riwayat, password tersimpan)
// ikut dipindahkan — tanpa itu, service yang baru berganti identitas mulai
// dari nol dan seluruh provider yang sudah disetel hilang dari pandangan user.
// Pemindahan memakai `mv` karena /root dan /home kerap berada di filesystem
// berbeda, dan os.Rename gagal melintasi batas itu.
//
// Tidak melakukan apa-apa untuk user root (tidak ada yang perlu dipindahkan)
// atau saat identitasnya tidak diketahui — jalur CLI tanpa sesi panel.
func pastikanUser9Router(u *userInfo) {
	if u == nil || u.UID == 0 || u.Home == "" || u.Name == "" {
		return
	}
	inginkan := konfigUser9Router(u)
	if b, err := os.ReadFile(dropUser9Router); err == nil && string(b) == inginkan {
		return
	}
	// Service dihentikan dulu: memindahkan direktori data di bawah proses yang
	// sedang memegang database SQLite-nya adalah cara yang rapi untuk merusak
	// database itu.
	_, aktif := run("systemctl", "is-active", "--quiet", "9router.service")
	if aktif == nil {
		_, _ = run("systemctl", "stop", "9router.service")
		// Dinyalakan lagi apa pun yang terjadi di bawah. Tanpa defer, satu
		// kegagalan menulis drop-in meninggalkan 9router MATI — gejalanya
		// jauh lebih buruk daripada bug yang sedang diperbaiki.
		defer func() { _, _ = run("systemctl", "start", "9router.service") }()
	}
	pindahkanData9Router(u)
	if err := os.MkdirAll(dropDir9Router, 0o755); err != nil {
		log.Printf("9router: %s gagal dibuat: %v", dropDir9Router, err)
		return
	}
	if err := os.WriteFile(dropUser9Router, []byte(inginkan), 0o644); err != nil {
		log.Printf("9router: drop-in identitas user gagal ditulis: %v", err)
		return
	}
	_, _ = run("systemctl", "daemon-reload")
}

// pindahkanData9Router memindahkan ~/.9router milik root ke home user panel.
// Direktori tujuan yang SUDAH ada tidak disentuh: user mungkin pernah
// menjalankan 9router sendiri, dan datanya lebih berhak daripada salinan root.
func pindahkanData9Router(u *userInfo) {
	tujuan := filepath.Join(u.Home, ".9router")
	if _, err := os.Stat(tujuan); err == nil {
		return
	}
	asal := filepath.Join(homeRoot9Router, ".9router")
	if _, err := os.Stat(asal); err != nil {
		return
	}
	if _, err := run("mv", asal, tujuan); err != nil {
		log.Printf("9router: data di %s gagal dipindahkan ke %s: %v", asal, tujuan, err)
		return
	}
	if _, err := run("chown", "-R", strconv.Itoa(u.UID)+":"+strconv.Itoa(u.GID), tujuan); err != nil {
		log.Printf("9router: kepemilikan %s gagal diubah: %v", tujuan, err)
	}
}

// homeUser9Router membaca HOME yang sedang dipakai service 9router dari
// drop-in-nya. Dipakai purge, yang harus menghapus data di tempat ia benar-
// benar berada, bukan di tempat generasi unit sebelumnya menaruhnya. Path
// drop-in-nya parameter, bukan konstanta yang dibaca langsung, supaya bisa
// diuji tanpa menyentuh /etc/systemd mesin yang menjalankan test.
func homeUser9Router(dropIn string) string {
	b, err := os.ReadFile(dropIn)
	if err != nil {
		return ""
	}
	for _, baris := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(baris), "Environment=HOME="); ok {
			return v
		}
	}
	return ""
}

// sandiAcak membangun password dari alfabet tanpa karakter yang mudah
// tertukar saat dibaca ulang dari layar (0/O, 1/l/I) — password ini memang
// dimaksudkan untuk disalin manusia dari halaman Components.
func sandiAcak(n int) (string, error) {
	const alfabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alfabet[int(b[i])%len(alfabet)]
	}
	return string(b), nil
}

// catatan9Router mengembalikan password awal untuk ditampilkan di halaman
// Components. Kosong kalau adminnya menyetel sendiri lewat systemctl edit —
// panel tidak menebak-nebak kredensial yang bukan buatannya.
func catatan9Router() string {
	b, err := os.ReadFile(passFile9Router)
	if err != nil {
		return ""
	}
	pass := strings.TrimSpace(string(b))
	if pass == "" {
		return ""
	}
	// Password hanya berlaku kalau drop-in-nya masih memakai nilai ini.
	drop, err := os.ReadFile(dropFile9Router)
	if err != nil || !strings.Contains(string(drop), "INITIAL_PASSWORD="+pass) {
		return ""
	}
	return "Login awal: user `admin`, password `" + pass + "`. Ganti dari UI 9router setelah masuk."
}

// bacaUnit9Router mencari unit systemd 9router. Sumber satu-satunya ada
// di deploy/9router.service (disalin ke internal/helper/embed saat build
// lewat //go:embed), bukan duplikat string di kode. Urutan prioritas:
//
//  1. Unit tertanam dari //go:embed (selalu ada di binary release).
//  2. /etc/default/9router.service atau override admin di /usr/local/share
//     untuk kustomisasi ExecStart/env di luar panel.
func bacaUnit9Router() ([]byte, error) {
	if len(unit9RouterTertanam) > 0 {
		return unit9RouterTertanam, nil
	}
	for _, p := range []string{
		// install.sh menyalin unit ke sini supaya binary runtime yang
		// di-deploy terpisah dari source tree (mode Update) tetap
		// menemukan unit tanpa //go:embed.
		"/usr/local/share/linux-dashboard/9router.service",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, os.ErrNotExist
}

// hasNoSystemd mendeteksi lingkungan tanpa systemd init yang fungsional.
// Tiga heuristik dipakai bersama supaya tidak salah diagnosa di salah satu
// kasus tepi:
//
//  1. /run/systemd/system harus ada — ini filesystem tmpfs yang systemd
//     pasang saat init. WSL tanpa systemd-init dan container tanpa priv
//     mount biasanya tidak punya direktori ini.
//  2. PID 1 harus dipanggil "systemd" (comm di /proc/1/comm).
//  3. systemctl is-system-running tidak boleh bilang "offline" — fallback
//     kalau 1 dan 2 tidak cukup (mis. systemd ada tapi user scope).
func hasNoSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return true
	}
	if b, err := os.ReadFile("/proc/1/comm"); err == nil {
		comm := strings.TrimSpace(string(b))
		// Beberapa distro pakai "init" sebagai nama proses systemd.
		if comm != "systemd" && comm != "init" {
			return true
		}
	}
	return false
}

func npmInstallGlobal(pkg string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		// npm tidak punya status-fd seperti apt, jadi kemajuannya tidak bisa
		// dijadikan angka. Yang tetap bisa dilaporkan jujur adalah langkah
		// mana yang sedang berjalan — tanpa ini kartu komponen menunggu
		// berpuluh detik tanpa satu pun keterangan.
		setProgres(0, "", "memasang runtime Node.js")
		if err := installNode(); err != nil {
			return err
		}
	}
	// Paket yang lewat sini semuanya bergantung pada postinstall-nya: keempat
	// CLI agent adalah wrapper tipis yang binary aslinya baru disalin ke
	// tempatnya oleh postinstall, dan 9router memasang runtime-nya di situ.
	//
	// npm 12 — yang dipasang installer panel dari NodeSource — MEMBLOKIR
	// install script secara bawaan kecuali paketnya masuk allowScripts. npm
	// tetap keluar dengan kode 0 dan hanya menulis peringatan, jadi panel
	// menandai komponennya terpasang sementara `claude` yang terpasang cuma
	// mencetak "claude native binary not installed" tiap kali dijalankan.
	// --allow-scripts menyebut paket ini saja, bukan melonggarkan kebijakan
	// mesin secara umum.
	//
	// Dua flag lainnya menutup penyebab lama yang masih mungkin ada di npmrc
	// warisan image: omit=optional (binary per-platform tidak diunduh) dan
	// ignore-scripts=true (postinstall dimatikan menyeluruh).
	tahapBaru("mengunduh dan memasang paket npm")
	_, err := run("npm", "install", "-g",
		"--allow-scripts="+pkg, "--include=optional", "--ignore-scripts=false", pkg)
	return err
}

func npmUninstallGlobal(pkg string) error {
	_, err := run("npm", "uninstall", "-g", pkg)
	return err
}

// ---- Technitium DNS Server ----------------------------------------------
//
// Tidak ada paket .deb-nya: vendor hanya menyediakan install.sh yang mengunduh
// runtime ASP.NET Core ke /opt/dotnet, mengekstrak servernya, lalu memasang
// unit dns.service. Skrip yang sama dipakai untuk memperbarui.
//
// Efek samping skrip itu yang perlu diketahui sebelum menekan Pasang: ia
// mematikan systemd-resolved dan menulis ulang /etc/resolv.conf ke 127.0.0.1
// (cadangan berkas lama disimpan di direktori aplikasinya).

// dirTechnitium: /opt/technitium/dns untuk pemasangan baru, /etc/dns untuk
// mesin yang sudah memasang versi lama — install.sh memilih yang kedua kalau
// /etc/dns/config sudah ada. Config selalu di /etc/dns pada dua-duanya.
var dirTechnitium = []string{"/opt/technitium/dns", "/etc/dns"}

func technitiumTerpasang() bool {
	for _, d := range dirTechnitium {
		if _, err := os.Stat(filepath.Join(d, "DnsServerApp.dll")); err == nil {
			return true
		}
	}
	return false
}

// versiTechnitium membaca versi dari deps.json milik aplikasinya —
// `dotnet DnsServerApp.dll --version` berarti menyalakan runtime .NET hanya
// untuk satu baris teks, dan halaman Components memanggil ini tiap refresh.
func versiTechnitium() string {
	for _, d := range dirTechnitium {
		b, err := os.ReadFile(filepath.Join(d, "DnsServerApp.deps.json"))
		if err != nil {
			continue
		}
		if v := versiDariDeps(string(b)); v != "" {
			return v
		}
	}
	return ""
}

// versiDariDeps mengambil versi proyek dari deps.json .NET, yang menuliskan
// paket akarnya sebagai "<nama>/<versi>". Bukan json.Unmarshal: kuncinya ada
// di dalam dua peta bersarang yang namanya ikut berubah tiap rilis runtime,
// jadi memodelkannya berarti memelihara struct yang menua sendiri.
func versiDariDeps(isi string) string {
	_, sisa, ok := strings.Cut(isi, `"DnsServerApp/`)
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(sisa, `"`)
	if !ok {
		return ""
	}
	return v
}

func installTechnitium() error {
	// Skrip vendor berhenti dengan exit 1 kalau tidak menemukan init yang
	// dikenalnya — setelah mengunduh runtime .NET ~100 MB. Ditolak di sini
	// supaya user tidak menunggu unduhan yang sudah pasti berakhir gagal.
	// Jalur OpenRC milik skrip itu tidak dihitung: panel ini mengendalikan
	// service lewat systemctl saja, jadi komponennya akan terpasang tanpa
	// tombol Jalankan/Hentikan yang berfungsi.
	if hasNoSystemd() {
		return errInvalid(
			"mesin ini tidak menjalankan systemd — Technitium DNS butuh unit " +
				"systemd untuk dijalankan panel (WSL: `wsl --update` lalu restart dengan init)")
	}
	script, bersihkan, err := unduhSkrip("https://download.technitium.com/dns/install.sh", "technitium-install.sh")
	if err != nil {
		return err
	}
	defer bersihkan()
	tahapBaru("menjalankan skrip resmi Technitium DNS")
	if err := skripDenganProgres("/bin/sh", script, 2, batasPasang); err != nil {
		// Skrip vendor menulis kegagalannya ke stdout dan detailnya ke
		// install.log, jadi yang sampai ke sini cuma "exit status 1" — sebutkan
		// di mana alasannya bisa dibaca, bukan biarkan user menebak.
		return errInvalid("skrip Technitium gagal (%v) — detail di %s/install.log", err, dirTechnitium[0])
	}
	return nil
}

// uninstallTechnitium membuang server berikut unitnya, DAN mengembalikan
// resolusi nama mesin ini.
//
// Bagian kedua itu bukan tambahan sopan santun: install.sh mematikan
// systemd-resolved lalu mengarahkan /etc/resolv.conf ke 127.0.0.1. Mencopot
// servernya tanpa membatalkan itu meninggalkan mesin yang menanyakan setiap
// nama ke port 53 yang sudah tidak ada isinya — apt, update panel, dan semua
// yang lain berhenti bekerja, dengan gejala yang tidak menyerupai penyebabnya.
//
// Data server (/etc/dns: zona, blocklist, setelan) sengaja ditinggal, sama
// seperti komponen lain — hanya dihapus lewat purge kalau user memintanya.
func uninstallTechnitium() error {
	_, _ = run("systemctl", "disable", "--now", "dns.service")
	// Dinyalakan lebih dulu supaya stub-nya sudah ada saat resolv.conf
	// dikembalikan. Mesin tanpa systemd-resolved (container) cuma gagal di sini.
	_, _ = run("systemctl", "enable", "--now", "systemd-resolved")
	pulihkanResolvConf()
	_ = os.Remove("/etc/systemd/system/dns.service")
	_, _ = run("systemctl", "daemon-reload")
	// ponytail: pemasangan lama yang tinggal di /etc/dns cukup kehilangan
	// DnsServerApp.dll — sisa pustakanya menganggur dan ditimpa lagi kalau
	// dipasang ulang. Menyapu isinya berarti ikut menghapus config di folder
	// yang sama.
	_ = os.Remove(filepath.Join("/etc/dns", "DnsServerApp.dll"))
	// dns=none di NetworkManager.conf yang ditulis installer tidak dikembalikan:
	// resolv.conf sudah pulih, dan menyunting balik berkas itu bisa menimpa
	// setelan yang memang milik admin.
	return os.RemoveAll("/opt/technitium")
}

// penandaResolvTechnitium adalah baris pertama /etc/resolv.conf tulisan
// installer Technitium. Dipakai untuk mengenali berkas buatannya sendiri —
// di dua tempat yang sama-sama menentukan.
const penandaResolvTechnitium = "# Generated by Technitium DNS Server Installer"

// pulihkanResolvConf mengembalikan /etc/resolv.conf dari cadangan yang dibuat
// installer Technitium (`cp -a`, jadi symlink ke stub systemd-resolved tetap
// berupa symlink). Tanpa cadangan, stub systemd-resolved dipakai kalau ada.
func pulihkanResolvConf() {
	// Yang bukan tulisan installer tidak disentuh sama sekali: admin yang
	// sudah menyetel resolver sendiri setelah memasang Technitium tidak boleh
	// kehilangan setelan itu gara-gara mencopot komponennya. Berkas yang tidak
	// terbaca (hilang, symlink menggantung) tetap diperbaiki.
	if isi, err := os.ReadFile("/etc/resolv.conf"); err == nil &&
		!strings.Contains(string(isi), penandaResolvTechnitium) {
		return
	}
	for _, d := range dirTechnitium {
		bak := filepath.Join(d, "resolv.conf.bak")
		if _, err := os.Lstat(bak); err != nil {
			continue
		}
		// Cadangan yang isinya justru berkas buatan installer dilewati.
		// Cara resmi memperbarui Technitium adalah menjalankan install.sh
		// lagi, dan skrip itu menyalin resolv.conf yang sedang berlaku —
		// yang pada pemasangan kedua sudah berisi "nameserver 127.0.0.1"
		// tulisannya sendiri. Memulihkannya berarti mengarahkan seluruh
		// resolusi nama ke server yang baru saja dicopot.
		if isi, err := os.ReadFile(bak); err == nil &&
			strings.Contains(string(isi), penandaResolvTechnitium) {
			continue
		}
		// Tanpa menghapus lebih dulu: `cp -a` menimpa berkas yang sudah ada,
		// termasuk saat cadangannya berupa symlink. Menghapus duluan hanya
		// membuka jeda dengan mesin yang sama sekali tidak punya resolv.conf
		// kalau salinannya gagal.
		if _, err := run("cp", "-a", bak, "/etc/resolv.conf"); err == nil {
			return
		}
	}
	const stub = "/run/systemd/resolve/stub-resolv.conf"
	if _, err := os.Stat(stub); err != nil {
		return
	}
	_ = os.Remove("/etc/resolv.conf")
	_ = os.Symlink(stub, "/etc/resolv.conf")
}

// purgeTechnitium menghapus yang dibuat server ini sendiri: config, zona,
// blocklist, riwayat, log, dan akun sistem yang memilikinya.
func purgeTechnitium() error {
	for _, p := range []string{"/etc/dns", "/var/log/technitium"} {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	_, _ = run("userdel", "dns-server")
	return nil
}
