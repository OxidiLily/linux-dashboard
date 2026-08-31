package helper

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ---- Agent AI diarahkan ke 9router --------------------------------------
//
// Sesi pertama tiap agent di mesin yang belum dikonfigurasi berhenti menunggu
// user memilih provider: Hermes menyapa dengan "No inference provider is
// configured yet", OpenClaw dengan "no models available". Di panel ini
// jawabannya selalu sama — 9router yang sudah berjalan di mesin yang sama
// sebagai gateway OpenAI-compatible — jadi menyerahkan pilihan itu ke user
// berarti memintanya memutuskan sesuatu yang panel sudah tahu jawabannya, dan
// yang tidak semua user tahu 9router itu apa.
//
// Yang ditulis hanya keadaan AWAL. Config yang sudah menyebut provider tidak
// pernah disentuh: user yang sengaja pindah ke Anthropic, OpenAI, atau model
// lokal lain tidak boleh ditarik kembali diam-diam tiap membuka sesi.

const (
	base9Router  = "http://127.0.0.1:20128/v1"
	model9Router = "oc/mimo-v2.5-free"
	// idProvider9Router dipakai OpenClaw sebagai awalan nama model
	// ("9router/oc/mimo-v2.5-free"). Tanpa disebut eksplisit, OpenClaw
	// menurunkannya sendiri dari URL jadi "custom-127-0-0-1-20128" — benar
	// secara teknis, tapi tidak ada yang mengenalinya saat membaca config.
	idProvider9Router = "9router"
	// kunciPlaceholderLama adalah nilai yang ditulis rilis panel sebelumnya
	// dengan asumsi gateway lokal tidak memeriksa Authorization. Asumsi itu
	// salah: /v1/models memang menjawab tanpa header, tapi
	// /v1/chat/completions membalas 401 Invalid API key. Nilai ini dikenali
	// supaya mesin yang terlanjur menerimanya ikut diperbaiki.
	kunciPlaceholderLama = "9router"
)

// siapkanProvider9Router mengonfigurasi agent yang akan dijalankan supaya
// memakai 9router, sebagai user pemilik sesi.
//
// Per-user, bukan sekali saat instalasi, dengan alasan yang sama seperti
// siapkanToolingAgent: daemon berjalan sebagai root, jadi config yang ditulis
// saat memasang komponen hanya mendarat di /root dan tidak pernah terlihat
// oleh akun panel lain.
func siapkanProvider9Router(u *userInfo, perintah string) {
	if u.Home == "" {
		return
	}
	if perintah != "hermes" && perintah != "openclaw" {
		return
	}
	// Tanpa kunci, jangan konfigurasi apa pun: agent yang diarahkan ke
	// gateway lalu ditolak 401 lebih membingungkan daripada agent yang
	// menanyakan providernya sendiri. Kunci kosong juga berarti 9router
	// belum pernah hidup di mesin ini.
	kunci := kunciAPI9Router()
	if kunci == "" {
		return
	}
	switch perintah {
	case "hermes":
		siapkanConfigHermes(u, kunci)
	case "openclaw":
		siapkanConfigOpenClaw(u, kunci)
	}
}

// ---- Hermes --------------------------------------------------------------

// blokModelHermes memakai ${OPENAI_API_KEY} alih-alih menempelkan nilainya:
// config.yaml adalah berkas yang wajar disalin antar mesin, .env di sebelahnya
// tidak. Hermes memuat ~/.hermes/.env sebelum membaca env proses (lihat
// load_hermes_dotenv di cli.py), jadi kunci di sana pasti terbaca.
var blokModelHermes = strings.Join([]string{
	"model:",
	"  default: " + model9Router,
	"  provider: custom",
	"  base_url: " + base9Router,
	"  api_key: ${OPENAI_API_KEY}",
	"",
}, "\n")

func siapkanConfigHermes(u *userInfo, kunci string) {
	dir := filepath.Join(u.Home, ".hermes")

	cfg := filepath.Join(dir, "config.yaml")
	isi, _ := os.ReadFile(cfg)
	if !punyaKunciYAML(string(isi), "model") {
		// Blok ditaruh di DEPAN, bukan di belakang: config yang sudah ada
		// bisa berakhir di tengah blok bersarang, dan menempel di sana akan
		// mengubah arti kunci yang sama sekali lain.
		if err := tulisBerkasUser(cfg, blokModelHermes+string(isi), u, 0o600); err != nil {
			log.Printf("config hermes: %s: %v", cfg, err)
		}
	}

	// Kunci hanya diurus kalau config-nya memang menunjuk gateway kita.
	// Config yang diarahkan user ke OpenAI atau Anthropic memakai
	// OPENAI_API_KEY untuk kunci yang sama sekali lain, dan menimpanya akan
	// merusak setelan yang sengaja dibuat.
	if isiAkhir, _ := os.ReadFile(cfg); !strings.Contains(string(isiAkhir), base9Router) {
		return
	}

	env := filepath.Join(dir, ".env")
	isiEnv, _ := os.ReadFile(env)
	if lama, ada := nilaiVarEnv(string(isiEnv), "OPENAI_API_KEY"); ada && lama == kunci {
		return
	}
	// Nilai apa pun selain kunci gateway saat ini adalah kunci basi: bisa
	// placeholder rilis lama, bisa kunci dari sebelum data 9router dihapus
	// dan gateway membuat "Default Key" baru. Dua-duanya menghasilkan 401
	// yang baru terlihat setelah user mengetik pesan pertamanya.
	if err := tulisBerkasUser(env, gantiVarEnv(string(isiEnv), "OPENAI_API_KEY", kunci), u, 0o600); err != nil {
		log.Printf("config hermes: %s: %v", env, err)
	}
}

// ---- OpenClaw ------------------------------------------------------------

// siapkanConfigOpenClaw memakai perintah onboarding milik OpenClaw sendiri,
// bukan menulis openclaw.json dengan tangan. Selain provider, onboarding itu
// juga menyiapkan workspace, direktori sesi, dan konfigurasi gateway — semua
// yang dibutuhkan agar TUI-nya langsung bisa dipakai, dan semua yang akan
// ketinggalan kalau panel hanya menempelkan satu blok provider.
//
// --skip-health dipakai karena gateway OpenClaw belum tentu hidup saat sesi
// dibuka, dan onboarding yang menunggunya akan gagal di mesin yang sebenarnya
// sehat. Gateway dijalankan OpenClaw sendiri saat TUI-nya start.
//
// ponytail: batas 60 detik milik jalankanSebagaiUser jadi atap di sini, dan
// ini berjalan sebelum PTY dibuka. Kalau suatu saat onboarding OpenClaw
// melambat, pindahkan pemanggilannya ke setelah PTY hidup seperti
// perbaikiAgent, supaya user melihat prosesnya alih-alih terminal kosong.
func siapkanConfigOpenClaw(u *userInfo, kunci string) {
	if sudahPunyaProviderOpenClaw(filepath.Join(u.Home, ".openclaw", "openclaw.json"), kunci) {
		return
	}
	err := jalankanSebagaiUser(u, "openclaw", "onboard",
		"--non-interactive", "--accept-risk", "--skip-health",
		"--auth-choice", "custom-api-key",
		"--custom-provider-id", idProvider9Router,
		"--custom-base-url", base9Router,
		"--custom-api-key", kunci,
		"--custom-model-id", model9Router,
		"--custom-compatibility", "openai",
	)
	if err != nil {
		log.Printf("config openclaw: onboarding 9router untuk %s: %v", u.Name, err)
	}
}

// sudahPunyaProviderOpenClaw membaca config OpenClaw sebagai JSON, bukan
// mencari potongan teks: nama "9router" bisa muncul di mana saja dalam berkas
// itu — komentar, nama model, URL — dan yang menentukan hanyalah apakah ia
// terdaftar sebagai provider.
func sudahPunyaProviderOpenClaw(path, kunci string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cfg struct {
		Models struct {
			Providers map[string]struct {
				BaseURL string `json:"baseUrl"`
				APIKey  string `json:"apiKey"`
			} `json:"providers"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		// Config rusak atau format berubah: jangan timpa apa pun. Onboarding
		// yang menabrak config tak terbaca lebih merusak daripada agent yang
		// meminta konfigurasi sendiri.
		return true
	}
	p, ada := cfg.Models.Providers[idProvider9Router]
	if !ada {
		return false
	}
	// Provider terdaftar tapi kuncinya bukan kunci gateway saat ini = kunci
	// basi, biasanya sisa dari sebelum data 9router dihapus dan gateway
	// membuat "Default Key" baru. Onboarding diulang supaya tersambung lagi.
	// Provider yang menunjuk URL lain sengaja dibiarkan: itu bukan gateway
	// kita meski namanya sama.
	return p.BaseURL != base9Router || p.APIKey == kunci
}

// ---- kunci API 9router ---------------------------------------------------

// kunciAPI9Router membaca API key gateway langsung dari database 9router.
//
// 9router membuat "Default Key" sendiri saat pertama kali hidup, jadi key ini
// ada di setiap mesin yang 9router-nya pernah jalan — tidak ada langkah manual
// yang perlu dibebankan ke user. Dibaca, bukan ditebak.
//
// Dua lokasi dicoba karena HOME service-nya berbeda antar generasi unit: unit
// panel yang sekarang memakai /root, unit panel lama mengarahkannya ke
// StateDirectory.
func kunciAPI9Router() string {
	for _, home := range []string{"/root", "/var/lib/9router"} {
		db := filepath.Join(home, ".9router", "db", "data.sqlite")
		if _, err := os.Stat(db); err != nil {
			continue
		}
		kunci, err := bacaKunci9Router(db)
		if err != nil {
			log.Printf("9router: baca API key dari %s: %v", db, err)
			continue
		}
		if kunci != "" {
			return kunci
		}
	}
	return ""
}

func bacaKunci9Router(path string) (string, error) {
	// query_only, bukan mode=ro: database ini dipakai 9router dalam mode WAL,
	// dan koneksi read-only butuh berkas -shm yang bisa ditulis. query_only
	// menutup jalur tulis dari sisi kita tanpa menghalangi mekanisme WAL.
	dsn := fmt.Sprintf("file:%s?_pragma=query_only(1)&_pragma=busy_timeout(3000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()

	ctx, batal := context.WithTimeout(context.Background(), 5*time.Second)
	defer batal()

	var kunci string
	err = db.QueryRowContext(ctx,
		`SELECT key FROM apiKeys WHERE isActive = 1 ORDER BY createdAt DESC LIMIT 1`).Scan(&kunci)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return kunci, err
}

// ---- utilitas berkas config ---------------------------------------------

// punyaKunciYAML mencari kunci tingkat atas — yang tertulis di kolom pertama,
// tanpa indentasi. Kunci bernama sama di dalam blok bersarang tidak dihitung.
func punyaKunciYAML(isi, kunci string) bool {
	for _, baris := range strings.Split(isi, "\n") {
		if strings.HasPrefix(baris, kunci+":") {
			return true
		}
	}
	return false
}

// nilaiVarEnv membaca deklarasi variabel dari berkas .env, termasuk yang
// diawali `export`. Baris yang dikomentari tidak dihitung sebagai deklarasi —
// justru itu bentuk paling umum dari "disiapkan tapi belum diisi".
func nilaiVarEnv(isi, nama string) (string, bool) {
	for _, baris := range strings.Split(isi, "\n") {
		b := strings.TrimSpace(baris)
		b = strings.TrimPrefix(b, "export ")
		if nilai, ok := strings.CutPrefix(b, nama+"="); ok {
			return strings.Trim(strings.TrimSpace(nilai), `"'`), true
		}
	}
	return "", false
}

// gantiVarEnv mengganti deklarasi pertama variabel, atau menambahkannya di
// akhir kalau belum ada. Baris lain dipertahankan apa adanya — berkas ini
// bisa berisi kredensial milik user yang tidak ada urusannya dengan panel.
func gantiVarEnv(isi, nama, nilai string) string {
	baris := strings.Split(isi, "\n")
	for i, b := range baris {
		t := strings.TrimSpace(b)
		t = strings.TrimPrefix(t, "export ")
		if strings.HasPrefix(t, nama+"=") {
			baris[i] = nama + "=" + nilai
			return strings.Join(baris, "\n")
		}
	}
	hasil := isi
	if hasil != "" && !strings.HasSuffix(hasil, "\n") {
		hasil += "\n"
	}
	return hasil + nama + "=" + nilai + "\n"
}
