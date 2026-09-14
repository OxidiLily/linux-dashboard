package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---- Arkon disambungkan ke setiap AI Agent sebagai server MCP -------------
//
// Arkon menyajikan knowledge hub-nya lewat endpoint MCP di /mcp, dan tanpa
// pendaftaran ini komponen yang sudah terpasang tidak mengubah apa pun untuk
// agent: tiap user harus menyalin token dan menyunting config agent-nya sendiri
// — persis pekerjaan manual yang dihapus panel untuk 9router dan keempat alat
// wajib.
//
// Yang membedakannya dari 9router: 9router adalah gateway INFERENSI (satu base
// URL + satu kunci, sama untuk semua orang), sementara Arkon adalah sumber
// PENGETAHUAN yang setiap isinya disaring per identitas. Token MCP Arkon bukan
// kunci layanan — ia identitas seorang Employee, dan daftar tool serta dokumen
// yang terlihat diturunkan dari peran dan departemen employee itu
// (ScopedToolsMiddleware di app/mcp/middleware.py).
//
// Karena itu panel tidak boleh membagikan satu token ke semua orang. Yang
// dipakai di sini adalah pemetaan GRUP LINUX → identitas Arkon: user di grup
// sudo memakai identitas ber-peran knowledge_manager, selebihnya viewer. Grup
// yang sudah jadi dasar kewenangan di mesin ini dipakai kembali sebagai dasar
// kewenangan di Arkon, jadi tidak ada daftar hak akses kedua yang harus diurus
// admin secara terpisah.
//
// Yang TIDAK diberikan pemetaan ini: identitas per orang. Semua user bergrup
// sudo berbagi satu identitas Arkon, jadi audit log Arkon mencatat "Linux
// Dashboard (pengelola)" — bukan nama manusianya — untuk setiap tindakan yang
// datang lewat agent. Itu harga dari pendaftaran yang berjalan tanpa campur
// tangan user; identitas per orang menuntut alur OAuth di browser, yang tidak
// bisa diselesaikan daemon sebelum sesi terminal dibuka.

const (
	// Agent berjalan di mesin yang sama dengan Arkon, jadi loopback — bukan IP
	// LAN. Token MCP ikut di setiap permintaan, dan mengirimkannya lewat
	// jaringan tanpa TLS adalah hal yang tidak perlu dilakukan sama sekali
	// kalau tujuannya ada di localhost.
	basisAPIArkon = "http://127.0.0.1:" + portAPIArkon
	urlMCPArkon   = basisAPIArkon + "/mcp"

	// namaServerMCPArkon dipakai sebagai kunci entri di config tiap agent.
	namaServerMCPArkon = "arkon"

	// dirTokenArkon menyimpan token per tier. Milik root, 0700: isinya
	// kredensial yang memberi akses ke seluruh isi knowledge hub sesuai
	// tier-nya, dan tidak ada user biasa yang perlu membacanya dari sini —
	// mereka menerimanya lewat config agent masing-masing.
	dirTokenArkon = "/var/lib/linux-dashboard/arkon"

	// batasAPIArkon sengaja pendek. Seluruh pendaftaran ini berjalan SEBELUM
	// PTY dibuka (lihat handleTerminal), dan sesi pertama sebuah tier memanggil
	// API sampai empat kali berurutan — login, cari employee, buat employee,
	// terbitkan token. Dengan 30 detik per panggilan, API yang menerima koneksi
	// tapi lambat menjawab (jendela startup compose) menahan terminal sampai dua
	// menit tanpa satu pun tanda kehidupan di browser. API-nya loopback dan
	// jawaban normalnya milidetik; lima detik sudah sangat longgar, dan kasus
	// terburuk turun ke dua puluh detik. Arkon yang benar-benar mati ditolak
	// seketika (connection refused), tidak menunggu batas ini.
	batasAPIArkon = 5 * time.Second
)

// tierArkon adalah tingkat kewenangan yang dipetakan dari grup Linux.
//
// Hanya dua, dan itu disengaja. Arkon mengenal empat global_role (viewer,
// contributor, knowledge_manager, admin), tapi panel hanya punya satu pembeda
// kewenangan yang benar-benar berarti di mesin Linux: apakah user ini punya
// sudo atau tidak. Memetakan grup Linux sembarang ke empat peran Arkon berarti
// mengarang kebijakan yang tidak pernah diminta siapa pun — dan kebijakan
// karangan itu yang nanti harus ditebak admin saat hasilnya tidak sesuai
// harapan. Tier tambahan ditambahkan di sini kalau memang ada grup yang
// artinya jelas.
type tierArkon struct {
	nama       string // dipakai sebagai nama berkas cache & sufiks email
	globalRole string // global_role Arkon
	role       string // role sistem Arkon: admin | employee
}

// Tier tertinggi memakai global_role knowledge_manager, BUKAN admin, dan
// role sistem employee, bukan admin.
//
// Ini pembatasan yang disengaja. Yang dibutuhkan seorang agent dari Arkon
// adalah membaca dan mengkurasi pengetahuan; ia tidak pernah punya urusan
// membuat employee, mengubah peran orang lain, atau membaca audit log — dan
// itulah yang dibawa global_role "admin" (permission org:employees:manage di
// app/routers/rbac.py). Token tier ini tersalin ke config agent SETIAP user
// bergrup sudo, jadi tiap kewenangan tambahan di dalamnya ikut tersebar ke
// sebanyak itu berkas. Identitas admin Arkon yang sebenarnya tetap satu, milik
// panel, dan hanya dipakai untuk menerbitkan token — tidak pernah dibagikan.
var (
	tierPengelolaArkon = tierArkon{"pengelola", "knowledge_manager", "employee"}
	tierViewerArkon    = tierArkon{"viewer", "viewer", "employee"}
)

// grupAdminArkon adalah grup Linux yang diperlakukan sebagai admin Arkon.
//
// Ketiganya adalah nama grup sudo yang dipakai distribusi berbeda: "sudo" di
// Debian/Ubuntu, "wheel" di RHEL/Arch/SUSE, "admin" di Ubuntu lama. Panel ini
// menargetkan Debian/Ubuntu, tapi mencantumkan ketiganya lebih murah daripada
// salah menurunkan hak akses seorang admin di mesin yang kebetulan memakai
// konvensi lain.
var grupAdminArkon = map[string]bool{"sudo": true, "wheel": true, "admin": true}

// tierUntukUser memetakan keanggotaan grup seorang user ke tier Arkon.
//
// root selalu admin tanpa memeriksa grup: di banyak mesin single-user root
// tidak terdaftar di grup sudo sama sekali karena ia memang tidak
// membutuhkannya.
func tierUntukUser(u *userInfo) tierArkon {
	if u == nil {
		return tierViewerArkon
	}
	if u.UID == 0 {
		return tierPengelolaArkon
	}
	akun, err := user.Lookup(u.Name)
	if err != nil {
		// Tidak bisa dipastikan = tier terendah. Kegagalan pembacaan grup tidak
		// boleh berakhir dengan pemberian hak admin.
		log.Printf("arkon: grup %s tidak terbaca, memakai tier viewer: %v", u.Name, err)
		return tierViewerArkon
	}
	gids, err := akun.GroupIds()
	if err != nil {
		log.Printf("arkon: grup %s tidak terbaca, memakai tier viewer: %v", u.Name, err)
		return tierViewerArkon
	}
	for _, g := range gids {
		grp, err := user.LookupGroupId(g)
		if err != nil {
			continue
		}
		if grupAdminArkon[grp.Name] {
			return tierPengelolaArkon
		}
	}
	return tierViewerArkon
}

// siapkanArkonMCP mendaftarkan Arkon sebagai server MCP pada agent yang akan
// dijalankan, sebagai user pemilik sesi.
//
// Per-sesi, bukan sekali saat instalasi, dengan alasan yang sama seperti
// siapkanToolingAgent dan siapkanProvider9Router: daemon berjalan sebagai root,
// jadi config yang ditulis saat memasang komponen hanya mendarat di /root.
//
// Dipanggil ulang tiap sesi juga yang membuat perpindahan grup ikut terbawa:
// user yang baru dimasukkan ke grup sudo mendapat token admin pada sesi
// berikutnya tanpa ada yang perlu menekan tombol apa pun.
//
// ponytail: yang TIDAK diselesaikan mekanisme itu adalah pencabutan. User yang
// DIKELUARKAN dari grup sudo tetap memegang salinan token admin di config
// agent-nya, dan token itu masih sah sampai ada yang merotasinya dari portal
// Arkon. Jalan naiknya: bandingkan tier sesi dengan tier yang tercatat di
// berkas penanda, dan panggil POST /api/employees/{id}/token untuk merotasi
// token tier yang ditinggalkan begitu ada user yang turun tier.
func siapkanArkonMCP(u *userInfo, perintah string) {
	if u == nil || u.Home == "" {
		return
	}
	if _, ok := penulisMCPAgent[perintah]; !ok {
		return
	}
	// Arkon belum terpasang = tidak ada yang bisa didaftarkan. Bukan kesalahan:
	// komponennya memang opsional.
	if !arkonTerpasang() {
		return
	}
	tier := tierUntukUser(u)
	token, err := tokenTierArkon(tier)
	if err != nil {
		// Kegagalan di sini TIDAK menahan sesi agent: agent tanpa Arkon tetap
		// agent yang bisa dipakai, dan menahan terminal karena knowledge hub
		// belum siap adalah kerugian yang lebih besar daripada manfaatnya.
		log.Printf("arkon mcp: token tier %s untuk %s: %v", tier.nama, u.Name, err)
		return
	}
	if err := penulisMCPAgent[perintah](u, token); err != nil {
		log.Printf("arkon mcp: config %s untuk %s: %v", perintah, u.Name, err)
	}
}

// ---- token per tier ------------------------------------------------------

// berkasTierArkon menyimpan identitas Arkon milik satu tier.
type berkasTierArkon struct {
	EmployeeID string `json:"employee_id"`
	Token      string `json:"token"`
}

// kunciTokenArkon membuat penerbitan token berjalan satu per satu.
//
// WAJIB, bukan kehati-hatian berlebihan: dua sesi agent yang dibuka nyaris
// bersamaan pada mesin yang cache tokennya belum ada akan sama-sama memanggil
// POST /employees/{id}/token — dan panggilan kedua MEROTASI token, membatalkan
// yang baru saja ditulis panggilan pertama ke config agent sesi pertama.
// Hasilnya sesi yang kelihatan berhasil dikonfigurasi tapi setiap permintaan
// MCP-nya dijawab 401. Seluruh sesi melewati satu daemon helper, jadi mutex
// di dalam proses sudah cukup untuk menutupnya.
var kunciTokenArkon sync.Mutex

// tokenTierArkon mengembalikan token MCP milik sebuah tier, membuatnya lebih
// dulu kalau belum ada.
//
// Token disimpan panel karena Arkon memberikannya HANYA SEKALI: generate_token
// mengembalikan nilai mentahnya lalu menyimpan hash-nya saja (lihat
// app/services/mcp_auth_service.py), jadi tidak ada jalan membacanya kembali.
// Tanpa cache ini, tiap sesi agent akan merotasi token tier-nya dan
// membatalkan token yang sudah tertulis di config sesi-sesi sebelumnya.
//
// ponytail: cache yang ada dipercaya tanpa dicek ke Arkon. Token yang
// dirotasi atau dicabut admin dari portal Arkon tetap disodorkan ke setiap
// sesi baru sampai berkas cache-nya dihapus dengan tangan — pemeriksaan
// hidup-matinya butuh login admin + satu panggilan lagi tiap sesi, tepat
// sebelum PTY dibuka, dan itu jeda yang baru saja dipangkas. Jalan naiknya:
// tombol "Segarkan token Arkon" di kartu komponen yang menghapus
// dirTokenArkon, atau probe ringan ke /mcp dengan token cache yang memicu
// penerbitan ulang saat dijawab 401. Purge sudah membersihkannya sendiri.
func tokenTierArkon(t tierArkon) (string, error) {
	kunciTokenArkon.Lock()
	defer kunciTokenArkon.Unlock()

	berkas := filepath.Join(dirTokenArkon, "tier-"+t.nama+".json")
	if b, err := os.ReadFile(berkas); err == nil {
		var simpan berkasTierArkon
		if json.Unmarshal(b, &simpan) == nil && tokenArkonSah(simpan.Token) {
			return simpan.Token, nil
		}
	}

	jwt, err := masukAdminArkon()
	if err != nil {
		return "", err
	}
	id, err := pastikanEmployeeArkon(jwt, t)
	if err != nil {
		return "", err
	}
	token, err := terbitkanTokenArkon(jwt, id)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dirTokenArkon, 0o700); err != nil {
		return "", err
	}
	b, err := json.Marshal(berkasTierArkon{EmployeeID: id, Token: token})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(berkas, b, 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// emailTierArkon menyusun alamat identitas tier. Domain .invalid (RFC 2606)
// dipakai dengan sengaja: identitas ini milik mesin, bukan orang, dan tidak
// boleh ada yang mengira alamatnya bisa dikirimi surat.
func emailTierArkon(t tierArkon) string {
	return "linux-dashboard-" + t.nama + "@panel.invalid"
}

// masukAdminArkon login ke API Arkon memakai akun admin bawaan yang
// dibangkitkan panel saat memasang komponen.
//
// Kredensialnya dibaca dari .env stack, bukan disimpan panel di tempat kedua:
// berkas itu sudah jadi sumber kebenaran untuk stack-nya, sudah ber-mode 0600,
// dan satu kredensial yang disimpan di dua tempat adalah satu tempat yang bisa
// basi tanpa ketahuan.
func masukAdminArkon() (string, error) {
	b, err := os.ReadFile(envArkon)
	if err != nil {
		return "", fmt.Errorf("baca %s: %w", envArkon, err)
	}
	email, _ := nilaiVarEnv(string(b), "DEFAULT_ADMIN_EMAIL")
	sandi, _ := nilaiVarEnv(string(b), "DEFAULT_ADMIN_PASSWORD")
	if email == "" || sandi == "" {
		return "", fmt.Errorf("DEFAULT_ADMIN_EMAIL/PASSWORD tidak ada di %s", envArkon)
	}

	var jawab struct {
		AccessToken string `json:"access_token"`
	}
	if err := panggilAPIArkon(http.MethodPost, "/api/auth/login", "",
		map[string]string{"email": email, "password": sandi}, &jawab); err != nil {
		return "", err
	}
	if jawab.AccessToken == "" {
		return "", fmt.Errorf("login Arkon tidak mengembalikan access_token")
	}
	return jawab.AccessToken, nil
}

// pastikanEmployeeArkon mengembalikan id employee milik tier, membuatnya kalau
// belum ada.
//
// department_ids sengaja dibiarkan kosong. Employee tanpa departemen hanya
// melihat sumber ber-scope global (lihat komentar EmployeeCreate di
// app/routers/rbac.py), dan itu default yang benar: panel tidak tahu apa-apa
// tentang pembagian departemen di organisasi ini, dan menebaknya berarti
// membuka dokumen satu departemen untuk seluruh mesin. Admin yang ingin
// identitas tier ini melihat lebih banyak menambahkan departemennya sendiri
// dari portal Arkon.
func pastikanEmployeeArkon(jwt string, t tierArkon) (string, error) {
	email := emailTierArkon(t)

	var daftar struct {
		Items []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"items"`
	}
	if err := panggilAPIArkon(http.MethodGet,
		"/api/employees?search="+url.QueryEscape(email), jwt, nil, &daftar); err == nil {
		for _, e := range daftar.Items {
			// Perbandingan e-mail penuh, bukan hasil pencarian apa adanya:
			// parameter search cocok secara parsial, dan employee lain yang
			// alamatnya kebetulan memuat string ini tidak boleh diambil alih.
			if strings.EqualFold(e.Email, email) {
				return e.ID, nil
			}
		}
	}

	sandi, err := rahasiaAcak(24)
	if err != nil {
		return "", err
	}
	var dibuat struct {
		ID string `json:"id"`
	}
	// Password identitas ini dibangkitkan lalu dibuang: yang dipakai panel
	// hanya token MCP-nya, dan tidak ada seorang pun yang perlu login sebagai
	// identitas mesin ini. Kalau suatu saat perlu, admin merotasinya dari
	// portal Arkon.
	if err := panggilAPIArkon(http.MethodPost, "/api/employees", jwt, map[string]any{
		"name":           "Linux Dashboard (" + t.nama + ")",
		"email":          email,
		"password":       sandi,
		"role":           t.role,
		"global_role":    t.globalRole,
		"department_ids": []string{},
	}, &dibuat); err != nil {
		return "", err
	}
	if dibuat.ID == "" {
		return "", fmt.Errorf("Arkon tidak mengembalikan id employee")
	}
	return dibuat.ID, nil
}

func terbitkanTokenArkon(jwt, employeeID string) (string, error) {
	var jawab struct {
		Token string `json:"token"`
	}
	if err := panggilAPIArkon(http.MethodPost,
		"/api/employees/"+employeeID+"/token", jwt, nil, &jawab); err != nil {
		return "", err
	}
	if !tokenArkonSah(jawab.Token) {
		return "", fmt.Errorf("Arkon mengembalikan token MCP dengan bentuk yang tidak dikenal")
	}
	return jawab.Token, nil
}

// bentukTokenArkon adalah bentuk token yang diterbitkan Arkon:
// "ark_" + secrets.token_urlsafe(32) (app/services/mcp_auth_service.py).
var bentukTokenArkon = regexp.MustCompile(`^ark_[A-Za-z0-9_-]{20,}$`)

// tokenArkonSah memeriksa bentuk token SEBELUM ia disisipkan ke berkas config.
//
// Ini pemeriksaan di pintu masuk, bukan di tiap penulis: tiga penulis di bawah
// menyalin nilainya ke dalam string literal TOML, deklarasi .env, dan nilai
// JSON. Alfabet URL-safe memang tidak memuat tanda kutip atau baris baru —
// tapi itu jaminan dari kode Arkon hari ini, dan sebuah nilai yang datang
// lewat HTTP dari proses lain tidak boleh dipercaya begitu saja untuk
// disambung ke dalam berkas yang dibaca agent sebagai konfigurasi.
func tokenArkonSah(token string) bool {
	return bentukTokenArkon.MatchString(token)
}

// panggilAPIArkon adalah satu-satunya jalur HTTP ke Arkon di berkas ini.
func panggilAPIArkon(metode, jalur, jwt string, badan any, hasil any) error {
	var isi io.Reader
	if badan != nil {
		b, err := json.Marshal(badan)
		if err != nil {
			return err
		}
		isi = bytes.NewReader(b)
	}
	req, err := http.NewRequest(metode, basisAPIArkon+jalur, isi)
	if err != nil {
		return err
	}
	if badan != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	res, err := (&http.Client{Timeout: batasAPIArkon}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// Badan dibatasi: ini API lokal yang dipercaya, tapi membaca respons tanpa
	// batas ke dalam memori daemon adalah kebiasaan yang tidak perlu dimulai.
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s: %s", metode, jalur, res.Status, strings.TrimSpace(string(b)))
	}
	if hasil == nil {
		return nil
	}
	return json.Unmarshal(b, hasil)
}

// ---- penulisan config per agent ------------------------------------------

// penulisMCPAgent memetakan perintah agent → cara mendaftarkan server MCP.
//
// Kelimanya mendukung MCP lewat HTTP, tapi tidak satu pun memakai bentuk yang
// sama — dan tiga di antaranya punya jebakan yang membuat pendaftaran "berhasil"
// tanpa pernah benar-benar tersambung. Itu sebabnya tiap agent punya penulisnya
// sendiri alih-alih satu penulis dengan tabel nama kunci.
var penulisMCPAgent = map[string]func(*userInfo, string) error{
	"claude":   tulisMCPClaude,
	"codex":    tulisMCPCodex,
	"opencode": tulisMCPOpencode,
	"hermes":   tulisMCPHermes,
	"openclaw": tulisMCPOpenclaw,
}

// headerOtorisasiArkon menyusun nilai header yang dipakai semua agent.
func headerOtorisasiArkon(token string) string { return "Bearer " + token }

// tulisBerkasRahasiaUser adalah tulisBerkasUser untuk berkas yang sesudah
// ditulis memuat token MCP.
//
// tulisBerkasUser sengaja mempertahankan mode berkas yang sudah ada, supaya
// panel tidak pernah MELONGGARKAN berkas kredensial milik user. Di sini
// arahnya sebaliknya: ~/.claude.json lahir 0644 dari CLI-nya sendiri, dan
// sesudah token disisipkan ia harus jadi 0600 — memperketat tidak pernah
// merusak apa pun, dan tanpa ini token terbaca akun lain di mesin yang
// direktori home-nya bisa dilalui.
func tulisBerkasRahasiaUser(path, isi string, u *userInfo) error {
	if err := tulisBerkasUser(path, isi, u, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// tulisMCPClaude mendaftarkan Arkon di ~/.claude.json.
//
// Ditulis sebagai JSON langsung, bukan lewat `claude mcp add`, karena dua hal
// yang sama-sama penting: perintah itu menolak nama yang sudah terdaftar (jadi
// tidak idempoten, sementara fungsi ini jalan tiap sesi), dan ia butuh binary
// claude yang bisa dijalankan — padahal justru sesi yang rusak binernya adalah
// sesi yang paling butuh config-nya benar.
func tulisMCPClaude(u *userInfo, token string) error {
	return sisipkanJSONMCP(filepath.Join(u.Home, ".claude.json"), u,
		[]string{"mcpServers", namaServerMCPArkon}, map[string]any{
			"type":    "http",
			"url":     urlMCPArkon,
			"headers": map[string]any{"Authorization": headerOtorisasiArkon(token)},
		})
}

// tulisMCPOpencode mendaftarkan Arkon di ~/.config/opencode/opencode.json.
//
// "oauth": false WAJIB disebut. OpenCode mendeteksi dukungan OAuth dari server
// MCP-nya sendiri, dan Arkon memang mengumumkan metadata OAuth (RFC 8414) di
// endpoint yang sama — jadi tanpa penolakan eksplisit, OpenCode akan mencoba
// alur OAuth interaktif dan mengabaikan header statis yang baru saja ditulis
// di sini.
func tulisMCPOpencode(u *userInfo, token string) error {
	return sisipkanJSONMCP(filepath.Join(u.Home, ".config", "opencode", "opencode.json"), u,
		[]string{"mcp", namaServerMCPArkon}, map[string]any{
			"type":    "remote",
			"url":     urlMCPArkon,
			"enabled": true,
			"oauth":   false,
			"headers": map[string]any{"Authorization": headerOtorisasiArkon(token)},
		})
}

// tulisMCPOpenclaw mendaftarkan Arkon di ~/.openclaw/openclaw.json.
//
// transport "streamable-http" WAJIB disebut: bawaan OpenClaw adalah SSE, dan
// endpoint /mcp Arkon berbicara Streamable HTTP. Dibiarkan default, entri ini
// tampak terdaftar rapi di config lalu gagal menyambung setiap kali dipakai.
//
// Berkas ini juga yang memuat blok provider 9router (lihat
// sudahPunyaProviderOpenClaw), jadi penulisannya wajib menggabung — bukan
// menimpa.
func tulisMCPOpenclaw(u *userInfo, token string) error {
	return sisipkanJSONMCP(filepath.Join(u.Home, ".openclaw", "openclaw.json"), u,
		[]string{"mcp", "servers", namaServerMCPArkon}, map[string]any{
			"url":       urlMCPArkon,
			"transport": "streamable-http",
			"headers":   map[string]any{"Authorization": headerOtorisasiArkon(token)},
		})
}

// sisipkanJSONMCP menyisipkan satu entri ke dalam berkas config JSON milik
// user, mempertahankan seluruh isi lainnya.
//
// Membaca-menggabung-menulis, tidak pernah menimpa: ketiga berkas yang memakai
// fungsi ini memuat konfigurasi milik user yang tidak ada urusannya dengan
// panel — riwayat proyek di .claude.json, provider 9router di openclaw.json.
//
// Berkas yang tidak bisa diurai TIDAK ditimpa. Config rusak yang ditimpa panel
// berarti user kehilangan setelannya tanpa pernah diberi tahu; dilewati, ia
// hanya kehilangan satu entri MCP dan pesan errornya masuk log.
func sisipkanJSONMCP(path string, u *userInfo, kunci []string, nilai map[string]any) error {
	lama, _ := os.ReadFile(path)
	baru, berubah, err := gabungJSONMCP(lama, kunci, nilai)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !berubah {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return tulisBerkasRahasiaUser(path, string(baru), u)
}

// gabungJSONMCP menyisipkan nilai pada jalur kunci di dalam dokumen JSON,
// mengembalikan dokumen baru dan apakah isinya benar-benar berubah.
//
// Fungsi murni, dipisah dari penulisannya supaya penggabungan — satu-satunya
// bagian yang bisa menghapus konfigurasi milik user — bisa diuji tanpa HOME
// sungguhan dan tanpa hak root untuk chown.
func gabungJSONMCP(isi []byte, kunci []string, nilai map[string]any) ([]byte, bool, error) {
	cfg := map[string]any{}
	if len(bytes.TrimSpace(isi)) > 0 {
		if err := json.Unmarshal(isi, &cfg); err != nil {
			return nil, false, fmt.Errorf("tidak bisa diurai sebagai JSON, dilewati: %w", err)
		}
	}

	// Turun sampai induk entri, membuat map perantara yang belum ada.
	//
	// Kunci yang SUDAH ada tapi bukan objek tidak diambil alih: "mcp": false
	// di opencode.json adalah keputusan user, dan menggantinya diam-diam
	// dengan objek berisi Arkon membuang keputusan itu tanpa pernah
	// memberitahunya. Dihentikan dengan error yang masuk log, sama seperti
	// JSON yang tidak bisa diurai.
	induk := cfg
	for _, k := range kunci[:len(kunci)-1] {
		lama, ada := induk[k]
		anak, ok := lama.(map[string]any)
		if ada && !ok {
			return nil, false, fmt.Errorf("kunci %q sudah ada tapi bukan objek, dilewati", k)
		}
		if !ok {
			anak = map[string]any{}
			induk[k] = anak
		}
		induk = anak
	}
	daun := kunci[len(kunci)-1]

	// Tidak ada yang ditulis kalau isinya sudah sama persis: berkas ini dibaca
	// agent tiap start, dan menulis ulang tiap sesi hanya mengubah mtime tanpa
	// mengubah apa pun yang dipakai.
	if lama, ok := induk[daun]; ok {
		a, errA := json.Marshal(lama)
		b, errB := json.Marshal(nilai)
		if errA == nil && errB == nil && bytes.Equal(a, b) {
			return isi, false, nil
		}
	}
	induk[daun] = nilai

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(b, '\n'), true, nil
}

// blokMCPHermes menyusun blok YAML mcp_servers milik Hermes.
//
// Tokennya TIDAK ditempel di config.yaml melainkan dirujuk lewat
// ${ARKON_MCP_TOKEN}, mengikuti pola yang sudah dipakai blokModelHermes untuk
// OPENAI_API_KEY: config.yaml adalah berkas yang wajar disalin antar mesin,
// .env di sebelahnya tidak. Hermes memuat ~/.hermes/.env sebelum membaca env
// proses, jadi nilainya pasti terbaca.
func entriMCPHermes(indent string) []string {
	return []string{
		indent + namaServerMCPArkon + ":",
		indent + "  url: " + urlMCPArkon,
		indent + "  headers:",
		indent + `    Authorization: "Bearer ${ARKON_MCP_TOKEN}"`,
		indent + "  enabled: true",
	}
}

var blokMCPHermes = "mcp_servers:\n" + strings.Join(entriMCPHermes("  "), "\n") + "\n"

// sisipkanMCPHermes menyisipkan entri Arkon ke config.yaml Hermes,
// mengembalikan isi baru dan apakah ada yang berubah.
//
// Dua jalur, dan keduanya perlu. Kalau mcp_servers belum ada, bloknya
// ditaruh di DEPAN — alasan sama dengan blokModelHermes: config yang sudah
// ada bisa berakhir di tengah blok bersarang, dan menempel di sana mengubah
// arti kunci yang sama sekali lain. Kalau mcp_servers SUDAH ada, entri Arkon
// disisipkan tepat di bawah barisnya dengan indentasi yang dipakai entri
// lain di blok itu. Meniru pola singleton `model:` di sini — hanya menulis
// kalau kuncinya belum ada — berarti user yang sudah mendaftarkan satu server
// MCP lain tidak akan pernah mendapat Arkon, tanpa satu pun pesan.
//
// Fungsi murni, dipisah dari penulisannya supaya bisa diuji tanpa HOME.
func sisipkanMCPHermes(isi string) (string, bool, error) {
	if !punyaKunciYAML(isi, "mcp_servers") {
		return blokMCPHermes + isi, true, nil
	}
	baris := strings.Split(isi, "\n")
	for i, b := range baris {
		if !strings.HasPrefix(b, "mcp_servers:") {
			continue
		}
		// `mcp_servers: {}` atau `mcp_servers: !!null` — nilai gaya flow di
		// baris yang sama. Menyisipkan anak berindentasi di bawahnya
		// menghasilkan YAML yang tidak valid, dan tidak ada cara menyunting
		// bentuk itu dengan aman tanpa parser sungguhan.
		if sisa := strings.TrimSpace(strings.TrimPrefix(b, "mcp_servers:")); sisa != "" && !strings.HasPrefix(sisa, "#") {
			return isi, false, fmt.Errorf("mcp_servers memakai nilai satu baris (%q), dilewati", sisa)
		}
		indent := indentAnakYAML(baris[i+1:])
		if punyaAnakYAML(baris[i+1:], indent, namaServerMCPArkon) {
			return isi, false, nil
		}
		entri := entriMCPHermes(indent)
		baru := make([]string, 0, len(baris)+len(entri))
		baru = append(baru, baris[:i+1]...)
		baru = append(baru, entri...)
		baru = append(baru, baris[i+1:]...)
		return strings.Join(baru, "\n"), true, nil
	}
	return isi, false, nil
}

// indentAnakYAML membaca indentasi anak pertama sebuah blok mapping dari
// baris-baris setelah kuncinya. Blok kosong (baris berikutnya kunci tingkat
// atas lain, atau akhir berkas) memakai dua spasi, bawaan yang ditulis
// blokMCPHermes.
func indentAnakYAML(setelah []string) string {
	for _, b := range setelah {
		t := strings.TrimSpace(b)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		n := len(b) - len(strings.TrimLeft(b, " "))
		if n == 0 {
			break
		}
		return strings.Repeat(" ", n)
	}
	return "  "
}

// punyaAnakYAML menjawab apakah blok mapping sudah memuat anak bernama nama
// pada indentasi yang diberikan. Pencarian berhenti di baris pertama yang
// indentasinya lebih dangkal — di sanalah blok itu berakhir.
func punyaAnakYAML(setelah []string, indent, nama string) bool {
	for _, b := range setelah {
		t := strings.TrimSpace(b)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		n := len(b) - len(strings.TrimLeft(b, " "))
		if n < len(indent) {
			return false
		}
		if b == indent+nama+":" {
			return true
		}
	}
	return false
}

// tulisMCPHermes mendaftarkan Arkon di ~/.hermes/config.yaml + .env.
func tulisMCPHermes(u *userInfo, token string) error {
	dir := filepath.Join(u.Home, ".hermes")
	cfg := filepath.Join(dir, "config.yaml")

	isi, _ := os.ReadFile(cfg)
	baru, berubah, err := sisipkanMCPHermes(string(isi))
	if err != nil {
		return fmt.Errorf("%s: %w", cfg, err)
	}
	if berubah {
		if err := tulisBerkasUser(cfg, baru, u, 0o600); err != nil {
			return err
		}
	}

	// Token selalu disegarkan, bahkan kalau bloknya sudah ada: rotasi token
	// tier harus sampai ke sesi berikutnya, dan .env inilah satu-satunya tempat
	// nilainya disimpan.
	env := filepath.Join(dir, ".env")
	isiEnv, _ := os.ReadFile(env)
	if lama, ada := nilaiVarEnv(string(isiEnv), "ARKON_MCP_TOKEN"); ada && lama == token {
		return nil
	}
	return tulisBerkasRahasiaUser(env, gantiVarEnv(string(isiEnv), "ARKON_MCP_TOKEN", token), u)
}

// penandaMCPCodex menandai awal blok yang ditulis panel di config.toml.
const penandaMCPCodex = "# linux-dashboard: server MCP Arkon"

// tulisMCPCodex mendaftarkan Arkon di ~/.codex/config.toml.
//
// http_headers dengan nilai literal, BUKAN bearer_token_env_var: opsi itu
// menyebut NAMA variabel lingkungan, bukan nilainya, jadi memakainya menuntut
// panel ikut menyuntikkan variabel itu ke lingkungan sesi — jalur kedua yang
// bisa putus tanpa jejak, untuk hasil yang sama persis.
//
// Blok ditulis di AKHIR berkas dan dibatasi penanda. TOML membolehkan tabel
// ditulis dalam urutan mana pun, jadi menambahkannya di akhir tidak pernah
// memotong tabel lain — berbeda dengan YAML, yang justru sebaliknya.
func tulisMCPCodex(u *userInfo, token string) error {
	path := filepath.Join(u.Home, ".codex", "config.toml")
	lama, _ := os.ReadFile(path)
	isi := string(lama)

	blok := strings.Join([]string{
		penandaMCPCodex,
		"[mcp_servers." + namaServerMCPArkon + "]",
		`url = "` + urlMCPArkon + `"`,
		`http_headers = { Authorization = "` + headerOtorisasiArkon(token) + `" }`,
		"",
	}, "\n")

	if i := strings.Index(isi, penandaMCPCodex); i >= 0 {
		// Blok lama dibuang seluruhnya lalu ditulis ulang. Menyunting barisnya
		// di tempat akan meninggalkan token basi kalau formatnya pernah
		// berubah antar rilis panel.
		akhir := akhirBlokTOML(isi, i)
		if strings.Contains(isi[i:akhir], headerOtorisasiArkon(token)) {
			return nil
		}
		isi = isi[:i] + isi[akhir:]
	}
	if isi != "" && !strings.HasSuffix(isi, "\n") {
		isi += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return tulisBerkasRahasiaUser(path, isi+blok, u)
}

// akhirBlokTOML mencari akhir blok yang dimulai di posisi mulai: baris tabel
// berikutnya ("[...]" di kolom pertama) atau akhir berkas.
func akhirBlokTOML(isi string, mulai int) int {
	sisa := isi[mulai:]
	baris := strings.Split(sisa, "\n")
	pos := mulai
	// Baris pertama adalah penanda, baris kedua tabel milik blok ini sendiri —
	// keduanya dilewati sebelum mencari tabel berikutnya.
	for i, b := range baris {
		if i > 1 && strings.HasPrefix(strings.TrimSpace(b), "[") {
			return pos
		}
		pos += len(b) + 1
		if pos > len(isi) {
			return len(isi)
		}
	}
	return len(isi)
}
