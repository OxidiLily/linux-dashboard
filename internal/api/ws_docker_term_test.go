package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// dialTermWS membuka /ws/docker/terminal. id opsional ("" = tanpa id).
func dialTermWS(t *testing.T, ts *httptest.Server, cookie, id string) *websocket.Conn {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := url.Values{}
	if id != "" {
		q.Set("id", id)
	}
	h := http.Header{}
	h.Set("Origin", ts.URL)
	if cookie != "" {
		h.Set("Cookie", sessionCookie+"="+cookie)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx,
		"ws://"+u.Host+"/ws/docker/terminal?"+q.Encode(),
		&websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// Tanpa sesi, terminal container tidak boleh terbuka sama sekali.
func TestWSDockerTerminalTanpaSesi(t *testing.T) {
	r, _ := buatServerTTL(t, &helperTiruan{}, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()

	if kode := bacaKodeTutup(t, dialTermWS(t, ts, "", "stirling-pdf")); kode != 4401 {
		t.Fatalf("kode tutup = %d, harap 4401", kode)
	}
}

// `docker exec` setara root di container mana pun — sesi tanpa sudo ditolak.
func TestWSDockerTerminalTanpaSudo(t *testing.T) {
	r, st := buatServerTTL(t, &helperTiruan{}, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()
	kuki := buatSesi(t, st, "ani", false)

	if kode := bacaKodeTutup(t, dialTermWS(t, ts, kuki, "stirling-pdf")); kode != 4403 {
		t.Fatalf("kode tutup = %d, harap 4403", kode)
	}
}

// id container wajib — permintaan tanpa id ditolak sebelum sampai ke helper.
func TestWSDockerTerminalTanpaID(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerTTL(t, tiruan, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()
	kuki := buatSesi(t, st, "ani", true)

	if kode := bacaKodeTutup(t, dialTermWS(t, ts, kuki, "")); kode != 4400 {
		t.Fatalf("kode tutup = %d, harap 4400", kode)
	}
	for _, c := range tiruan.riwayat() {
		if c != helperproto.CmdAuthSudo {
			t.Fatalf("helper tetap dipanggil tanpa id: %v", tiruan.riwayat())
		}
	}
}

// Sesi sudo: keluaran "PTY" dari helper diteruskan sebagai pesan biner, dan
// id container sampai ke helper utuh — tanpa field perintah, karena shell
// container adalah satu-satunya isi sesi.
func TestWSDockerTerminalStreamSampaiKeKlien(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerTTL(t, tiruan, 12)
	ts := httptest.NewServer(r)
	defer ts.Close()
	kuki := buatSesi(t, st, "ani", true)

	conn := dialTermWS(t, ts, kuki, "stirling-pdf")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jenis, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("baca pesan: %v", err)
	}
	if jenis != websocket.MessageBinary {
		t.Fatalf("jenis pesan = %v, harap biner (keluaran PTY)", jenis)
	}
	if !strings.Contains(string(data), "term-tiruan") {
		t.Fatalf("pesan = %q, harap memuat term-tiruan", data)
	}

	ditemukan := false
	for _, c := range tiruan.riwayat() {
		if c == helperproto.CmdDockerTerm {
			ditemukan = true
		}
	}
	if !ditemukan {
		t.Fatalf("helper tidak pernah menerima %s (riwayat: %v)", helperproto.CmdDockerTerm, tiruan.riwayat())
	}
	args := string(tiruan.args)
	if !strings.Contains(args, "stirling-pdf") {
		t.Fatalf("argumen helper = %s, harap memuat id container", args)
	}
	if strings.Contains(args, "command") {
		t.Fatalf("argumen helper masih membawa field perintah: %s", args)
	}
}
