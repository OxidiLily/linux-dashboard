package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Pemasangan CLI agent AI memakai perintah resmi dari dokumentasi vendornya,
// dijalankan dengan identitas user panel yang menekan Pasang.
//
// Sebelumnya panel memasang keempatnya lewat `npm install -g` sebagai root.
// Itu keliru pada dua tingkat sekaligus:
//
//  1. Bukan cara yang didokumentasikan. Paket npm-nya ada, tapi bukan itu
//     jalur yang diuji vendornya, dan untuk sebagian agent isinya hanya
//     wrapper yang binary aslinya harus diturunkan postinstall — satu langkah
//     yang gagal diam-diam membuat panel melaporkan "terpasang" untuk perintah
//     yang setiap kali dijalankan hanya mencetak "native binary not installed".
//
//  2. Terpasang untuk root, bukan untuk user yang memakainya. Semua installer
//     resmi di bawah memasang ke dalam $HOME dan memang dirancang begitu:
//     binernya harus bisa DITULIS ULANG oleh pemakainya supaya fitur
//     pembaruan otomatis bekerja. Dipasang root ke prefix milik root, agent
//     yang jalan sebagai user panel melaporkan "Auto-update failed: no write
//     permission to npm prefix" pada setiap kali start dan tidak pernah bisa
//     memperbarui dirinya sendiri. Installer Claude Code bahkan menolak
//     berjalan di bawah sudo persis karena alasan ini.
//
// Karena itu perintah di bawah disalin APA ADANYA dari dokumentasi masing-
// masing — termasuk `| sh` milik codex yang memang berbeda dari `| bash`
// milik yang lain — dan dijalankan dengan kredensial user panel.
var perintahInstallAgen = map[string]string{
	"claude-code": "curl -fsSL https://claude.ai/install.sh | bash",
	"hermes":      "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash",
	"opencode":    "curl -fsSL https://opencode.ai/install | bash",
	"codex":       "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
	"openclaw":    "curl -fsSL https://openclaw.ai/install.sh | bash",
}

// dirBinAgen adalah direktori tempat installer resmi meletakkan binernya,
// relatif terhadap HOME user:
//
//	.local/bin               claude.ai/install.sh, chatgpt.com/codex/install.sh,
//	                         dan hermes-agent…/install.sh saat non-root
//	.opencode/bin            opencode.ai/install
//	.openclaw/tools/node/bin openclaw.ai/install.sh saat ia memasang Node.js
//	                         user-space (Node sistem dengan SQLite bermasalah)
//	.npm-global/bin          prefix npm milik user, dipakai openclaw & codex
//	                         kalau prefix npm sistem tidak bisa ditulis
//	.bun/bin                 opencode versi lama lewat bun
//
// HARUS tetap sinkron dengan daftar PATH di agent-loop.sh: yang satu memutuskan
// apakah kartu Components menyalakan tanda "terpasang", yang satu memutuskan
// apakah sesi AI Agent benar-benar menemukan binernya. Kalau keduanya berbeda,
// panel akan mengaku terpasang lalu gagal menjalankannya — atau sebaliknya.
var dirBinAgen = []string{
	".local/bin",
	".opencode/bin",
	".openclaw/tools/node/bin",
	".npm-global/bin",
	".bun/bin",
}

// batasInstallAgen membatasi lama satu pemasangan agent.
//
// Wajib ada, bukan kehati-hatian berlebihan: installer ini mengunduh puluhan
// megabyte dan sebagian memasang runtime Node.js-nya sendiri. Tanpa batas,
// satu unduhan yang menggantung menahan permintaan Pasang selamanya — helper
// tidak memasang deadline baca dan klien HTTP panel pun tidak — sehingga user
// tidak pernah melihat pesan berhasil maupun gagal.
const batasInstallAgen = 20 * time.Minute

// bisaDieksekusi menjawab satu pertanyaan: apakah path ini berkas yang bisa
// dijalankan. Symlink diikuti (os.Stat, bukan Lstat) karena installer resmi
// gemar menaruh symlink launcher di .local/bin.
func bisaDieksekusi(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0
}

// cariBinerAgenDiRumah mencari binary agent di dalam satu HOME.
func cariBinerAgenDiRumah(binary, home string) (string, bool) {
	if home == "" {
		return "", false
	}
	for _, d := range dirBinAgen {
		p := filepath.Join(home, d, binary)
		if bisaDieksekusi(p) {
			return p, true
		}
	}
	return "", false
}

// cariBinerAgen menjawab "apakah agent ini tersedia untuk user INI" —
// pertanyaan yang menentukan apakah tombol Pasang masih perlu dijalankan.
// Jalur sistem tetap ikut diperiksa: mesin yang memasang agent lewat rilis
// panel sebelumnya (npm global) tidak boleh tiba-tiba dilaporkan kosong.
func cariBinerAgen(binary string, u *userInfo) (string, bool) {
	if u != nil {
		if p, ok := cariBinerAgenDiRumah(binary, u.Home); ok {
			return p, true
		}
	}
	return lookBinary(binary)
}

// agenSehatUntuk menjawab pertanyaan yang lebih tajam daripada "binernya ada
// atau tidak": apakah agent ini terpasang dengan cara yang membuat SELURUH
// fungsinya bekerja untuk user ini.
//
// Pembedanya kepemilikan berkas. Agent yang dipasang panel versi lama lewat
// `npm install -g` sebagai root ada di PATH dan memang bisa dijalankan siapa
// pun — tapi binernya milik root, jadi pembaruan otomatis agent gagal di
// setiap start ("Auto-update failed: no write permission to npm prefix") dan
// TIDAK ADA satu pun tombol di panel yang bisa memperbaikinya, karena panel
// menganggapnya sudah terpasang lalu pulang lebih awal. Instalasi seperti itu
// harus dihitung BELUM terpasang untuk user tersebut, supaya tombol Pasang
// menggantinya dengan instalasi resmi di dalam home user.
//
// root dikecualikan: kalau yang memakai panel memang root, biner milik root
// justru bisa ia tulis sendiri dan tidak ada yang perlu diperbaiki.
func agenSehatUntuk(binary string, u *userInfo) bool {
	if u == nil {
		_, ok := agenTerpasangDiMesin(binary)
		return ok
	}
	if _, ok := cariBinerAgenDiRumah(binary, u.Home); ok {
		return true
	}
	p, ok := lookBinary(binary)
	if !ok {
		return false
	}
	if u.UID == 0 {
		return true
	}
	return pemilikBerkas(p) == u.UID
}

// pemilikBerkas mengembalikan UID pemilik, atau -1 kalau tidak terbaca.
func pemilikBerkas(path string) int {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int(st.Uid)
}

// catatanAgenLama menjelaskan instalasi warisan yang masih bisa dijalankan
// tapi tidak bisa memperbarui dirinya sendiri.
//
// Ditulis machine-wide karena kartu Components tidak tahu siapa yang sedang
// bertanya; kalimatnya karena itu menyebutkan syaratnya ("kalau Anda memakai
// panel sebagai user biasa") alih-alih memastikan sesuatu yang belum tentu
// benar untuk pembacanya.
func catatanAgenLama(binary string) string {
	p, ok := lookBinary(binary)
	if !ok || pemilikBerkas(p) != 0 {
		return ""
	}
	for _, home := range rumahAkunManusia() {
		if _, ada := cariBinerAgenDiRumah(binary, home); ada {
			return ""
		}
	}
	return "Terpasang system-wide sebagai root — jalur npm yang dipakai panel versi lama. " +
		"Kalau Anda memakai panel sebagai user biasa, pembaruan otomatis agent ini akan gagal " +
		"setiap kali dijalankan. Tekan Pasang untuk memasang ulang dengan installer resmi " +
		"vendor ke home Anda."
}

// agenTerpasangDiMesin menjawab pertanyaan yang ditanyakan kartu Components,
// yang tidak tahu user mana yang sedang bertanya: apakah agent ini ada untuk
// SALAH SATU akun manusia di mesin ini, atau system-wide.
func agenTerpasangDiMesin(binary string) (string, bool) {
	if p, ok := lookBinary(binary); ok {
		return p, true
	}
	for _, home := range rumahAkunManusia() {
		if p, ok := cariBinerAgenDiRumah(binary, home); ok {
			return p, true
		}
	}
	return "", false
}

// rumahAkunManusia mengumpulkan HOME setiap akun login dari /etc/passwd —
// termasuk root, yang memang bisa jadi pemilik instalasi di mesin single-user.
func rumahAkunManusia() []string {
	users, err := listLinuxUsers()
	if err != nil {
		return nil
	}
	var out []string
	for _, u := range users {
		if u.Home != "" {
			out = append(out, u.Home)
		}
	}
	return out
}

// versiAgen membaca versi dari binary agent di mana pun ia terpasang.
//
// Hasilnya di-cache lewat versiTersimpan dengan kunci berisi ukuran & waktu
// ubah berkas: sebagian CLI ini butuh beberapa detik hanya untuk mencetak
// versinya, dan halaman Components memprobe seluruh katalog sekaligus.
func versiAgen(binary string) func() string {
	return func() string {
		path, ok := agenTerpasangDiMesin(binary)
		if !ok {
			return ""
		}
		return versiTersimpan(path, func() string { return firstLine(tryRun(path, "--version")) })
	}
}

// envAgen menyusun lingkungan pemasangan milik user.
//
// pathAgen menyusun PATH yang memuat seluruh dirBinAgen milik user lebih dulu,
// baru jalur sistem. Dipisah dari envAgen karena bukan hanya installer agent
// yang membutuhkannya: unit systemd 9router juga dijalankan dengan PATH ini,
// supaya `which <agent>` di dalam 9router menemukan biner yang sama dengan
// yang dilihat halaman Components. Dua penyusun PATH yang berbeda berarti
// panel dan 9router bisa berbeda pendapat soal agent mana yang terpasang.
func pathAgen(u *userInfo) string {
	jalur := make([]string, 0, len(dirBinAgen)+1)
	for _, d := range dirBinAgen {
		jalur = append(jalur, filepath.Join(u.Home, d))
	}
	jalur = append(jalur, "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	return strings.Join(jalur, ":")
}

// PATH memuat dirBinAgen lebih dulu supaya installer yang memeriksa "apakah
// versi lama sudah ada" menemukan miliknya sendiri. HOME/USER/LOGNAME wajib:
// setiap installer di atas menaruh berkasnya relatif terhadap $HOME, dan
// tanpa ketiganya mereka jatuh ke HOME milik proses helper, yaitu /root.
// SHELL menentukan berkas rc mana yang disunting saat installer menambahkan
// direktorinya ke PATH login user.
func envAgen(u *userInfo) []string {
	shell := u.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{
		"PATH=" + pathAgen(u),
		"HOME=" + u.Home,
		"USER=" + u.Name,
		"LOGNAME=" + u.Name,
		"SHELL=" + shell,
		"DEBIAN_FRONTEND=noninteractive",
		"LC_ALL=C",
	}
}

// installAgenResmi menjalankan perintah pemasangan resmi sebagai user panel.
//
// Perintahnya diserahkan ke `bash -c` utuh, bukan dipecah jadi array: bentuk
// yang didokumentasikan vendor adalah sebuah PIPELINE (`curl … | bash`), dan
// memecahnya berarti menuliskan ulang perintah yang justru diminta dipakai apa
// adanya. Perintahnya sendiri konstanta di berkas ini — tidak ada satu pun
// bagian yang berasal dari input user, jadi tidak ada jalan injeksi.
//
// TIDAK ada TTY yang diberikan, dan itu disengaja. Installer Hermes dan
// OpenClaw menawarkan wizard interaktif kalau /dev/tty bisa dibuka; tanpa TTY
// keduanya melewatinya sendiri sambil mencetak cara menjalankannya nanti.
// Itulah sebabnya panel tidak perlu lagi menambahkan flag seperti
// --skip-setup: memotong perintah resmi tidak dibutuhkan untuk hasil yang sama.
func installAgenResmi(nama, binary string, u *userInfo) error {
	perintah, ok := perintahInstallAgen[nama]
	if !ok {
		return errInvalid("tidak ada perintah pemasangan resmi untuk %q", nama)
	}
	if u == nil {
		return errInvalid("pemasangan %s butuh identitas user panel", nama)
	}
	// Installer menulis ke $HOME. Home yang belum ada (akun baru yang belum
	// pernah login) membuat semuanya gagal dengan error yang menyebut path
	// dalam, bukan penyebabnya.
	if fi, err := os.Stat(u.Home); err != nil || !fi.IsDir() {
		return errInvalid("home %s milik user %s tidak ada — login sekali lewat Terminal panel lalu ulangi", u.Home, u.Name)
	}

	// Kelima perintah resmi diawali `curl`. Di image server minimal (LXC,
	// cloud-init tanpa paket tambahan) curl belum tentu ada, dan tanpa
	// pemeriksaan ini kegagalannya muncul sebagai "curl: command not found"
	// di dalam keluaran installer — kalimat yang tidak menyebut bahwa panel
	// sendiri bisa memasangnya dalam satu langkah.
	if _, ada := lookBinary("curl"); !ada {
		tahapBaru("memasang curl, prasyarat installer resmi")
		if err := aptInstall("curl"); err != nil {
			return errInvalid("curl belum terpasang dan panel gagal memasangnya: %v", err)
		}
	}

	tahapBaru(fmt.Sprintf("menjalankan installer resmi %s sebagai %s", nama, u.Name))

	ctx, batal := context.WithTimeout(context.Background(), batasInstallAgen)
	defer batal()

	cmd := exec.CommandContext(ctx, "bash", "-c", perintah)
	cmd.Dir = u.Home
	cmd.Env = envAgen(u)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}

	pipa, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	ekor := bacaKeProgres(pipa)
	errJalan := cmd.Wait()
	if errJalan != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return errInvalid("installer %s belum selesai setelah %s dan dihentikan. "+
				"Jaringan ke server vendor mungkin sangat lambat — coba lagi nanti", nama, batasInstallAgen)
		}
		// Baris terakhir installer adalah tempat penyebabnya ditulis; error Go
		// sendiri hanya berbunyi "exit status 1" dan tidak menolong siapa pun.
		return errInvalid("installer %s gagal: %s", nama, firstNonEmpty(ekor(), errJalan.Error()))
	}
	if _, ok := cariBinerAgen(binary, u); !ok {
		return errInvalid("installer %s selesai tanpa error tapi binernya tidak ditemukan di %s. "+
			"Keluaran terakhir: %s", nama, u.Home, ekor())
	}
	return nil
}

// bacaKeProgres menyalurkan keluaran installer ke bar progres dan menyimpan
// beberapa baris terakhir untuk dipakai sebagai pesan error.
//
// Fungsi yang dikembalikan baru boleh dipanggil SETELAH cmd.Wait(): sebelum
// itu goroutine pembacanya masih menulis ke slice yang sama.
func bacaKeProgres(r io.Reader) func() string {
	selesai := make(chan struct{})
	var ekor []string
	go func() {
		defer close(selesai)
		sc := bufio.NewScanner(r)
		// Installer OpenClaw mencetak baris ringkasan yang panjang; batas
		// bawaan bufio (64 KiB) cukup, tapi buffer awal yang lebih besar
		// menghindari realokasi tiap baris.
		sc.Buffer(make([]byte, 0, 8*1024), 256*1024)
		for sc.Scan() {
			baris := strings.TrimSpace(bersihkanANSI(sc.Text()))
			if baris == "" {
				continue
			}
			setProgres(0, "", baris)
			ekor = append(ekor, baris)
			if len(ekor) > 8 {
				ekor = ekor[1:]
			}
		}
	}()
	return func() string {
		<-selesai
		return strings.Join(ekor, " · ")
	}
}

// bersihkanANSI membuang escape sequence pewarnaan. Installer ini menulis
// untuk terminal berwarna; kode mentahnya akan tampil sebagai sampah di kartu
// komponen dan di pesan error.
func bersihkanANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			// Lewati sampai huruf penutup sequence (mis. "m" pada "\x1b[31m").
			for i++; i < len(s); i++ {
				if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// uninstallAgen menghapus agent dari SETIAP rumah user yang memilikinya, lalu
// dari jalur sistem.
//
// Dijalankan lintas user karena kartu Components pun melapor lintas user:
// menghapus milik satu user saja akan menyisakan kartu yang tetap berkata
// "terpasang" sesudah tombol Hapus ditekan, tanpa cara apa pun dari panel
// untuk membereskannya.
//
// Hanya berkas yang memang dipasang installer yang dihapus — daftar dirData
// per agent, bukan sapuan glob atas HOME. Data konfigurasi (mis. ~/.claude)
// TIDAK ikut: itu milik user, dan menghapusnya berarti membuang riwayat serta
// kredensial yang tidak bisa dikembalikan.
func uninstallAgen(nama, binary string) error {
	dirs := dirPasangAgen[nama]
	// npm lebih dulu: ia yang tahu symlink mana yang dibuatnya sendiri.
	// Menghapus symlink-nya duluan hanya menyisakan paketnya di
	// /usr/lib/node_modules, dan `npm uninstall` sesudahnya tidak lagi punya
	// jejak untuk dibereskan. Instalasi lama ini masih ada di mesin yang
	// pernah memakai rilis panel sebelumnya.
	if pkg, ok := paketNpmAgenLama[nama]; ok {
		_ = npmUninstallGlobal(pkg)
	}
	for _, home := range append(rumahAkunManusia(), "/root") {
		for _, d := range dirBinAgen {
			_ = os.Remove(filepath.Join(home, d, binary))
		}
		for _, d := range dirs {
			_ = os.RemoveAll(filepath.Join(home, d))
		}
	}
	for _, d := range []string{"/usr/local/bin", "/usr/bin"} {
		_ = os.Remove(filepath.Join(d, binary))
	}
	for _, d := range dirs {
		_ = os.RemoveAll(filepath.Join("/usr/local/lib", filepath.Base(d)))
	}
	return nil
}

// dirPasangAgen: direktori PEMASANGAN (bukan konfigurasi) yang dibuat
// installer di dalam HOME. Dipisah dari data supaya uninstall tidak pernah
// menyentuh percakapan, sesi, atau kredensial milik user.
var dirPasangAgen = map[string][]string{
	"opencode": {".opencode/bin"},
	"openclaw": {".openclaw/tools"},
	"hermes":   {".hermes/hermes-agent"},
}

// paketNpmAgenLama adalah paket npm global yang dipakai panel SEBELUM pindah
// ke installer resmi. Disimpan hanya untuk keperluan pembersihan.
var paketNpmAgenLama = map[string]string{
	"claude-code": "@anthropic-ai/claude-code",
	"codex":       "@openai/codex",
	"opencode":    "opencode-ai",
	"openclaw":    "openclaw",
}
