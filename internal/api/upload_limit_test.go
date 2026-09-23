package api

import (
	"strconv"
	"strings"
	"testing"
)

func TestBatasUploadBerkas(t *testing.T) {
	berkas := int64(uploadMaxBerkas)
	total := int64(uploadMaxTotal)
	cases := []struct {
		nama  string
		total int64
		mau   int64
	}{
		{"permintaan kosong dibatasi batas berkas", 0, berkas},
		{"jauh di bawah batas berkas", 1024, berkas},
		{"pas di batas berkas", berkas, berkas},
		{"sisa kuota permintaan lebih kecil", total - 5, 5},
		{"sisa kuota permintaan pas sama batas berkas", total - berkas, berkas},
		{"kuota permintaan persis habis", total, 0},
		{"kuota permintaan sudah lewat", total + 1, 0},
	}
	for _, c := range cases {
		if got := batasUploadBerkas(c.total); got != c.mau {
			t.Errorf("%s: batasUploadBerkas(%d) = %d, mau %d", c.nama, c.total, got, c.mau)
		}
	}
}

// Pesan penolakan harus menyebut batas yang kena; kalau tidak, log menyesatkan.
func TestPesanBatasUploadMenyebutBatasYangKena(t *testing.T) {
	pesanTotal := pesanBatasUpload(5)
	if !strings.Contains(pesanTotal, "per permintaan") {
		t.Errorf("jatah tersisa harus dilaporkan sebagai batas per permintaan: %q", pesanTotal)
	}
	if !strings.Contains(pesanTotal, strconv.Itoa(uploadMaxTotal>>30)+" GiB") {
		t.Errorf("pesan tidak menyebut angka batas permintaan: %q", pesanTotal)
	}
	if strings.Contains(pesanTotal, "berkas melebihi") {
		t.Errorf("jatah tersisa bukan pelanggaran batas per berkas: %q", pesanTotal)
	}

	pesanBerkas := pesanBatasUpload(int64(uploadMaxBerkas))
	if !strings.Contains(pesanBerkas, "berkas melebihi batas") {
		t.Errorf("jatah penuh harus dilaporkan sebagai batas per berkas: %q", pesanBerkas)
	}
	if !strings.Contains(pesanBerkas, strconv.Itoa(uploadMaxBerkas>>30)+" GiB") {
		t.Errorf("pesan tidak menyebut angka batas berkas: %q", pesanBerkas)
	}
	if strings.Contains(pesanBerkas, "per permintaan") {
		t.Errorf("pelanggaran batas berkas tidak boleh disebut sebagai batas permintaan: %q", pesanBerkas)
	}
}
