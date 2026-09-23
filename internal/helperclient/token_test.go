package helperclient

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Client helper harus mengirim capability token pada SETIAP permintaan, dan
// tidak boleh mengirim identitas apa pun yang bisa disalahartikan helper
// sebagai dasar otorisasi. Test di sini memakai socket Unix sungguhan supaya
// yang diperiksa adalah byte yang benar-benar keluar dari client.

type perekam struct {
	mu  sync.Mutex
	req []helperproto.Request
}

func (p *perekam) catat(r helperproto.Request) {
	p.mu.Lock()
	p.req = append(p.req, r)
	p.mu.Unlock()
}

func (p *perekam) terakhir(t *testing.T) helperproto.Request {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.req) == 0 {
		t.Fatal("helper tiruan tidak menerima permintaan")
	}
	return p.req[len(p.req)-1]
}

// pasangClientUji menyalakan helper tiruan yang mencatat isi permintaan lalu
// membalas sesuai balasE (kosong = sukses).
func pasangClientUji(t *testing.T, rekam *perekam, balasE *helperproto.Response) *Client {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	secretPath := filepath.Join(dir, "secret.key")
	if err := os.WriteFile(secretPath, []byte("secret-uji-client\n"), 0o600); err != nil {
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
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				line, err := br.ReadBytes('\n')
				if err != nil {
					return
				}
				var req helperproto.Request
				for i, b := range line {
					if b == ' ' {
						_ = json.Unmarshal(line[i+1:], &req)
						break
					}
				}
				rekam.catat(req)
				resp := helperproto.Response{OK: true}
				if balasE != nil {
					resp = *balasE
				}
				b, _ := json.Marshal(resp)
				_, _ = c.Write(append(b, '\n'))
			}(conn)
		}
	}()

	c, err := New(sock, secretPath, "")
	if err != nil {
		t.Fatalf("client helper: %v", err)
	}
	return c
}

func TestClientKirimTokenPadaCall(t *testing.T) {
	rekam := &perekam{}
	c := pasangClientUji(t, rekam, nil)

	if err := c.Call(helperproto.CmdSvcAction, "token-sesi-ani",
		helperproto.ServiceArgs{Name: "nginx", Action: "restart"}, nil); err != nil {
		t.Fatalf("Call gagal: %v", err)
	}

	req := rekam.terakhir(t)
	if req.Cmd != helperproto.CmdSvcAction {
		t.Fatalf("cmd = %q", req.Cmd)
	}
	if req.Token != "token-sesi-ani" {
		t.Fatalf("token = %q, harap token yang diberikan pemanggil", req.Token)
	}
	// Nama user yang diklaim tidak boleh ikut terkirim: helper tidak lagi
	// menerima identitas dari pemanggil, dan mengirimkannya hanya mengundang
	// kode baru untuk mempercayainya lagi.
	if req.Username != "" {
		t.Fatalf("username terkirim = %q, harap kosong", req.Username)
	}
}

func TestClientKirimTokenPadaStream(t *testing.T) {
	rekam := &perekam{}
	c := pasangClientUji(t, rekam, nil)

	st, err := c.Stream(helperproto.CmdFileRead, "token-sesi-stream",
		helperproto.ReadArgs{Path: "/home/ani/berkas.txt"})
	if err != nil {
		t.Fatalf("Stream gagal: %v", err)
	}
	defer st.Close()

	req := rekam.terakhir(t)
	if req.Cmd != helperproto.CmdFileRead {
		t.Fatalf("cmd = %q", req.Cmd)
	}
	if req.Token != "token-sesi-stream" {
		t.Fatalf("token pada stream = %q", req.Token)
	}
	if req.Username != "" {
		t.Fatalf("username pada stream = %q, harap kosong", req.Username)
	}
}

// Token kosong tetap dikirim sebagai kosong — client tidak boleh mengarang
// identitas, dan helper yang menolaknya adalah perilaku yang benar.
func TestClientTokenKosongTidakDigantiIdentitas(t *testing.T) {
	rekam := &perekam{}
	c := pasangClientUji(t, rekam, nil)

	if err := c.Call(helperproto.CmdCronGet, "", nil, nil); err != nil {
		t.Fatalf("Call gagal: %v", err)
	}
	req := rekam.terakhir(t)
	if req.Token != "" || req.Username != "" {
		t.Fatalf("permintaan = %+v, harap tanpa token dan tanpa username", req)
	}
}

// Kode sesi tidak sah dari helper harus sampai apa adanya ke layer API, yang
// memetakannya ke HTTP 401.
func TestClientMeneruskanKodeSesiTidakValid(t *testing.T) {
	rekam := &perekam{}
	c := pasangClientUji(t, rekam, &helperproto.Response{
		OK:    false,
		Code:  helperproto.ErrSesiTidakValid,
		Error: "sesi tidak valid atau sudah berakhir — login ulang diperlukan",
	})

	err := c.Call(helperproto.CmdCronGet, "token-basi", nil, nil)
	if err == nil {
		t.Fatal("kegagalan helper tidak dilaporkan")
	}
	if Code(err) != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", Code(err), helperproto.ErrSesiTidakValid)
	}
}
