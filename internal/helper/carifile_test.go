package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pohon uji:
//
//	akar/
//	  laporan.txt
//	  catatan.md
//	  sub/
//	    LAPORAN-2024.txt
//	    dalam/
//	      laporan-lama.txt
//	      lain.log
//	  kosong/
func buatPohonUji(t *testing.T) string {
	t.Helper()
	akar := t.TempDir()
	if err := os.MkdirAll(filepath.Join(akar, "sub", "dalam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(akar, "kosong"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"laporan.txt", "catatan.md", "sub/LAPORAN-2024.txt", "sub/dalam/laporan-lama.txt", "sub/dalam/lain.log"} {
		if err := os.WriteFile(filepath.Join(akar, filepath.FromSlash(f)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return akar
}

func namaHit(hits []helperproto.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}

// jalanCari membungkus pemanggilan agar test tidak perlu menyebut tiga nilai
// kembalian di setiap tempat.
func jalanCari(akar, kueri string, saring bool, maks int) ([]helperproto.SearchHit, helperproto.SearchHasil) {
	h := cariRekursif(akar, kueri, saring, maks)
	return h.Hits, h
}

func relHit(hits []helperproto.SearchHit, nama string) string {
	for _, h := range hits {
		if h.Name == nama {
			return h.Rel
		}
	}
	return ""
}

// Setara `grep -r`: kecocokan harus ditemukan sampai ke subfolder terdalam,
// bukan hanya di folder yang sedang dibuka.
func TestCariRekursifMenembusSubfolder(t *testing.T) {
	akar := buatPohonUji(t)
	hits, hasil := jalanCari(akar, "laporan", true, 500)

	if len(hits) != 3 {
		t.Fatalf("harus menemukan 3 berkas laporan, dapat %d: %v", len(hits), namaHit(hits))
	}
	if hasil.Alasan != "" {
		t.Fatalf("pohon kecil tidak boleh berhenti lebih awal, alasan=%q", hasil.Alasan)
	}
	// Berkas di kedalaman dua harus ikut, dan lokasinya relatif ke akar.
	if rel := relHit(hits, "laporan-lama.txt"); rel != filepath.FromSlash("sub/dalam/laporan-lama.txt") {
		t.Fatalf("rel laporan-lama.txt = %q, harap sub/dalam/laporan-lama.txt", rel)
	}
}

func TestCariRekursifMengabaikanKapital(t *testing.T) {
	akar := buatPohonUji(t)
	hits, _ := jalanCari(akar, "LAPORAN", true, 500)
	if len(hits) != 3 {
		t.Fatalf("kapital harus diabaikan, dapat %d: %v", len(hits), namaHit(hits))
	}
}

func TestCariRekursifKueriKosongTidakMenyapuApaPun(t *testing.T) {
	akar := buatPohonUji(t)
	// Kueri kosong = tidak ada hasil. Mengembalikan SELURUH isi pohon akan
	// membuat membuka pencarian di folder besar langsung membanjiri UI.
	for _, q := range []string{"", "   "} {
		if hits, _ := jalanCari(akar, q, true, 500); len(hits) != 0 {
			t.Fatalf("kueri %q harus menghasilkan 0 baris, dapat %d", q, len(hits))
		}
	}
}

func TestCariRekursifMenghormatiBatasHasil(t *testing.T) {
	akar := t.TempDir()
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(akar, "berkas"+string(rune('a'+i))+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, hasil := jalanCari(akar, "berkas", true, 5)
	if len(hits) != 5 {
		t.Fatalf("hasil harus dibatasi 5, dapat %d", len(hits))
	}
	if hasil.Alasan != AlasanHasil {
		t.Fatalf("alasan harus %q, dapat %q", AlasanHasil, hasil.Alasan)
	}
	if !hasil.Truncated {
		t.Fatalf("pemotongan harus dinyatakan lewat Truncated")
	}
}

func TestCariRekursifMenemukanDirektoriJuga(t *testing.T) {
	akar := buatPohonUji(t)
	hits, _ := jalanCari(akar, "dalam", true, 500)
	if len(hits) != 1 || !hits[0].IsDir {
		t.Fatalf("folder yang cocok harus ikut ditemukan, dapat %v", namaHit(hits))
	}
}

// Folder tanpa izin baca tidak boleh menggagalkan seluruh pencarian — sama
// seperti `grep -r` yang melaporkan dan melanjutkan.
func TestCariRekursifMelewatiFolderTanpaIzin(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root melewati pemeriksaan izin")
	}
	akar := buatPohonUji(t)
	tertutup := filepath.Join(akar, "tertutup")
	if err := os.MkdirAll(tertutup, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tertutup, 0o755) })

	hits, _ := jalanCari(akar, "laporan", true, 500)
	if len(hits) != 3 {
		t.Fatalf("folder tanpa izin harus dilewati, bukan menggagalkan: %v", namaHit(hits))
	}
}

func TestCariRekursifTidakMengikutiSymlinkDirektori(t *testing.T) {
	akar := buatPohonUji(t)
	// Symlink yang menunjuk ke akar sendiri: kalau diikuti, penelusuran tidak
	// pernah selesai. Pola ini yang bikin `find -L` menggantung.
	if err := os.Symlink(akar, filepath.Join(akar, "sub", "putar")); err != nil {
		t.Skipf("symlink tidak didukung: %v", err)
	}
	hits, _ := jalanCari(akar, "laporan", true, 500)
	// Tetap 3: symlink yang menunjuk direktori tidak ditelusuri.
	if len(hits) != 3 {
		t.Fatalf("symlink direktori tidak boleh ditelusuri, dapat %d: %v", len(hits), namaHit(hits))
	}
}

func TestCariRekursifMencocokkanSubstringBukanHanyaAwalan(t *testing.T) {
	akar := buatPohonUji(t)
	hits, _ := jalanCari(akar, "2024", true, 500)
	if len(hits) != 1 || hits[0].Name != "LAPORAN-2024.txt" {
		t.Fatalf("substring harus cocok di tengah nama, dapat %v", namaHit(hits))
	}
}

func TestCariRekursifMenghitungDirektoriYangDikunjungi(t *testing.T) {
	akar := buatPohonUji(t)
	hasil := cariRekursif(akar, "laporan", true, 500)
	// akar + sub + sub/dalam + kosong
	if hasil.Dirs != 4 {
		t.Fatalf("direktori dikunjungi = %d, harap 4", hasil.Dirs)
	}
}

func TestBatasiKueriCari(t *testing.T) {
	if got := batasiKueri(strings.Repeat("x", 500)); len([]rune(got)) != maksKueriCari {
		t.Fatalf("kueri harus dipotong ke %d rune, dapat %d", maksKueriCari, len([]rune(got)))
	}
	if got := batasiKueri("  laporan  "); got != "laporan" {
		t.Fatalf("spasi tepi harus dibuang, dapat %q", got)
	}
}

// Pseudo-filesystem tidak boleh ditelusuri: isinya dibangkitkan kernel, basi
// saat dikirim, dan membanjiri hasil dengan nama seperti "fd"/"task".
func TestPseudoFsDikenali(t *testing.T) {
	for _, p := range []string{"/proc", "/sys"} {
		if !pseudoFs(p) {
			t.Errorf("%s harus dikenali sebagai pseudo-filesystem", p)
		}
	}
	// Folder data user sendiri TIDAK boleh dianggap pseudo — kalau salah,
	// pencarian di home berhenti tanpa hasil sama sekali.
	if pseudoFs(t.TempDir()) {
		t.Errorf("direktori biasa (tmp) tidak boleh dianggap pseudo-filesystem")
	}
}

// /dev bukan devtmpfs di sebagian sistem (container, WSL): di mesin ini
// `stat -f %T /dev` menjawab tmpfs. Karena itu pengenalannya lewat ISI —
// direktori yang memuat device node — bukan lewat jenis filesystem.
func TestDirSistemMengenaliDirektoriPerangkat(t *testing.T) {
	ents, err := os.ReadDir("/dev")
	if err != nil {
		t.Skipf("/dev tidak terbaca: %v", err)
	}
	if !dirSistem("/dev", ents) {
		t.Fatalf("/dev harus dikenali sebagai direktori sistem")
	}

	// Direktori data biasa tidak boleh dikenali sebagai direktori sistem:
	// salah di sini membuat pencarian di folder user berhenti diam-diam.
	akar := buatPohonUji(t)
	biasa, err := os.ReadDir(akar)
	if err != nil {
		t.Fatal(err)
	}
	if dirSistem(akar, biasa) {
		t.Fatalf("direktori data biasa tidak boleh dianggap direktori sistem")
	}

	// tmpfs TANPA device node (mis. /tmp, /run) juga tidak boleh dianggap
	// sistem: home di sebagian pemasangan memakai tmpfs.
	tmps, err := os.ReadDir("/tmp")
	if err == nil && dirSistem("/tmp", tmps) {
		t.Fatalf("/tmp tidak boleh dianggap direktori sistem")
	}
}

// Penelusuran di dalam /proc tidak boleh menghabiskan seluruh jatah folder:
// akarnya dibaca, anak-anaknya tidak ditelusuri.
func TestCariTidakMenelusuriPseudoFs(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc tidak ada")
	}
	hasil := cariRekursif("/proc", "e", false, 500)
	// /proc sendiri + beberapa proses yang sempat dibaca, bukan puluhan ribu
	// direktori yang ada di dalam pohonnya.
	if hasil.Dirs > 50 {
		t.Fatalf("/proc seharusnya tidak ditelusuri dalam-dalam, Dirs=%d", hasil.Dirs)
	}
}

// Penelusuran TIDAK boleh menurun ke /dev. Diuji lewat perilaku cariRekursif,
// bukan lewat dirSistem langsung: yang penting adalah apakah fungsinya
// benar-benar dipakai di jalur penelusuran.
func TestCariTidakMenurunKeDev(t *testing.T) {
	if _, err := os.Stat("/dev"); err != nil {
		t.Skip("/dev tidak ada")
	}
	// Dijalankan sebagai root? Izinkan: yang diuji bukan izin, tapi apakah
	// penelusuran memutuskan untuk menurun.
	hasil := cariRekursif("/dev", "fd", false, 500)
	for _, h := range hasil.Hits {
		if strings.HasPrefix(h.Rel, "shm/") || strings.HasPrefix(h.Rel, "pts/") ||
			strings.HasPrefix(h.Rel, "mqueue/") || strings.HasPrefix(h.Rel, "hugepages/") {
			t.Fatalf("tidak boleh menurun ke dalam /dev: %s", h.Rel)
		}
	}
	// Dirs harus kecil (hanya /dev sendiri), bukan pohon di bawahnya.
	if hasil.Dirs > 1 {
		t.Fatalf("/dev tidak boleh ditelusuri, Dirs=%d", hasil.Dirs)
	}
}

// Plafon waktu harus menghentikan penelusuran dan MELAPOR, bukan menggantung.
func TestCariRekursifMenghormatiBatasWaktu(t *testing.T) {
	akar := buatPohonUji(t)
	asli := batasWaktuCari
	// Negatif = sudah lewat sejak awal; penelusuran harus berhenti di
	// pemeriksaan pertama.
	batasWaktuCari = -1
	t.Cleanup(func() { batasWaktuCari = asli })

	hasil := cariRekursif(akar, "laporan", true, 500)
	if hasil.Alasan != AlasanWaktu {
		t.Fatalf("alasan harus %q, dapat %q", AlasanWaktu, hasil.Alasan)
	}
	if len(hasil.Hits) != 0 {
		t.Fatalf("tidak boleh ada hasil saat waktu sudah habis, dapat %d", len(hasil.Hits))
	}
}
