package api

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// helperTiruan mencatat command yang benar-benar diterima, lalu membalas
// dengan nilai yang disuntikkan test.
type helperTiruan struct {
	cmd      string
	username string
	args     []byte
	// balas adalah Data yang dikirim balik helper (di-marshal ke JSON).
	balas any
	// balasE, kalau diisi, dikirim sebagai kegagalan helper berkode.
	balasE error
}

// pasangHelperTiruan menjalankan helper daemon TIRUAN di Unix socket
// sungguhan, lalu mengembalikan client asli yang menunjuk ke sana.
//
// Sengaja bukan interface palsu: dengan begini yang diuji adalah client
// sungguhan (framing, HMAC, parsing respons) plus handler sungguhan. Yang
// diganti hanya sisi seberangnya.
func pasangHelperTiruan(t *testing.T, tiruan *helperTiruan) *helperclient.Client {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	secretPath := filepath.Join(dir, "secret.key")

	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("tulis secret: %v", err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen socket: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go layaniTiruan(conn, tiruan)
		}
	}()

	hc, err := helperclient.New(sock, secretPath)
	if err != nil {
		t.Fatalf("client helper: %v", err)
	}
	return hc
}

func layaniTiruan(conn net.Conn, tiruan *helperTiruan) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	// Framing: "<hex-hmac> <json-request>\n". Signature tidak diverifikasi —
	// yang diuji adalah sisi KLIEN-nya, bukan kerahasiaannya.
	if _, payload, ok := cutSpasi(line); ok {
		var req helperproto.Request
		if json.Unmarshal(payload, &req) == nil {
			tiruan.cmd = req.Cmd
			tiruan.username = req.Username
			tiruan.args = req.Args
		}
	}
	resp := helperproto.Response{OK: true}
	if tiruan.balasE != nil {
		var he *helperclient.Error
		if ok := asClientErr(tiruan.balasE, &he); ok {
			resp.OK = false
			resp.Code = he.Code
			resp.Error = he.Msg
			resp.Params = he.Params
		} else {
			resp.OK = false
			resp.Code = helperproto.ErrInternal
			resp.Error = tiruan.balasE.Error()
		}
	} else if tiruan.balas != nil {
		if b, err := json.Marshal(tiruan.balas); err == nil {
			resp.Data = b
		}
	}
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
}

func cutSpasi(b []byte) (string, []byte, bool) {
	for i, c := range b {
		if c == ' ' {
			return string(b[:i]), b[i+1:], true
		}
	}
	return "", nil, false
}

func asClientErr(err error, target **helperclient.Error) bool {
	if e, ok := err.(*helperclient.Error); ok {
		*target = e
		return true
	}
	return false
}
