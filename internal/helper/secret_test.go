package helper

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func tulisSecret(t *testing.T, path, isi string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(isi), mode); err != nil {
		t.Fatalf("tulis secret: %v", err)
	}
	// os.WriteFile tunduk pada umask; mode harus pasti supaya yang diuji
	// memang pemeriksaan mode, bukan kebetulan umask mesin uji.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

// Symlink tidak boleh diikuti. Siapa pun yang punya akses tulis ke direktori
// secret bisa memasang symlink ke berkas pilihannya; kalau helper mengikutinya,
// secret penyerang yang dipakai tanpa jejak.
func TestSecretMenolakSymlink(t *testing.T) {
	dir := t.TempDir()
	nyata := filepath.Join(dir, "nyata")
	tulisSecret(t, nyata, "0123456789abcdef0123456789abcdef", 0o600)
	tautan := filepath.Join(dir, "tautan")
	if err := os.Symlink(nyata, tautan); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := loadSecretFile(tautan, os.Getuid()); err == nil {
		t.Fatal("secret dibaca lewat symlink")
	}
}

// Berkas yang bisa ditulis grup atau user lain bukan lagi rahasia: pihak lain
// cukup menimpa isinya dengan secret pilihannya, dan helper memuatnya.
func TestSecretMenolakBerkasYangBisaDitulisPihakLain(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o660, 0o606, 0o666, 0o622} {
		path := filepath.Join(dir, "secret-"+mode.String()[1:])
		tulisSecret(t, path, "0123456789abcdef0123456789abcdef", mode)
		if _, err := loadSecretFile(path, os.Getuid()); err == nil {
			t.Fatalf("secret mode %04o diterima", mode)
		}
	}
}

func TestSecretMenolakPemilikLain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	tulisSecret(t, path, "0123456789abcdef0123456789abcdef", 0o600)
	if _, err := loadSecretFile(path, os.Getuid()+1); err == nil {
		t.Fatal("secret milik uid lain diterima")
	}
}

func TestSecretMenolakIsiTerlaluPendek(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	tulisSecret(t, path, "pendek", 0o600)
	if _, err := loadSecretFile(path, os.Getuid()); err == nil {
		t.Fatal("secret terlalu pendek diterima")
	}
}

func TestSecretMenerimaBerkasYangBenar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	isi := "0123456789abcdef0123456789abcdef"
	tulisSecret(t, path, isi+"\n", 0o600)

	got, err := loadSecretFile(path, os.Getuid())
	if err != nil {
		t.Fatalf("secret sah ditolak: %v", err)
	}
	if string(got) != isi {
		t.Fatalf("isi secret = %q, ingin %q", got, isi)
	}
}

// Instalasi lama menyimpan secret di dalam state dir web. Helper harus
// MENYALINNYA ke lokasi baru saat start berikutnya: kalau hanya memperingatkan,
// lokasi yang bisa ditulis user service web itu dipakai selamanya, dan tujuan
// pemisahan direktori tidak pernah tercapai.
func TestMigrasiSecretLamaKeLokasiBaru(t *testing.T) {
	dir := t.TempDir()
	lama := filepath.Join(dir, "lama", "secret.key")
	if err := os.MkdirAll(filepath.Dir(lama), 0o750); err != nil {
		t.Fatalf("buat direktori lama: %v", err)
	}
	isi := strings.Repeat("a", 48)
	// 0640 = bentuk asli secret di state dir web (root:linux-dashboard).
	tulisSecret(t, lama, isi, 0o640)
	baru := filepath.Join(dir, "baru", "secret.key")

	got, err := loadOrCreateSecret(baru, lama, "grup-yang-tidak-ada")
	if err != nil {
		t.Fatalf("muat secret lama: %v", err)
	}
	if string(got) != isi {
		t.Fatalf("secret dimuat = %q, ingin isi lama", got)
	}
	b, err := os.ReadFile(baru)
	if err != nil {
		t.Fatalf("secret tidak disalin ke lokasi baru: %v", err)
	}
	if string(b) != isi {
		t.Fatalf("isi salinan = %q", b)
	}
	// Berkas lama sengaja TIDAK dihapus (rollback ke versi panel sebelumnya
	// butuh berkas itu), dan setelah salinannya ada ia tidak dibaca lagi.
	if _, err := os.Lstat(lama); err != nil {
		t.Fatalf("berkas lama ikut terhapus: %v", err)
	}
}

// Berkas yang sudah ada tidak pernah ditimpa, dan symlink di lokasi secret
// tidak diikuti — kalau tidak, pihak yang bisa membuat symlink di direktori
// secret bisa menanam secret pilihannya.
func TestBuatSecretTidakMenimpaBerkasYangAda(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	lama := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tulisSecret(t, path, lama, 0o600)

	if _, err := loadOrCreateSecret(path, "", "grupp-yang-tidak-ada"); err != nil {
		t.Fatalf("muat secret yang ada: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baca secret: %v", err)
	}
	if string(got) != lama {
		t.Fatal("secret yang sudah ada ditimpa")
	}
}

func TestSecretYangSudahAdaTetapDapatDibacaGrupWeb(t *testing.T) {
	grup, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.Atoi(grup.Gid)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	tulisSecret(t, path, strings.Repeat("a", 48), 0o600)

	if err := pastikanIzinSecret(path, gid); err != nil {
		t.Fatalf("pastikan izin: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %04o, ingin 0640", st.Mode().Perm())
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); !ok || int(stat.Gid) != gid {
		t.Fatalf("gid secret tidak menjadi %d", gid)
	}
}
