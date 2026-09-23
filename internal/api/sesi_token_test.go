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

// Sesi panel menyimpan capability token dari helper, dan token itulah yang
// dikirim pada setiap pemanggilan helper. Kalau tokennya hilang, dicabut, atau
// kedaluwarsa, helper menolaknya sebagai sesi tidak sah — dan API harus
// menjawab 401, bukan 500: user perlu diminta login ulang, bukan diberi tahu
// bahwa servernya rusak.

// buatSesiTanpaToken meniru sesi lama/rusak: baris sesi ada, tapi tanpa token.
func TestSesiTanpaTokenDitolak401(t *testing.T) {
	tiruan := &helperTiruan{periksaToken: true, tokenSah: map[string]bool{}, balas: helperproto.CronHasil{}}
	r, st := buatServerCron(t, tiruan)
	// Sesi dibuat langsung tanpa token: inilah bentuk sesi yang tersisa dari
	// instalasi lama (kolom helper_token kosong).
	ses, err := st.CreateSession("ani", "/home/ani", "127.0.0.1", true, "", time.Hour)
	if err != nil {
		t.Fatalf("buat sesi: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: ses.ID})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("sesi tanpa token = %d, harap 401 (body: %s)", w.Code, w.Body.String())
	}
	var body errBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan errBody: %v", err)
	}
	if body.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode di body = %q, harap %q", body.Code, helperproto.ErrSesiTidakValid)
	}
}

// Token yang sudah dicabut/kedaluwarsa di helper harus berakhir sebagai 401,
// bukan 500 — bentuk yang sama dengan token kosong, karena helper memang tidak
// membedakan sebabnya.
func TestSesiTokenDicabutAtauKedaluwarsaDitolak401(t *testing.T) {
	tiruan := &helperTiruan{
		periksaToken: true,
		tokenSah:     map[string]bool{}, // tidak ada token yang sah lagi
		balas:        helperproto.CronHasil{},
	}
	r, st := buatServerCron(t, tiruan)
	ses, err := st.CreateSession("ani", "/home/ani", "127.0.0.1", true, "tok-sudah-dicabut", time.Hour)
	if err != nil {
		t.Fatalf("buat sesi: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: ses.ID})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token dicabut = %d, harap 401 (body: %s)", w.Code, w.Body.String())
	}
}

// Sesi yang tokennya sah harus berjalan normal: perubahan ini tidak boleh
// mengubah perilaku panel bagi user yang sesinya benar-benar ada.
func TestSesiDenganTokenSahBerjalanNormal(t *testing.T) {
	tiruan := &helperTiruan{
		periksaToken: true,
		tokenSah:     map[string]bool{"tok-ani": true},
		balas:        helperproto.CronHasil{Isi: "*/5 * * * * /bin/true\n"},
	}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true) // buatSesi menyimpan token "tok-ani"

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("token sah = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.token != "tok-ani" {
		t.Fatalf("token ke helper = %q, harap token sesi", tiruan.token)
	}
}

// Token yang diterbitkan helper saat login harus benar-benar disimpan di sesi
// dan dipakai pada permintaan berikutnya — bukan dibuang setelah login.
func TestLoginMenyimpanTokenHelperDanMemakainya(t *testing.T) {
	tiruan := &helperTiruan{
		periksaToken: true,
		tokenSah:     map[string]bool{"tok-dari-helper": true},
		balas: helperproto.LoginResult{
			UID: 1000, GID: 1000, Home: "/home/ani", Shell: "/bin/bash", Sudo: true,
			Token: "tok-dari-helper",
		},
	}
	r, st := buatServerCron(t, tiruan)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"ani","password":"rahasia"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login tidak mengirim cookie sesi")
	}

	// Cookie tidak boleh memuat token helper: token itu capability untuk
	// helper daemon, dan browser tidak pernah butuh melihatnya.
	ses, ok := st.GetSession(cookies[0].Value)
	if !ok {
		t.Fatal("sesi hasil login tidak ada di store")
	}
	if ses.HelperToken != "tok-dari-helper" {
		t.Fatalf("token di sesi = %q, harap token dari helper", ses.HelperToken)
	}
	if strings.Contains(cookies[0].Value, "tok-dari-helper") {
		t.Fatal("token helper ikut bocor ke cookie sesi")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req2.AddCookie(cookies[0])
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("permintaan setelah login = %d, harap 200 (body: %s)", w2.Code, w2.Body.String())
	}
	if tiruan.token != "tok-dari-helper" {
		t.Fatalf("token ke helper = %q, harap %q", tiruan.token, "tok-dari-helper")
	}
}

// Helper yang menjawab OK tapi tanpa token berarti versinya tidak cocok dengan
// web app. Sesi tanpa token akan ditolak pada setiap permintaan berikutnya,
// jadi lebih jujur gagal sekarang (503) daripada membuat sesi yang mati sejak
// lahir.
func TestLoginTanpaTokenDariHelperTidakMembuatSesiMati(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.LoginResult{
		UID: 1000, GID: 1000, Home: "/home/ani", Shell: "/bin/bash", Sudo: true,
	}}
	r, _ := buatServerCron(t, tiruan)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"ani","password":"rahasia"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("login diterima tanpa token helper (body: %s)", w.Body.String())
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("login tanpa token = %d, harap 503", w.Code)
	}
}

// Logout harus mematikan sesi panel DAN mencabut tokennya di helper.
func TestLogoutMencabutTokenDiHelper(t *testing.T) {
	tiruan := &helperTiruan{periksaToken: true, tokenSah: map[string]bool{"tok-ani": true}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}

	var terlihat bool
	for _, c := range tiruan.riwayat() {
		if c == helperproto.CmdAuthLogout {
			terlihat = true
		}
	}
	if !terlihat {
		t.Fatalf("logout tidak memanggil %s (riwayat: %v)", helperproto.CmdAuthLogout, tiruan.riwayat())
	}
	if tiruan.token != "tok-ani" {
		t.Fatalf("token yang dicabut = %q, harap token sesi", tiruan.token)
	}
}

// Ganti password sendiri mencabut sesi lain (store) — dan token helper sesi
// lain itu juga harus mati di helper. Yang membuktikan jalur ini bukan test
// API melainkan internal/helper (token_test.go): helper yang mencabut token
// user saat auth.passwd dijalankan, karena web app memang tidak memegang token
// sesi lain. Di sini diperiksa bagian yang ada di sisi web app: permintaan
// ganti password tetap berjalan dan membawa token sesi.
func TestGantiPasswordMembawaTokenSesi(t *testing.T) {
	tiruan := &helperTiruan{periksaToken: true, tokenSah: map[string]bool{"tok-ani": true}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"old_password":"lama","new_password":"baru-sekali"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/account/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ganti password = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdAuthPasswd {
		t.Fatalf("command helper = %q, harap %q", tiruan.cmd, helperproto.CmdAuthPasswd)
	}
	if tiruan.token != "tok-ani" {
		t.Fatalf("token ke helper = %q, harap token sesi", tiruan.token)
	}
}
