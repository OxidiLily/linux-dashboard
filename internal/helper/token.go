package helper

import (
	"crypto/rand"
	"encoding/hex"
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
	// sesiTTL menyamakan umur token dengan masa hidup sesi panel
	// (config.Config.SessionTTLHours, bawaan 12 jam). Token yang hidup lebih
	// lama dari sesinya hanya menambah jendela serangan tanpa menambah guna.
	sesiTTL = 12 * time.Hour
	// maxTokenSesi membatasi jumlah token yang disimpan. Setiap login dan
	// setiap verifikasi password ulang menerbitkan token baru; tanpa batas,
	// map-nya tumbuh selamanya oleh permintaan yang tidak butuh autentikasi.
	maxTokenSesi = 4096
)

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
