package helper

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Arkon self-hosted dipasang dengan meng-clone repo resminya lalu menjalankan
// `docker compose up -d --build`, bentuk yang didokumentasikan docs/SETUP.md.
//
// Bedanya dengan Supabase penting dan menentukan seluruh berkas ini: Supabase
// punya setup.sh resmi yang MEMBANGKITKAN kuncinya sendiri, Arkon tidak. Yang
// disediakan Arkon hanya .env.docker.example, dan setiap nilai di dalamnya
// adalah placeholder yang tidak boleh sampai hidup di mesin sungguhan —
// POSTGRES_PASSWORD=arkon_secret, SECRET_KEY=change-me-…, MINIO_SECRET_KEY=
// minioadmin123, dan DEFAULT_ADMIN_PASSWORD=admin123 yang bahkan diperingatkan
// Arkon sendiri saat start (app/main.py). Memasang apa adanya berarti
// menyalakan knowledge hub berisi dokumen internal dengan password yang
// tertulis di README publik.
//
// Karena itu panel membangkitkan sendiri keenam rahasianya lewat crypto/rand.
// Ini memang tanggung jawab yang sengaja DIHINDARI di supabase.go — tapi di
// sana yang dihindari adalah menyalin derivasi JWT berlapis milik Supabase,
// sesuatu yang berubah tiap rilis. Di sini yang dibangkitkan hanya password
// dan kunci acak biasa: tidak ada derivasi, tidak ada format yang bisa menua,
// dan alternatifnya bukan "skrip resmi yang lebih tahu" melainkan placeholder
// yang pasti salah.
const (
	dirArkon       = "/opt/arkon"
	proyekArkon    = dirArkon + "/arkon"
	composeArkon   = proyekArkon + "/docker-compose.yml"
	envArkon       = proyekArkon + "/.env.docker"
	contohEnvArkon = proyekArkon + "/.env.docker.example"
	repoArkon      = "https://github.com/nduckmink/arkon.git"

	// namaProyekArkon dipakai sebagai `-p` pada setiap perintah compose.
	//
	// WAJIB eksplisit, tidak boleh diturunkan dari nama direktori seperti
	// bawaan compose: uninstall memindahkan folder proyek ke bekas-<stempel>,
	// dan sesudah itu nama project ikut berubah jadi "bekas-20060102-150405".
	// Volume bernama yang sudah ada tetap memakai awalan "arkon_", jadi tanpa
	// -p perintah compose berikutnya berbicara tentang stack yang sama sekali
	// lain — dan `down -v` di jalur purge tidak akan menemukan satu pun volume
	// yang harus dihapusnya.
	namaProyekArkon = "arkon"

	// Port yang dipetakan compose ke host. API 5055 adalah satu-satunya yang
	// benar-benar wajib dari LAN: di sanalah endpoint /mcp berada, dan itu yang
	// dihubungi CLI agent. 3119 adalah portal admin Next.js.
	//
	// MinIO (9002/9003) sengaja TIDAK didaftarkan ke firewall. Isinya berkas
	// mentah yang sudah diunggah, dan Arkon menyajikannya lewat presigned URL
	// yang dibangkitkan API — membuka konsol objek storage ke seluruh LAN
	// adalah keputusan admin, bukan efek samping menekan Pasang.
	portAPIArkon = "5055"
	portWebArkon = "3119"

	// Batas tunggu `docker compose up -d --wait`. Jauh lebih longgar daripada
	// Supabase (600 detik) karena alasan yang nyata: Arkon tidak punya image
	// siap pakai di registry mana pun, jadi `--build` di sini benar-benar
	// mengompilasi backend Python DAN frontend Next.js di mesin ini. Di server
	// 2 core yang jadi target panel, build pertama memakan puluhan menit.
	tungguArkon = "2700"
)

// catatanArkon ditampilkan di kartu komponen selama Arkon terpasang.
//
// Alasannya sama dengan catatanSupabase: akun admin pertama dibangkitkan saat
// pemasangan dan tidak pernah diketik user, jadi panel adalah satu-satunya
// pihak yang tahu di mana nilainya. Nilainya sendiri TIDAK dicetak — kartu
// komponen terbaca sekali pandang oleh siapa pun yang melihat layar, dan
// halaman ini yang paling sering ikut terpotret saat user melaporkan masalah.
const catatanArkon = "Akun admin pertama dibangkitkan saat pemasangan. Email dan passwordnya ada di " +
	"DEFAULT_ADMIN_EMAIL dan DEFAULT_ADMIN_PASSWORD pada berkas .env.docker stack — buka lewat " +
	"System → Docker → arkon → tombol .env (di disk: /opt/arkon/arkon/.env.docker). " +
	"Portal admin ada di port " + portWebArkon + ", endpoint MCP di port " + portAPIArkon + "/mcp."

// arkonTerpasang: berkas compose ada = stack-nya sudah di-clone ke mesin ini.
func arkonTerpasang() bool {
	_, err := os.Stat(composeArkon)
	return err == nil
}

// versiArkon membaca identitas commit yang sedang ter-checkout.
//
// Arkon tidak menulis berkas stempel versi seperti .supabase-version milik
// Supabase, dan tidak punya tag rilis yang dipakai installer-nya. Yang
// benar-benar menentukan versi deployment ini adalah commit yang di-clone,
// jadi itu yang dibaca — lewat git, bukan dengan menebak dari nama folder.
//
// safe.directory WAJIB: folder proyek diserahkan ke user panel saat pemasangan
// (milikiProyekArkon), sementara pembacaan ini dijalankan daemon sebagai root.
// git ≥ 2.35.2 menolak membuka repo milik user lain — termasuk untuk root —
// dengan "detected dubious ownership", dan tanpa ini kartu komponen diam-diam
// tidak pernah menampilkan versi.
func versiArkon() string {
	res, err := runIn(proyekArkon, nil, "git", "-c", "safe.directory="+proyekArkon,
		"describe", "--tags", "--always", "--dirty")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// rahasiaAcak membangkitkan satu nilai acak yang aman dipakai sebagai password
// atau kunci.
//
// base64 URL-safe, bukan hex dan bukan base64 biasa: nilainya ikut masuk ke
// DATABASE_URL sebagai bagian userinfo, dan alfabet URL-safe ("-" dan "_"
// menggantikan "+" dan "/") adalah satu-satunya yang tidak perlu di-escape di
// sana. Padding dibuang karena "=" pun punya arti tersendiri di berkas .env.
func rahasiaAcak(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// installArkon meng-clone repo resmi lalu menyalakan stack-nya.
func installArkon(u *userInfo) error {
	// Sisa pemasangan lama. uninstall tanpa hapus data memindahkannya ke
	// bekas-*, jadi folder yang masih ada di sini artinya ada yang menaruhnya
	// dengan tangan — dan `git clone` ke direktori tidak kosong gagal dengan
	// pesan yang tidak menyebut apa yang harus dilakukan user.
	if _, err := os.Stat(proyekArkon); err == nil {
		return errInvalid("folder %s sudah ada tapi bukan hasil pemasangan panel — "+
			"pindahkan atau hapus foldernya dulu, lalu tekan Pasang lagi", proyekArkon)
	}

	// Docker dipasang lewat jalur panel, bukan diserahkan ke user, dengan
	// alasan yang sama seperti Supabase: tanpa keanggotaan grup docker,
	// halaman System → Docker menjawab "permission denied" untuk stack yang
	// baru saja dipasang panel.
	if _, ada := lookBinary("docker"); !ada {
		tahapBaru("memasang Docker, prasyarat Arkon")
		if err := installDocker(); err != nil {
			return err
		}
	}
	if u != nil {
		tambahkanKeGrupDocker(u.Name)
	}

	// git bukan bagian dari image server minimal (LXC, cloud-init tanpa paket
	// tambahan), dan tanpa pemeriksaan ini kegagalannya muncul sebagai
	// "git: command not found" — kalimat yang tidak menyebut bahwa panel
	// sendiri bisa memasangnya dalam satu langkah.
	if _, ada := lookBinary("git"); !ada {
		tahapBaru("memasang git, prasyarat Arkon")
		if err := aptInstall("git"); err != nil {
			return errInvalid("git belum terpasang dan panel gagal memasangnya: %v", err)
		}
	}

	if err := os.MkdirAll(dirArkon, 0o755); err != nil {
		return err
	}

	tahapBaru("mengunduh sumber Arkon")
	// --depth 1: panel memasang versi terbaru, bukan menyediakan riwayat untuk
	// dikembangkan di mesin ini. Riwayat penuh repo ini besar dan tidak dipakai
	// satu pun jalur di panel.
	if err := jalankanKeProgres(dirArkon, "git", "clone", "--depth", "1", repoArkon, proyekArkon); err != nil {
		return errInvalid("clone repo Arkon gagal: %v", err)
	}

	tahapBaru("membangkitkan rahasia Arkon")
	if err := siapkanEnvArkon(); err != nil {
		return errInvalid("menyiapkan .env.docker Arkon: %v", err)
	}

	// Kepemilikan diserahkan SEBELUM stack dinyalakan, selagi belum ada berkas
	// yang ditulis container — sama seperti Supabase. Bedanya di sini seluruh
	// isi folder boleh diserahkan: data Arkon ada di VOLUME BERNAMA
	// (postgres_data, redis_data, minio_data), bukan bind mount di dalam
	// folder proyek, jadi tidak ada direktori milik proses container yang bisa
	// rusak karena berpindah pemilik.
	if err := milikiProyekArkon(u); err != nil {
		log.Printf("arkon: kepemilikan proyek: %v", err)
	}

	tahapBaru("membangun & menjalankan stack Arkon (build pertama bisa puluhan menit)")
	return jalankanKeProgres(proyekArkon, "docker", "compose", "-p", namaProyekArkon,
		"--env-file", ".env.docker", "up", "-d", "--build", "--wait", "--wait-timeout", tungguArkon)
}

// rahasiaArkon mengumpulkan seluruh nilai acak untuk satu pemasangan.
//
// Dibangkitkan sekaligus di muka, bukan satu per satu sambil menulis:
// kegagalan crypto/rand di tengah jalan tidak boleh meninggalkan .env yang
// sebagian sudah acak dan sebagian masih placeholder — bentuk kegagalan paling
// berbahaya di berkas ini, karena hasilnya terlihat sudah diurus.
type rahasiaArkon struct {
	Postgres   string
	SecretKey  string
	Pepper     string
	Admin      string
	MinioAkses string
	MinioSandi string
}

func bangkitkanRahasiaArkon() (rahasiaArkon, error) {
	var r rahasiaArkon
	for _, bidang := range []struct {
		tujuan *string
		nByte  int
	}{
		{&r.Postgres, 24},
		{&r.SecretKey, 32},
		{&r.Pepper, 32},
		{&r.Admin, 18},
		{&r.MinioAkses, 12},
		{&r.MinioSandi, 24},
	} {
		nilai, err := rahasiaAcak(bidang.nByte)
		if err != nil {
			return rahasiaArkon{}, err
		}
		*bidang.tujuan = nilai
	}
	return r, nil
}

// gantiRahasiaArkon mengganti setiap placeholder .env.docker.example dengan
// nilai sungguhan, dan mengarahkan URL publiknya ke ip (kosong = biarkan).
//
// Fungsi murni, dipisah dari siapkanEnvArkon dengan alasan yang sama seperti
// entriKonfigSupabase: ini keputusan yang kalau salah membuat knowledge hub
// berisi dokumen internal hidup dengan password yang tertulis di README publik,
// dan ia harus bisa diuji tanpa root, tanpa Docker, dan tanpa Arkon terpasang.
func gantiRahasiaArkon(isi string, r rahasiaArkon, ip string) string {
	pengguna := firstNonEmpty(ambilVarEnv(isi, "POSTGRES_USER"), "arkon")
	basisData := firstNonEmpty(ambilVarEnv(isi, "POSTGRES_DB"), "arkon")

	ganti := map[string]string{
		"POSTGRES_PASSWORD": r.Postgres,
		// DATABASE_URL memuat password yang sama dan HARUS ikut diganti.
		// Melewatkannya adalah kegagalan yang paling mudah terjadi di sini:
		// Postgres lahir dengan password baru, API tetap menghubunginya dengan
		// "arkon_secret", dan yang terlihat user cuma container api yang
		// restart terus tanpa pernah menyebut password sebagai sebabnya.
		"DATABASE_URL":           "postgresql+asyncpg://" + pengguna + ":" + r.Postgres + "@postgres:5432/" + basisData,
		"SECRET_KEY":             r.SecretKey,
		"MCP_TOKEN_PEPPER":       r.Pepper,
		"DEFAULT_ADMIN_PASSWORD": r.Admin,
		"MINIO_ACCESS_KEY":       r.MinioAkses,
		"MINIO_SECRET_KEY":       r.MinioSandi,
	}

	// URL publik diarahkan ke IP LAN dengan alasan yang sama persis seperti
	// Supabase: nilai bawaannya localhost, dan itu yang dipakai BROWSER user
	// untuk memanggil API — dibuka dari laptop mana pun, halamannya muncul lalu
	// setiap permintaannya jatuh ke localhost laptop itu sendiri.
	//
	// NEXT_PUBLIC_API_URL punya jebakan tambahan yang disebut docs/SETUP.md: ia
	// variabel BUILD-TIME milik Next.js, jadi harus sudah benar SEBELUM
	// `--build` berjalan. Mengubahnya setelah stack hidup tidak berpengaruh apa
	// pun sampai frontend-nya dibangun ulang.
	if ip != "" {
		ganti["NEXT_PUBLIC_API_URL"] = "http://" + ip + ":" + portAPIArkon
		ganti["MINIO_PUBLIC_ENDPOINT"] = ip + ":9002"
		// CORS_ORIGINS bawaannya "*". Dibiarkan begitu, API Arkon menerima
		// permintaan berkredensial dari halaman web mana pun yang kebetulan
		// dibuka user di jaringan yang sama. Dipersempit ke dua origin yang
		// memang dipakai: portal admin dan API-nya sendiri.
		ganti["CORS_ORIGINS"] = "http://" + ip + ":" + portWebArkon + ",http://" + ip + ":" + portAPIArkon
	}

	for kunci, nilai := range ganti {
		isi = gantiVarEnv(isi, kunci, nilai)
	}
	return isi
}

// ambilVarEnv membaca nilai variabel, mengembalikan "" kalau tidak ada —
// pembungkus nilaiVarEnv untuk pemanggil yang tidak peduli bedanya "tidak ada"
// dan "ada tapi kosong".
func ambilVarEnv(isi, nama string) string {
	v, _ := nilaiVarEnv(isi, nama)
	return v
}

// siapkanEnvArkon menyalin .env.docker.example jadi .env.docker dengan seluruh
// placeholder-nya diganti nilai sungguhan.
func siapkanEnvArkon() error {
	b, err := os.ReadFile(contohEnvArkon)
	if err != nil {
		return err
	}
	r, err := bangkitkanRahasiaArkon()
	if err != nil {
		return err
	}
	isi := gantiRahasiaArkon(string(b), r, ipLokal())

	// 0600, bukan 0644 seperti .env Supabase: berkas ini memuat password admin
	// Arkon DAN MCP_TOKEN_PEPPER — pepper yang dipakai memverifikasi setiap
	// token MCP. Siapa pun yang bisa membacanya bisa masuk sebagai admin
	// knowledge hub, jadi ia tidak boleh terbaca user lain di mesin yang sama.
	return os.WriteFile(envArkon, []byte(isi), 0o600)
}

// milikiProyekArkon menyerahkan folder proyek ke user panel.
//
// Alasannya sama dengan milikiKonfigSupabase: clone dan penulisan .env
// dikerjakan helper daemon sebagai root, sementara panel menulis berkas sebagai
// USER yang login — sehingga menyimpan .env.docker dari halaman System → Docker
// berakhir "permission denied" untuk berkas yang justru dibuat panel sendiri.
//
// Di sini seluruh isi folder ikut diserahkan tanpa pengecualian: Arkon menyimpan
// datanya di volume bernama, bukan bind mount, jadi tidak ada direktori milik
// proses container di dalam folder ini yang bisa rusak.
func milikiProyekArkon(u *userInfo) error {
	if u == nil {
		return nil
	}
	return filepathWalkChown(proyekArkon, u.UID, u.GID)
}

// uninstallArkon menghentikan stack lalu MEMINDAHKAN folder proyeknya.
//
// Volume bernama sengaja TIDAK ikut dihapus (`down` tanpa -v): seluruh isi
// knowledge hub — dokumen yang diunggah, wiki hasil kompilasi, akun, dan audit
// log — ada di sana. Halaman Components menjanjikan data tetap disimpan kecuali
// user mencentang "hapus data", dan menghapus volume di jalur uninstall biasa
// akan membuang semuanya tanpa pernah menanyakannya.
func uninstallArkon() error {
	if arkonTerpasang() {
		if _, err := runIn(proyekArkon, nil, "docker", "compose", "-p", namaProyekArkon,
			"--env-file", ".env.docker", "down", "--remove-orphans"); err != nil {
			// Container yang gagal berhenti tidak boleh menahan uninstall:
			// foldernya tetap dipindahkan, dan sisa container terlihat di
			// halaman System → Docker.
			log.Printf("arkon: `compose down` gagal: %v", err)
		}
	}
	if _, err := os.Stat(proyekArkon); err != nil {
		return nil
	}
	bekas := filepath.Join(dirArkon, "bekas-"+time.Now().Format("20060102-150405"))
	if err := os.Rename(proyekArkon, bekas); err != nil {
		return err
	}
	log.Printf("arkon: data lama disimpan di %s — hapus sendiri kalau tidak dipakai lagi", bekas)
	return nil
}

// purgeArkon membuang seluruh jejak, termasuk volume bernama dan folder bekas-*
// yang ditinggalkan uninstall sebelumnya.
func purgeArkon() error {
	// Volume dihapus lewat nama project, bukan dengan menebak nama volumenya
	// satu per satu: `down -v` tahu persis volume mana milik stack ini. Folder
	// proyeknya mungkin sudah dipindahkan uninstall, jadi kegagalan di sini
	// wajar dan tidak menghentikan pembersihan direktori.
	if arkonTerpasang() {
		if _, err := runIn(proyekArkon, nil, "docker", "compose", "-p", namaProyekArkon,
			"--env-file", ".env.docker", "down", "-v", "--remove-orphans"); err != nil {
			log.Printf("arkon: `compose down -v` gagal: %v", err)
		}
	}
	// Volume yang tertinggal dari folder proyek yang sudah dipindahkan tidak
	// terjangkau `down -v` mana pun lagi; dihapus best-effort lewat namanya,
	// yang stabil karena -p namaProyekArkon dipakai sejak pemasangan.
	_, _ = run("docker", "volume", "rm",
		namaProyekArkon+"_postgres_data",
		namaProyekArkon+"_redis_data",
		namaProyekArkon+"_minio_data")
	// Cache token tier ikut dibuang, dan ini WAJIB — bukan sekadar kerapian.
	// Token di dalamnya adalah identitas employee di database yang baru saja
	// dihapus bersama volumenya. Dibiarkan, pemasangan berikutnya lahir dengan
	// database kosong sementara cache masih menyodorkan token lama yang
	// bentuknya sah: setiap sesi agent menerima token mati, tidak ada jalur
	// yang menyembuhkannya sendiri, dan tidak satu pun pesan menyebut cache
	// sebagai sebabnya. Sengaja hanya di purge, bukan uninstall: uninstall
	// menyimpan volume database, jadi token di cache tetap berlaku.
	_ = os.RemoveAll(dirTokenArkon)
	return os.RemoveAll(dirArkon)
}
