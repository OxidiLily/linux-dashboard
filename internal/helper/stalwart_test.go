package helper

import (
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
		if !strings.Contains(hasil, "# "+kunci+" dimatikan panel") {
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
}
