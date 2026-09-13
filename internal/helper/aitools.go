package helper

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ---- Alat & Skill wajib untuk semua AI Agent -----------------------------
//
// Panel ini memasang empat alat di setiap sesi AI Agent, bukan menyerahkannya
// ke masing-masing user: rtk dan graphify memangkas pemakaian token secara
// signifikan, ponytail menahan agent dari menulis kode yang sebenarnya tidak
// perlu ada, dan browser-use memberinya satu-satunya jalan untuk mengerjakan
// tugas yang menuntut browser sungguhan. Kalau pemasangannya opsional, sesi
// pertama tiap user berjalan tanpa satu pun dari alat ini.
//
// caveman pernah ada di daftar ini dan sengaja dikeluarkan: perannya sebagai
// skill/harness directive tumpang tindih dengan ponytail, dan pemilik mesin
// memilih memakai ponytail saja. Sisi CLI-nya juga memasang hook pada
// pipeline Bash yang sama dengan rtk — dua filter keluaran bertumpuk pada
// satu perintah. Jangan dimasukkan kembali tanpa diminta.
//
// Sumber dokumentasi resmi (dibaca saat menulis installer ini):
//
//	rtk          https://github.com/rtk-ai/rtk#quick-start
//	graphify     https://github.com/Graphify-Labs/graphify#install
//	ponytail     https://github.com/DietrichGebert/ponytail#install
//	browser-use  https://browser-use.com — https://docs.browser-use.com

// agenAI adalah komponen yang memicu penyediaan toolchain saat dipasang.
var agenAI = []string{"hermes", "claude-code", "codex", "opencode", "openclaw"}

func komponenAgenAI(name string) bool {
	for _, n := range agenAI {
		if n == name {
			return true
		}
	}
	return false
}

// alatWajibAI adalah urutan pemasangan: rtk lebih dulu karena hook-nya yang
// menulis ulang perintah shell juga menguntungkan pemasangan berikutnya.
// browser-use terakhir — venv-nya yang paling besar, dan alat lain tidak
// menunggu apa pun darinya.
var alatWajibAI = []string{"rtk", "graphify", "ponytail", "browser-use"}

// pastikanToolchainAI memasang alat yang belum ada. Kegagalan satu alat tidak
// membatalkan pemasangan agent-nya: agent tetap bisa dipakai, hanya tanpa
// alat itu, dan statusnya terlihat jelas di halaman Components.
func pastikanToolchainAI() {
	for _, nama := range alatWajibAI {
		c, ok := components[nama]
		if !ok {
			continue
		}
		if componentStatus(nama).Installed {
			continue
		}
		if err := c.install(); err != nil {
			log.Printf("toolchain AI: pemasangan %s gagal: %v", nama, err)
		}
	}
	lupakanCacheKomponen()
}

// ---- installer per alat --------------------------------------------------

// salinBinary menyalin binary ke /usr/local/bin. Skrip resmi rtk memasang ke
// $HOME/.local/bin, dan HOME daemon adalah /root yang ber-mode 0700 — user
// panel lain tidak akan pernah bisa membacanya dari sana.
func salinBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("binary %s tidak ditemukan setelah instalasi: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Chmod(dst, 0o755)
}

// installRTK memakai skrip resmi (README §Quick Start) dan berhenti setelah
// binary-nya terpasang.
//
// Pendaftaran ke agent (`rtk init …`) TIDAK dilakukan di sini. Installer
// berjalan sebagai root, jadi apa pun yang ditulisnya hanya berlaku untuk
// /root — sementara agent dijalankan sebagai user panel yang membukanya, dan
// tiap agent butuh flag berbeda. Itu urusan siapkanToolingAgent, yang jalan
// per-user tepat sebelum sesi agent dibuka.
func installRTK() error {
	tahapBaru("menjalankan skrip resmi rtk")
	script, bersihkan, err := unduhSkrip(
		"https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh", "rtk-install.sh")
	if err != nil {
		return err
	}
	defer bersihkan()
	if _, err := run("/bin/sh", script); err != nil {
		return err
	}
	return salinBinary("/root/.local/bin/rtk", "/usr/local/bin/rtk")
}

func uninstallRTK() error {
	_ = os.Remove("/usr/local/bin/rtk")
	_ = os.Remove("/root/.local/bin/rtk")
	return nil
}

const (
	pipxHome   = "/opt/pipx"
	pipxBinDir = "/usr/local/bin"
)

func envPipx() []string {
	return []string{"PIPX_HOME=" + pipxHome, "PIPX_BIN_DIR=" + pipxBinDir}
}

// installGraphify memakai pipx, bukan `pip install` langsung: Debian 12 dan
// Ubuntu 23.04+ menandai Python sistem sebagai externally-managed (PEP 668),
// jadi pip global ditolak. pipx dengan PIPX_BIN_DIR=/usr/local/bin membuat
// binary-nya terlihat oleh semua sesi terminal, bukan cuma root.
//
// Nama paket PyPI-nya `graphifyy` (dua y) sementara CLI-nya `graphify` —
// bukan salah ketik, itu memang beda di dokumentasi resminya.
//
// Dokumentasi menyebut `uv tool install graphifyy` sebagai jalur yang
// dianjurkan, tapi uv memasang ke $HOME/.local/bin milik pemanggilnya — untuk
// daemon root itu berarti /root/.local/bin yang ber-mode 0700, tidak terlihat
// user panel mana pun. pipx memberi isolasi yang sama dengan direktori binary
// yang bisa ditentukan, jadi itu yang dipakai.
//
// Pendaftaran platform (`graphify install --platform …`) tidak dilakukan di
// sini — lihat alasannya di installRTK.
func installGraphify() error {
	if _, err := exec.LookPath("pipx"); err != nil {
		if err := aptInstall("pipx"); err != nil {
			return err
		}
	}
	tahapBaru("memasang graphify lewat pipx")
	_, err := runIn("", envPipx(), "pipx", "install", "graphifyy")
	return err
}

func uninstallGraphify() error {
	_, err := runIn("", envPipx(), "pipx", "uninstall", "graphifyy")
	return err
}

// installBrowserUse memasang CLI Browser Use — kendali browser lewat CDP yang
// dipakai agent untuk tugas yang benar-benar menuntut halaman web: login,
// klik, isi form, ambil data dari halaman yang butuh JavaScript.
//
// Dokumentasi resminya menyebut `uv tool install --python 3.12 --upgrade
// --force browser-use`, dan itu tidak dipakai di sini karena alasan yang sama
// persis dengan graphify: uv memasang ke $HOME/.local/bin milik pemanggilnya,
// dan HOME daemon adalah /root yang ber-mode 0700 — tidak satu pun user panel
// bisa menjalankan berkas di sana. pipx memberi isolasi venv yang sama dengan
// direktori binary yang bisa ditentukan (PIPX_BIN_DIR=/usr/local/bin), jadi
// satu pemasangan terlihat oleh semua sesi AI Agent.
//
// Efek samping yang perlu diketahui: paket ini mendaftarkan lima console
// script (browser-use, browseruse, bu, browser, browser-use-tui) dan pipx
// memasang semuanya ke /usr/local/bin — dua di antaranya bernama sangat umum.
// Dibiarkan apa adanya karena `pipx uninstall` membuang kelimanya lagi, dan
// menyaringnya berarti memasang paket dengan tangan di luar pipx.
//
// ponytail: pipx memakai python sistem, jadi mesin dengan Python < 3.11
// (Debian 11 ke bawah) akan ditolak paketnya. Jalan naiknya kalau itu muncul:
// pasang uv system-wide lalu `uv tool install --python 3.12` dengan
// UV_TOOL_BIN_DIR=/usr/local/bin — uv menurunkan interpreter-nya sendiri.
func installBrowserUse() error {
	if _, err := exec.LookPath("pipx"); err != nil {
		if err := aptInstall("pipx"); err != nil {
			return err
		}
	}
	tahapBaru("memasang browser-use lewat pipx")
	_, err := runIn("", envPipx(), "pipx", "install", "browser-use")
	return err
}

func uninstallBrowserUse() error {
	_, err := runIn("", envPipx(), "pipx", "uninstall", "browser-use")
	return err
}

// ---- Headroom (dipakai Token Saver 9router) -------------------------------

const (
	// venvHeadroom sengaja BUKAN pipx dan BUKAN symlink di /usr/local/bin.
	//
	// 9router memilih interpreter Python-nya dengan `dirname(which headroom)`
	// lalu mencari python3 di direktori yang sama. Symlink pipx di
	// /usr/local/bin membuat direktori itu jadi /usr/local/bin, yang tidak
	// punya python3 — jadi 9router jatuh ke python SISTEM, dan setiap
	// pemasangan extras dari halaman Token Saver berakhir dengan
	// "error: externally-managed-environment … pip install exited with
	// code=1" (PEP 668). Menaruh python3 di /usr/local/bin bukan jalan
	// keluarnya: itu membajak python3 seluruh mesin.
	//
	// Venv biasa menyelesaikannya di akarnya: bin-nya memuat headroom DAN
	// python3 yang punya headroom-ai, jadi 9router menemukan pasangan yang
	// benar dan extras [code]/[ml] terpasang ke dalam venv ini.
	venvHeadroom = "/opt/headroom"
	binHeadroom  = venvHeadroom + "/bin"
)

// installHeadroom memasang CLI Headroom — lapisan kompresi konteks yang
// dipakai halaman Token Saver milik 9router ("Compress context (Headroom)").
//
// Tanpa ini kartu itu berbunyi "Not installed" dan satu-satunya petunjuk yang
// diberikan 9router adalah perintah `pip install "headroom-ai[proxy]"` yang
// harus diketik user sendiri di terminal — dan di Debian 12 / Ubuntu 23.04+
// perintah itu ditolak PEP 668. Karena itu ia dipasang bersama 9router (lihat
// install9Router), bukan ditinggalkan sebagai pekerjaan manual.
//
// Extra [proxy] wajib: itu yang membawa server proxy + endpoint /v1/compress
// yang dipanggil 9router. Paket intinya saja hanya menyediakan library Python.
func installHeadroom(u *userInfo) error {
	// Rilis sebelumnya memasang lewat pipx, dan symlink /usr/local/bin/headroom
	// peninggalannya akan MENANG atas venv ini (9router mencari /usr/local/bin
	// lebih dulu) — jadi pemasangan yang benar tetap menghasilkan extras yang
	// gagal. Dibuang di sini, bukan cuma di uninstall.
	bersihkanHeadroomPipx()

	tahapBaru("menyiapkan virtualenv headroom")
	if err := pastikanVenv(venvHeadroom); err != nil {
		return err
	}
	tahapBaru("memasang headroom-ai[proxy]")
	// --upgrade supaya memasang ulang di atas venv lama menarik versi baru,
	// bukan berhenti dengan "already satisfied".
	if _, err := run(binHeadroom+"/python3", "-m", "pip", "install", "--upgrade", "headroom-ai[proxy]"); err != nil {
		return err
	}
	serahkanVenvHeadroom(u)
	return pasangUnitHeadroom(u)
}

// unitHeadroomTertanam adalah salinan deploy/headroom.service yang ikut
// compile. Sumber kebenarannya deploy/headroom.service; untuk sinkron:
// `cp deploy/headroom.service internal/helper/embed/headroom.service`.
//
//go:embed embed/headroom.service
var unitHeadroomTertanam []byte

const (
	unitDstHeadroom  = "/etc/systemd/system/headroom.service"
	dropDirHeadroom  = unitDstHeadroom + ".d"
	dropUserHeadroom = dropDirHeadroom + "/10-user.conf"
)

// pasangUnitHeadroom memasang unit systemd Headroom lalu menjalankannya.
//
// Tanpa ini Headroom terpasang tapi tidak pernah hidup: halaman Token Saver
// menampilkan "Stopped" di mesin yang paketnya jelas ada, dan satu-satunya
// cara menghidupkannya adalah menekan Start di halaman itu setiap kali mesin
// reboot — 9router menjalankan proxy sebagai proses lepas yang pid-nya
// disimpan di ~/.9router/headroom/proxy.pid, bukan sebagai service.
// Memasangnya sebagai komponen panel tapi membiarkan hidup-matinya jadi
// pekerjaan manual user adalah setengah pekerjaan.
//
// Identitasnya disamakan dengan 9router lewat drop-in: cache CCR dan statistik
// kompresi ditulis ke $HOME, dan dua proses yang menulis ke HOME berbeda
// berarti angka yang ditampilkan Token Saver bukan angka milik proxy yang
// benar-benar melayani permintaan.
func pasangUnitHeadroom(u *userInfo) error {
	// Unit yang sudah ada tidak ditimpa — admin yang menyetel port atau
	// ExecStart sendiri tidak boleh kehilangan setelannya tiap pasang ulang.
	if _, err := os.Stat(unitDstHeadroom); err != nil {
		if len(unitHeadroomTertanam) == 0 {
			return fmt.Errorf("unit systemd headroom tidak tersedia di binary panel")
		}
		if err := os.WriteFile(unitDstHeadroom, unitHeadroomTertanam, 0o644); err != nil {
			return fmt.Errorf("tulis %s: %w", unitDstHeadroom, err)
		}
	}
	pastikanUserHeadroom(u)
	if _, err := run("systemctl", "daemon-reload"); err != nil && !hasNoSystemd() {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	// Kegagalan start tidak dilaporkan sebagai kegagalan pemasangan: di
	// WSL/LXC tanpa systemd init yang utuh systemctl selalu gagal, sementara
	// paket dan unit-nya sudah benar-benar terpasang.
	if _, err := run("systemctl", "enable", "--now", "headroom.service"); err != nil {
		log.Printf("headroom: service gagal dijalankan: %v", err)
	}
	return nil
}

// pastikanUserHeadroom menulis drop-in identitas user, dan me-restart service
// kalau isinya berubah — drop-in baru tidak berlaku pada proses yang sudah
// berjalan dengan identitas lama.
func pastikanUserHeadroom(u *userInfo) {
	if u == nil || u.UID == 0 || u.Home == "" || u.Name == "" {
		return
	}
	inginkan := "[Service]\n" +
		"User=" + u.Name + "\n" +
		"Group=" + strconv.Itoa(u.GID) + "\n" +
		"Environment=HOME=" + u.Home + "\n"
	if b, err := os.ReadFile(dropUserHeadroom); err == nil && string(b) == inginkan {
		return
	}
	if err := os.MkdirAll(dropDirHeadroom, 0o755); err != nil {
		log.Printf("headroom: %s gagal dibuat: %v", dropDirHeadroom, err)
		return
	}
	if err := os.WriteFile(dropUserHeadroom, []byte(inginkan), 0o644); err != nil {
		log.Printf("headroom: drop-in identitas user gagal ditulis: %v", err)
		return
	}
	_, _ = run("systemctl", "daemon-reload")
	if _, err := run("systemctl", "is-active", "--quiet", "headroom.service"); err == nil {
		_, _ = run("systemctl", "restart", "headroom.service")
	}
}

// serahkanVenvHeadroom memberikan venv ke user panel.
//
// Tombol "Install" extras di halaman Token Saver menjalankan
// `python3 -m pip install headroom-ai[code]` dari dalam proses 9router, yang
// berjalan sebagai user panel — bukan root. Venv milik root berarti pip itu
// ditolak dengan permission denied, dan yang terlihat user adalah kegagalan
// yang sama persis seperti sebelum perbaikan ini. Kepemilikan diserahkan ke
// akun pemicunya, bukan dibiarkan di root.
//
// Tanpa identitas user (jalur CLI tanpa sesi panel) tidak ada yang bisa
// diserahkan; proxy-nya tetap jalan, hanya extras yang butuh sudo.
func serahkanVenvHeadroom(u *userInfo) {
	if u == nil || u.Name == "" || u.UID == 0 {
		return
	}
	if _, err := run("chown", "-R", strconv.Itoa(u.UID)+":"+strconv.Itoa(u.GID), venvHeadroom); err != nil {
		log.Printf("headroom: kepemilikan %s gagal diubah: %v", venvHeadroom, err)
	}
}

// pastikanVenv membuat virtualenv kalau belum ada. python3-venv tidak selalu
// terpasang di Debian/Ubuntu server — di sana `python3 -m venv` gagal dengan
// pesan yang menyuruh memasang paket itu, jadi dicoba sekali lalu diulang.
func pastikanVenv(dir string) error {
	if _, err := os.Stat(dir + "/bin/python3"); err == nil {
		return nil
	}
	if _, err := run("python3", "-m", "venv", dir); err == nil {
		return nil
	}
	if err := aptInstall("python3-venv"); err != nil {
		return err
	}
	_, err := run("python3", "-m", "venv", dir)
	return err
}

// bersihkanHeadroomPipx membuang instalasi pipx dari rilis sebelumnya. Gagal
// diabaikan: di mesin yang tidak pernah memakainya memang tidak ada apa-apa.
func bersihkanHeadroomPipx() {
	_, _ = runIn("", envPipx(), "pipx", "uninstall", "headroom-ai")
	_ = os.Remove(pipxBinDir + "/headroom")
}

func uninstallHeadroom() error {
	// Service dihentikan dan unitnya dibuang lebih dulu: unit yang menunjuk
	// venv yang sudah tidak ada hanya menghasilkan status=203/EXEC berulang,
	// dan pemasangan berikutnya menemukannya masih ada lalu membiarkannya.
	_, _ = run("systemctl", "disable", "--now", "headroom.service")
	_ = os.RemoveAll(dropDirHeadroom)
	_ = os.Remove(unitDstHeadroom)
	_, _ = run("systemctl", "daemon-reload")
	bersihkanHeadroomPipx()
	return os.RemoveAll(venvHeadroom)
}

// versiHeadroom membaca versi dari metadata dist-info di dalam venv, bukan
// `headroom --version`: CLI Python itu memuat kompresornya saat start dan
// butuh beberapa detik, sementara halaman Components memprobe seluruh katalog
// sekaligus.
func versiHeadroom() string {
	m, _ := filepath.Glob(venvHeadroom + "/lib/python*/site-packages/headroom_ai-*.dist-info")
	if len(m) == 0 {
		return ""
	}
	nama := filepath.Base(m[0])
	nama = strings.TrimPrefix(nama, "headroom_ai-")
	return strings.TrimSuffix(nama, ".dist-info")
}

// headroomTerpasang memeriksa binary di dalam venv, bukan lewat PATH: venv ini
// sengaja TIDAK punya symlink di /usr/local/bin (lihat venvHeadroom).
func headroomTerpasang() bool {
	_, err := os.Stat(binHeadroom + "/headroom")
	return err == nil
}

// pastikanHeadroom memasang Headroom kalau belum ada. Kegagalannya TIDAK
// membatalkan pemasangan 9router: gateway-nya tetap berfungsi penuh tanpa
// kompresi konteks, dan status Headroom terlihat sendiri di halaman Components.
func pastikanHeadroom(u *userInfo) {
	if headroomTerpasang() {
		// Venv yang sudah ada tapi masih milik root dari rilis sebelumnya
		// tetap membuat tombol extras gagal — penyembuhannya di jalur yang
		// dilewati tiap pemasangan, bukan cuma di jalur instalasi baru.
		serahkanVenvHeadroom(u)
		// Alasan yang sama untuk service-nya: mesin yang memasang Headroom
		// sebelum rilis ini punya venv lengkap TANPA unit systemd, jadi
		// Token Saver melaporkan "Stopped" selamanya sampai ada yang menekan
		// Start manual. Unit menyusul di sini, bukan hanya saat venv baru.
		if err := pasangUnitHeadroom(u); err != nil {
			log.Printf("9router: unit headroom gagal dipasang: %v", err)
		}
		return
	}
	if err := installHeadroom(u); err != nil {
		log.Printf("9router: pemasangan headroom gagal: %v", err)
	}
}

// versiPipx membaca versi paket dari metadata venv pipx.
//
// Bukan lewat `<binary> --version`: CLI browser-use tidak punya flag itu, dan
// argumen yang tidak dikenalnya diperlakukan sebagai kode yang harus
// dijalankan di browser — probe versi akan mencoba menghidupkan Chrome.
func versiPipx(paket string) func() string {
	return func() string {
		path := filepath.Join(pipxHome, "venvs", paket, "pipx_metadata.json")
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		var m struct {
			MainPackage struct {
				Version string `json:"package_version"`
			} `json:"main_package"`
		}
		if json.Unmarshal(b, &m) != nil {
			return ""
		}
		return m.MainPackage.Version
	}
}

// penandaPonytail mencatat bahwa plugin sudah didaftarkan. ponytail bukan
// binary — ia plugin/harness directive per-CLI, jadi tidak ada yang bisa
// dicari lewat PATH untuk menentukan statusnya.
const penandaPonytail = "/var/lib/linux-dashboard/ponytail.terpasang"

// perintahPluginPonytail memetakan CLI agent → langkah pendaftaran plugin,
// persis seperti tabel instalasi di README ponytail.
var perintahPluginPonytail = map[string][][]string{
	"claude": {
		{"plugin", "marketplace", "add", "DietrichGebert/ponytail"},
		{"plugin", "install", "ponytail@ponytail"},
	},
	"codex": {
		{"plugin", "marketplace", "add", "DietrichGebert/ponytail"},
		{"plugin", "add", "ponytail@ponytail"},
	},
	"copilot": {
		{"plugin", "marketplace", "add", "DietrichGebert/ponytail"},
		{"plugin", "install", "ponytail@ponytail"},
	},
	"clawhub": {
		{"install", "ponytail"},
	},
}

// installPonytail mendaftarkan plugin ke setiap CLI agent yang ada di mesin.
// Kalau tidak satu pun CLI-nya terpasang — atau marketplace tidak bisa
// dihubungi — instalasi tetap dianggap berhasil: arahan ponytail (level ultra
// + tiga skill bundle) ditulis ke berkas instruksi tiap agent oleh
// siapkanArahanAI, dan itu yang benar-benar mengubah perilaku agent.
func installPonytail() error {
	var kena []string
	for cli, langkah := range perintahPluginPonytail {
		if _, err := exec.LookPath(cli); err != nil {
			continue
		}
		gagal := false
		for _, args := range langkah {
			if _, err := run(cli, args...); err != nil {
				log.Printf("toolchain AI: ponytail untuk %s gagal: %v", cli, err)
				gagal = true
				break
			}
		}
		if !gagal {
			kena = append(kena, cli)
		}
	}
	if err := os.MkdirAll(filepath.Dir(penandaPonytail), 0o755); err != nil {
		return err
	}
	isi := "arahan\n"
	if len(kena) > 0 {
		isi = strings.Join(kena, "\n") + "\n"
	}
	return os.WriteFile(penandaPonytail, []byte(isi), 0o644)
}

func uninstallPonytail() error {
	return os.Remove(penandaPonytail)
}

func ponytailTerpasang() bool {
	_, err := os.Stat(penandaPonytail)
	return err == nil
}

// ---- pemulihan binary agent yang terpasang tapi cacat --------------------

// komponenAgenPerBinary memetakan nama binary yang dijalankan sesi AI Agent
// ke komponen panel yang memasangnya. Namanya sama untuk semua agent kecuali
// Claude Code, yang komponennya berbeda dari nama binary-nya.
//
// hermes sengaja tidak ada di sini: ia dipasang lewat skrip resmi vendor,
// bukan npm, jadi tidak terkena bentuk kerusakan yang ditangani fungsi di
// bawah — dan memasang ulang agent yang sebenarnya sehat itu mahal.
var komponenAgenPerBinary = map[string]string{
	"claude":   "claude-code",
	"codex":    "codex",
	"opencode": "opencode",
	"openclaw": "openclaw",
}

// perbaikanAgentDicoba menahan pemasangan ulang agar terjadi paling banyak
// sekali per agent selama daemon hidup. Tanpa ini, agent yang tetap rusak
// setelah diperbaiki akan memicu pemasangan ulang berpuluh detik SETIAP kali
// user membuka halaman AI Agent — gangguan yang jauh lebih parah daripada
// kerusakan aslinya.
var perbaikanAgentDicoba sync.Map

// perluPerbaikanAgent menjawab apakah agent ini terpasang tapi tidak bisa
// dijalankan — satu-satunya keadaan yang layak diperbaiki diam-diam.
//
// Bentuk kerusakan yang nyata terjadi: npm 12 memblokir install script secara
// bawaan, sementara CLI agent berbasis npm adalah wrapper tipis yang binary
// aslinya baru disalin ke tempatnya oleh postinstall. Hasilnya `claude` ada di
// PATH tapi tiap eksekusinya hanya mencetak "claude native binary not
// installed", dan yang dilihat user di panel cuma supervisor yang mencoba tiga
// kali lalu menyerah. Mesin yang komponennya dipasang panel versi lama tetap
// membawa kerusakan itu sampai ada yang memasang ulang secara manual — dan
// justru itu yang tidak boleh dibebankan ke user.
func perluPerbaikanAgent(perintah string) bool {
	if _, ok := komponenAgenPerBinary[perintah]; !ok {
		return false
	}
	if _, sudah := perbaikanAgentDicoba.Load(perintah); sudah {
		return false
	}
	// Belum terpasang sama sekali bukan urusan di sini: halaman AI Agent sudah
	// menampilkan tombol pasangnya, dan memasang diam-diam di balik layar
	// menyembunyikan keputusan yang seharusnya milik user.
	if _, ada := lookBinarySistem(perintah); !ada {
		return false
	}
	return !binaryBisaJalan(perintah)
}

// perbaikiAgent memasang ulang komponen agent lewat jalur install yang SAMA
// dengan halaman Components — termasuk identitas user-nya.
//
// Identitas itu yang membuat perbaikan ini benar-benar memperbaiki. Instalasi
// yang cacat di mesin lama adalah paket npm global milik root; memasang ulang
// sebagai root hanya menghasilkan kerusakan yang sama sekali lagi. Dijalankan
// atas nama user sesi, jalur installnya berpindah ke installer resmi vendor di
// dalam home user itu, dan itulah instalasi yang sesudahnya benar-benar bisa
// dijalankan — dan bisa memperbarui dirinya sendiri.
//
// Kegagalan tidak membatalkan sesi: pesan error asli dari agent jauh lebih
// berguna bagi user daripada terminal yang menolak muncul tanpa penjelasan.
func perbaikiAgent(perintah string, u *userInfo) {
	perbaikanAgentDicoba.Store(perintah, true)
	nama := komponenAgenPerBinary[perintah]
	c, ok := components[nama]
	if !ok {
		return
	}
	log.Printf("agent %s: binary ada tapi gagal dijalankan — memasang ulang komponen %s", perintah, nama)
	if err := jalankanInstall(c, u); err != nil {
		log.Printf("agent %s: pemasangan ulang gagal: %v", perintah, err)
		return
	}
	lupakanCacheKomponen()
}

// binaryBisaJalan menguji agent lewat `--version`. Yang dipakai adalah KODE
// KELUARNYA, bukan keluarannya: wrapper npm yang binary native-nya tidak
// pernah disalin justru mencetak banyak teks sebelum keluar dengan kode 1,
// jadi "ada keluaran" bukan tanda sehat.
func binaryBisaJalan(nama string) bool {
	ctx, batal := context.WithTimeout(context.Background(), batasProbeVersi)
	defer batal()
	cmd := exec.CommandContext(ctx, nama, "--version")
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LC_ALL=C",
	}
	return cmd.Run() == nil
}
