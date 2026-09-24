package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

// Status sudo disalin ke baris sesi saat login (store.Session.Sudo). Kalau
// keanggotaan grup sudo dicabut di luar panel (`deluser dewi sudo`), salinan
// itu basi: endpoint yang butuh sudo tetap terbuka di sisi API, walaupun
// helper — yang memeriksa ulang keadaan akun — akan menolak perintahnya.
//
// Perbaikannya dua sisi: helper menyediakan command auth.sudo yang menjawab
// keadaan SEKARANG (diuji di internal/helper/auth_sudo_test.go), dan middleware
// autentikasi web app menyegarkan nilai yang tersimpan dari jawaban itu.
// Test di sini menguji sisi web app-nya, lewat router SUNGGUHAN: yang mudah
// salah bukan store-nya, melainkan apakah middleware benar-benar memanggil
// penyegaran itu, membatasinya, dan menyimpannya kembali.

// endpointUjiSudo adalah endpoint yang di-gate requireSudo. GET
// /api/settings/update dipilih karena requireSudo-nya berjalan SEBELUM
// perjalanan ke helper, jadi jawabannya hanya ditentukan status sudo sesi.
const endpointUjiSudo = "/api/settings/update"

func ptrBool(b bool) *bool { return &b }

// mintaUjiSudo mengirim satu permintaan ke endpointUjiSudo dengan cookie sesi.
func mintaUjiSudo(t *testing.T, r http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpointUjiSudo, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
	r.ServeHTTP(w, req)
	return w
}

func kodeErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body errBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan errBody: %v (%s)", err, w.Body.String())
	}
	return body.Code
}

func sudoTersimpan(t *testing.T, st *store.Store, id string) bool {
	t.Helper()
	sess, ok := st.GetSession(id)
	if !ok {
		t.Fatal("sesi hilang dari store")
	}
	return sess.Sudo
}

func hitungProbeSudo(t *testing.T, tiruan *helperTiruan) int {
	t.Helper()
	n := 0
	for _, c := range tiruan.riwayat() {
		if c == helperproto.CmdAuthSudo {
			n++
		}
	}
	return n
}

// Inti: sesi yang tersimpan sebagai sudoer, sementara helper melaporkan haknya
// sudah dicabut, tidak boleh lolos requireSudo — dan penyegarannya harus
// mendarat di store, supaya permintaan berikutnya membaca nilai yang benar.
func TestRequireSudoMemakaiStatusTerkiniDariHelper(t *testing.T) {
	tiruan := &helperTiruan{sudo: ptrBool(false), balas: helperproto.UpdateStatus{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	// Permintaan pertama: penyegaran berjalan di middleware, jadi permintaan
	// INI sudah ditolak.
	w := mintaUjiSudo(t, r, sess)
	if w.Code != http.StatusForbidden {
		t.Fatalf("requireSudo dengan hak yang sudah dicabut = %d, harap 403 (body: %s)", w.Code, w.Body.String())
	}
	if kode := kodeErr(t, w); kode != helperproto.ErrRequiresSudo {
		t.Fatalf("kode = %q, harap %q", kode, helperproto.ErrRequiresSudo)
	}
	if sudoTersimpan(t, st, sess) {
		t.Error("status sudo di store tidak disegarkan jadi false — permintaan berikutnya akan memakai nilai basi lagi")
	}

	// Permintaan kedua: ditolak DARI NILAI YANG TERSIMPAN, dan probe-nya tidak
	// diulang dalam jendela yang sama (satu probe per permintaan HTTP akan
	// membuat setiap pemuatan halaman menambah satu perjalanan ke helper).
	w2 := mintaUjiSudo(t, r, sess)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("permintaan kedua = %d, harap 403 (body: %s)", w2.Code, w2.Body.String())
	}
	if n := hitungProbeSudo(t, tiruan); n != 1 {
		t.Fatalf("probe auth.sudo = %d kali untuk dua permintaan, harap 1 (sekali per %v per sesi)", n, sudoCekInterval)
	}
}

// Hak yang BARU diberikan harus terlihat juga — kalau tidak, admin yang baru
// ditambahkan ke grup sudo tetap ditolak panel sampai ia login ulang, dan itu
// persis keluhan "kenapa tombolnya masih tidak bisa dipakai".
func TestSesiNonSudoDinaikkanSaatHakBaruDiberikan(t *testing.T) {
	tiruan := &helperTiruan{sudo: ptrBool(true), balas: helperproto.UpdateStatus{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", false)

	w := mintaUjiSudo(t, r, sess)
	if w.Code != http.StatusOK {
		t.Fatalf("requireSudo dengan hak yang baru diberikan = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if !sudoTersimpan(t, st, sess) {
		t.Error("status sudo di store tidak disegarkan jadi true")
	}
}

// Helper yang tidak bisa dihubungi BUKAN bukti pencabutan. Nilai yang tersimpan
// harus dibiarkan apa adanya: satu gangguan helper (restart, socket sibuk)
// tidak boleh mencabut sudo semua admin sekaligus.
func TestHelperTidakTerhubungTidakMengubahStatusSudo(t *testing.T) {
	// sudo diisi false supaya jelas bahwa yang menyelamatkan bukan jawaban
	// helper, melainkan aturan "gagal membaca ≠ dicabut".
	tiruan := &helperTiruan{sudo: ptrBool(false), mati: true}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := mintaUjiSudo(t, r, sess)
	if w.Code == http.StatusForbidden {
		t.Fatalf("helper yang tidak bisa dihubungi mencabut hak sudo sesi (body: %s)", w.Body.String())
	}
	if !sudoTersimpan(t, st, sess) {
		t.Error("status sudo tersimpan berubah padahal helper tidak pernah menjawab")
	}
}

// Setelah jendela pemeriksaan lewat, hasilnya diperiksa ULANG — kalau tidak,
// satu jawaban "tidak sudo" akan menempel sampai sesinya berakhir walaupun
// haknya sudah dikembalikan.
func TestProbeSudoDiulangSetelahJendelanyaLewat(t *testing.T) {
	simpan := sudoCekInterval
	t.Cleanup(func() { sudoCekInterval = simpan })
	sudoCekInterval = 0

	tiruan := &helperTiruan{sudo: ptrBool(false), balas: helperproto.UpdateStatus{}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	if w := mintaUjiSudo(t, r, sess); w.Code != http.StatusForbidden {
		t.Fatalf("permintaan pertama = %d, harap 403 (body: %s)", w.Code, w.Body.String())
	}

	// Hak dikembalikan di luar panel.
	tiruan.mu.Lock()
	tiruan.sudo = ptrBool(true)
	tiruan.mu.Unlock()

	if w := mintaUjiSudo(t, r, sess); w.Code != http.StatusOK {
		t.Fatalf("permintaan kedua (hak sudah dikembalikan) = %d, harap 200 (body: %s)", w.Code, w.Body.String())
	}
	if !sudoTersimpan(t, st, sess) {
		t.Error("status sudo di store tidak diperbarui jadi true")
	}
}

// Sesi lama/rusak yang tidak punya token helper tidak boleh ikut diprobe: tidak
// ada token untuk ditanyakan, dan satu probe tambahan hanya menambah perjalanan
// ke helper pada setiap permintaan yang memang akan ditolak.
func TestSesiTanpaTokenTidakIkutProbeSudo(t *testing.T) {
	tiruan := &helperTiruan{sudo: ptrBool(false), periksaToken: true, tokenSah: map[string]bool{}}
	r, st := buatServerCron(t, tiruan)
	sess, err := st.CreateSession("ani", "/home/ani", "127.0.0.1", true, "", time.Hour)
	if err != nil {
		t.Fatalf("buat sesi: %v", err)
	}

	mintaUjiSudo(t, r, sess.ID)

	if n := hitungProbeSudo(t, tiruan); n != 0 {
		t.Fatalf("probe auth.sudo = %d kali untuk sesi tanpa token, harap 0", n)
	}
	if sudoTersimpan(t, st, sess.ID) != true {
		t.Error("status sudo sesi tanpa token berubah tanpa jawaban helper")
	}
}
