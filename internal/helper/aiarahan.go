package helper

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Arahan alat & skill wajib ditulis ke berkas instruksi global tiap agent,
// bukan diandalkan pada plugin/skill yang harus dipasang manual per user.
//
// Alasannya praktis: memasang plugin lewat marketplace butuh CLI-nya hidup,
// jaringan, dan sering satu langkah interaktif — sementara berkas instruksi
// dibaca agent apa adanya di awal tiap sesi. Plugin tetap dipasang kalau bisa
// (lihat installPonytail); berkas ini yang menjamin arahannya selalu sampai.

const (
	penandaMulai   = "<!-- MULAI linux-dashboard: alat & skill wajib -->"
	penandaSelesai = "<!-- SELESAI linux-dashboard: alat & skill wajib -->"
)

// arahanAlatAI adalah isi blok terkelola. Ditulis apa adanya di antara dua
// penanda supaya isi berkas milik user di luar blok tidak pernah tersentuh.
const arahanAlatAI = `## Alat & Skill wajib (dikelola Linux Dashboard)

Sesi AI Agent ini dibekali tiga alat di bawah. Pakai sebagai jalur utama,
bukan sebagai opsi tambahan. Baca dokumentasi resminya sebelum memakai
perintah yang belum pernah kamu pakai di mesin ini.

### 1. rtk — pemangkas token untuk perintah shell
` + "`https://github.com/rtk-ai/rtk#quick-start`" + `

Perintah shell yang keluarannya panjang (git, ls, cat, grep, npm, docker,
kubectl, dan ~100 lainnya) dijalankan lewat ` + "`rtk <perintah>`" + `. Panel sudah
mendaftarkan rtk untuk agent ini dengan target yang sesuai, jadi penulisan
ulang berjalan otomatis tanpa perlu kamu ingat. Meta command yang tetap
dipanggil langsung:

    rtk gain              # ringkasan penghematan token
    rtk gain --history    # riwayat per perintah
    rtk discover          # peluang yang terlewat
    rtk proxy <cmd>       # eksekusi mentah tanpa filter (untuk debugging)

Saat keluaran sebuah perintah terlihat janggal — kode keluar tidak cocok
dengan isinya, teks terpotong di tengah, atau ringkasan yang tidak masuk
akal — ulangi lewat ` + "`rtk proxy <cmd>`" + ` sebelum menyimpulkan apa pun.
Yang kamu baca mungkin hasil filter, bukan keluaran perintahnya.

### 2. graphify — knowledge graph kode
` + "`https://github.com/Graphify-Labs/graphify#install`" + `

Sebelum menelusuri repo yang belum kamu kenal, bangun grafnya sekali lalu
baca ringkasannya — jangan membuka berkas satu per satu untuk mencari relasi:

    graphify .            # menghasilkan graph.html, GRAPH_REPORT.md, graph.json

Parsing-nya deterministik lewat tree-sitter (tanpa vector store), jadi
hasilnya bisa dipercaya untuk menjawab "siapa memanggil apa" lintas berkas.

### 3. ponytail — harness "lazy senior dev" (level ultra)
` + "`https://github.com/DietrichGebert/ponytail#install`" + `

Kode terbaik adalah kode yang tidak pernah ditulis. Sebelum menambah kode
baru, turuni tangga keputusan ini dan berhenti di anak tangga pertama yang
menjawab: (1) apakah ini memang perlu ada? (2) apakah sudah ada di repo ini?
(3) apakah pustaka standar sudah menyediakannya? (4) apakah fitur bawaan
platform sudah cukup? Baru setelah keempatnya "tidak", tulis kode.

Penyederhanaan yang sengaja memotong sudut dengan batas yang diketahui
ditandai komentar ` + "`ponytail:`" + ` yang menyebut batas itu sekaligus jalan
naiknya — itu yang nanti dipanen /ponytail-debt jadi ledger.

Perintah yang tersedia:

    /ponytail ultra    # setel intensitas (lite/full/ultra/off) — pakai ultra
    /ponytail-review   # periksa diff berjalan untuk over-engineering
    /ponytail-audit    # periksa seluruh repo, bukan cuma diff
    /ponytail-debt     # kumpulkan jalan pintas "ponytail:" jadi satu ledger

Tiga perintah pemeriksa di atas menyerahkan daftar temuan, dan daftar itu
ditulis lengkap: satu baris penuh per temuan, dengan lokasinya — berkas dan
baris — supaya bisa langsung ditindaklanjuti tanpa menjalankan ulang
pemeriksaannya. Jangan
gabungkan beberapa temuan jadi satu baris ringkasan; temuan yang hilang
dari daftar tidak bisa dikerjakan, dan pembacanya tidak punya cara tahu
ada yang hilang. Yang boleh diringkas adalah penjelasan di sekitar daftar,
bukan daftarnya.
`

// berkasArahanAgent memetakan perintah CLI agent → berkas instruksi global
// miliknya, relatif terhadap home user. Satu agent bisa punya lebih dari satu
// lokasi yang dibaca; semuanya ditulis supaya arahan tetap sampai walau
// konvensi berubah antar versi.
var berkasArahanAgent = map[string][]string{
	"claude":   {".claude/CLAUDE.md"},
	"codex":    {".codex/AGENTS.md"},
	"opencode": {".config/opencode/AGENTS.md"},
	"openclaw": {".openclaw/AGENTS.md"},
	"hermes":   {".hermes/AGENTS.md"},
}

// wiringAgent menggambarkan cara mendaftarkan rtk & graphify untuk satu agent.
// Keduanya punya target per-agent sendiri; memanggil bentuk defaultnya saja
// hanya mendaftarkan Claude Code, sehingga user yang memilih Codex atau
// OpenClaw menjalankan agent tanpa satu pun alat aktif.
type wiringAgent struct {
	// rtkArgs kosong = rtk belum punya target untuk agent ini.
	rtkArgs []string
	// graphifyPlatform kosong = graphify belum punya platform untuk agent ini.
	graphifyPlatform string
}

// wiringAlatAgent disusun dari `rtk init --help` dan `graphify install --help`
// pada versi yang dipasang panel, lalu tiap kombinasi diuji non-interaktif
// dengan HOME terpisah. Yang dicatat di sini hanya yang benar-benar keluar
// dengan status 0 — bukan seluruh daftar di dokumentasi, karena panel hanya
// menawarkan lima agent ini.
//
// --auto-patch dan --no-trust-filters wajib untuk target yang menambal
// settings.json: tanpa keduanya rtk bertanya ke terminal, dan daemon tidak
// punya siapa pun untuk menjawab. --no-trust-filters dipilih (bukan
// --trust-filters) supaya filter pihak ketiga tidak diaktifkan diam-diam.
var wiringAlatAgent = map[string]wiringAgent{
	"claude": {
		rtkArgs:          []string{"init", "-g", "--auto-patch", "--no-trust-filters"},
		graphifyPlatform: "claude",
	},
	"codex": {
		// --codex memakai AGENTS.md + RTK.md dan tidak menambal hook Claude,
		// jadi tidak ada prompt yang perlu dimatikan.
		rtkArgs:          []string{"init", "-g", "--codex"},
		graphifyPlatform: "codex",
	},
	"opencode": {
		rtkArgs:          []string{"init", "-g", "--opencode", "--auto-patch", "--no-trust-filters"},
		graphifyPlatform: "opencode",
	},
	"hermes": {
		rtkArgs:          []string{"init", "-g", "--agent", "hermes"},
		graphifyPlatform: "hermes",
	},
	"openclaw": {
		// rtk belum menyediakan target OpenClaw — daftar --agent miliknya
		// tidak memuatnya. graphify menyebut platform ini "claw".
		graphifyPlatform: "claw",
	},
}

// siapkanArahanAI menulis blok arahan ke berkas instruksi milik agent yang
// akan dijalankan, sebagai user itu sendiri (bukan root).
//
// Dipanggil tiap sesi AI Agent dibuka, bukan sekali saat instalasi: akun
// panel bisa dibuat setelah komponen dipasang, dan home user baru tidak
// pernah dilewati installer. Operasinya idempoten — kalau isinya sudah sama
// persis, tidak ada berkas yang disentuh.
func siapkanArahanAI(u *userInfo, perintah string) {
	daftar, ok := berkasArahanAgent[perintah]
	if !ok || u.Home == "" {
		return
	}
	for _, rel := range daftar {
		if err := tulisBlokArahan(filepath.Join(u.Home, rel), u.Home, u.UID, u.GID); err != nil {
			log.Printf("arahan AI: %s untuk %s: %v", rel, u.Name, err)
		}
	}
}

// siapkanToolingAgent mendaftarkan rtk & graphify ke agent yang akan
// dijalankan, sebagai user pemilik sesi.
//
// Harus per-user, bukan sekali saat instalasi: daemon helper berjalan sebagai
// root, jadi `rtk init -g` yang dipanggil installer hanya menambal
// /root/.claude/settings.json. Akun panel lain membuka agent dengan HOME
// miliknya sendiri dan tidak akan pernah melihat hook itu.
//
// Harus per-agent juga: bentuk default kedua alat hanya mendaftarkan Claude
// Code. User yang memilih Codex, OpenCode, Hermes, atau OpenClaw sebelumnya
// mendapat agent tanpa satu pun alat aktif meski komponennya "Terpasang".
//
// WAJIB dipanggil SETELAH siapkanArahanAI: `rtk init -g` menolak menulis
// kalau ~/.claude belum ada — ia tidak membuat direktorinya sendiri — dan
// direktori itulah yang baru saja dibuat saat menulis blok arahan.
func siapkanToolingAgent(u *userInfo, perintah string) {
	w, ok := wiringAlatAgent[perintah]
	if !ok || u.Home == "" {
		return
	}
	// Pendaftaran diulang hanya kalau binary alatnya berubah. Tanpa penanda,
	// tiap membuka sesi agent membayar dua proses eksternal untuk hasil yang
	// sama persis — dan itu jeda yang terasa sebelum terminal muncul.
	penanda := filepath.Join(u.Home, ".config", "linux-dashboard", "tooling-"+perintah)
	stempel := stempelAlatAI()
	if b, err := os.ReadFile(penanda); err == nil && strings.TrimSpace(string(b)) == stempel {
		return
	}

	if len(w.rtkArgs) > 0 {
		if err := jalankanSebagaiUser(u, "rtk", w.rtkArgs...); err != nil {
			log.Printf("tooling AI: rtk untuk %s (%s): %v", perintah, u.Name, err)
		}
	}
	if w.graphifyPlatform != "" {
		if err := jalankanSebagaiUser(u, "graphify", "install", "--platform", w.graphifyPlatform); err != nil {
			log.Printf("tooling AI: graphify untuk %s (%s): %v", perintah, u.Name, err)
		}
	}

	if err := tulisBerkasUser(penanda, stempel+"\n", u, 0o644); err != nil {
		log.Printf("tooling AI: tulis penanda %s: %v", penanda, err)
	}
}

// stempelAlatAI mengidentifikasi versi rtk & graphify lewat identitas
// berkasnya. Alat yang di-upgrade menghasilkan stempel berbeda, sehingga
// pendaftarannya otomatis diulang tanpa perlu memanggil `--version` (satu
// proses eksternal lagi) tiap sesi.
func stempelAlatAI() string {
	var b strings.Builder
	for _, nama := range []string{"rtk", "graphify"} {
		p, ok := lookBinarySistem(nama)
		if !ok {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d ", nama, fi.Size(), fi.ModTime().UnixNano())
	}
	return strings.TrimSpace(b.String())
}

// lookBinarySistem mencari binary HANYA di direktori sistem.
//
// lookBinary biasa memulai dari exec.LookPath, yang memakai PATH milik proses
// daemon. PATH itu bisa memuat /root/.local/bin — lokasi default installer
// rtk dan uv — dan berkas di sana memang ada untuk root tapi TIDAK bisa
// dieksekusi user panel mana pun, karena /root sendiri ber-mode 0700.
// Perintah yang dijalankan dengan identitas user lain harus diselesaikan ke
// lokasi yang benar-benar bisa mereka baca, kalau tidak fork/exec gagal
// dengan "permission denied" yang tidak menyebut sebabnya sama sekali.
func lookBinarySistem(nama string) (string, bool) {
	for _, dir := range []string{
		"/usr/local/bin", "/usr/bin", "/bin",
		"/usr/local/sbin", "/usr/sbin", "/sbin",
	} {
		p := filepath.Join(dir, nama)
		fi, err := os.Stat(p)
		if err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// tulisBerkasUser menulis berkas milik user panel, membuat direktorinya kalau
// perlu dan menyerahkan kepemilikannya.
//
// Mode hanya berlaku saat berkasnya BARU: berkas yang sudah ada mempertahankan
// mode-nya sendiri, supaya .env berisi kredensial asli tidak tiba-tiba
// dilonggarkan oleh panel.
func tulisBerkasUser(path, isi string, u *userInfo, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	serahkanRantai(dir, u.Home, u.UID, u.GID)
	if err := os.WriteFile(path, []byte(isi), mode); err != nil {
		return err
	}
	return os.Chown(path, u.UID, u.GID)
}

// jalankanSebagaiUser menjalankan satu perintah dengan identitas user panel.
//
// Batas waktu wajib: perintah ini dijalankan tepat sebelum PTY dibuka, jadi
// alat yang menggantung (menunggu jawaban prompt, atau jaringan yang mati)
// akan menahan terminal user dari terbuka sama sekali.
func jalankanSebagaiUser(u *userInfo, nama string, args ...string) error {
	jalur, ok := lookBinarySistem(nama)
	if !ok {
		return fmt.Errorf("%s belum terpasang system-wide", nama)
	}
	ctx, batal := context.WithTimeout(context.Background(), 60*time.Second)
	defer batal()

	cmd := exec.CommandContext(ctx, jalur, args...)
	cmd.Dir = u.Home
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:" + u.Home + "/.local/bin",
		"HOME=" + u.Home,
		"USER=" + u.Name,
		"LOGNAME=" + u.Name,
		"LC_ALL=C",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}
	var keluaran bytes.Buffer
	cmd.Stdout, cmd.Stderr = &keluaran, &keluaran
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, firstLine(keluaran.String()))
	}
	return nil
}

// tulisBlokArahan menyisipkan atau memperbarui blok terkelola di sebuah
// berkas markdown. Isi user di luar penanda dipertahankan utuh.
func tulisBlokArahan(path, home string, uid, gid int) error {
	blok := penandaMulai + "\n" + arahanAlatAI + penandaSelesai + "\n"

	lama, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	baru := gantiBlok(string(lama), blok)
	if baru == string(lama) {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Direktori config agent milik user, bukan root — kalau dibuat root dan
	// tidak di-chown, CLI agent gagal menulis state-nya sendiri di sesi
	// berikutnya dengan "permission denied" yang membingungkan. MkdirAll bisa
	// membuat lebih dari satu tingkat (.config/opencode), jadi seluruh rantai
	// sampai home ikut diperiksa, bukan hanya direktori terakhir.
	serahkanRantai(dir, home, uid, gid)
	if err := os.WriteFile(path, []byte(baru), 0o644); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

// serahkanRantai menyerahkan kepemilikan direktori dari dir naik sampai (tapi
// tidak termasuk) home. Direktori yang sudah bukan milik root dilewati —
// yang diperbaiki hanya yang baru saja dibuat daemon.
func serahkanRantai(dir, home string, uid, gid int) {
	for d := dir; strings.HasPrefix(d, home+string(filepath.Separator)); d = filepath.Dir(d) {
		if err := chownJikaBaru(d, uid, gid); err != nil {
			return
		}
	}
}

// gantiBlok mengembalikan isi berkas dengan blok terkelola yang sudah
// diperbarui. Blok baru ditempel di akhir kalau penanda belum ada.
func gantiBlok(isi, blok string) string {
	mulai := strings.Index(isi, penandaMulai)
	selesai := strings.Index(isi, penandaSelesai)
	if mulai >= 0 && selesai > mulai {
		akhir := selesai + len(penandaSelesai)
		// Ikut telan newline setelah penanda penutup supaya penggantian
		// berulang tidak menumpuk baris kosong.
		if akhir < len(isi) && isi[akhir] == '\n' {
			akhir++
		}
		return isi[:mulai] + blok + isi[akhir:]
	}
	if strings.TrimSpace(isi) == "" {
		return blok
	}
	if !strings.HasSuffix(isi, "\n") {
		isi += "\n"
	}
	return isi + "\n" + blok
}

// chownJikaBaru hanya mengubah pemilik direktori yang memang milik root —
// direktori yang sudah dipakai user tidak diutak-atik.
func chownJikaBaru(dir string, uid, gid int) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 {
		return nil
	}
	return os.Chown(dir, uid, gid)
}
