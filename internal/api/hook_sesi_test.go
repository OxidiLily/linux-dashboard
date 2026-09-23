package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Hook yang menghubungkan aksi (login, logout, ubah user) dengan pencabutan
// sesi diuji lewat router sungguhan: yang mudah salah di sini bukan logika
// store-nya, tapi apakah handler benar-benar MEMANGGIL pencabutan itu. Sebelum
// test ini, baris pemanggilnya bisa dihapus tanpa satu pun test gagal.

// Cookie penghapus sesi harus memakai atribut yang sama PERSIS dengan cookie
// yang dibuat saat login — bukan ekspektasi yang ditulis ulang di test, karena
// divergensi di sisi login justru yang tidak akan tertangkap dengan cara itu.
func TestCookieLogoutSamaDenganCookieLogin(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.LoginResult{
		UID: 1000, GID: 1000, Home: "/home/ani", Shell: "/bin/bash", Sudo: true,
		Groups: []string{"ani", "sudo"},
	}}
	r, _ := buatServerCron(t, tiruan)

	// 1. Login sungguhan: ambil cookie yang benar-benar dikirim server.
	wLogin := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"ani","password":"rahasia"}`)
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	reqLogin.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wLogin, reqLogin)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login = %d, harap 200 (body: %s)", wLogin.Code, wLogin.Body.String())
	}
	cookies := wLogin.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login tidak mengirim cookie sesi")
	}
	login := cookies[0]
	if login.Value == "" {
		t.Fatal("cookie login tanpa nilai")
	}

	// 2. Logout memakai cookie itu.
	wOut := httptest.NewRecorder()
	reqOut := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqOut.AddCookie(&http.Cookie{Name: sessionCookie, Value: login.Value})
	r.ServeHTTP(wOut, reqOut)
	if wOut.Code != http.StatusOK {
		t.Fatalf("logout = %d, harap 200 (body: %s)", wOut.Code, wOut.Body.String())
	}
	hapus := wOut.Result().Cookies()
	if len(hapus) == 0 {
		t.Fatal("logout tidak mengirim cookie penghapus")
	}
	c := hapus[0]

	if c.Name != login.Name || c.Path != login.Path ||
		c.HttpOnly != login.HttpOnly || c.Secure != login.Secure ||
		c.SameSite != login.SameSite {
		t.Fatalf("atribut cookie logout %+v tidak sama dengan cookie login %+v", c, login)
	}
	if c.Value != "" {
		t.Errorf("cookie penghapus masih bernilai %q", c.Value)
	}
	if c.Expires.After(time.Now()) {
		t.Errorf("cookie penghapus kedaluwarsa di %v (masa depan)", c.Expires)
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge cookie penghapus = %d, mau negatif", c.MaxAge)
	}
}

// Mengubah akun ORANG LAIN mencabut sesi akun itu: status sudo disalin saat
// login, jadi tanpa pencabutan hak barunya tidak pernah dihitung ulang.
func TestUserModifyMencabutSesiTarget(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	admin := buatSesi(t, st, "ani", true)
	korban := buatSesi(t, st, "budi", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/account/users/budi",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: admin})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ubah user = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}

	if _, ok := st.GetSession(korban); ok {
		t.Error("sesi user yang diubah tidak dicabut")
	}
	if _, ok := st.GetSession(admin); !ok {
		t.Error("sesi admin yang mengubah ikut tercabut")
	}
}

// Admin yang mengubah akunnya SENDIRI tidak boleh terlempar keluar, tapi sesi
// lain milik akun itu tetap dicabut (perilaku sama seperti ganti password).
func TestUserModifyAkunSendiriHanyaMencabutSesiLain(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	aktif := buatSesi(t, st, "ani", true)
	lain := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/account/users/ani",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: aktif})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ubah akun sendiri = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}

	if _, ok := st.GetSession(aktif); !ok {
		t.Error("sesi yang sedang dipakai ikut tercabut saat admin mengubah akunnya sendiri")
	}
	if _, ok := st.GetSession(lain); ok {
		t.Error("sesi lain milik akun yang sama tidak dicabut")
	}
}

// Reset password akun lain juga harus mencabut sesinya; tanpa itu, sesi lama
// tetap hidup dengan password yang sudah diganti admin.
func TestUserResetPasswordMencabutSesiTarget(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	admin := buatSesi(t, st, "ani", true)
	korban := buatSesi(t, st, "budi", false)

	body, _ := json.Marshal(helperproto.PasswdArgs{Target: "budi", NewPassword: "baru-sekali"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/account/users/budi/password",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: admin})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reset password = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if _, ok := st.GetSession(korban); ok {
		t.Error("sesi target reset password tidak dicabut")
	}
	if _, ok := st.GetSession(admin); !ok {
		t.Error("sesi admin yang mereset ikut tercabut")
	}
}
