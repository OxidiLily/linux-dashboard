package api

import (
	"context"
	"log"
	"net/http"
	"os/user"
	"strconv"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

const sessionCookie = "lindash_session"

type ctxKey int

const sessionKey ctxKey = iota

// throttle membatasi percobaan login.
//
// Dua dimensi dihitung terpisah karena keduanya menutup serangan berbeda:
// per (user|IP) menahan tebak-tebakan password satu akun dari satu alamat,
// dan per user menahan tebakan yang menyebar ke banyak alamat (IPv6 /64,
// botnet, atau header forwarded di depan reverse proxy yang tidak dikonfigurasi
// untuk mengganti alamat asal). Batas per user sengaja lebih longgar daripada
// per IP: angka yang terlalu kecil justru jadi alat penguncian akun orang lain
// — penyerang cukup mengirim belasan percobaan gagal atas nama korban.
type throttle struct {
	mu sync.Mutex
	// attempts di-key "username|ip"; perUser di-key username; perIP di-key ip.
	attempts map[string][]time.Time
	perUser  map[string][]time.Time
	perIP    map[string][]time.Time
}

const (
	throttleWindow = 5 * time.Minute
	throttleMax    = 5
	// throttleUserMax: percobaan gagal per username dari SELURUH alamat.
	throttleUserMax = 20
	// throttleIPMax: percobaan gagal dari satu alamat, apa pun username-nya.
	//
	// Ini yang menahan penyisipan key palsu. Tanpa batas per alamat, penyerang
	// bisa mengirim ribuan percobaan dengan username acak: tiap percobaan
	// membuat key baru, dan karena jumlah key dibatasi, yang terbuang justru
	// catatan percobaan atas akun korban (yang terakhir dicoba paling lama).
	// Dengan batas ini, dari satu alamat ia hanya sanggup menyisipkan beberapa
	// puluh entri sebelum alamatnya sendiri diblokir.
	throttleIPMax = 50
	// throttleMaxEntries: batas jumlah key yang disimpan. Tanpa batas ini,
	// kesalahan login dengan username acak menumbuhkan map selamanya — memori
	// proses web adalah sumber daya yang bisa dihabiskan tanpa autentikasi.
	throttleMaxEntries = 2000
)

func newThrottle() *throttle {
	return &throttle{
		attempts: map[string][]time.Time{},
		perUser:  map[string][]time.Time{},
		perIP:    map[string][]time.Time{},
	}
}

func throttleKey(username, ip string) string { return username + "|" + ip }

// reauthThrottleKey memberi namespace sendiri untuk prompt password ulang
// (reset sesi terminal di system.go).
//
// Login dan prompt itu memakai kredensial yang sama, tetapi menyatukan
// penghitungnya berarti dua puluh salah-ketik pada prompt sudo — dari sesi yang
// memang sudah login — ikut memblokir LOGIN username tersebut. Batas per alamat
// (perIP) tetap dipakai bersama, karena yang dijaga di sana adalah laju
// percobaan dari satu alamat, bukan satu fitur.
func reauthThrottleKey(username string) string { return "reauth|" + username }

// pruneLocked membuang catatan yang lebih tua dari jendela, dipanggil dengan
// mu terkunci.
func (t *throttle) pruneLocked(now time.Time) {
	cutoff := now.Add(-throttleWindow)
	for k, list := range t.attempts {
		kept := list[:0]
		for _, at := range list {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(t.attempts, k)
			continue
		}
		t.attempts[k] = kept
	}
	for k, list := range t.perUser {
		kept := list[:0]
		for _, at := range list {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(t.perUser, k)
			continue
		}
		t.perUser[k] = kept
	}
	for k, list := range t.perIP {
		kept := list[:0]
		for _, at := range list {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(t.perIP, k)
			continue
		}
		t.perIP[k] = kept
	}
}

// evictLocked membuang key dengan percobaan terakhir paling lama sampai jumlah
// key kembali di bawah batas. Dipanggil dengan mu terkunci.
func (t *throttle) evictLocked() {
	for len(t.attempts) > throttleMaxEntries {
		oldestKey, varOldest := "", time.Time{}
		for k, list := range t.attempts {
			last := list[len(list)-1]
			if varOldest.IsZero() || last.Before(varOldest) {
				oldestKey, varOldest = k, last
			}
		}
		if oldestKey == "" {
			return
		}
		delete(t.attempts, oldestKey)
	}
	for len(t.perUser) > throttleMaxEntries {
		oldestKey, varOldest := "", time.Time{}
		for k, list := range t.perUser {
			last := list[len(list)-1]
			if varOldest.IsZero() || last.Before(varOldest) {
				oldestKey, varOldest = k, last
			}
		}
		if oldestKey == "" {
			return
		}
		delete(t.perUser, oldestKey)
	}
	for len(t.perIP) > throttleMaxEntries {
		oldestKey, varOldest := "", time.Time{}
		for k, list := range t.perIP {
			last := list[len(list)-1]
			if varOldest.IsZero() || last.Before(varOldest) {
				oldestKey, varOldest = k, last
			}
		}
		if oldestKey == "" {
			return
		}
		delete(t.perIP, oldestKey)
	}
}

// allowedAt melaporkan apakah percobaan berikutnya masih boleh, tanpa mencatat.
func (t *throttle) allowedAt(username, ip string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	key := throttleKey(username, ip)
	if list := t.attempts[key]; len(list) >= throttleMax {
		return false, time.Until(list[0].Add(throttleWindow))
	}
	if list := t.perUser[username]; len(list) >= throttleUserMax {
		return false, time.Until(list[0].Add(throttleWindow))
	}
	if list := t.perIP[ip]; len(list) >= throttleIPMax {
		return false, time.Until(list[0].Add(throttleWindow))
	}
	return true, 0
}

func (t *throttle) recordAt(username, ip string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	key := throttleKey(username, ip)
	t.attempts[key] = append(t.attempts[key], now)
	t.perUser[username] = append(t.perUser[username], now)
	t.perIP[ip] = append(t.perIP[ip], now)
	t.evictLocked()
}

// gc membuang seluruh catatan yang sudah kedaluwarsa. Dipanggil berkala dari
// server supaya key yang tidak pernah dicoba lagi tidak menetap di memori.
func (t *throttle) gc(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
}

func (t *throttle) allowed(username, ip string) (bool, time.Duration) {
	return t.allowedAt(username, ip, time.Now())
}

func (t *throttle) record(username, ip string) { t.recordAt(username, ip, time.Now()) }

func (t *throttle) reset(username, ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, throttleKey(username, ip))
	delete(t.perUser, username)
	// perIP sengaja TIDAK dihapus saat satu login berhasil: kalau dihapus,
	// pemegang satu kredensial sah bisa terus mengosongkan kuota alamatnya
	// sendiri dan menghapus jejak percobaan gagal dari alamat itu. Catatannya
	// hilang sendiri setelah jendelanya lewat.
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sessionUser struct {
	Username string   `json:"username"`
	Sudo     bool     `json:"sudo"`
	Home     string   `json:"home"`
	Shell    string   `json:"shell"`
	UID      int      `json:"uid"`
	Groups   []string `json:"groups"`
	// MustChangePassword menandai akun yang password-nya wajib diganti
	// sebelum dipakai — dideteksi dari `chage -l` (field "Password expires"
	// = "must be changed" atau "never" + max=0). Dipakai untuk menampilkan
	// banner di topbar supaya user tidak terjebak SSH banner "Default
	// password must be changed" tanpa tahu harus ke mana.
	MustChangePassword bool `json:"must_change_password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ip := clientIP(r)
	key := req.Username
	if ok, retry := s.throttle.allowed(key, ip); !ok {
		s.store.LogActivity(req.Username, "login_failed", "throttled",
			map[string]any{"reason": "rate limit"}, ip)
		writeJSON(w, http.StatusTooManyRequests, errBody{
			Error: "Terlalu banyak percobaan login. Coba lagi dalam " + retry.Round(time.Second).String(),
		})
		return
	}

	var res helperproto.LoginResult
	// Token kosong: auth.login tidak butuh token (ia justru yang menerbitkannya).
	// TTLHours diteruskan supaya umur token helper SAMA dengan umur sesi panel:
	// token yang mati lebih dulu daripada sesinya membuat setiap permintaan
	// dijawab 401 dan user dipaksa login ulang tanpa sebab yang terlihat.
	err := s.helper.Call(helperproto.CmdAuthLogin, "",
		helperproto.LoginArgs{Username: req.Username, Password: req.Password, TTLHours: s.cfg.SessionTTLHours}, &res)
	if err != nil {
		// Kegagalan infrastruktur (helper mati, socket tidak terjangkau) TIDAK
		// boleh dilaporkan sebagai password salah: user akan mengetik ulang
		// password yang sebenarnya benar, dan penyebab aslinya tidak terlihat
		// dari mana pun. Hanya kode `denied` dari PAM yang berarti kredensial
		// memang salah.
		if helperclient.Code(err) != helperproto.ErrDenied {
			log.Printf("login %q gagal karena masalah sistem: %v", req.Username, err)
			s.store.LogActivity(req.Username, "login_failed", "system_error",
				map[string]any{"reason": err.Error()}, ip)
			writeErr(w, http.StatusServiceUnavailable,
				"Layanan autentikasi tidak tersedia. Cek status linux-dashboard-helper.service.")
			return
		}
		s.throttle.record(key, ip)
		s.store.LogActivity(req.Username, "login_failed", "", map[string]any{"reason": err.Error()}, ip)
		writeErr(w, http.StatusUnauthorized, "Username atau password salah")
		return
	}
	// Helper yang menjawab OK tapi tanpa token berarti versi helper-nya tidak
	// sepakat dengan web app (mis. helper lama saat panel baru dipasang).
	// Sesi yang dibuat tanpa token akan ditolak helper pada SETIAP permintaan
	// berikutnya, jadi lebih jujur dilaporkan sebagai masalah layanan
	// sekarang daripada membuat sesi yang mati sejak lahir.
	if res.Token == "" {
		log.Printf("login %q: helper tidak menerbitkan token sesi (versi helper tidak cocok?)", req.Username)
		s.store.LogActivity(req.Username, "login_failed", "system_error",
			map[string]any{"reason": "helper tidak menerbitkan token sesi"}, ip)
		writeErr(w, http.StatusServiceUnavailable,
			"Layanan autentikasi tidak tersedia. Cek status linux-dashboard-helper.service.")
		return
	}
	s.throttle.reset(key, ip)

	sess, err := s.store.CreateSession(req.Username, res.Home, ip, res.Sudo, res.Token,
		time.Duration(s.cfg.SessionTTLHours)*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat session: "+err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.Expires,
	})
	s.store.LogActivity(req.Username, "login_success", "", nil, ip)
	writeJSON(w, http.StatusOK, sessionUser{
		Username: req.Username, Sudo: res.Sudo, Home: res.Home,
		Shell: res.Shell, UID: res.UID, Groups: res.Groups,
		MustChangePassword: res.MustChangePassword,
	})
}

func expiredSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Expires ikut dikirim: sebagian browser (dan klien non-browser)
		// mengabaikan MaxAge pada cookie Secure di koneksi HTTP, sehingga
		// cookie sesi tetap terkirim setelah "logout".
		Expires: time.Unix(0, 0),
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	_ = s.store.DeleteSession(sess.ID)
	// Sesi panelnya mati, jadi token helper-nya ikut dicabut. Tanpa ini token
	// tetap sah sampai kedaluwarsa walaupun cookie-nya sudah dibuang.
	s.cabutTokenHelper(sess.HelperToken)
	http.SetCookie(w, expiredSessionCookie(s.cfg.SecureCookie))
	s.store.LogActivity(sess.Username, "logout", "", nil, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// cabutTokenHelper meminta helper daemon mencabut satu token capability.
//
// Kegagalan TIDAK boleh menggagalkan aksi pemanggilnya: logout harus tetap
// berhasil walau helper-nya sedang mati, dan token yang tidak tercabut akan
// kedaluwarsa sendiri seperti sesi panelnya — keduanya memakai setelan umur
// yang sama (DASHBOARD_SESSION_TTL_HOURS).
func (s *Server) cabutTokenHelper(token string) {
	if token == "" || s.helper == nil {
		return
	}
	if err := s.helper.Call(helperproto.CmdAuthLogout, token, nil, nil); err != nil {
		log.Printf("cabut token sesi di helper gagal: %v", err)
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	out := sessionUser{Username: sess.Username, Sudo: sess.Sudo, Home: sess.Home}
	// Session store hanya menyimpan username/home/sudo, jadi UID & shell harus
	// dibaca ulang dari /etc/passwd — tanpa ini frontend menampilkan "uid 0"
	// untuk semua user setelah sesi di-restore.
	if u, err := user.Lookup(sess.Username); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			out.UID = uid
		}
		if gids, err := u.GroupIds(); err == nil {
			for _, gid := range gids {
				if g, err := user.LookupGroupId(gid); err == nil {
					out.Groups = append(out.Groups, g.Name)
				}
			}
		}
	}
	var status helperproto.PasswordStatusResult
	if err := s.helper.Call(helperproto.CmdAuthPasswordStatus, sess.HelperToken, nil, &status); err != nil {
		writeHelperErr(w, err)
		return
	}
	out.MustChangePassword = status.MustChangePassword
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.sessionFromRequest(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "Sesi tidak valid atau sudah berakhir")
			return
		}
		// Status sudo disegarkan SEBELUM handler berjalan: requireSudo di
		// dalamnya membaca nilai yang sudah diperbarui, jadi keanggotaan grup
		// yang dicabut di luar panel tidak menyisakan endpoint terbuka.
		s.segarkanSudo(&sess)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func (s *Server) sessionFromRequest(r *http.Request) (store.Session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.Session{}, false
	}
	return s.store.GetSession(c.Value)
}

func sessionFrom(r *http.Request) store.Session {
	sess, _ := r.Context().Value(sessionKey).(store.Session)
	return sess
}

// sudoCekInterval membatasi seberapa sering status sudo satu sesi diperiksa
// ulang ke helper. Variabel, bukan konstanta, supaya test bisa memendekkannya
// tanpa menunggu.
var sudoCekInterval = 30 * time.Second

// segarkanSudo memeriksa ulang status sudo sesi ke helper, lalu menyimpannya
// kembali ke store.
//
// Status sudo disalin ke baris sesi saat login, sementara keanggotaan grup sudo
// bisa dicabut di luar panel (`deluser dewi sudo`). Tanpa penyegaran ini,
// salinan basi itu tetap dipakai sampai sesinya berakhir: endpoint yang butuh
// sudo masih lolos di sisi API, dan user baru tahu setelah perintahnya ditolak
// helper — penolakan yang datang dari tempat yang jauh dari sebabnya.
//
// Pemeriksaan dibatasi sudoCekInterval per sesi. Satu probe per permintaan HTTP
// berarti setiap pemuatan halaman menambah satu perjalanan ke socket helper,
// sementara jawabannya hanya berubah kalau keadaan akun berubah.
//
// Gagal menghubungi helper BUKAN bukti pencabutan: nilai yang tersimpan
// dibiarkan apa adanya, dan penanda waktunya tetap ditulis supaya helper yang
// mati tidak membuat setiap permintaan menunggu timeout. Satu gangguan helper
// (restart, socket sibuk) tidak boleh mencabut sudo semua admin sekaligus —
// pemeriksaan yang sesungguhnya tetap dilakukan helper pada setiap perintah
// ber-sudo (lihat sudoMasihAda).
func (s *Server) segarkanSudo(sess *store.Session) {
	if s.helper == nil || sess.HelperToken == "" {
		// Tanpa token tidak ada yang bisa ditanyakan: sesi lama seperti ini
		// memang ditolak helper pada setiap permintaan, dan probe tambahan
		// hanya menambah perjalanan yang pasti gagal.
		return
	}
	if !s.bolehCekSudo(sess.ID, time.Now()) {
		return
	}
	var res helperproto.SudoResult
	if err := s.helper.Call(helperproto.CmdAuthSudo, sess.HelperToken, nil, &res); err != nil {
		log.Printf("cek status sudo sesi %q dilewati (nilai tersimpan dibiarkan): %v", sess.Username, err)
		return
	}
	if res.Sudo == sess.Sudo {
		return
	}
	if err := s.store.SetSessionSudo(sess.ID, res.Sudo); err != nil {
		log.Printf("simpan status sudo sesi %q gagal: %v", sess.Username, err)
		return
	}
	sess.Sudo = res.Sudo
}

// bolehCekSudo melaporkan apakah sesi ini boleh diperiksa ulang sekarang, dan
// mencatat waktunya kalau boleh. Pemanggil yang mendapat true WAJIB mencoba
// pemeriksaan itu — jatah jendelanya sudah terpakai, supaya percobaan yang
// gagal pun tidak diulang pada setiap permintaan.
func (s *Server) bolehCekSudo(id string, now time.Time) bool {
	s.sudoMu.Lock()
	defer s.sudoMu.Unlock()
	if s.sudoCek == nil {
		s.sudoCek = map[string]time.Time{}
	}
	if last, ok := s.sudoCek[id]; ok && now.Sub(last) < sudoCekInterval {
		return false
	}
	s.sudoCek[id] = now
	// Peta ini seumur proses, sedangkan sesi datang dan pergi. Entri sesi yang
	// sudah lewat dua jendela (sesi di-logout, cookie dibuang) disapu saat
	// petanya membesar — tanpa itu satu-satunya jejak sesi lama adalah memori
	// yang tidak pernah kembali.
	if len(s.sudoCek) > 512 {
		for id, t := range s.sudoCek {
			if now.Sub(t) > 2*sudoCekInterval {
				delete(s.sudoCek, id)
			}
		}
	}
	return true
}

// requireSudo dipakai handler yang aksinya butuh privilege, supaya UI dapat
// pesan jelas sebelum request sampai ke helper daemon.
func requireSudo(w http.ResponseWriter, r *http.Request) bool {
	if sessionFrom(r).Sudo {
		return true
	}
	writeJSON(w, http.StatusForbidden, errBody{
		Error: "Aksi ini butuh akses sudo",
		Code:  helperproto.ErrRequiresSudo,
	})
	return false
}
