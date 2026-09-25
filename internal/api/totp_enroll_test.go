package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
	appTotp "linux-dashboard/OxidiLily/internal/totp"
)

func kodeSekarang(t *testing.T, secret string) string {
	t.Helper()
	code, err := appTotp.Code(secret, time.Now().Unix()/30)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestTOTPEnrollUlangTidakMematikanFaktorAktif(t *testing.T) {
	pasangKeyTOTP(t)
	tiruan := &helperTiruan{balas: helperproto.LoginResult{UID: 1000, Home: "/home/ani", Token: "token-reauth"}}
	r, st := buatServerCron(t, tiruan)
	aktifkanTOTP(t, st, "ani", "JBSWY3DPEHPK3PXP")
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/account/totp/enroll", strings.NewReader(`{"password":"rahasia"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("enroll ulang faktor aktif=%d body=%s", w.Code, w.Body.String())
	}
	status, err := st.TOTPStatus("ani")
	if err != nil || !status.Enabled {
		t.Fatalf("enroll ulang mematikan TOTP: status=%+v err=%v", status, err)
	}
}

func TestTOTPEnroll(t *testing.T) {
	pasangKeyTOTP(t)
	tiruan := &helperTiruan{balas: helperproto.LoginResult{UID: 1000, Home: "/home/ani", Token: "token-reauth"}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		return w
	}
	status := send(http.MethodGet, "/api/settings/account/totp", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%d %s", status.Code, status.Body.String())
	}

	begin := send(http.MethodPost, "/api/settings/account/totp/enroll", `{"password":"rahasia"}`)
	if begin.Code != http.StatusOK {
		t.Fatalf("enroll=%d %s", begin.Code, begin.Body.String())
	}
	var enrollment struct{ Secret, URI string }
	if err := json.Unmarshal(begin.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	if enrollment.Secret == "" || !strings.HasPrefix(enrollment.URI, "otpauth://") {
		t.Fatalf("enrollment=%+v", enrollment)
	}
	// Token helper hasil reauth wajib dicabut, bukan disimpan sebagai sesi kedua.
	seenLogout := false
	for _, cmd := range tiruan.riwayat() {
		if cmd == helperproto.CmdAuthLogout {
			seenLogout = true
		}
	}
	if !seenLogout {
		t.Fatal("token reauth tidak dicabut")
	}

	confirm := send(http.MethodPost, "/api/settings/account/totp/confirm", `{"code":"000000"}`)
	if confirm.Code != http.StatusUnauthorized {
		t.Fatalf("kode salah=%d", confirm.Code)
	}
	// Confirmation dengan recovery tidak boleh; gunakan kode TOTP aktual lewat helper test terpisah dari secret response.
	// Endpoint menerima kode saat ini dan hanya saat itu mengaktifkan faktor kedua.
	code := kodeSekarang(t, enrollment.Secret)
	confirm = send(http.MethodPost, "/api/settings/account/totp/confirm", `{"code":"`+code+`"}`)
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", confirm.Code, confirm.Body.String())
	}
	var enabled struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(confirm.Body.Bytes(), &enabled); err != nil || len(enabled.RecoveryCodes) != 8 {
		t.Fatalf("recovery=%v err=%v", enabled.RecoveryCodes, err)
	}
	status = send(http.MethodGet, "/api/settings/account/totp", "")
	if strings.Contains(status.Body.String(), enabled.RecoveryCodes[0]) {
		t.Fatal("recovery code ditampilkan ulang")
	}

	// Counter kode konfirmasi harus ikut tersimpan. Kalau masih -1, kode yang
	// sama dapat dipakai lagi untuk login selama jendela 30 detiknya.
	loginW := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"ani","password":"rahasia"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(loginW, loginReq)
	var loginChallenge struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginChallenge); err != nil {
		t.Fatal(err)
	}
	replayW := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/api/auth/totp", strings.NewReader(`{"challenge":"`+loginChallenge.Challenge+`","code":"`+code+`"}`))
	replayReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(replayW, replayReq)
	if replayW.Code != http.StatusUnauthorized {
		t.Fatalf("kode enrollment dipakai ulang=%d", replayW.Code)
	}

	disable := send(http.MethodDelete, "/api/settings/account/totp", `{"password":"rahasia"}`)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable=%d %s", disable.Code, disable.Body.String())
	}
	state, _ := st.TOTPStatus("ani")
	if state.Enabled {
		t.Fatal("TOTP masih aktif")
	}
}
