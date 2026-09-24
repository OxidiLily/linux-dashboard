package helper

import (
	"encoding/json"
	"errors"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// auth.sudo menjawab pertanyaan web app: "hak sudo pemilik token ini MASIH ada?"
//
// Web app menyalin status sudo ke baris sesinya saat login. Keanggotaan grup
// sudo bisa dicabut di luar panel (`deluser dewi sudo`), dan tanpa cara
// menanyakan keadaan terkini, salinan itu tetap dipakai sampai sesinya
// berakhir: endpoint yang butuh sudo tetap terbuka di sisi API walaupun helper
// (yang memeriksa ulang) akan menolak perintahnya.
//
// Test di sini menembak daemon lewat Unix socket sungguhan — framing, HMAC,
// dan handle() yang sama dengan produksi — karena urutan "periksa token dulu,
// baru jawab status" itu yang mudah salah.

// hasilSudo mengurai jawaban auth.sudo, dan gagal kalau helper menolaknya.
func hasilSudo(t *testing.T, resp helperproto.Response) helperproto.SudoResult {
	t.Helper()
	if !resp.OK {
		t.Fatalf("auth.sudo dijawab gagal: kode %q (%s)", resp.Code, resp.Error)
	}
	var res helperproto.SudoResult
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		t.Fatalf("jawaban auth.sudo tidak terbaca: %v (%s)", err, resp.Data)
	}
	return res
}

// Inti: token diterbitkan saat haknya masih ada, lalu keanggotaan grupnya
// dicabut di luar panel. Jawabannya harus keadaan SEKARANG, bukan salinan yang
// tersimpan di token saat login.
func TestAuthSudoMelaporkanKeadaanTerkini(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, false), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)

	res := hasilSudo(t, h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthSudo,
		"token": token,
	}))
	if res.Sudo {
		t.Fatal("auth.sudo melaporkan sudo masih ada padahal keanggotaan grup sudah dicabut")
	}
}

// Sebaliknya juga harus benar: hak yang BARU diberikan harus terlihat, supaya
// halaman yang butuh sudo tidak terus meminta user login ulang setelah
// admin menambahkannya ke grup sudo.
func TestAuthSudoMelaporkanHakYangBaruDiberikan(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, true), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", false), sesiTTL)

	res := hasilSudo(t, h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthSudo,
		"token": token,
	}))
	if !res.Sudo {
		t.Fatal("auth.sudo melaporkan sudo tidak ada padahal akunnya baru ditambahkan ke grup sudo")
	}
}

// Identitas tetap ditentukan token, bukan klaim pemanggil: permintaan tanpa
// token tidak boleh mendapat jawaban apa pun.
func TestAuthSudoTanpaTokenDitolak(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, true), nil
	})
	h := jalankanHelperSocket(t)

	resp := h.kirim(t, map[string]any{
		"cmd":      helperproto.CmdAuthSudo,
		"username": "dewi",
	})
	if resp.OK {
		t.Fatal("auth.sudo dijawab tanpa token")
	}
	if resp.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrSesiTidakValid)
	}
}

// Token yang sudah dicabut (logout, ganti password, akun diubah) juga ditolak:
// status sudo yang sudah dihitung ulang untuk sesi yang mati tidak boleh
// dilaporkan seolah sesinya masih ada.
func TestAuthSudoTokenDicabutDitolak(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, true), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)
	h.srv.cabutTokenUser("dewi")

	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthSudo,
		"token": token,
	})
	if resp.OK {
		t.Fatal("auth.sudo dijawab untuk token yang sudah dicabut")
	}
	if resp.Code != helperproto.ErrSesiTidakValid {
		t.Fatalf("kode = %q, harap %q", resp.Code, helperproto.ErrSesiTidakValid)
	}
}

// Keadaan akun yang tidak terbaca dijawab sebagai KEGAGALAN, bukan "tidak
// sudo".
//
// Pemanggilnya menyimpan jawaban ini ke baris sesi, jadi satu kegagalan baca
// yang sesaat (basis data akun sibuk, NSS bermasalah) tidak boleh mencabut hak
// sudo semua admin sekaligus. Yang menahan aksi root tetap pemeriksaan
// fail-closed di jalur eksekusi (sudoMasihAda), dan di situ "tidak bisa
// memastikan" memang berarti tidak berhak.
func TestAuthSudoGagalBacaBukanBerartiTidakSudo(t *testing.T) {
	gantiPembacaIdentitas(t, func(string) (*userInfo, error) {
		return nil, errors.New("basis data akun tidak terbaca")
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("dewi", true), sesiTTL)

	resp := h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthSudo,
		"token": token,
	})
	if resp.OK {
		t.Fatalf("auth.sudo menjawab sukses padahal keadaan akun tidak terbaca: %s", resp.Data)
	}
	if resp.Code == helperproto.ErrSesiTidakValid {
		t.Fatalf("kegagalan baca dilaporkan sebagai sesi tidak sah: %+v", resp)
	}
}

// Sesi yang identitasnya masih hidup tapi bukan sudoer tetap harus bisa
// bertanya — jawabannya "tidak", bukan penolakan. Kalau tidak, halaman yang
// di-gate requireSudo tidak bisa membedakan "tidak berhak" dari "sesi rusak",
// dan user biasa yang membuka halaman admin mendapat 401 alih-alih 403.
func TestAuthSudoSesiNonSudoerTetapDijawab(t *testing.T) {
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, false), nil
	})
	h := jalankanHelperSocket(t)
	token, _ := h.srv.terbitkanToken(userUji("ani", false), sesiTTL)

	res := hasilSudo(t, h.kirim(t, map[string]any{
		"cmd":   helperproto.CmdAuthSudo,
		"token": token,
	}))
	if res.Sudo {
		t.Fatal("user tanpa sudo dilaporkan berhak")
	}
	// auth.sudo sendiri bukan aksi privileged: ia hanya membaca keadaan.
	if _, ok := h.srv.tokenUser(token); !ok {
		t.Fatal("token sesi non-sudoer ikut tercabut setelah auth.sudo")
	}
}
