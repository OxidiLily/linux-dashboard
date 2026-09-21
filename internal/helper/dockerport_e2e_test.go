package helper

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Uji ujung-ke-ujung terhadap `docker` dan `ufw` SUNGGUHAN di mesin ini.
//
// Tidak ikut `make test`: uji ini membuat container, dan salah satunya
// menyentuh firewall mesin yang menjalankannya. Dijalankan hanya bila diminta
// eksplisit:
//
//	LINDASH_E2E=1 go test ./internal/helper -run TestE2E -v
//	sudo LINDASH_E2E=1 go test ./internal/helper -run TestE2EPortContainerKeUfw -v
//
// Isi test di atas semua membuktikan keputusan reconciler lewat tiruan. Yang
// tidak bisa dibuktikan tiruan justru dua hal yang paling mudah salah: apakah
// `docker inspect --format` yang dipakai benar-benar menghasilkan baris yang
// bisa dibaca, dan apakah perintah ufw yang dihasilkan benar-benar diterima
// ufw. Keduanya hanya terjawab oleh binary sungguhan.

// TestE2EPortContainerDocker memeriksa sisi docker-nya saja, jadi tidak butuh
// root — cukup keanggotaan grup docker. Yang diperiksa: publikasi port
// container sungguhan terbaca, dan container yang berhenti benar-benar hilang
// dari daftar yang dianggap "sedang jalan".
func TestE2EPortContainerDocker(t *testing.T) {
	if os.Getenv("LINDASH_E2E") == "" {
		t.Skip("lewat: set LINDASH_E2E=1 untuk menguji terhadap docker sungguhan")
	}
	if _, ada := lookBinary("docker"); !ada {
		t.Skip("lewat: docker tidak terpasang")
	}
	gambar := gambarUji(t)
	if gambar == "" {
		t.Skip("lewat: tidak ada image docker di mesin ini")
	}

	port := portUjiBebas(t, false)
	nama := "lindash-e2e-docker"
	jalankanContainerUji(t, nama, port, gambar)

	// `docker run -d` sudah mengikat portnya sebelum kembali, tapi binding-nya
	// baru muncul di inspect lewat docker-proxy yang dijalankan belakangan —
	// jadi ditunggu sebentar alih-alih dipercaya begitu saja.
	if !tungguPortTerbit(t, port) {
		t.Fatalf("port %s tidak pernah muncul sebagai publikasi container yang berjalan", port)
	}
	t.Logf("port %s terbaca sebagai publikasi container yang berjalan", port)

	if _, err := run("docker", "stop", nama); err != nil {
		t.Fatalf("hentikan container uji: %v", err)
	}
	if !tungguPortHilang(t, port) {
		t.Errorf("port %s masih terbaca sebagai publikasi padahal container-nya sudah berhenti", port)
	} else {
		t.Logf("port %s hilang dari daftar begitu container berhenti", port)
	}
}

// TestE2EPortContainerKeUfw menjalankan siklus lengkapnya: container jalan →
// ufw mengizinkan portnya, container berhenti → izinnya dicabut.
func TestE2EPortContainerKeUfw(t *testing.T) {
	if os.Getenv("LINDASH_E2E") == "" {
		t.Skip("lewat: set LINDASH_E2E=1 untuk menguji terhadap docker + ufw sungguhan")
	}
	if os.Geteuid() != 0 {
		t.Skip("lewat: butuh root untuk membaca dan mengubah ufw")
	}
	for _, bin := range []string{"docker", "ufw"} {
		if _, ada := lookBinary(bin); !ada {
			t.Skipf("lewat: %s tidak terpasang", bin)
		}
	}
	gambar := gambarUji(t)
	if gambar == "" {
		t.Skip("lewat: tidak ada image docker di mesin ini")
	}

	// Catatan diarahkan ke berkas sementara: uji ini memeriksa perilaku
	// reconciler, bukan mengubah catatan panel mesin.
	lamaState := pathStatePortDocker
	pathStatePortDocker = t.TempDir() + "/docker-ports.json"
	t.Cleanup(func() { pathStatePortDocker = lamaState })

	port := portUjiBebas(t, true)
	nama := "lindash-e2e-ufw"
	sebelum := kunciRuleUfw(t)
	jalankanContainerUji(t, nama, port, gambar)
	t.Cleanup(func() {
		// Rule yang dibuat uji ini dicabut lagi. Yang ikut tercabut tentu saja
		// rule container lain yang sedang jalan — reconciler membukanya juga
		// selama uji ini berjalan, dan firewall harus kembali persis seperti
		// sebelum uji. Yang tidak ada sebelum uji saja yang disentuh.
		for _, e := range bacaStatePortDocker().Rules {
			if e.Dibuat && !sebelum[kunciPortDocker(e.Port, e.Proto)] {
				_ = ufwHapusRule(aturanDocker(e.Port, e.Proto, e.Container))
			}
		}
		_ = tulisStatePortDocker(statePortDocker{})
	})

	// `docker run -d` sudah mengikat portnya sebelum kembali, tapi binding-nya
	// muncul di inspect lewat docker-proxy yang dijalankan belakangan — jadi
	// ditunggu sebentar alih-alih dipercaya begitu saja.
	if !tungguPortTerbit(t, port) {
		t.Fatalf("port %s tidak pernah muncul sebagai publikasi container", port)
	}
	t.Logf("container %s jalan dengan port host %s", nama, port)

	if err := sinkronkanPortDocker(); err != nil {
		t.Fatalf("sinkronkanPortDocker: %v", err)
	}
	if !ruleUfwAda(t, port) {
		t.Fatalf("rule ufw untuk %s/tcp tidak ada sesudah container jalan", port)
	}
	t.Logf("ufw kini mengizinkan %s/tcp (dibuat reconciler)", port)

	// Container lain di mesin ini juga ikut terdaftar. Yang penting di sini:
	// rule milik port uji dicatat sebagai milik panel, supaya nanti dicabut.
	st := bacaStatePortDocker()
	tercatat := false
	for _, e := range st.Rules {
		if e.Port == port && e.Proto == "tcp" {
			tercatat = e.Dibuat
		}
	}
	if !tercatat {
		t.Errorf("port %s tidak tercatat sebagai rule milik panel: %+v", port, st.Rules)
	}

	// Container berhenti → port host-nya lepas → izinnya tidak boleh tertinggal.
	if _, err := run("docker", "stop", nama); err != nil {
		t.Fatalf("hentikan container uji: %v", err)
	}
	if err := sinkronkanPortDocker(); err != nil {
		t.Fatalf("sinkronkanPortDocker sesudah container berhenti: %v", err)
	}
	if ruleUfwAda(t, port) {
		t.Errorf("rule ufw untuk %s/tcp masih ada padahal container-nya sudah berhenti", port)
	} else {
		t.Logf("rule ufw untuk %s/tcp sudah dicabut sesudah container berhenti", port)
	}
}

// gambarUji mengambil satu image yang sudah ada di mesin, tanpa menarik apa pun
// dari jaringan.
func gambarUji(t *testing.T) string {
	t.Helper()
	res, err := run("docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return ""
	}
	for _, baris := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if b := strings.TrimSpace(baris); b != "" && b != "<none>:<none>" {
			return b
		}
	}
	return ""
}

// jalankanContainerUji menyalakan satu container uji yang mempublikasikan
// `port`, lalu mendaftarkan penghapusannya. Entrypoint-nya diganti `sleep` agar
// service bawaan image tidak ikut menyala dan berebut port dengan container
// yang sudah jalan di mesin ini.
func jalankanContainerUji(t *testing.T, nama, port, gambar string) {
	t.Helper()
	_, _ = run("docker", "rm", "-f", nama)
	if _, err := run("docker", "run", "-d", "--name", nama,
		"--publish", "0.0.0.0:"+port+":80", "--entrypoint", "sleep", gambar, "300"); err != nil {
		t.Fatalf("jalankan container uji: %v", err)
	}
	t.Cleanup(func() { _, _ = run("docker", "rm", "-f", nama) })
}

// portUjiBebas mencari port TCP yang tidak sedang dipakai — dan, bila ufw
// diminta diperiksa, yang belum punya rule di sana. Tanpa itu uji ini bisa
// menutupi rule yang sudah ada atau bertabrakan dengan layanan yang jalan.
func portUjiBebas(t *testing.T, cekUfw bool) string {
	t.Helper()
	for _, p := range []string{"29999", "29998", "29997", "29996", "29995"} {
		ln, err := net.Listen("tcp", ":"+p)
		if err != nil {
			continue
		}
		_ = ln.Close()
		if cekUfw && ruleUfwAda(t, p) {
			continue
		}
		return p
	}
	t.Skip("tidak ada port uji yang bebas")
	return ""
}

// kunciRuleUfw mendata rule yang sudah ada SEBELUM uji berjalan; hanya rule di
// luar daftar ini yang boleh dicabut lagi oleh pembersihan.
func kunciRuleUfw(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	st, err := ufwStatus()
	if err != nil {
		t.Fatalf("ufw status: %v", err)
	}
	for _, r := range st.Rules {
		out[kunciPortDocker(r.Port, r.Proto)] = true
	}
	return out
}

// ruleUfwAda menjawab apakah ufw sungguhan memuat rule allow untuk port TCP itu.
func ruleUfwAda(t *testing.T, port string) bool {
	t.Helper()
	st, err := ufwStatus()
	if err != nil {
		t.Fatalf("ufw status: %v", err)
	}
	for _, r := range st.Rules {
		if r.Port == port && r.Proto == "tcp" && strings.EqualFold(r.Action, "allow") {
			return true
		}
	}
	return false
}

// tungguPortTerbit menunggu sampai port itu muncul sebagai publikasi container
// yang sedang jalan.
func tungguPortTerbit(t *testing.T, port string) bool {
	t.Helper()
	return tungguTerbit(t, port, true)
}

// tungguPortHilang menunggu sampai port itu tidak lagi muncul.
func tungguPortHilang(t *testing.T, port string) bool {
	t.Helper()
	return tungguTerbit(t, port, false)
}

func tungguTerbit(t *testing.T, port string, mauAda bool) bool {
	t.Helper()
	batas := time.Now().Add(10 * time.Second)
	for time.Now().Before(batas) {
		terbit, err := portDockerBerjalan()
		if err == nil {
			ada := false
			for _, p := range terbit {
				if p.Port == port && p.Proto == "tcp" {
					ada = true
				}
			}
			if ada == mauAda {
				return true
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
