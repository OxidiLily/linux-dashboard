package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// dialLogWS membuka /ws/docker/logs dengan cookie sesi yang diberikan.
func dialLogWS(t *testing.T, ts *httptest.Server, cookie string) *websocket.Conn {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	h := http.Header{}
	h.Set("Origin", ts.URL)
	if cookie != "" {
		h.Set("Cookie", sessionCookie+"="+cookie)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx,
		"ws://"+u.Host+"/ws/docker/logs?id=stirling-pdf&tail=5",
		&websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// bacaKodeTutup membaca sampai koneksi ditutup dan mengembalikan kode-nya.
func bacaKodeTutup(t *testing.T, conn *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			return ce.Code
		}
		t.Fatalf("tutup dengan error tak dikenal: %v", err)
		return 0
	}
}

// Tanpa cookie sesi, WebSocket tidak boleh membuka stream log container.
func TestWSDockerLogsTanpaSesi(t *testing.T) {
	r, _ := buatServerTTL(t, &helperTiruan{}, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()

	if kode := bacaKodeTutup(t, dialLogWS(t, ts, "")); kode != 4401 {
		t.Fatalf("kode tutup = %d, harap 4401", kode)
	}
}

// Stream log butuh sudo, sama seperti endpoint HTTP-nya.
func TestWSDockerLogsTanpaSudo(t *testing.T) {
	r, st := buatServerTTL(t, &helperTiruan{}, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()
	kuki := buatSesi(t, st, "ani", false)

	if kode := bacaKodeTutup(t, dialLogWS(t, ts, kuki)); kode != 4403 {
		t.Fatalf("kode tutup = %d, harap 4403", kode)
	}
}

// Sesi sudo: byte dari helper diteruskan apa adanya ke browser, dan argumen
// yang sampai ke helper memuat id container yang diminta.
func TestWSDockerLogsStreamSampaiKeKlien(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerTTL(t, tiruan, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()
	kuki := buatSesi(t, st, "ani", true)

	conn := dialLogWS(t, ts, kuki)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("baca pesan: %v", err)
	}
	if strings.TrimRight(string(data), "\n") != "log-tiruan" {
		t.Fatalf("pesan = %q, harap %q", data, "log-tiruan")
	}

	ditemukan := false
	for _, c := range tiruan.riwayat() {
		if c == helperproto.CmdDockerLogs {
			ditemukan = true
		}
	}
	if !ditemukan {
		t.Fatalf("helper tidak pernah menerima %s (riwayat: %v)", helperproto.CmdDockerLogs, tiruan.riwayat())
	}
	if args := string(tiruan.args); !strings.Contains(args, "stirling-pdf") || !strings.Contains(args, `"tail":5`) {
		t.Fatalf("argumen helper = %s, harap memuat id dan tail", args)
	}
}

// Pesan close frame dibatasi 123 byte oleh RFC 6455; lebih panjang membuat
// library membuang frame-nya dan klien hanya melihat 1006 tanpa sebab.
// Potongan juga harus tetap UTF-8 valid dan tanpa karakter kendali.
func TestAlasanTutupDipotong(t *testing.T) {
	if hsl := alasanTutup(strings.Repeat("x", 300) + "\nripsi"); len(hsl) > 120 {
		t.Fatalf("panjang alasan = %d, harap <= 120", len(hsl))
	}
	if strings.ContainsAny(alasanTutup("a\nb	c"), "\n	") {
		t.Fatal("karakter kendali lolos ke close reason")
	}
	// Unicode multi-byte tidak boleh terpotong jadi byte rusak.
	if hsl := alasanTutup(strings.Repeat("é", 200)); !utf8.ValidString(hsl) {
		t.Fatalf("alasanTutup memotong di tengah rune: %q", hsl)
	}
}

// Kuota web: slot diambil sampai penuh, lalu ditolak, dan kembali setelah
// dilepas. Handler yang masih berjalan dari test lain harus menyelesaikan
// dirinya dulu supaya pengukuran tidak dicampur.
func TestKuotaLogWeb(t *testing.T) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		muKuotaLog.Lock()
		kosong := kuotaLogAktif == 0
		muKuotaLog.Unlock()
		if kosong {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kuota log tidak pernah kembali kosong — slot bocor")
		}
		time.Sleep(20 * time.Millisecond)
	}

	var diambil int
	for i := 0; i < kuotaLogMaks; i++ {
		if !ambilSlotLog() {
			t.Fatalf("slot %d ditolak padahal belum penuh", i+1)
		}
		diambil++
	}
	if ambilSlotLog() {
		t.Fatal("kuota penuh masih mengeluarkan slot")
	}
	for i := 0; i < diambil; i++ {
		lepasSlotLog()
	}
	if !ambilSlotLog() {
		t.Fatal("slot tidak kembali setelah dilepas")
	}
	lepasSlotLog()
}
