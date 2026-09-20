package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setBarisEnv menyunting berkas env milik Stalwart. Yang diuji di sini adalah
// dua hal yang menentukan apakah panel boleh menyentuh berkas milik service
// vendor sama sekali:
//
//   - hanya baris AKTIF `KUNCI=...` yang diganti — komentar contoh tulisan
//     skrip installer (`#STALWART_RECOVERY_ADMIN=admin:changeme`) tidak boleh
//     ikut berubah, karena itu setelan admin yang tidak ada urusannya.
//   - mematikan baris berarti menghapus NILAINYA, bukan mengomentari baris
//     beserta password yang sudah tidak berlaku. Berkas env itu 0640 dan
//     dibaca grup akun service.
func TestSetBarisEnv(t *testing.T) {
	const kunci = "STALWART_RECOVERY_ADMIN"
	awal := "# Environment variables for the Stalwart service.\n" +
		"#\n" +
		"# Fixed administrator credentials — format: username:password\n" +
		"#STALWART_RECOVERY_ADMIN=admin:changeme\n"

	t.Run("menambah kalau belum ada", func(t *testing.T) {
		hasil := setBarisEnv(awal, kunci, kunci+"=admin:rahasia1")
		if !strings.Contains(hasil, kunci+"=admin:rahasia1") {
			t.Fatalf("baris baru tidak ditambahkan: %q", hasil)
		}
		// Komentar contoh milik installer harus utuh.
		if !strings.Contains(hasil, "#STALWART_RECOVERY_ADMIN=admin:changeme") {
			t.Fatalf("komentar installer ikut berubah: %q", hasil)
		}
		if strings.Count(hasil, "#STALWART_RECOVERY_ADMIN=") != 1 {
			t.Fatalf("komentar installer terduplikasi: %q", hasil)
		}
	})

	t.Run("mengganti baris aktif", func(t *testing.T) {
		awal2 := awal + kunci + "=admin:lama\n"
		hasil := setBarisEnv(awal2, kunci, kunci+"=admin:baru")
		if !strings.Contains(hasil, kunci+"=admin:baru") {
			t.Fatalf("nilai baru tidak tertulis: %q", hasil)
		}
		if strings.Contains(hasil, "admin:lama") {
			t.Fatalf("nilai lama masih ada: %q", hasil)
		}
		// Hasil ganti tidak boleh menambah baris kembar.
		if strings.Count(hasil, kunci+"=") != 2 { // satu aktif, satu contoh komentar
			t.Fatalf("jumlah baris tidak seperti diharapkan: %q", hasil)
		}
	})

	t.Run("mematikan baris tanpa meninggalkan password", func(t *testing.T) {
		awal2 := awal + kunci + "=admin:rahasia1\n"
		hasil := setBarisEnv(awal2, kunci, "")
		if strings.Contains(hasil, "admin:rahasia1") {
			t.Fatalf("password masih tertinggal di berkas: %q", hasil)
		}
		if !strings.Contains(hasil, komentarTutupStalwart) {
			t.Fatalf("baris tidak dikomentari dengan keterangan: %q", hasil)
		}
		// Berkas yang tidak punya baris aktif tidak berubah sama sekali.
		if lagi := setBarisEnv(hasil, kunci, ""); lagi != hasil {
			t.Fatalf("pemanggilan kedua mengubah berkas: %q", lagi)
		}
	})

	t.Run("duplikat aktif dibuang", func(t *testing.T) {
		awal2 := kunci + "=admin:a\n" + kunci + "=admin:b\n"
		hasil := setBarisEnv(awal2, kunci, kunci+"=admin:a")
		if strings.Contains(hasil, "admin:b") {
			t.Fatalf("duplikat dibiarkan: %q", hasil)
		}
		if strings.Count(hasil, kunci+"=admin:a") != 1 {
			t.Fatalf("baris aktif tidak tunggal: %q", hasil)
		}
	})

	t.Run("baris lain tidak tersentuh", func(t *testing.T) {
		awal2 := "STALWART_HOSTNAME=mail.example.com\n" + kunci + "=admin:a\n"
		hasil := setBarisEnv(awal2, kunci, "")
		if !strings.Contains(hasil, "STALWART_HOSTNAME=mail.example.com") {
			t.Fatalf("baris setelan lain hilang: %q", hasil)
		}
	})

	t.Run("komentar penutup panel dibuang saat dipaku lagi", func(t *testing.T) {
		// Urutan nyata: baris dipaku, dimatikan panel, lalu dipaku lagi (mis.
		// server dikembalikan ke mode bootstrap). Berkas yang berakhir dengan
		// baris aktif DAN komentar penutup yang bertentangan membuat pembaca
		// berikutnya menebak mana yang berlaku.
		mati := setBarisEnv(kunci+"=admin:a\n", kunci, "")
		hasil := setBarisEnv(mati, kunci, kunci+"=admin:a")
		if strings.Contains(hasil, komentarTutupStalwart) {
			t.Fatalf("komentar penutup masih ada: %q", hasil)
		}
		if strings.Count(hasil, kunci+"=admin:a") != 1 {
			t.Fatalf("baris aktif tidak tunggal: %q", hasil)
		}
	})
}

// berkasUji membuat lingkungan kredensial Stalwart di direktori sementara:
// env gaya-vendor, berkas password, dan (opsional) config.json.
func berkasUji(t *testing.T, configAda bool) jalurKredensialStalwart {
	t.Helper()
	dir := t.TempDir()
	j := jalurKredensialStalwart{
		Env:    filepath.Join(dir, "stalwart.env"),
		Pass:   filepath.Join(dir, "stalwart-password"),
		Config: filepath.Join(dir, "config.json"),
	}
	env := "# Environment variables for the Stalwart service.\n" +
		"#\n" +
		"#STALWART_RECOVERY_ADMIN=admin:changeme\n"
	if err := os.WriteFile(j.Env, []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	if configAda {
		if err := os.WriteFile(j.Config, []byte("{}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return j
}

// barisAktif mengembalikan baris `KUNCI=...` yang benar-benar berlaku di berkas
// env. Komentar contoh bawaan skrip installer
// (`#STALWART_RECOVERY_ADMIN=admin:changeme`) TIDAK dihitung — pemeriksaan yang
// memakai strings.Contains saja akan menganggapnya sebagai kredensial aktif dan
// meloloskan regresi yang justru harus ditangkap.
func barisAktif(isi, kunci string) []string {
	var out []string
	for _, b := range strings.Split(isi, "\n") {
		if strings.HasPrefix(b, kunci+"=") {
			out = append(out, b)
		}
	}
	return out
}

// Alur kredensial bootstrap Stalwart, diuji lewat berkas sungguhan.
//
// Regresi yang dijaga di sini nyata dan pernah lolos ke mesin: halaman
// Components memanggil penyelarasan ini SETIAP kali status komponen dibaca,
// dan versi pertama menutup baris STALWART_RECOVERY_ADMIN tanpa memeriksa
// config.json. Akibatnya pembacaan status pertama setelah pemasangan langsung
// mengomentari kredensial yang baru saja dipaku: password bootstrap yang
// benar-benar berlaku tidak pernah muncul di kartu komponen, dan satu-satunya
// jalan masuk ke WebUI tinggal membaca journal server.
func TestKredensialBootstrapStalwart(t *testing.T) {
	t.Run("dipaku saat masih bootstrap", func(t *testing.T) {
		j := berkasUji(t, false)
		padukanKredensialStalwart(j, false)

		isi, err := os.ReadFile(j.Env)
		if err != nil {
			t.Fatal(err)
		}
		pass := passwordTersimpanDi(j.Pass)
		aktif := barisAktif(string(isi), kunciRecoveryStalwart)
		if len(aktif) != 1 || aktif[0] != kunciRecoveryStalwart+"="+stalwartAkunAdmin+":"+pass {
			t.Fatalf("kredensial tidak dipaku dengan nilai yang tersimpan: %q", isi)
		}
		catatan := catatanStalwartDi(j)
		if !strings.HasPrefix(catatan, "Login awal: user `"+stalwartAkunAdmin+"`, password `") {
			t.Fatalf("catatan kredensial tidak ditampilkan: %q", catatan)
		}
		// Catatannya harus memuat password yang benar-benar tersimpan.
		if pass == "" || !strings.Contains(catatan, pass) {
			t.Fatalf("catatan tidak memuat password tersimpan: %q", catatan)
		}
	})

	t.Run("pembacaan status berulang tidak mematikan kredensial", func(t *testing.T) {
		j := berkasUji(t, false)
		// Sepuluh kali berturut-turut, seperti halaman Components yang dibuka
		// berulang atau di-refresh.
		for i := 0; i < 10; i++ {
			padukanKredensialStalwart(j, false)
			if catatanStalwartDi(j) == "" {
				t.Fatalf("kredensial hilang pada pembacaan ke-%d", i+1)
			}
		}
		isi, _ := os.ReadFile(j.Env)
		if strings.Contains(string(isi), komentarTutupStalwart) {
			t.Fatalf("baris dikomentari padahal masih bootstrap: %q", isi)
		}
	})

	t.Run("tutup tidak menyentuh baris selagi masih bootstrap", func(t *testing.T) {
		// Pagar di dalam penutup itu sendiri — inilah yang hilang di versi
		// pertama. Tanpa pemeriksaan config.json, siapa pun yang memanggil
		// penutup ini (dulu: jalur pembacaan status) langsung mengomentari
		// kredensial yang baru dipaku, dan kartu komponen berhenti menampilkan
		// password yang benar-benar berlaku.
		j := berkasUji(t, false)
		padukanKredensialStalwart(j, false)
		tutupKredensialBootstrapStalwartDi(j)

		isi, _ := os.ReadFile(j.Env)
		if len(barisAktif(string(isi), kunciRecoveryStalwart)) != 1 {
			t.Fatalf("kredensial dimatikan padahal masih bootstrap: %q", isi)
		}
		if catatanStalwartDi(j) == "" {
			t.Fatal("catatan kredensial hilang padahal masih bootstrap")
		}
	})

	t.Run("dipaku ulang setelah barisnya hilang", func(t *testing.T) {
		j := berkasUji(t, false)
		padukanKredensialStalwart(j, false)

		// Kerusakan yang mungkin: baris aktif dibuang orang lain.
		isi, _ := os.ReadFile(j.Env)
		rusak := setBarisEnv(string(isi), kunciRecoveryStalwart, "")
		if err := os.WriteFile(j.Env, []byte(rusak), 0o640); err != nil {
			t.Fatal(err)
		}
		if catatanStalwartDi(j) != "" {
			t.Fatal("catatan tetap tampil padahal barisnya sudah hilang")
		}
		// Pembacaan status berikutnya harus mengembalikannya sendiri.
		padukanKredensialStalwart(j, false)
		if catatanStalwartDi(j) == "" {
			t.Fatal("kredensial tidak dipaku ulang oleh pembacaan status")
		}
	})

	t.Run("ditutup setelah wizard selesai", func(t *testing.T) {
		j := berkasUji(t, false)
		padukanKredensialStalwart(j, false)

		// Wizard selesai: Stalwart menulis config.json.
		if err := os.WriteFile(j.Config, []byte("{}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		padukanKredensialStalwart(j, false)

		isi, _ := os.ReadFile(j.Env)
		if aktif := barisAktif(string(isi), kunciRecoveryStalwart); len(aktif) != 0 {
			t.Fatalf("kredensial bootstrap masih aktif setelah wizard: %q", aktif)
		}
		if pass := passwordTersimpanDi(j.Pass); pass != "" && strings.Contains(string(isi), pass) {
			t.Fatalf("password masih tertinggal di berkas env: %q", isi)
		}
		if catatan := catatanStalwartDi(j); catatan != "" {
			t.Fatalf("catatan masih ditampilkan setelah wizard: %q", catatan)
		}
	})

	t.Run("tidak menyentuh kredensial milik admin", func(t *testing.T) {
		j := berkasUji(t, true) // wizard sudah selesai
		// Admin memasang kredensial recovery sendiri; panel tidak punya berkas
		// password, jadi tidak ada yang boleh diubah.
		admin := kunciRecoveryStalwart + "=admin:punya-admin\n"
		if err := os.WriteFile(j.Env, []byte(admin), 0o640); err != nil {
			t.Fatal(err)
		}
		padukanKredensialStalwart(j, false)
		isi, _ := os.ReadFile(j.Env)
		if string(isi) != admin {
			t.Fatalf("berkas admin diubah panel: %q", isi)
		}
	})
}
