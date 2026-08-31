package helper

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ---- Alat & Skill wajib untuk semua AI Agent -----------------------------
//
// Panel ini memasang tiga alat di setiap sesi AI Agent, bukan menyerahkannya
// ke masing-masing user: rtk dan graphify memangkas pemakaian token secara
// signifikan, dan ponytail menahan agent dari menulis kode yang sebenarnya
// tidak perlu ada. Kalau pemasangannya opsional, sesi pertama tiap user
// berjalan tanpa satu pun dari alat ini.
//
// caveman pernah ada di daftar ini dan sengaja dikeluarkan: perannya sebagai
// skill/harness directive tumpang tindih dengan ponytail, dan pemilik mesin
// memilih memakai ponytail saja. Sisi CLI-nya juga memasang hook pada
// pipeline Bash yang sama dengan rtk — dua filter keluaran bertumpuk pada
// satu perintah. Jangan dimasukkan kembali tanpa diminta.
//
// Sumber dokumentasi resmi (dibaca saat menulis installer ini):
//
//	rtk       https://github.com/rtk-ai/rtk#quick-start
//	graphify  https://github.com/Graphify-Labs/graphify#install
//	ponytail  https://github.com/DietrichGebert/ponytail#install

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
var alatWajibAI = []string{"rtk", "graphify", "ponytail"}

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
	_, err := runIn("", envPipx(), "pipx", "install", "graphifyy")
	return err
}

func uninstallGraphify() error {
	_, err := runIn("", envPipx(), "pipx", "uninstall", "graphifyy")
	return err
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

// perbaikiAgent memasang ulang komponen agent lewat jalur install yang sama
// dengan halaman Components. Dijalankan sebagai root oleh daemon helper — satu-
// satunya titik di jalur sesi yang punya hak menulis /usr/lib/node_modules.
//
// Kegagalan tidak membatalkan sesi: pesan error asli dari agent jauh lebih
// berguna bagi user daripada terminal yang menolak muncul tanpa penjelasan.
func perbaikiAgent(perintah string) {
	perbaikanAgentDicoba.Store(perintah, true)
	nama := komponenAgenPerBinary[perintah]
	c, ok := components[nama]
	if !ok || c.install == nil {
		return
	}
	log.Printf("agent %s: binary ada tapi gagal dijalankan — memasang ulang komponen %s", perintah, nama)
	if err := c.install(); err != nil {
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
