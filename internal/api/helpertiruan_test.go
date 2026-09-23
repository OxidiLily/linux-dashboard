package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// helperTiruan mencatat command yang benar-benar diterima, lalu membalas
// dengan nilai yang disuntikkan test.
type helperTiruan struct {
	mu       sync.Mutex
	cmd      string
	username string
	args     []byte
	// cmds mencatat SEMUA command yang diterima, berurutan. Satu permintaan
	// HTTP bisa memanggil helper lebih dari sekali (mis. upload: file.write
	// per berkas, lalu file.remove untuk berkas parsial), dan yang justru
	// perlu diuji adalah panggilan terakhir itu.
	cmds []string
	// paths mencatat argumen path dari tiap command, berurutan.
	paths []string
	// balas adalah Data yang dikirim balik helper (di-marshal ke JSON).
	balas any
	// balasE, kalau diisi, dikirim sebagai kegagalan helper berkode.
	balasE error
}

// catat menyimpan satu command yang diterima.
func (h *helperTiruan) catat(cmd, username string, args []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cmd, h.username, h.args = cmd, username, args
	h.cmds = append(h.cmds, cmd)
	var pa helperproto.PathArgs
	if json.Unmarshal(args, &pa) == nil && pa.Path != "" {
		h.paths = append(h.paths, pa.Path)
	}
}

// riwayat mengembalikan salinan daftar command yang diterima.
func (h *helperTiruan) riwayat() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.cmds...)
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

	// Tanpa lokasi lama: helper tiruan ini selalu menulis secret di path uji.
	hc, err := helperclient.New(sock, secretPath, "")
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
	var cmd string
	if _, payload, ok := cutSpasi(line); ok {
		var req helperproto.Request
		if json.Unmarshal(payload, &req) == nil {
			cmd = req.Cmd
			tiruan.catat(req.Cmd, req.Username, req.Args)
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

	// Jalur stream (file.write): setelah response awal, klien mengirim byte
	// berkas sampai menutup arah tulis, lalu MENUNGGU konfirmasi akhir.
	// Tanpa meniru dua fase ini, Stream.Selesai() di klien menggantung dan
	// jalur upload tidak bisa diuji sama sekali.
	if cmd == helperproto.CmdFileWrite {
		_, _ = io.Copy(io.Discard, br)
		akh, _ := json.Marshal(helperproto.Response{OK: true})
		_, _ = conn.Write(append(akh, '\n'))
	}
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
