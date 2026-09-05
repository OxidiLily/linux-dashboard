package helper

import (
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Supabase self-hosted dipasang lewat setup.sh resmi (https://supabase.link/setup.sh),
// bukan dengan menyusun docker-compose sendiri.
//
// Alasannya bukan kemalasan: skrip itu yang memegang seluruh urutan yang
// didokumentasikan di https://supabase.com/docs/guides/self-hosting/docker —
// sparse-clone folder docker/ dari tag rilis self-hosted, salin .env.example,
// lalu bangkitkan JWT_SECRET, ANON_KEY, SERVICE_ROLE_KEY, POSTGRES_PASSWORD,
// dan DASHBOARD_PASSWORD lewat utils/generate-keys.sh dan
// utils/add-new-auth-keys.sh. Menyalin sebagian langkah itu ke dalam panel
// berarti panel harus ikut menua bersama setiap perubahan rilis Supabase, dan
// yang paling gampang tertinggal justru pembangkitan kuncinya — satu-satunya
// bagian yang kalau salah membuat deployment terbuka untuk siapa pun.
//
// Yang dikerjakan panel di sekitarnya hanya tiga: memastikan Docker terpasang
// lewat jalur panel sendiri (supaya user pemakainya masuk grup docker),
// mengganti URL publik dari localhost ke IP LAN mesin ini, dan menyalakan
// stack-nya sekali supaya langsung terlihat di halaman System → Docker.
const (
	dirSupabase      = "/opt/supabase"
	proyekSupabase   = dirSupabase + "/supabase-project"
	composeSupabase  = proyekSupabase + "/docker-compose.yml"
	setupSupabaseURL = "https://supabase.link/setup.sh"

	// Port gateway (Kong/Envoy) — satu-satunya pintu masuk HTTP Supabase:
	// Studio, REST, Auth, Realtime, dan Storage semuanya lewat sini.
	// API_GW_HTTP_PORT di .env.example.
	portGatewaySupabase = "8000"

	// dirVolumeSupabase = satu-satunya folder yang tetap milik root: isinya
	// bind mount yang ditulis container. Lihat milikiKonfigSupabase.
	dirVolumeSupabase = "volumes"

	// Batas tunggu `docker compose up -d --wait`. WAJIB ada: tanpa
	// --wait-timeout, compose menunggu container sehat SELAMANYA, dan satu
	// service yang tidak pernah sehat menahan permintaan Pasang tanpa pernah
	// memberi user pesan berhasil maupun gagal — pola kegagalan yang sudah
	// pernah menggigit panel ini lewat `tailscale up`.
	tungguSupabase = "600"
)

// catatanSupabase ditampilkan di kartu komponen selama Supabase terpasang.
//
// Ada karena tanpa itu pemasangan lewat panel berakhir di jalan buntu: yang
// pertama menyambut user saat membuka Studio adalah kotak basic auth milik
// gateway, dan password yang diminta dibangkitkan setup.sh — tidak pernah
// diketik user, tidak pernah ditampilkan di mana pun. Panel yang memasangnya
// adalah satu-satunya pihak yang bisa memberi tahu di mana nilainya.
//
// Nilainya sendiri TIDAK dicetak di sini. Kartu komponen terbaca sekali
// pandang oleh siapa pun yang melihat layar — dan halaman ini yang paling
// sering ikut terpotret saat user melaporkan masalah. Yang disebut hanya nama
// kuncinya dan di mana berkasnya, satu klik dari penyunting .env stack yang
// memang sudah ada di halaman System → Docker.
//
// Sengaja kalimat tetap, bukan disusun dari isi .env: catatan yang memuat
// nilai berubah-ubah tidak punya kunci terjemahan, dan halaman ini dipakai
// dalam dua bahasa.
const catatanSupabase = "Login Studio memakai basic auth. Username dan passwordnya ada di " +
	"DASHBOARD_USERNAME dan DASHBOARD_PASSWORD pada berkas .env stack — buka lewat " +
	"System → Docker → supabase → tombol .env (di disk: /opt/supabase/supabase-project/.env)."

// supabaseTerpasang: berkas compose ada = stack-nya sudah dibuat di mesin ini.
// Bukan lewat PATH — Supabase bukan binary.
func supabaseTerpasang() bool {
	_, err := os.Stat(composeSupabase)
	return err == nil
}

// versiSupabase membaca stempel yang ditulis setup.sh (.supabase-version),
// berisi "ref=self-hosted/v0.7.0" atau SHA commit kalau dipasang dari HEAD.
// Berkas itu juga yang dipakai update.sh Supabase sebagai merge base, jadi
// isinya memang identitas versi deployment ini.
func versiSupabase() string {
	b, err := os.ReadFile(filepath.Join(proyekSupabase, ".supabase-version"))
	if err != nil {
		return ""
	}
	for _, baris := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(baris), "ref="); ok {
			return v
		}
	}
	return ""
}

// installSupabase menjalankan setup.sh resmi di /opt/supabase.
func installSupabase(u *userInfo) error {
	// Sisa pemasangan lama (uninstall tanpa hapus data memindahkannya ke
	// bekas-*, jadi ini hanya terjadi kalau ada yang menaruhnya dengan
	// tangan). setup.sh sendiri berhenti dengan "Target ... already exists",
	// tapi pesannya tidak menyebut apa yang harus dilakukan user.
	if _, err := os.Stat(proyekSupabase); err == nil {
		return errInvalid("folder %s sudah ada tapi bukan hasil pemasangan panel — "+
			"pindahkan atau hapus foldernya dulu, lalu tekan Pasang lagi", proyekSupabase)
	}

	// Docker dipasang lewat jalur panel, bukan dibiarkan ke setup.sh. Skrip
	// Supabase memang bisa memasang Docker sendiri, tapi ia tidak tahu apa-apa
	// soal user panel: tanpa keanggotaan grup docker, halaman System → Docker
	// menjawab "permission denied" untuk stack yang baru saja dipasang panel.
	if _, ada := lookBinary("docker"); !ada {
		tahapBaru("memasang Docker, prasyarat Supabase")
		if err := installDocker(); err != nil {
			return err
		}
	}
	if u != nil {
		tambahkanKeGrupDocker(u.Name)
	}

	script, bersihkan, err := unduhSkrip(setupSupabaseURL, "supabase-setup.sh")
	if err != nil {
		return err
	}
	defer bersihkan()
	if err := os.MkdirAll(dirSupabase, 0o755); err != nil {
		return err
	}

	// -y = non-interaktif, bentuk yang memang didokumentasikan skripnya
	// (`sh setup.sh -y`). Bukan pemotongan: tanpa TTY skrip ini sudah memilih
	// jalur non-interaktif sendiri, dan -y membuatnya eksplisit alih-alih
	// bergantung pada /dev/tty yang kebetulan tidak bisa dibuka daemon.
	tahapBaru("menjalankan setup.sh resmi Supabase")
	if err := jalankanKeProgres(dirSupabase, "/bin/sh", script, "-y"); err != nil {
		return errInvalid("setup.sh Supabase gagal: %v", err)
	}

	// URL publik bawaan .env.example adalah http://localhost:8000. Di server
	// itu berarti Studio hanya bekerja dari mesin itu sendiri: nilai ini yang
	// dipakai BROWSER untuk memanggil API, jadi dibuka dari laptop mana pun
	// halamannya muncul lalu setiap permintaannya jatuh ke localhost laptop.
	if ip := ipLokal(); ip != "" {
		if err := arahkanURLSupabase(filepath.Join(proyekSupabase, ".env"), ip); err != nil {
			log.Printf("supabase: URL publik tetap localhost: %v", err)
		}
	}

	// Konfigurasi diserahkan ke user panel SEBELUM stack dinyalakan, selagi
	// volumes/db/data belum dibuat Postgres — lihat milikiKonfigSupabase.
	if err := milikiKonfigSupabase(u); err != nil {
		log.Printf("supabase: kepemilikan konfigurasi: %v", err)
	}

	// run.sh adalah pembungkus resmi (`sh run.sh start` = `docker compose up
	// -d --wait`); dipakai apa adanya supaya override COMPOSE_FILE yang
	// dipilih user lewat `run.sh config add …` ikut terbaca.
	tahapBaru("menjalankan stack Supabase")
	return jalankanKeProgres(proyekSupabase, "/bin/sh", "run.sh", "start", "--wait-timeout", tungguSupabase)
}

// milikiKonfigSupabase menyerahkan berkas konfigurasi Supabase ke user panel.
//
// setup.sh dijalankan helper daemon, yaitu root, jadi SELURUH isi folder
// proyek lahir milik root. Panel sendiri menulis berkas sebagai USER yang
// login — worker-nya berjalan dengan kredensial user supaya kernel yang
// menegakkan izinnya — sehingga menyimpan `.env` dari halaman System → Docker
// (dan dari File Manager) berakhir "permission denied" untuk berkas yang
// justru dibuat panel itu sendiri. Itu bentuk kegagalan yang paling
// membingungkan: panel mengizinkan, lalu kernel menolak.
//
// `volumes/` sengaja TIDAK disentuh, dan itu bukan kehati-hatian berlebihan:
// isinya bind mount yang DITULIS container dengan UID masing-masing —
// volumes/db/data milik proses postgres di dalam image, volumes/storage milik
// storage-api. Menyerahkannya ke user panel membuat container gagal menulis,
// kerusakan yang jauh lebih mahal daripada .env yang tidak bisa disunting.
// Berkas konfigurasi di dalam volumes/ (envoy, vector, pooler, seed SQL)
// hanya dibaca container, tapi ikut ditinggalkan supaya aturannya tetap satu
// kalimat yang bisa diingat: folder volumes/ milik container, sisanya milik
// user.
//
// Direktori proyeknya sendiri ikut diserahkan supaya user bisa MENAMBAH
// berkas di sana — docker-compose.override.yml, misalnya — bukan cuma
// menyunting yang sudah ada.
func milikiKonfigSupabase(u *userInfo) error {
	if u == nil {
		return nil
	}
	jalur, err := entriKonfigSupabase(proyekSupabase)
	if err != nil {
		return err
	}
	// Direktori proyeknya sendiri tidak rekursif — kalau tidak, volumes/ ikut
	// terbawa lewat pintu belakang.
	if err := os.Lchown(proyekSupabase, u.UID, u.GID); err != nil {
		return err
	}
	for _, p := range jalur {
		if err := filepathWalkChown(p, u.UID, u.GID); err != nil {
			return err
		}
	}
	return nil
}

// entriKonfigSupabase mendaftar isi folder proyek yang boleh diserahkan ke
// user, yaitu semuanya KECUALI volumes/. Dipisah dari pemindahan kepemilikan
// supaya keputusannya — satu-satunya bagian yang bisa salah dan merusak
// database — bisa diuji tanpa root dan tanpa Supabase terpasang.
func entriKonfigSupabase(root string) ([]string, error) {
	ent, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ent {
		if e.Name() == dirVolumeSupabase {
			continue
		}
		out = append(out, filepath.Join(root, e.Name()))
	}
	return out, nil
}

// uninstallSupabase menghentikan stack lalu MEMINDAHKAN folder proyeknya,
// bukan menghapusnya.
//
// Seluruh data Supabase ada di dalam folder itu — Postgres di
// volumes/db/data, berkas Storage di volumes/storage, dan .env yang memuat
// JWT_SECRET beserta password database. Halaman Components menjanjikan data
// tetap disimpan kecuali user mencentang "hapus data", jadi menghapus folder
// ini di jalur uninstall biasa akan membuang database tanpa pernah
// menanyakannya. Dipindahkan, bukan ditinggal di tempat, supaya kartunya
// kembali ke "belum terpasang" dan pemasangan berikutnya tidak ditolak
// setup.sh karena target sudah ada.
func uninstallSupabase() error {
	if supabaseTerpasang() {
		if _, err := runIn(proyekSupabase, nil, "/bin/sh", "run.sh", "stop", "--remove-orphans"); err != nil {
			// Container yang gagal berhenti tidak boleh menahan uninstall:
			// foldernya tetap dipindahkan, dan sisa container terlihat di
			// halaman System → Docker.
			log.Printf("supabase: `run.sh stop` gagal: %v", err)
		}
	}
	if _, err := os.Stat(proyekSupabase); err != nil {
		return nil
	}
	bekas := filepath.Join(dirSupabase, "bekas-"+time.Now().Format("20060102-150405"))
	if err := os.Rename(proyekSupabase, bekas); err != nil {
		return err
	}
	log.Printf("supabase: data lama disimpan di %s — hapus sendiri kalau tidak dipakai lagi", bekas)
	return nil
}

// purgeSupabase membuang seluruh jejak, termasuk folder bekas-* yang
// ditinggalkan uninstall sebelumnya.
func purgeSupabase() error {
	// db-config dan deno-cache adalah satu-satunya volume BERNAMA di compose
	// Supabase (data sebenarnya semua bind mount di dalam folder proyek).
	// Dihapus best-effort supaya halaman Docker tidak menyisakan volume yatim;
	// kegagalannya tidak berarti apa-apa.
	//
	// Awalannya "supabase_", bukan nama folder: docker-compose.yml Supabase
	// menyetel `name: supabase` di tingkat atas, jadi nama project-nya tidak
	// diturunkan dari `supabase-project/`. Itu juga yang membuat stack ini
	// aman dikelola halaman System → Docker — slug nama stack ("supabase")
	// kebetulan sama persis dengan nama project yang sudah berjalan, jadi
	// tidak ada container yang berpindah nama sesudah sekali `down`.
	_, _ = run("docker", "volume", "rm", "supabase_db-config", "supabase_deno-cache")
	return os.RemoveAll(dirSupabase)
}

// arahkanURLSupabase menulis ulang URL publik di .env ke alamat yang benar-
// benar bisa dijangkau dari LAN.
func arahkanURLSupabase(path, ip string) error {
	lama, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	baru := gantiURLSupabase(string(lama), ip)
	if baru == string(lama) {
		return nil
	}
	// Mode berkas dipertahankan: .env memuat JWT_SECRET dan password
	// database, dan setup.sh sudah menulisnya dengan mode miliknya sendiri.
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(baru), fi.Mode().Perm())
}

// gantiURLSupabase mengganti dua baris URL di isi .env.
//
// Hanya dua, dan itu disengaja. SITE_URL (bawaan http://localhost:3000)
// menunjuk APLIKASI milik user, bukan Supabase — menebaknya dengan IP server
// justru salah. Selebihnya biar diubah admin lewat penyunting .env stack di
// halaman System → Docker, mis. saat Supabase ditaruh di balik domain.
func gantiURLSupabase(isi, ip string) string {
	dasar := "http://" + ip + ":" + portGatewaySupabase
	ganti := map[string]string{
		"SUPABASE_PUBLIC_URL": dasar,
		"API_EXTERNAL_URL":    dasar + "/auth/v1",
	}
	baris := strings.Split(isi, "\n")
	for i, b := range baris {
		for kunci, nilai := range ganti {
			// Prefix "KUNCI=" saja: baris komentar "# API_EXTERNAL_URL=…" di
			// .env.example tidak boleh ikut diubah jadi setelan aktif.
			if strings.HasPrefix(b, kunci+"=") {
				baris[i] = kunci + "=" + nilai
			}
		}
	}
	return strings.Join(baris, "\n")
}

// ipLokal mengembalikan IPv4 interface yang memegang default route.
//
// Sumbernya sama dengan subnetLokal di portkomponen.go — bukan hostname, yang
// di LXC dan WSL kerap tidak resolve dari device lain.
func ipLokal() string {
	ip, _, err := net.ParseCIDR(alamatLokalCIDR())
	if err != nil {
		return ""
	}
	return ip.String()
}

// jalankanKeProgres menjalankan perintah panjang di sebuah direktori sambil
// menyalurkan keluarannya ke bar progres halaman Components.
//
// Dipakai untuk skrip yang butuh CWD tertentu — skripDenganProgres tidak bisa
// dipakai di sini karena ia tidak menyetel Dir, dan setup.sh Supabase membuat
// folder proyeknya di dalam direktori kerja.
func jalankanKeProgres(dir, nama string, args ...string) error {
	cmd := exec.Command(nama, args...)
	cmd.Dir = dir
	cmd.Env = envInstall()
	pipa, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	ekor := bacaKeProgres(pipa)
	if err := cmd.Wait(); err != nil {
		// Baris terakhir skrip menjelaskan sebabnya; error Go sendiri cuma
		// berbunyi "exit status 1".
		return errInvalid("%s", firstNonEmpty(ekor(), err.Error()))
	}
	return nil
}
