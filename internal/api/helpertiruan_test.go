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
	mu  sync.Mutex
	cmd string
	// username adalah field klaim yang dikirim klien. Helper sungguhan TIDAK
	// memakainya lagi untuk memutuskan hak; ia hanya dicatat supaya test bisa
	// membuktikan klien tidak lagi mengandalkannya.
	username string
	// token adalah capability token yang dikirim klien. Inilah yang dipakai
	// helper sungguhan untuk menentukan identitas pemanggil.
	token string
	args  []byte
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
	// periksaToken, kalau dinyalakan, membuat tiruan ini berperilaku seperti
	// helper sungguhan: permintaan non-login dengan token tidak dikenal
	// ditolak sebagai session_invalid. Bawaan mati supaya test lama yang
	// sengaja membuat sesi tanpa token tetap menguji hal lain.
	periksaToken bool
	// tokenSah adalah daftar token yang diterima saat periksaToken menyala.
	tokenSah map[string]bool
	// sudo adalah jawaban command auth.sudo. nil berarti tiruan ini
	// berperilaku seperti helper versi lama yang belum mengenal auth.sudo: ia
	// menjawab gagal, dan web app harus membiarkan status sudo yang tersimpan
	// apa adanya.
	sudo *bool
	// mati meniru helper yang TIDAK BISA DIHUBUNGI: koneksinya diterima lalu
	// ditutup tanpa jawaban apa pun (setara helper yang sedang restart).
	mati bool
}

// riwayatOperasi mengembalikan command yang diterima SELAIN probe status sudo
// milik middleware autentikasi.
//
// Probe itu berjalan sebelum handler mana pun, jadi tanpa disaring ia akan
// tampak seperti panggilan operasi pada test yang justru memeriksa bahwa satu
// permintaan HTTP tidak pernah sampai ke helper.
func (h *helperTiruan) riwayatOperasi() []string {
	var out []string
	for _, c := range h.riwayat() {
		if c != helperproto.CmdAuthSudo {
			out = append(out, c)
		}
	}
	return out
}

// catat menyimpan satu command yang diterima.
func (h *helperTiruan) catat(cmd, username, token string, args []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cmd, h.username, h.token, h.args = cmd, username, token, args
	h.cmds = append(h.cmds, cmd)
	var pa helperproto.PathArgs
	if json.Unmarshal(args, &pa) == nil && pa.Path != "" {
		h.paths = append(h.paths, pa.Path)
	}
}

// tolakToken melaporkan apakah token yang diterima harus ditolak. Dijalankan
// dengan mu terkunci oleh pemanggil.
func (h *helperTiruan) tolakToken(cmd, token string) bool {
	if !h.periksaToken || cmd == helperproto.CmdAuthLogin {
		return false
	}
	return !h.tokenSah[token]
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
	var tolak bool
	if _, payload, ok := cutSpasi(line); ok {
		var req helperproto.Request
		if json.Unmarshal(payload, &req) == nil {
			cmd = req.Cmd
			tiruan.mu.Lock()
			tolak = tiruan.tolakToken(req.Cmd, req.Token)
			tiruan.mu.Unlock()
			tiruan.catat(req.Cmd, req.Username, req.Token, req.Args)
		}
	}
	tiruan.mu.Lock()
	mati := tiruan.mati
	tiruan.mu.Unlock()
	if mati {
		// Helper tidak bisa dihubungi: koneksi ditutup tanpa jawaban. Klien
		// harus gagal membaca, bukan menerima jawaban kosong yang bisa
		// disalahartikan sebagai "status sudo tidak ada".
		return
	}
	resp := helperproto.Response{OK: true}
	switch {
	case tolak:
		// Perilaku helper sungguhan: klaim username diabaikan, token yang
		// tidak dikenal ditolak sebagai sesi tidak sah.
		resp.OK = false
		resp.Code = helperproto.ErrSesiTidakValid
		resp.Error = "sesi tidak valid atau sudah berakhir — login ulang diperlukan"
	case cmd == helperproto.CmdAuthSudo:
		tiruan.mu.Lock()
		jawaban := tiruan.sudo
		tiruan.mu.Unlock()
		if jawaban == nil {
			// Helper versi lama: command ini belum ada. Web app harus
			// membiarkan status sudo yang tersimpan apa adanya.
			resp.OK = false
			resp.Code = helperproto.ErrInvalid
			resp.Error = "command tidak dikenal: " + helperproto.CmdAuthSudo
		} else if b, err := json.Marshal(helperproto.SudoResult{Sudo: *jawaban}); err == nil {
			resp.Data = b
		}
	case tiruan.balasE != nil:
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
	case tiruan.balas != nil:
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
