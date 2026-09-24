package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Test endpoint /api/files/search lewat router sungguhan.
//
// Yang diuji di sini adalah hal yang tidak terlihat di handler: rutenya
// terdaftar, identitas sesi yang diteruskan memang akun yang login, kueri
// kosong tidak pernah sampai ke helper, dan hasilnya memuat `rel` (lokasi
// relatif) — field yang membuat daftar hasil berguna saat pencariannya
// menembus subfolder.

func TestCariFileButuhSesi(t *testing.T) {
	r, _ := buatServerCron(t, &helperTiruan{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/files/search?path=/home/ani&q=x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tanpa sesi = %d, harap %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCariFileKirimIdentitasDanHasilRel(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.SearchHasil{
		Hits: []helperproto.SearchHit{
			{Name: "laporan-lama.txt", Path: "/home/ani/sub/dalam/laporan-lama.txt", Rel: "sub/dalam/laporan-lama.txt"},
		},
		Dirs: 4,
	}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/search?path=/home/ani&q=laporan", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdFileSearch {
		t.Fatalf("command helper = %q, harap %q", tiruan.cmd, helperproto.CmdFileSearch)
	}
	// Identitas yang dipakai helper adalah TOKEN sesi — nama user yang
	// diklaim pemanggil tidak lagi menentukan hak apa pun.
	ses, ok := st.GetSession(sess)
	if !ok {
		t.Fatal("sesi hilang dari store")
	}
	if tiruan.token != ses.HelperToken || tiruan.token == "" {
		t.Fatalf("token ke helper = %q, harap token sesi %q", tiruan.token, ses.HelperToken)
	}
	var args helperproto.SearchArgs
	if err := json.Unmarshal(tiruan.args, &args); err != nil {
		t.Fatalf("args helper tidak terbaca: %v (%s)", err, tiruan.args)
	}
	if args.Query != "laporan" || args.Path != "/home/ani" {
		t.Fatalf("args = %+v, harap query laporan di /home/ani", args)
	}

	var body struct {
		Query     string                  `json:"query"`
		Hits      []helperproto.SearchHit `json:"hits"`
		Dirs      int                     `json:"dirs"`
		Truncated bool                    `json:"truncated"`
		Alasan    string                  `json:"alasan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON hasil: %v", err)
	}
	if len(body.Hits) != 1 {
		t.Fatalf("hits = %d, harap 1", len(body.Hits))
	}
	// `rel` inilah yang membedakan hasil rekursif dari daftar folder biasa:
	// tanpa itu UI hanya punya path absolut yang panjang.
	if body.Hits[0].Rel != "sub/dalam/laporan-lama.txt" {
		t.Fatalf("rel = %q", body.Hits[0].Rel)
	}
	if body.Dirs != 4 {
		t.Fatalf("dirs = %d, harap 4", body.Dirs)
	}
}

func TestCariFileKueriKosongTidakSampaiHelper(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.SearchHasil{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	for _, q := range []string{"", "%20%20"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/files/search?path=/home/ani&q="+q, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("kueri %q = %d, harap 400", q, w.Code)
		}
		if len(tiruan.riwayatOperasi()) != 0 {
			t.Fatalf("helper tidak boleh dipanggil untuk kueri kosong, justru %q", tiruan.riwayatOperasi())
		}
	}
}

func TestCariFilePathKosongPakaiHome(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.SearchHasil{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/search?q=apa", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("tanpa path = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	var args helperproto.SearchArgs
	if err := json.Unmarshal(tiruan.args, &args); err != nil {
		t.Fatalf("args helper tidak terbaca: %v", err)
	}
	if args.Path != "/home/ani" {
		t.Fatalf("path = %q, harap home sesi /home/ani", args.Path)
	}
}

func TestCariFileHasilKosongTetapArray(t *testing.T) {
	// `hits: null` di JSON memaksa klien menambahkan penjaga sendiri di setiap
	// tempat pemakaian. Array kosong adalah bentuk yang sama untuk "tidak ada".
	tiruan := &helperTiruan{balas: helperproto.SearchHasil{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/search?path=/home/ani&q=zzz", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), `"hits":[]`) {
		t.Fatalf("hits harus array kosong, dapat %s", w.Body.String())
	}
}

func TestCariFileMeneruskanAlasanBerhenti(t *testing.T) {
	// UI memilih kalimat per sebab: "persempit kata kunci" salah saat yang
	// habis adalah waktunya. Kalau alasannya tidak diteruskan, perbedaan itu
	// hilang di perjalanan dan semua pemotongan berbunyi sama.
	tiruan := &helperTiruan{balas: helperproto.SearchHasil{
		Hits:      []helperproto.SearchHit{},
		Truncated: true,
		Alasan:    "time",
		Dirs:      120,
	}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/search?path=/home/ani&q=x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	var body struct {
		Alasan    string `json:"alasan"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON hasil: %v", err)
	}
	if body.Alasan != "time" {
		t.Fatalf("alasan = %q, harap \"time\"", body.Alasan)
	}
	if !body.Truncated {
		t.Fatalf("truncated harus true saat ada alasan berhenti")
	}
}
