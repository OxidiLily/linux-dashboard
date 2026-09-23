package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// berisi melaporkan apakah daftar memuat nilai yang dicari.
func berisi(daftar []string, cari string) bool {
	for _, s := range daftar {
		if s == cari {
			return true
		}
	}
	return false
}

// Batas upload diuji lewat jalur HTTP sungguhan: yang perlu dibuktikan bukan
// angka batasnya, melainkan bahwa berkas parsial benar-benar DIBUANG lewat
// helper dan berkas yang sudah mendarat dilaporkan ke klien. Tanpa test ini,
// panggilan file.remove bisa hilang tanpa satu pun test gagal, dan kuota disk
// tetap terpakai oleh berkas yang ditolak.
func TestUploadMenolakBerkasLewatBatasDanMembuangParsialnya(t *testing.T) {
	simpanBerkas, simpanTotal, simpanPart := uploadMaxBerkas, uploadMaxTotal, uploadMaxPart
	uploadMaxBerkas, uploadMaxTotal, uploadMaxPart = 8, 4096, 10
	t.Cleanup(func() {
		uploadMaxBerkas, uploadMaxTotal, uploadMaxPart = simpanBerkas, simpanTotal, simpanPart
	})

	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	kecil, err := mw.CreateFormFile("files", "kecil.txt")
	if err != nil {
		t.Fatalf("buat part: %v", err)
	}
	if _, err := kecil.Write([]byte("abc")); err != nil {
		t.Fatalf("tulis part kecil: %v", err)
	}
	besar, err := mw.CreateFormFile("files", "besar.txt")
	if err != nil {
		t.Fatalf("buat part: %v", err)
	}
	if _, err := besar.Write([]byte(strings.Repeat("x", 40))); err != nil {
		t.Fatalf("tulis part besar: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("tutup multipart: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload?path=/home/ani", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload lewat batas = %d, harap %d (body: %s)",
			w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}

	riwayat := tiruan.riwayat()
	if !berisi(riwayat, helperproto.CmdFileWrite) {
		t.Errorf("helper tidak pernah menerima %s: %v", helperproto.CmdFileWrite, riwayat)
	}
	if !berisi(riwayat, helperproto.CmdFileRemove) {
		t.Errorf("berkas parsial tidak dibuang lewat %s: %v", helperproto.CmdFileRemove, riwayat)
	}
	if !berisi(tiruan.paths, "/home/ani/besar.txt") {
		t.Errorf("path berkas parsial tidak dikirim ke helper: %v", tiruan.paths)
	}

	var body struct {
		Error     string   `json:"error"`
		Tersimpan []string `json:"tersimpan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON: %v (%s)", err, w.Body.String())
	}
	if !strings.Contains(body.Error, "berkas melebihi batas") {
		t.Errorf("pesan penolakan tidak menyebut batas per berkas: %q", body.Error)
	}
	if !berisi(body.Tersimpan, "/home/ani/kecil.txt") {
		t.Errorf("berkas yang sudah tersimpan tidak dilaporkan: %v", body.Tersimpan)
	}
}

// Batas per permintaan: berkas pertama menghabiskan kuota, berkas berikutnya
// ditolak dengan pesan yang menyebut batas permintaan (bukan batas berkas).
func TestUploadMenolakSaatKuotaPermintaanHabis(t *testing.T) {
	simpanBerkas, simpanTotal, simpanPart := uploadMaxBerkas, uploadMaxTotal, uploadMaxPart
	uploadMaxBerkas, uploadMaxTotal, uploadMaxPart = 10<<30, 4, 10
	t.Cleanup(func() {
		uploadMaxBerkas, uploadMaxTotal, uploadMaxPart = simpanBerkas, simpanTotal, simpanPart
	})

	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, nama := range []string{"a.txt", "b.txt"} {
		p, err := mw.CreateFormFile("files", nama)
		if err != nil {
			t.Fatalf("buat part: %v", err)
		}
		if _, err := p.Write([]byte("abc")); err != nil {
			t.Fatalf("tulis part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("tutup multipart: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload?path=/home/ani", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload lewat kuota permintaan = %d, harap %d (body: %s)",
			w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON: %v (%s)", err, w.Body.String())
	}
	if !strings.Contains(body.Error, "per permintaan") {
		t.Errorf("pesan penolakan tidak menyebut batas per permintaan: %q", body.Error)
	}
}
