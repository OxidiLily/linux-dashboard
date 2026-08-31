package api

import (
	"context"
	"log"
	"net/http"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

const sessionCookie = "lindash_session"

type ctxKey int

const sessionKey ctxKey = iota

// throttle membatasi percobaan login per kombinasi user+IP.
// PAM tidak menyediakan proteksi brute force, jadi harus di layer aplikasi.
type throttle struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

const (
	throttleWindow = 5 * time.Minute
	throttleMax    = 5
)

func newThrottle() *throttle {
	return &throttle{attempts: map[string][]time.Time{}}
}

// allowed melaporkan apakah percobaan berikutnya masih boleh, tanpa mencatat.
func (t *throttle) allowed(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-throttleWindow)
	kept := t.attempts[key][:0]
	for _, at := range t.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	t.attempts[key] = kept
	if len(kept) >= throttleMax {
		return false, time.Until(kept[0].Add(throttleWindow))
	}
	return true, 0
}

func (t *throttle) record(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.attempts[key] = append(t.attempts[key], time.Now())
}

func (t *throttle) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key)
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
	key := req.Username + "|" + ip
	if ok, retry := s.throttle.allowed(key); !ok {
		s.store.LogActivity(req.Username, "login_failed", "throttled",
			map[string]any{"reason": "rate limit"}, ip)
		writeJSON(w, http.StatusTooManyRequests, errBody{
			Error: "Terlalu banyak percobaan login. Coba lagi dalam " + retry.Round(time.Second).String(),
		})
		return
	}

	var res helperproto.LoginResult
	err := s.helper.Call(helperproto.CmdAuthLogin, req.Username,
		helperproto.LoginArgs{Username: req.Username, Password: req.Password}, &res)
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
		s.throttle.record(key)
		s.store.LogActivity(req.Username, "login_failed", "", map[string]any{"reason": err.Error()}, ip)
		writeErr(w, http.StatusUnauthorized, "Username atau password salah")
		return
	}
	s.throttle.reset(key)

	sess, err := s.store.CreateSession(req.Username, res.Home, ip, res.Sudo,
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
		MustChangePassword: passwordMustChange(req.Username),
	})
}

// passwordMustChange mengembalikan true kalau `chage -l` melaporkan
// "Password must be changed" (field 3 /etc/shadow = 0) ATAU password sudah
// expired. Shell banner SSH menampilkan kalimat itu saat login pertama di
// banyak image Ubuntu/Debian; di panel kita tampilkan banner sendiri yang
// mengarahkan user ke menu Akun, jadi tidak ada pesan asing yang tampil di
// tempat lain.
func passwordMustChange(username string) bool {
	out, err := exec.Command("chage", "-l", username).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "password must be changed") {
			return true
		}
		if strings.HasPrefix(l, "password expires") {
			// "Password expires: never" = tidak perlu ganti.
			// "Password expires: password must be changed" = field 3 == 0.
			if !strings.Contains(l, "never") {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	_ = s.store.DeleteSession(sess.ID)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
	s.store.LogActivity(sess.Username, "logout", "", nil, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	out.MustChangePassword = passwordMustChange(sess.Username)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.sessionFromRequest(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "Sesi tidak valid atau sudah berakhir")
			return
		}
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
