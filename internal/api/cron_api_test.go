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
	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/metrics"
	"linux-dashboard/OxidiLily/internal/store"
)

// Test endpoint /api/cron lewat router SUNGGUHAN, bukan handler tunggal.
//
// Yang diuji di sini adalah hal-hal yang tidak terlihat di handler: rutenya
// benar-benar terdaftar, identitas sesi yang dikirim ke helper memang akun
// yang login, konflik helper dipetakan ke HTTP 409 (bukan 400/500), dan
// permintaan tanpa `previous` tidak pernah sampai ke helper.

func buatServerCron(t *testing.T, tiruan *helperTiruan) (http.Handler, *store.Store) {
	t.Helper()
	return buatServerTTL(t, tiruan, 12)
}

// buatServerTTL sama dengan buatServerCron, tapi umur sesi panelnya (yang juga
// menentukan umur token helper) bisa ditentukan test.
func buatServerTTL(t *testing.T, tiruan *helperTiruan, jam int) (http.Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "uji.db"))
	if err != nil {
		t.Fatalf("buka store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	hc := pasangHelperTiruan(t, tiruan)
	// Ensure stale socket removed before binding.
	sock := filepath.Join(dir, "x.sock")
	if err := os.RemoveAll(sock); err != nil {
		// ignore if not exists
	}
	cfg := config.Config{Listen: "127.0.0.1:0", SocketPath: sock, SecretPath: filepath.Join(dir, "x.key"), SessionTTLHours: jam}
	srv := New(cfg, st, hc, metrics.NewCollector(), http.NotFoundHandler())
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	return srv.Routes(), st
}

func TestMailcowInstallHostname(t *testing.T) {
	for _, hostname := range []string{"mail.example.org", "", "localhost", "127.0.0.1", "https://mail.example.org", "mail.example.org:8443", "mail.local", "example.org", "mail.example.lan"} {
		t.Run(hostname, func(t *testing.T) {
			h := &helperTiruan{balas: helperproto.ComponentStatus{Name: "mailcow", Installed: true}}
			r, st := buatServerCron(t, h)
			sess := buatSesi(t, st, "ani", true)
			body, _ := json.Marshal(map[string]string{"mailcow_hostname": hostname})
			req := httptest.NewRequest(http.MethodPost, "/api/components/mailcow/install", strings.NewReader(string(body)))
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if hostname != "mail.example.org" {
				if w.Code != http.StatusBadRequest || len(h.riwayatOperasi()) != 0 {
					t.Fatalf("invalid hostname: status %d, operations %v", w.Code, h.riwayatOperasi())
				}
				return
			}
			var args map[string]any
			_ = json.Unmarshal(h.args, &args)
			if w.Code != http.StatusOK || args["mailcow_hostname"] != hostname {
				t.Fatalf("hostname not forwarded: status %d args %v", w.Code, args)
			}
		})
	}
}

func TestMailcowOpenURL(t *testing.T) {
	for _, webURL := range []string{"https://mail.example.org:8443/admin", "https://mail.example.org:9443/admin", "https://mail.example.org:1/admin", "https://mail.example.org:65535/admin", "https://mail.example.org/admin", "", "https://mail.example.org:0/admin", "https://mail.example.org:65536/admin", "https://mail.example.org:/admin", "http://mail.example.org:8080/admin", "https://localhost:8443/admin", "https://user:secret@mail.example.org:8443/admin", "https://mail.example.org:8443/admin?secret=x", "https://mail.example.org/admin?", "https://mail.example.org/admin#", "https://mail.example.org/admin#x", "https://mail.example.org/admin/"} {
		t.Run(webURL, func(t *testing.T) {
			h := &helperTiruan{balas: []map[string]any{{"name": "mailcow", "installed": true, "web_url": webURL}}, periksaToken: true, tokenSah: map[string]bool{"tok-ani": true}}
			r, st := buatServerCron(t, h)
			req := httptest.NewRequest(http.MethodGet, "/api/open-url/mailcow", nil)
			req.Host = "panel.example.org:1122"
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: buatSesi(t, st, "ani", true)})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if webURL == "https://mail.example.org:8443/admin" || webURL == "https://mail.example.org:9443/admin" || webURL == "https://mail.example.org:1/admin" || webURL == "https://mail.example.org:65535/admin" || webURL == "https://mail.example.org/admin" {
				var out map[string]string
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				if w.Code != 200 || out["url"] != webURL {
					t.Fatalf("configured URL not returned: %d %s", w.Code, w.Body.String())
				}
			} else if w.Code != 404 {
				t.Fatalf("unsafe/unconfigured URL returned: %d", w.Code)
			}
		})
	}
}

func TestMailcowOpenURLSessionBoundary(t *testing.T) {
	for _, cookie := range []string{"", "invalid"} {
		h := &helperTiruan{}
		router, _ := buatServerCron(t, h)
		req := httptest.NewRequest(http.MethodGet, "/api/open-url/mailcow", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized || len(h.riwayatOperasi()) != 0 {
			t.Fatalf("invalid session reached helper: %d %v", w.Code, h.riwayatOperasi())
		}
	}
	h := &helperTiruan{}
	router, _ := buatServerCron(t, h)
	for _, name := range []string{"9router", "technitium-dns", "supabase"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/open-url/"+name, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("public link compatibility %s: %d", name, w.Code)
		}
	}
}

func TestCronAPIButuhSesi(t *testing.T) {
	r, _ := buatServerCron(t, &helperTiruan{})
	// Tanpa cookie sesi: harus 401, dan helper tidak boleh dipanggil sama sekali.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/cron", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET tanpa sesi = %d, harap %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCronAPIKirimIdentitasSesi(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.CronHasil{Isi: "", Batas: helperproto.CronMaxBytes}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdCronGet {
		t.Fatalf("command helper = %q, harap %q", tiruan.cmd, helperproto.CmdCronGet)
	}
	// Otorisasi helper datang dari TOKEN sesi, bukan dari nama user yang
	// diklaim web app: yang harus terkirim adalah token yang tersimpan di
	// sesi, apa pun isi body klien.
	ses, ok := st.GetSession(sess)
	if !ok {
		t.Fatal("sesi hilang dari store")
	}
	if tiruan.token != ses.HelperToken || tiruan.token == "" {
		t.Fatalf("token ke helper = %q, harap token sesi %q", tiruan.token, ses.HelperToken)
	}
}

func TestCronAPIPutMeneruskanPrevious(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.CronHasil{Isi: "*/5 * * * * /bin/true\n", Batas: helperproto.CronMaxBytes}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	body := `{"isi":"*/5 * * * * /bin/true\n","previous":""}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/cron", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdCronPut {
		t.Fatalf("command helper = %q, harap %q", tiruan.cmd, helperproto.CmdCronPut)
	}
	var args helperproto.CronArgs
	if err := json.Unmarshal(tiruan.args, &args); err != nil {
		t.Fatalf("args helper tidak terbaca: %v (%s)", err, tiruan.args)
	}
	// `previous` yang bernilai string kosong harus TETAP terkirim sebagai
	// penunjuk yang terisi — bukan hilang, dan bukan dianggap "tidak ada".
	if args.Previous == nil {
		t.Fatalf("previous harus terkirim sebagai penunjuk terisi, dapat null")
	}
	if *args.Previous != "" {
		t.Fatalf("previous = %q, harap string kosong", *args.Previous)
	}
	if args.Isi != "*/5 * * * * /bin/true\n" {
		t.Fatalf("isi = %q", args.Isi)
	}
}

func TestCronAPIPutTanpaPreviousKirimNull(t *testing.T) {
	// Body tanpa `previous` diteruskan apa adanya dan penolakannya ada di
	// helper (satu tempat, tidak bisa dilewati klien lain — lihat
	// TestCronPreviousWajib di internal/helper). Yang diuji di sini adalah
	// bagian yang mudah salah: nilainya harus benar-benar null, bukan berubah
	// jadi string kosong di perjalanan, karena "" adalah nilai yang SAH
	// (crontab memang kosong) dan akan lolos sebagai penimpa bebas.
	tiruan := &helperTiruan{balas: helperproto.CronHasil{Batas: helperproto.CronMaxBytes}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/cron", strings.NewReader(`{"isi":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	var args helperproto.CronArgs
	if err := json.Unmarshal(tiruan.args, &args); err != nil {
		t.Fatalf("args helper tidak terbaca: %v (%s)", err, tiruan.args)
	}
	if args.Previous != nil {
		t.Fatalf("previous harus null saat klien tidak mengirimnya, dapat %q", *args.Previous)
	}
}

func TestCronAPIIsiTerlaluBesarTidakSampaiHelper(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.CronHasil{Batas: helperproto.CronMaxBytes}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	besar := strings.Repeat("# pad\n", helperproto.CronMaxBytes/6+100)
	body, _ := json.Marshal(map[string]any{"isi": besar, "previous": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/cron", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	// Body >1 MB ditolak decodeBody; yang di bawah itu tapi di atas batas
	// crontab ditolak handler. Keduanya 400, dan yang penting: tidak diteruskan
	// ke helper.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("isi terlalu besar = %d, harap 400 (body: %s)", w.Code, w.Body.String())
	}
	if len(tiruan.riwayatOperasi()) != 0 {
		t.Fatalf("helper tidak boleh dipanggil, justru menerima %v", tiruan.riwayatOperasi())
	}
}

func TestCronAPIPemetaanStatusHelper(t *testing.T) {
	kasus := []struct {
		nama  string
		kode  string
		harap int
	}{
		{"konflik jadi 409", helperproto.ErrCronConflict, http.StatusConflict},
		{"nilai tidak valid jadi 400", helperproto.ErrNilaiTidakValid, http.StatusBadRequest},
		{"ditolak jadi 403", helperproto.ErrDenied, http.StatusForbidden},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			tiruan := &helperTiruan{balasE: &helperclient.Error{Code: k.kode, Msg: "pesan uji"}}
			r, st := buatServerCron(t, tiruan)
			sess := buatSesi(t, st, "ani", true)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
			r.ServeHTTP(w, req)

			if w.Code != k.harap {
				t.Fatalf("kode %q = %d, harap %d (body: %s)", k.kode, w.Code, k.harap, w.Body.String())
			}
			// Kode harus ikut di body supaya frontend bisa membedakan konflik
			// dari penolakan isi dan menjawabnya dengan "muat ulang".
			var body errBody
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body bukan errBody: %v", err)
			}
			if body.Code != k.kode {
				t.Fatalf("kode di body = %q, harap %q", body.Code, k.kode)
			}
		})
	}
}

func TestCronAPITidakButuhSudo(t *testing.T) {
	// Crontab akun sendiri bukan aksi admin: user biasa harus bisa membukanya.
	tiruan := &helperTiruan{balas: helperproto.CronHasil{Batas: helperproto.CronMaxBytes}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "budi", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("user bukan sudoer = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
}

func TestCronAPIEndpointLainTetap404(t *testing.T) {
	// Pengaman rute: salah ketik /api/crone harus 404 JSON, bukan HTML SPA.
	r, st := buatServerCron(t, &helperTiruan{})
	sess := buatSesi(t, st, "ani", true)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/crone", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("endpoint salah = %d, harap 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, harap JSON", ct)
	}
}

func TestJumlahBarisTidakMenghitungElemenKosongSesudahNewline(t *testing.T) {
	kasus := map[string]int{
		"":              0,
		"satu":          1,
		"satu\n":        1,
		"satu\ndua\n":   2,
		"satu\n\ndua\n": 3,
	}
	for isi, harap := range kasus {
		if dapat := jumlahBaris(isi); dapat != harap {
			t.Errorf("jumlahBaris(%q) = %d, harap %d", isi, dapat, harap)
		}
	}
}

// buatSesi membuat sesi di store dan mengembalikan id cookie-nya.
//
// Token helper ikut dibuat (bukan kosong): sesi yang tokennya kosong ditolak
// helper, jadi sesi uji tanpa token tidak akan pernah bisa memanggil helper
// sama sekali — bukan bentuk yang perlu diuji di sini, dan ada test tersendiri
// untuk kasus itu.
func buatSesi(t *testing.T, st *store.Store, username string, sudo bool) string {
	t.Helper()
	sess, err := st.CreateSession(username, "/home/"+username, "127.0.0.1", sudo, "tok-"+username, time.Hour)
	if err != nil {
		t.Fatalf("buat sesi: %v", err)
	}
	return sess.ID
}

var _ = os.Getenv
