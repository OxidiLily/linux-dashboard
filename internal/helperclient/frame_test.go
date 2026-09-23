package helperclient

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Response helper belum dibatasi di sisi web, padahal isinya keluaran command
// yang berjalan sebagai root dan panjangnya ditentukan mesin. ReadBytes akan
// mengalokasikan sebesar apa pun sampai ketemu newline, jadi batasnya harus
// ditegakkan saat membaca.
func TestBacaBarisRespMenolakResponseTerlaluBesar(t *testing.T) {
	besar := strings.Repeat("x", batasFrameResp+16) + "\n"
	_, err := bacaBarisResp(bufio.NewReaderSize(strings.NewReader(besar), 4096))
	if err == nil {
		t.Fatal("response melebihi batas diterima")
	}
	if !strings.Contains(err.Error(), "melebihi") {
		t.Fatalf("error = %v, ingin menyebut batas ukuran", err)
	}
}

func TestBacaBarisRespMenerimaBarisBiasaDanPanjang(t *testing.T) {
	for _, isi := range []string{"{\"ok\":true}\n", strings.Repeat("y", 1<<20) + "\n"} {
		got, err := bacaBarisResp(bufio.NewReaderSize(strings.NewReader(isi), 4096))
		if err != nil {
			t.Fatalf("baris sah ditolak: %v", err)
		}
		if string(got) != isi {
			t.Fatalf("panjang hasil = %d, ingin %d", len(got), len(isi))
		}
	}
}

// Instalasi yang belum memindahkan secret harus tetap bisa jalan: helper dan web
// membaca lokasi yang sama, dan kalau salah satu sisi menolak start sementara
// sisi lain jalan, panel berakhir tanpa web app sama sekali.
func TestNewMemakaiSecretLamaKalauYangBaruBelumAda(t *testing.T) {
	dir := t.TempDir()
	lama := filepath.Join(dir, "lama.key")
	if err := os.WriteFile(lama, []byte("secret-lama-yang-panjang-sekali\n"), 0o600); err != nil {
		t.Fatalf("tulis secret lama: %v", err)
	}
	baru := filepath.Join(dir, "belum-ada", "secret.key")

	c, err := New("/tmp/tidak-dipakai.sock", baru, lama)
	if err != nil {
		t.Fatalf("client helper gagal dibuat: %v", err)
	}
	if string(c.secret) != "secret-lama-yang-panjang-sekali" {
		t.Fatalf("secret = %q", c.secret)
	}
}

func TestNewGagalKalauTidakAdaSecretSamaSekali(t *testing.T) {
	dir := t.TempDir()
	if _, err := New("/tmp/tidak-dipakai.sock",
		filepath.Join(dir, "a.key"), filepath.Join(dir, "b.key")); err == nil {
		t.Fatal("client helper dibuat tanpa secret")
	}
}
