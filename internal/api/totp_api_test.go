package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/metrics"
	"linux-dashboard/OxidiLily/internal/store"
	appTotp "linux-dashboard/OxidiLily/internal/totp"
)

func pasangKeyTOTP(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "totp.key")
	if err := os.WriteFile(p, []byte(strings.Repeat("K", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DASHBOARD_TOTP_KEY", p)
	return p
}

func aktifkanTOTP(t *testing.T, st interface {
	SetTOTPPending(string, []byte) error
	EnableTOTP(string, [][]byte) error
}, username, secret string) {
	t.Helper()
	c, err := appTotp.LoadCipher(appTotp.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.EncryptFor(username, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetTOTPPending(username, sealed); err != nil {
		t.Fatal(err)
	}
	if err = st.EnableTOTP(username, [][]byte{appTotp.RecoveryHash("ABCDE-FGHIJ")}); err != nil {
		t.Fatal(err)
	}
}

func TestTOTPChallengeIP(t *testing.T) {
	pasangKeyTOTP(t)
	tiruan := &helperTiruan{balas: helperproto.LoginResult{UID: 1000, Home: "/home/ani", Shell: "/bin/bash", Token: "tok-helper"}}
	r, st := buatServerCron(t, tiruan)
	aktifkanTOTP(t, st, "ani", "JBSWY3DPEHPK3PXP")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"ani","password":"rahasia"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:1111"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("login=%d body=%s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cookie dibuat sebelum faktor kedua")
	}
	var challenge struct {
		Challenge    string `json:"challenge"`
		TOTPRequired bool   `json:"totp_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	if !challenge.TOTPRequired || challenge.Challenge == "" {
		t.Fatalf("challenge=%+v", challenge)
	}

	code, _ := appTotp.Code("JBSWY3DPEHPK3PXP", time.Now().Unix()/30)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/totp", strings.NewReader(`{"challenge":"`+challenge.Challenge+`","code":"`+code+`"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.0.2.11:2222"
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("IP lain=%d body=%s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/totp", strings.NewReader(`{"challenge":"`+challenge.Challenge+`","code":"`+code+`"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.RemoteAddr = "192.0.2.10:3333"
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK || len(w3.Result().Cookies()) == 0 {
		t.Fatalf("TOTP=%d body=%s", w3.Code, w3.Body.String())
	}
}

func TestTOTPReplayDitolakDanRecoverySekaliPakai(t *testing.T) {
	pasangKeyTOTP(t)
	tiruan := &helperTiruan{balas: helperproto.LoginResult{UID: 1000, Home: "/home/ani", Token: "tok-helper"}}
	r, st := buatServerCron(t, tiruan)
	aktifkanTOTP(t, st, "ani", "JBSWY3DPEHPK3PXP")
	login := func() string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"ani","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var v struct {
			Challenge string `json:"challenge"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v.Challenge
	}
	code, _ := appTotp.Code("JBSWY3DPEHPK3PXP", time.Now().Unix()/30)
	verify := func(challenge, code string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/totp", strings.NewReader(`{"challenge":"`+challenge+`","code":"`+code+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := verify(login(), code); got != http.StatusOK {
		t.Fatalf("kode pertama=%d", got)
	}
	if got := verify(login(), code); got != http.StatusUnauthorized {
		t.Fatalf("replay=%d", got)
	}
	if got := verify(login(), "ABCDE-FGHIJ"); got != http.StatusOK {
		t.Fatalf("recovery pertama=%d", got)
	}
	if got := verify(login(), "ABCDE-FGHIJ"); got != http.StatusUnauthorized {
		t.Fatalf("recovery replay=%d", got)
	}
}

// Tantangan TOTP kedaluwarsa harus ikut dibersihkan oleh GC berkala, bukan
// hanya saat tantangan baru dibuat. Kalau tidak, token helper di dalamnya bisa
// tetap hidup di memori selama server tidak pernah lagi menerima login TOTP.
func TestGCTantanganTOTPKedaluwarsaMembuangTokenHelper(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "gc-totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tiruan := &helperTiruan{balas: helperproto.LoginResult{}}
	hc := pasangHelperTiruan(t, tiruan)
	cfg := config.Config{Listen: "127.0.0.1:0", SocketPath: filepath.Join(dir, "x.sock"), SecretPath: filepath.Join(dir, "x.key"), SessionTTLHours: 12}
	s := New(cfg, st, hc, metrics.NewCollector(), http.NotFoundHandler())

	// Tantangan dibuat seolah 1 jam lalu: sudah lewat TTL 5 menit sejak awal.
	if _, err := s.addTOTPChallenge("ani", "192.0.2.1",
		helperproto.LoginResult{Token: "tok-tantangan"}, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.gcTOTPChallenge(time.Now())

	s.totpMu.Lock()
	sisa := len(s.totpChallenges)
	s.totpMu.Unlock()
	if sisa != 0 {
		t.Fatalf("tantangan kedaluwarsa masih tersimpan: %d", sisa)
	}
	adaCabut := false
	for _, cmd := range tiruan.riwayat() {
		if cmd == helperproto.CmdAuthLogout {
			adaCabut = true
		}
	}
	if !adaCabut {
		t.Fatal("token helper tantangan kedaluwarsa tidak dicabut saat GC")
	}
}
