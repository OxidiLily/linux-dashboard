package helper

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Token capability (opaque) diterbitkan helper daemon saat autentikasi PAM
// berhasil, dan menjadi SATU-SATUNYA dasar otorisasi untuk permintaan
// berikutnya.
//
// Sebelumnya identitas diambil dari field `Username` yang dikirim web app.
// Field itu tidak pernah bisa diverifikasi helper: siapa pun yang bisa bicara
// ke socket — termasuk proses web app yang sudah dikompromi — cukup menuliskan
// nama user sudo mana pun di sana untuk mendapat seluruh hak user itu, tanpa
// pernah tahu passwordnya. Token menutup jalur itu: ia hanya ada di dalam
// memori helper, tidak bisa ditebak (32 byte acak), tidak bisa dipalsukan tanpa
// login PAM yang benar, dan bisa dicabut (logout, ganti password, user diubah
// atau dihapus).
const (
	// sesiTTL adalah umur token BAWAAN, dipakai kalau web app tidak
	// menentukan sendiri (LoginArgs.TTLHours <= 0). Angkanya menyamakan umur
	// token dengan masa hidup sesi panel bawaan
	// (config.Config.SessionTTLHours, 12 jam): token yang hidup lebih lama
	// dari sesinya hanya menambah jendela serangan tanpa menambah guna.
	sesiTTL = 12 * time.Hour
	// Batas umur token yang boleh diminta web app.
	//
	// Minimal satu menit: token yang lebih pendek dari itu sudah mati sebelum
	// satu perjalanan pulang-pergi panel selesai, sehingga user ditolak
	// sebelum sempat memakainya. Batas ini belum bisa dicapai lewat setelan
	// yang ada (jamnya bilangan bulat), tapi penjepitnya tetap dipasang supaya
	// setelan sub-jam di kemudian hari tidak menghasilkan token yang mati saat
	// dibuat.
	//
	// Maksimal 30 hari: token capability adalah kunci kedua di samping cookie
	// sesi, dan operator yang menaikkan TTL sesi sampai hitungan bulan harus
	// tetap menghadapi batas atas yang jelas. Nilai yang keterlaluan dijepit,
	// bukan ditolak — menolak login akan mengunci operator dari panelnya
	// sendiri karena satu salah tulis di /etc/default/linux-dashboard.
	sesiTTLMin  = time.Minute
	sesiTTLMaks = 720 * time.Hour
	// maxTokenSesi membatasi jumlah token yang disimpan. Setiap login dan
	// setiap verifikasi password ulang menerbitkan token baru; tanpa batas,
	// map-nya tumbuh selamanya oleh permintaan yang tidak butuh autentikasi.
	maxTokenSesi = 4096
)

// jepitTTL menahan umur token di dalam batas yang jelas.
func jepitTTL(ttl time.Duration) time.Duration {
	if ttl < sesiTTLMin {
		return sesiTTLMin
	}
	if ttl > sesiTTLMaks {
		return sesiTTLMaks
	}
	return ttl
}

// ttlToken menerjemahkan umur sesi panel (jam) menjadi umur token helper.
//
// Sebelumnya umur token dipaku 12 jam di sini, sementara sesi panel mengikuti
// config.SessionTTLHours. Operator yang menaikkan TTL sesi di atas 12 jam
// mendapat sesi panel yang masih hidup tetapi token helper yang sudah mati:
// setiap permintaan dijawab 401 dan user dipaksa login ulang tanpa sebab yang
// terlihat.
//
// Nilai <= 0 berarti pemanggil tidak menentukan apa pun → pakai bawaan. Nilai
// yang sangat besar dijepit SEBELUM dikalikan, supaya jam yang tak masuk akal
// tidak meluap (overflow) menjadi durasi negatif — token yang mati saat dibuat
// justru kegagalan yang paling membingungkan.
func ttlToken(jam int) time.Duration {
	if jam <= 0 {
		return sesiTTL
	}
	if jam > int(sesiTTLMaks/time.Hour) {
		return jepitTTL(sesiTTLMaks)
	}
	return jepitTTL(time.Duration(jam) * time.Hour)
}

// sesiToken adalah isi satu token: identitas yang sudah diverifikasi saat
// login, plus kapan tokennya berhenti berlaku.
type sesiToken struct {
	Username string
	Sudo     bool
	Home     string
	Expires  time.Time
	// info adalah identitas lengkap hasil lookupUser saat login (UID, GID,
	// daftar grup). Worker memakainya untuk menurunkan privilege ke user itu,
	// jadi ia ikut disimpan supaya identitas yang ditegakkan benar-benar
	// identitas yang diverifikasi saat login — bukan hasil pembacaan ulang
	// /etc/passwd yang bisa saja sudah berubah.
	info *userInfo
}

// errSesiTidakValid dipakai untuk SEMUA kegagalan token: tidak ada, tidak
// dikenal, sudah dicabut, atau sudah kedaluwarsa. Ketiganya sengaja tidak
// dibedakan dalam balasan — pemanggil yang sah tidak berkepentingan membedakan
// sebabnya, sementara pemanggil yang tidak sah tidak boleh mendapat petunjuk
// mana token yang pernah ada.
func errSesiTidakValid() error {
	return &helperErr{
		code: helperproto.ErrSesiTidakValid,
		msg:  "sesi tidak valid atau sudah berakhir — login ulang diperlukan",
	}
}

// terbitkanToken membuat token acak untuk identitas yang sudah diverifikasi.
// ttl dijadikan parameter supaya umur token bisa diuji tanpa menunggu.
func (s *Server) terbitkanToken(u *userInfo, ttl time.Duration) (string, time.Time) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand gagal berarti mesinnya rusak parah; token yang bisa
		// ditebak jauh lebih buruk daripada login yang gagal.
		return "", time.Time{}
	}
	token := hex.EncodeToString(b)
	expires := time.Now().Add(ttl)

	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]*sesiToken{}
	}
	s.buangTokenKedaluwarsaLocked(time.Now())
	s.pangkasTokenLocked()
	s.tokens[token] = &sesiToken{
		Username: u.Name,
		Sudo:     u.Sudo,
		Home:     u.Home,
		Expires:  expires,
		info:     u,
	}
	return token, expires
}

// terbitkanTokenLogin adalah jalur penerbitan token setelah kredensial
// diverifikasi: umur token ditentukan setelan operator yang dikirim web app
// (LoginArgs.TTLHours), bukan angka tetap di sisi helper.
func (s *Server) terbitkanTokenLogin(u *userInfo, args helperproto.LoginArgs) (string, time.Time) {
	return s.terbitkanToken(u, ttlToken(args.TTLHours))
}

// tokenUser mengembalikan identitas pemilik token. Token kosong, tidak dikenal,
// sudah dicabut, atau sudah kedaluwarsa → false. Pemanggil WAJIB menolak
// permintaan saat hasilnya false.
//
// Pemeriksaan ini dilakukan di dalam kunci supaya pencabutan yang berjalan
// bersamaan (logout, ganti password) tidak bisa kalah balapan dengan permintaan
// yang sedang diperiksa.
func (s *Server) tokenUser(token string) (*userInfo, bool) {
	if token == "" {
		return nil, false
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	rec, ok := s.tokens[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(rec.Expires) {
		delete(s.tokens, token)
		return nil, false
	}
	if rec.info == nil {
		delete(s.tokens, token)
		return nil, false
	}
	return rec.info, true
}

// identitasSekarang adalah seam untuk test: membaca keadaan akun yang berlaku
// SEKARANG, bukan salinan yang tersimpan saat login.
var identitasSekarang = lookupUser

// sudoMasihAda melaporkan apakah pemilik token MASIH anggota grup sudo menurut
// basis data akun saat ini.
//
// Status sudo tersalin ke token saat login, dan keanggotaan grup bisa dicabut
// di luar panel (mis. `deluser x sudo`). Token membuktikan SIAPA pemanggilnya,
// bukan bahwa haknya masih ada — jadi untuk perintah yang butuh sudo, haknya
// diperiksa ulang. Gagal membaca keadaan akun = dianggap tidak berhak
// (fail-closed), karena "tidak bisa memastikan" bukan alasan memberi hak root.
func sudoMasihAda(u *userInfo) bool {
	if u == nil {
		return false
	}
	segar, err := identitasSekarang(u.Name)
	if err != nil {
		return false
	}
	return segar.Sudo
}

// sudoTerkini melaporkan status sudo pemilik token menurut keadaan akun SAAT
// INI — pemeriksaan yang sama dengan sudoMasihAda, tapi untuk DIBACA, bukan
// untuk menolak perintah. Dipakai web app (CmdAuthSudo) supaya kolom sudo di
// baris sesinya tidak basi setelah keanggotaan grup dicabut di luar panel.
//
// Keadaan akun yang tidak terbaca dijawab sebagai KEGAGALAN, bukan "tidak
// sudo". Pemanggilnya menyimpan jawaban ini ke baris sesi, jadi satu kegagalan
// baca yang sesaat tidak boleh mencabut hak semua admin; jalur eksekusi tetap
// dijaga fail-closed oleh sudoMasihAda, sehingga "tidak bisa memastikan" di
// sana memang berarti tidak berhak.
func (s *Server) sudoTerkini(u *userInfo) (helperproto.SudoResult, error) {
	if u == nil {
		return helperproto.SudoResult{}, errSesiTidakValid()
	}
	segar, err := identitasSekarang(u.Name)
	if err != nil {
		return helperproto.SudoResult{}, &helperErr{
			code: helperproto.ErrInternal,
			msg:  fmt.Sprintf("keadaan akun %q tidak terbaca: %v", u.Name, err),
		}
	}
	return helperproto.SudoResult{Sudo: segar.Sudo}, nil
}

// cabutToken mencabut satu token (auth.logout). Token yang tidak ada diabaikan
// supaya logout selalu berhasil.
func (s *Server) cabutToken(token string) {
	if token == "" {
		return
	}
	s.tokenMu.Lock()
	delete(s.tokens, token)
	s.tokenMu.Unlock()
}

// cabutTokenUser mencabut SELURUH token milik satu user. Dipakai saat password
// user diganti oleh admin, saat akunnya diubah (keanggotaan grup termasuk
// grup sudo), dan saat akunnya dihapus: hak yang tersalin ke token lama harus
// dihitung ulang, bukan diwarisi dari keadaan saat login.
func (s *Server) cabutTokenUser(username string) {
	if username == "" {
		return
	}
	s.tokenMu.Lock()
	for t, rec := range s.tokens {
		if rec.Username == username {
			delete(s.tokens, t)
		}
	}
	s.tokenMu.Unlock()
}

// cabutTokenUserKecuali mencabut token satu user kecuali satu token — dipakai
// saat user mengganti password atau mengubah akunnya SENDIRI: sesi yang sedang
// dipakai tetap hidup (kalau tidak, ia terlempar keluar dari halaman yang
// sedang dibukanya), sementara sesi di perangkat lain mati.
func (s *Server) cabutTokenUserKecuali(username, simpan string) {
	if username == "" {
		return
	}
	s.tokenMu.Lock()
	for t, rec := range s.tokens {
		if rec.Username == username && t != simpan {
			delete(s.tokens, t)
		}
	}
	s.tokenMu.Unlock()
}

// gcTokens menyapu token kedaluwarsa berkala supaya map-nya tidak menahan
// identitas sesi yang sudah lama berakhir.
func (s *Server) gcTokens() {
	t := time.NewTicker(time.Hour)
	for range t.C {
		s.tokenMu.Lock()
		s.buangTokenKedaluwarsaLocked(time.Now())
		s.tokenMu.Unlock()
	}
}

// buangTokenKedaluwarsaLocked dipanggil dengan tokenMu terkunci.
func (s *Server) buangTokenKedaluwarsaLocked(sekarang time.Time) {
	for t, rec := range s.tokens {
		if sekarang.After(rec.Expires) {
			delete(s.tokens, t)
		}
	}
}

// pangkasTokenLocked membuang token yang paling dekat kedaluwarsanya kalau
// jumlahnya melewati batas. Dipanggil dengan tokenMu terkunci.
func (s *Server) pangkasTokenLocked() {
	for len(s.tokens) >= maxTokenSesi {
		var tertua string
		var kapan time.Time
		for t, rec := range s.tokens {
			if tertua == "" || rec.Expires.Before(kapan) {
				tertua, kapan = t, rec.Expires
			}
		}
		if tertua == "" {
			return
		}
		delete(s.tokens, tertua)
	}
}
