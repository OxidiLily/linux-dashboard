package helper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Token capability adalah satu-satunya dasar otorisasi helper daemon.
//
// Test di berkas ini menembak daemon lewat Unix socket SUNGGUHAN (framing,
// HMAC, dan handle() yang sama dengan produksi), bukan dengan memanggil
// dispatch() langsung — justru urutan "periksa token dulu, baru jalankan
// command" itulah yang mudah salah dan yang harus dijaga.
//
// Permintaan dibangun sebagai JSON mentah (map), bukan lewat
// helperproto.Request: yang diuji adalah kontrak di kabelnya — nama field
// "token"/"username" — bukan susunan struct di sisi Go.

type helperSocketUji struct {
	sock   string
	secret []byte
	srv    *Server
}

// jalankanHelperSocket menyalakan Server di socket sementara. Serve() sengaja
// tidak dipakai supaya test tidak ikut memanaskan probe komponen di latar;
// yang dijalankan tetap handle() yang sama dengan produksi.
func jalankanHelperSocket(t *testing.T) *helperSocketUji {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen socket uji: %v", err)
	}
	s := &Server{
		socketPath: sock,
		secret:     []byte("secret-uji-token"),
		ln:         ln,
		seenNonce:  map[string]time.Time{},
		tokens:     map[string]*sesiToken{},
	}
	t.Cleanup(func() { _ = s.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return &helperSocketUji{sock: sock, secret: s.secret, srv: s}
}

// kirim mengirim satu permintaan bertanda tangan dan mengembalikan responsnya.
func (h *helperSocketUji) kirim(t *testing.T, fields map[string]any) helperproto.Response {
	t.Helper()
	if _, ok := fields["ts"]; !ok {
		fields["ts"] = time.Now().Unix()
	}
	if _, ok := fields["nonce"]; !ok {
		fields["nonce"] = fmt.Sprintf("n-%d", time.Now().UnixNano())
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal permintaan: %v", err)
	}
	line := helperproto.Sign(h.secret, payload) + " " + string(payload) + "\n"

	conn, err := net.DialTimeout("unix", h.sock, 5*time.Second)
	if err != nil {
		t.Fatalf("dial helper: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatalf("tulis permintaan: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respLine, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("baca respons helper: %v", err)
	}
	var resp helperproto.Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("respons helper bukan JSON: %v (%s)", err, respLine)
	}
	return resp
}

func userUji(nama string, sudo bool) *userInfo {
	return &userInfo{
		Name: nama, UID: 1000, GID: 1000,
		Home: "/home/" + nama, Shell: "/bin/bash", Sudo: sudo,
	}
}

// Permintaan tanpa token harus ditolak, apa pun command-nya.
func TestTokenPermintaanTanpaTokenDitolak(t *testing.T) {
	h := jalankanHelperSocket(t)
	resp := h.kirim(t, map[string]any{
		"cmd":      helperproto.CmdCronGet,
		"username": "ani",
	})
	if resp.OK {
		t.Fatal("permintaan tanpa token diterima")
	}
	if resp.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrSesiTidakValid)
	}
}

// Ini inti perbaikan: pemanggil yang mengaku sebagai user sudo TANPA token
// tidak mendapat hak apa pun. Sebelum token ada, field `username` inilah yang
// dipercaya helper dan klaim "root" cukup untuk membuka command ber-sudo.
func TestTokenKlaimUsernameSudoTanpaTokenTidakMemberiHak(t *testing.T) {
	h := jalankanHelperSocket(t)
	// ufw.status ada di daftar sudoRequired: satu-satunya jalur lolosnya
	// adalah identitas dari token yang berstatus sudo.
	resp := h.kirim(t, map[string]any{
		"cmd":      helperproto.CmdUfwStatus,
		"username": "root",
	})
	if resp.OK {
		t.Fatal("klaim username root tanpa token justru dijalankan")
	}
	if resp.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q (klaim username tidak boleh menghasilkan %q)",
			resp.Code, helperproto.ErrSesiTidakValid, helperproto.ErrRequiresSudo)
	}
}

// Token harus benar-benar ada di peta helper; token karangan ditolak tanpa
// membocorkan apakah token itu pernah ada.
func TestTokenAsingDitolak(t *testing.T) {
	h := jalankanHelperSocket(t)
	resp := h.kirim(t, map[string]any{
		"cmd":      helperproto.CmdCronGet,
		"username": "ani",
		"token":    "token-karangan-yang-tidak-pernah-diterbitkan",
	})
	if resp.OK {
		t.Fatal("token asing diterima")
	}
	if resp.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrSesiTidakValid)
	}
}

// Sudo ditentukan oleh token, bukan oleh nama yang diklaim pemanggil: token
// milik user biasa tetap ditolak pada command ber-sudo walaupun permintaannya
// mengaku sebagai root.
func TestTokenNonSudoerTetapDitolakWalauMengakuRoot(t *testing.T) {
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("ani", false), sesiTTL)

	resp := h.kirim(t, map[string]any{
		"cmd":      helperproto.CmdUfwStatus,
		"username": "root",
		"token":    token,
	})
	if resp.OK {
		t.Fatal("token non-sudoer lolos ke command ber-sudo")
	}
	if resp.Code != helperproto.ErrRequiresSudo {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrRequiresSudo)
	}
}

// auth.logout mencabut token yang dipakai permintaan itu, dan setelah itu
// token yang sama tidak berlaku lagi.
func TestTokenDicabutLewatLogoutLangsungDitolak(t *testing.T) {
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("ani", false), sesiTTL)

	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthLogout,
		"token": token,
	})
	if !resp.OK {
		t.Fatalf("logout dengan token sah gagal: %+v", resp)
	}

	lain := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthLogout,
		"token": token,
	})
	if lain.OK {
		t.Fatal("token yang sudah dicabut masih dianggap sah")
	}
	if lain.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", lain.Code, helperproto.ErrSesiTidakValid)
	}

	// Logout tanpa token juga ditolak: pencabutan bukan jalur bebas token.
	tanpa := h.kirim(t, map[string]any{"cmd": helperproto.CmdAuthLogout})
	if tanpa.OK || tanpa.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("logout tanpa token = %+v, harap ditolak sebagai %q", tanpa, helperproto.ErrSesiTidakValid)
	}
}

// Token yang sah memberi identitas yang BENAR — termasuk status sudo — dan
// identitas itu diambil dari token, bukan dari field username.
func TestTokenSahMemberiIdentitasDariToken(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	sudoer := userUji("dewi", true)
	sudoer.Groups = []uint32{27}
	tokenSudo, _ := s.terbitkanToken(sudoer, sesiTTL)
	biasa, _ := s.terbitkanToken(userUji("ani", false), sesiTTL)

	u, ok := s.tokenUser(tokenSudo)
	if !ok {
		t.Fatal("token sah dianggap tidak sah")
	}
	if u.Name != "dewi" || !u.Sudo || u.Home != "/home/dewi" {
		t.Fatalf("identitas dari token = %+v, harap dewi/sudo/home", u)
	}
	// Identitas yang dipakai otorisasi harus identitas LENGKAP (UID/grup),
	// karena worker memakainya untuk menurunkan privilege.
	if u.UID != 1000 || len(u.Groups) != 1 {
		t.Fatalf("identitas token kehilangan UID/grup: %+v", u)
	}

	u2, ok := s.tokenUser(biasa)
	if !ok {
		t.Fatal("token user biasa dianggap tidak sah")
	}
	if u2.Name != "ani" || u2.Sudo {
		t.Fatalf("identitas = %+v, harap ani tanpa sudo", u2)
	}

	if _, ok := s.tokenUser(""); ok {
		t.Error("token kosong dianggap sah")
	}
	if _, ok := s.tokenUser("tidak-pernah-ada"); ok {
		t.Error("token tak dikenal dianggap sah")
	}
}

// Token kedaluwarsa tidak berlaku, dan tidak dibiarkan menumpuk di peta.
func TestTokenKedaluwarsaDitolak(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	token, _ := s.terbitkanToken(userUji("ani", false), -time.Second)
	if _, ok := s.tokenUser(token); ok {
		t.Fatal("token kedaluwarsa masih sah")
	}
	s.tokenMu.Lock()
	_, masihAda := s.tokens[token]
	s.tokenMu.Unlock()
	if masihAda {
		t.Error("token kedaluwarsa tidak dibuang dari peta")
	}
}

// Ganti password / akun diubah / akun dihapus mencabut token user itu.
func TestCabutTokenUserMematikanSeluruhTokenUser(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	t1, _ := s.terbitkanToken(userUji("ani", true), sesiTTL)
	t2, _ := s.terbitkanToken(userUji("ani", true), sesiTTL)
	tBudi, _ := s.terbitkanToken(userUji("budi", false), sesiTTL)

	s.cabutTokenUser("ani")
	if _, ok := s.tokenUser(t1); ok {
		t.Error("token ani #1 masih sah setelah pencabutan")
	}
	if _, ok := s.tokenUser(t2); ok {
		t.Error("token ani #2 masih sah setelah pencabutan")
	}
	if _, ok := s.tokenUser(tBudi); !ok {
		t.Error("token user lain ikut tercabut")
	}
}

// Ganti password sendiri: token yang sedang dipakai tetap hidup, token lain
// milik user yang sama mati.
func TestCabutTokenUserKecualiMenyisakanTokenBerjalan(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	aktif, _ := s.terbitkanToken(userUji("ani", true), sesiTTL)
	lain, _ := s.terbitkanToken(userUji("ani", true), sesiTTL)

	s.cabutTokenUserKecuali("ani", aktif)
	if _, ok := s.tokenUser(aktif); !ok {
		t.Error("token yang sedang dipakai ikut tercabut")
	}
	if _, ok := s.tokenUser(lain); ok {
		t.Error("token sesi lain masih hidup")
	}
}

// Token yang diterbitkan helper harus acak dan panjang: token yang bisa
// ditebak sama saja dengan tidak ada token.
func TestTokenAcakDanTidakBerulang(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	pertama, _ := s.terbitkanToken(userUji("ani", false), sesiTTL)
	if len(pertama) != 64 {
		t.Fatalf("panjang token = %d, harap 64 karakter hex (32 byte acak)", len(pertama))
	}
	for i := 0; i < 32; i++ {
		lagi, _ := s.terbitkanToken(userUji("ani", false), sesiTTL)
		if lagi == pertama {
			t.Fatal("token berulang")
		}
	}
}
