package helper

import (
	"strings"
	"testing"
)

// potongSampaiTerpasang memutuskan berapa commit yang benar-benar belum
// terpasang — angka yang dipakai modal Update untuk bilang "tertinggal N
// commit, dipasang sekaligus". Yang gampang salah di sini:
//
//   - `git log` menuliskan commit pendek (%h), sementara rev-parse HEAD
//     mengembalikan sha penuh; pencocokannya karena itu prefiks, bukan sama
//     dengan.
//   - commit yang sudah terpasang TIDAK ikut masuk daftar (dipotong tepat
//     sebelum barisnya), dan yang lebih baru daripadanya tetap ikut.
//   - checkout dangkal dari sumber lain tidak punya commit itu sama sekali:
//     daftarnya dikembalikan apa adanya dengan pasti=false, supaya modal tidak
//     mengaku daftar itu sebagai selisih yang persis.
func TestPotongSampaiTerpasang(t *testing.T) {
	penuhTerpasang := "c3412cb1658312caf8e486b31262bbd70a933932"

	logGit := strings.Join([]string{
		"9b82d9a commit 12",
		"7f1c4aa commit 11",
		"5d0e6bb commit 10",
		"c3412cb commit 3",
		"aaaaaaa commit 2",
		"bbbbbbb commit 1",
		"",
	}, "\n")

	t.Run("dipotong tepat sebelum commit terpasang", func(t *testing.T) {
		dapat, pasti := potongSampaiTerpasang(logGit, penuhTerpasang)
		if !pasti {
			t.Fatal("pasti harus true kalau commit terpasang ada di daftar")
		}
		if len(dapat) != 3 {
			t.Fatalf("harus 3 commit belum terpasang, dapat %d: %v", len(dapat), dapat)
		}
		if dapat[0] != "9b82d9a commit 12" {
			t.Fatalf("urutan harus terbaru dulu, dapat %q", dapat[0])
		}
		if strings.Contains(strings.Join(dapat, "\n"), "commit 2") {
			t.Fatalf("commit yang lebih lama dari versi terpasang tidak boleh ikut: %v", dapat)
		}
	})

	t.Run("sha penuh dicocokkan sebagai prefiks", func(t *testing.T) {
		// Baris pertama sudah sama dengan yang terpasang: panel tidak
		// tertinggal, dan daftarnya kosong (bukan seluruh riwayat).
		dapat, pasti := potongSampaiTerpasang(logGit, "9b82d9a684f6b497cb87948ef4dc50bff7d982b5")
		if !pasti || len(dapat) != 0 {
			t.Fatalf("harus kosong dan pasti, dapat %v pasti=%v", dapat, pasti)
		}
	})

	t.Run("commit terpasang tidak ada di daftar", func(t *testing.T) {
		dapat, pasti := potongSampaiTerpasang(logGit, "ffffffffffffffffffffffffffffffffffffffff")
		if pasti {
			t.Fatal("pasti harus false kalau commit terpasang tidak ketemu")
		}
		if len(dapat) != 6 {
			t.Fatalf("daftar dikembalikan apa adanya (6 baris), dapat %d", len(dapat))
		}
	})

	t.Run("sha pendek dari sumber lain tidak bikin panic", func(t *testing.T) {
		// rev-parse bisa mengembalikan nilai pendek/asing di checkout yang
		// rusak; fungsi ini tidak boleh mengasumsikan 40 karakter.
		dapat, pasti := potongSampaiTerpasang(logGit, "abc")
		if pasti || len(dapat) != 6 {
			t.Fatalf("diharapkan daftar apa adanya, dapat %d pasti=%v", len(dapat), pasti)
		}
		if _, pasti := potongSampaiTerpasang(logGit, ""); pasti {
			t.Fatal("tanpa versi lokal, daftar tidak boleh dianggap pasti")
		}
	})

	t.Run("log kosong", func(t *testing.T) {
		for _, isi := range []string{"", "\n\n", "   \n"} {
			dapat, pasti := potongSampaiTerpasang(isi, penuhTerpasang)
			if len(dapat) != 0 || pasti {
				t.Fatalf("log kosong (%q) harus menghasilkan daftar kosong, dapat %v", isi, dapat)
			}
		}
	})
}
