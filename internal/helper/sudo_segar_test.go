package helper

import (
	"errors"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Status sudo tersalin ke token saat login, sedangkan keanggotaan grup bisa
// dicabut di luar panel (mis. `deluser dewi sudo`). Token membuktikan SIAPA
// pemanggilnya, bukan bahwa haknya masih ada — jadi perintah ber-sudo harus
// memeriksa ulang keadaan akun saat ini.

// gantiPembacaIdentitas memasang stub pembaca keadaan akun, dan
// mengembalikannya saat test selesai.
func gantiPembacaIdentitas(t *testing.T, fn func(string) (*userInfo, error)) {
	t.Helper()
	simpan := identitasSekarang
	t.Cleanup(func() { identitasSekarang = simpan })
	identitasSekarang = fn
}

// Inti: hak sudo yang sudah dicabut di luar panel tidak boleh diwarisi dari
// token yang diterbitkan saat hak itu masih ada.
func TestSudoDiperiksaUlangSaatGrupDicabut(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, false), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)

	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdUfwStatus,
		"token": token,
	})
	if resp.OK {
		t.Fatal("perintah ber-sudo dijalankan padahal hak sudo sudah dicabut")
	}
	if resp.Code != helperproto.ErrRequiresSudo {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrRequiresSudo)
	}
}

// Tidak bisa memastikan hak sekarang = tidak memberi hak (fail-closed).
func TestSudoGagalDibacaTidakMemberiHak(t *testing.T) {
	gantiPembacaIdentitas(t, func(string) (*userInfo, error) {
		return nil, errors.New("basis data akun tidak terbaca")
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)

	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdUfwStatus,
		"token": token,
	})
	if resp.OK {
		t.Fatal("perintah ber-sudo dijalankan padahal keadaan akun tidak terbaca")
	}
	if resp.Code != helperproto.ErrRequiresSudo {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrRequiresSudo)
	}
}

// Selama haknya masih ada, jalur itu tetap jalan — pemeriksaan ulang ini tidak
// boleh mematikan fungsi normal.
func TestSudoMasihAdaSaatHakMasihAda(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, true), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)

	if !sudoMasihAda(userUji("dewi", true)) {
		t.Fatal("hak sudo yang masih ada dilaporkan hilang")
	}
	// Command ber-sudo kini lolos tahap otorisasi token; kegagalan lebih jauh
	// (ufw tidak ada di mesin uji) bukan lagi soal hak.
	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdUfwStatus,
		"token": token,
	})
	if resp.Code == helperproto.ErrRequiresSudo || resp.Code == helperproto.ErrSesiTidakValid {
		t.Fatalf("hak sudo yang sah ditolak: kode %q (%s)", resp.Code, resp.Error)
	}
}

// Fungsi murninya juga dijaga langsung: identitas nil, dan pemilik tanpa sudo.
func TestSudoMasihAdaMenolakTanpaBukti(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, false), nil
	})
	if sudoMasihAda(nil) {
		t.Error("identitas nil dianggap berhak sudo")
	}
	if sudoMasihAda(userUji("ani", false)) {
		t.Error("pemilik yang terbaca tanpa sudo dianggap berhak")
	}
}

// Token ber-sudo tetap harus dicabut saat password/akun berubah: jalur itu
// sudah ada, dan test ini menjaga agar pemeriksaan ulang di atas tidak
// menggantikannya.
func TestTokenBerSudoHidupSampaiKedaluwarsaAtauDicabut(t *testing.T) {
	s := &Server{tokens: map[string]*sesiToken{}}
	token, _ := s.terbitkanToken(userUji("dewi", true), time.Hour)
	if _, ok := s.tokenUser(token); !ok {
		t.Fatal("token sah dianggap tidak sah")
	}
	s.cabutTokenUser("dewi")
	if _, ok := s.tokenUser(token); ok {
		t.Fatal("token masih sah setelah dicabut")
	}
}
