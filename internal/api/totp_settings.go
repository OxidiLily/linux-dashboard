package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	appTotp "linux-dashboard/OxidiLily/internal/totp"
)

type totpPasswordRequest struct {
	Password string `json:"password"`
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.TOTPStatus(sessionFrom(r).Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca status TOTP")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// reauthTOTP memverifikasi password Linux melalui PAM helper. Token capability
// yang terbit hanya untuk membuktikan reauth dan langsung dicabut; ia tidak
// pernah menjadi sesi browser kedua.
func (s *Server) reauthTOTP(w http.ResponseWriter, r *http.Request, password string) bool {
	sess := sessionFrom(r)
	ip := clientIP(r)
	key := reauthThrottleKey(sess.Username)
	if ok, _ := s.throttle.allowed(key, ip); !ok {
		writeErr(w, http.StatusTooManyRequests, "Terlalu banyak percobaan")
		return false
	}
	var res helperproto.LoginResult
	err := s.helper.Call(helperproto.CmdAuthLogin, "", helperproto.LoginArgs{
		Username: sess.Username, Password: password, TTLHours: s.cfg.SessionTTLHours,
	}, &res)
	if err != nil {
		if helperclient.Code(err) == helperproto.ErrDenied {
			s.throttle.record(key, ip)
			writeErr(w, http.StatusUnauthorized, "Password salah")
		} else {
			writeErr(w, http.StatusServiceUnavailable, "Layanan autentikasi tidak tersedia")
		}
		return false
	}
	if res.Token == "" {
		writeErr(w, http.StatusServiceUnavailable, "Layanan autentikasi tidak tersedia")
		return false
	}
	s.cabutTokenHelper(res.Token)
	s.throttle.reset(key, ip)
	return true
}

func (s *Server) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	username := sessionFrom(r).Username
	status, err := s.store.TOTPStatus(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca status TOTP")
		return
	}
	if status.Enabled {
		writeErr(w, http.StatusConflict, "TOTP sudah aktif — nonaktifkan terlebih dahulu sebelum enrollment ulang")
		return
	}
	var req totpPasswordRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reauthTOTP(w, r, req.Password) {
		return
	}
	secret, err := appTotp.GenerateSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat secret TOTP")
		return
	}
	cipher, err := appTotp.LoadCipher(appTotp.KeyPath())
	if err != nil {
		log.Printf("muat key TOTP: %v", err)
		writeErr(w, http.StatusServiceUnavailable, "Key TOTP tidak tersedia")
		return
	}
	sealed, err := cipher.EncryptFor(username, []byte(secret))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal mengenkripsi secret TOTP")
		return
	}
	if err := s.store.SetTOTPPending(username, sealed); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan enrollment TOTP")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": appTotp.URI(username, secret)})
}

func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var req totpConfirmRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	username := sessionFrom(r).Username
	ip := clientIP(r)
	// Enroll yang sukses meninggalkan pending secret; tanpa throttle, endpoint
	// confirm menjadi oracle tebakan kode 6 digit dari sesi yang dicuri.
	confirmKey := "totpconfirm|" + username
	if ok, _ := s.throttle.allowed(confirmKey, ip); !ok {
		writeErr(w, http.StatusTooManyRequests, "Terlalu banyak percobaan")
		return
	}
	sealed, ok, err := s.store.TOTPPending(username)
	if err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "Tidak ada enrollment TOTP tertunda")
		return
	}
	cipher, err := appTotp.LoadCipher(appTotp.KeyPath())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "Key TOTP tidak tersedia")
		return
	}
	secret, err := cipher.DecryptFor(username, sealed)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "Secret TOTP tidak dapat dibuka")
		return
	}
	counter, valid := appTotp.Verify(string(secret), strings.TrimSpace(req.Code), time.Now())
	if !valid {
		s.throttle.record(confirmKey, ip)
		writeErr(w, http.StatusUnauthorized, "Kode autentikasi salah")
		return
	}
	codes, hashes, err := appTotp.GenerateRecovery(8)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat recovery codes")
		return
	}
	if err := s.store.EnableTOTPAt(username, hashes, counter); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal mengaktifkan TOTP")
		return
	}
	s.throttle.reset(confirmKey, ip)
	s.store.LogActivity(username, "totp_enabled", "", nil, ip)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "recovery_codes": codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var req totpPasswordRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reauthTOTP(w, r, req.Password) {
		return
	}
	username := sessionFrom(r).Username
	if err := s.store.DisableTOTP(username); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menonaktifkan TOTP")
		return
	}
	s.store.LogActivity(username, "totp_disabled", "", nil, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}
