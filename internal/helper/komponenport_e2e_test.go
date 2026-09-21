package helper

import (
	"os"
	"sort"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// TestE2EPortKomponenKeUfw menguji reconciler port komponen terhadap ufw dan
// systemd SUNGGUHAN.
//
// Unit systemd uji dibuat di /run (bukan unit tetap), komponen uji didaftarkan
// di katalog yang sedang dipakai proses ini, lalu tiga keadaan diuji berurutan:
// layanan hidup → port terbuka dan berlabel; layanan mati → port dicabut;
// layanan hidup lagi → port dibuka lagi. Di akhir, firewall dibandingkan dengan
// keadaan sebelum uji.
//
// Uji ini menyentuh firewall mesin (dan karena reconciler-nya bekerja atas
// seluruh katalog, ia sekaligus merapikan rule komponen lain yang memang sudah
// waktunya: label dipasang, dan rule layanan yang sudah tidak ada dicabut).
// Karena itu ia TIDAK ikut `make test`:
//
//	sudo LINDASH_E2E=1 go test ./internal/helper -run TestE2EPortKomponenKeUfw -v
func TestE2EPortKomponenKeUfw(t *testing.T) {
	if os.Getenv("LINDASH_E2E") != "1" {
		t.Skip("butuh LINDASH_E2E=1: membuat unit systemd dan menyentuh ufw mesin ini")
	}
	if os.Geteuid() != 0 {
		t.Skip("butuh root: menulis unit ke /run/systemd/system dan menjalankan ufw")
	}
	if _, ada := lookBinary("ufw"); !ada {
		t.Skip("ufw tidak terpasang di mesin ini")
	}
	const (
		namaUji  = "lindash-e2e-port"
		labelUji = "Uji E2E"
		portUji  = "29997"
	)

	hapusRuleUji(t, portUji)
	sebelum := kunciRuleUfw(t)

	components[namaUji] = &component{
		Name: namaUji, Label: labelUji, Service: namaUji,
		ports:     []portKomponen{{Port: portUji, Proto: "tcp", Guna: "uji E2E"}},
		terpasang: func() bool { return true },
	}
	cadangan, adaCadangan := bacaBerkas(pathStatePortKomponen)
	t.Cleanup(func() {
		delete(components, namaUji)
		_ = jalankanDiam("systemctl", "stop", namaUji)
		_ = os.Remove(unitUji(namaUji))
		_ = jalankanDiam("systemctl", "daemon-reload")
		hapusRuleUji(t, portUji)
		pulihkanBerkas(t, pathStatePortKomponen, cadangan, adaCadangan)
	})

	// 1. Layanan hidup: portnya dibuka, lengkap dengan label pemiliknya.
	jalankanUnitUji(t, namaUji, "start")
	sinkronPortKomponenUji(t)
	if !ruleUfwAda(t, portUji) {
		t.Fatalf("port %s harus terbuka selama layanannya hidup", portUji)
	}
	if d := labelRuleUfw(t, portUji); d != labelUji {
		t.Errorf("label rule harus %q, dapat %q", labelUji, d)
	}

	// 2. Layanan mati: portnya dicabut — inilah yang diminta: port terbuka
	// untuk layanan yang sudah mati adalah lubang tanpa gunanya.
	jalankanUnitUji(t, namaUji, "stop")
	sinkronPortKomponenUji(t)
	if ruleUfwAda(t, portUji) {
		t.Errorf("port %s harus dicabut sesudah layanannya mati", portUji)
	}

	// 3. Layanan hidup lagi: portnya dibuka lagi tanpa campur tangan manusia.
	jalankanUnitUji(t, namaUji, "start")
	sinkronPortKomponenUji(t)
	if !ruleUfwAda(t, portUji) {
		t.Errorf("port %s harus dibuka lagi sesudah layanannya hidup", portUji)
	}

	periksaTidakAdaRuleKembar(t)

	// 4. Rule lama tanpa label milik komponen yang hidup: dilabeli, dan
	//    cakupannya dipertahankan apa adanya.
	dariLokal := subnetLokal()
	if dariLokal == "" {
		t.Skip("subnet lokal tidak terdeteksi — cakupan rule tidak bisa diuji di mesin ini")
	}
	hapusRuleUji(t, portUji)
	if err := ufwAdd(aturanPort(portKomponen{Port: portUji, Proto: "tcp"}, dariLokal)); err != nil {
		t.Fatalf("siapkan rule lama: %v", err)
	}
	sinkronPortKomponenUji(t)
	if d := labelRuleUfw(t, portUji); d != labelUji {
		t.Errorf("rule lama harus dilabeli %q, dapat %q", labelUji, d)
	}
	if dari := fromRuleUfw(t, portUji); dari != dariLokal {
		t.Errorf("cakupan rule tidak boleh berubah: %q jadi %q", dariLokal, dari)
	}

	// 5. Firewall kembali seperti semula. Yang boleh berbeda hanya rule untuk
	//    layanan yang memang sudah tidak ada (mis. 631/tcp sesudah CUPS dicopot);
	//    rule lain harus utuh, karena uji ini tidak sedang menguji pencabutan itu.
	periksaTidakAdaRuleKembar(t)
	hapusRuleUji(t, portUji)
	sesudah := kunciRuleUfw(t)
	var hilang []string
	for k := range sebelum {
		if !sesudah[k] {
			hilang = append(hilang, k)
		}
	}
	sort.Strings(hilang)
	for _, k := range hilang {
		if k != "631/tcp" {
			t.Errorf("rule %s hilang padahal tidak seharusnya", k)
		}
	}
	for k := range sesudah {
		if !sebelum[k] {
			t.Errorf("rule %s muncul tanpa sebab", k)
		}
	}
	t.Logf("rule sebelum %d, sesudah %d, dicabut: %v", len(sebelum), len(sesudah), hilang)
}

// periksaTidakAdaRuleKembar memastikan satu bentuk rule (port + protokol +
// cakupan) hanya punya satu baris. Dua baris untuk bentuk yang sama — biasanya
// satu tanpa label dan satu berlabel — membuat halaman Firewall menampilkan
// baris dobel dan nomor rule bergeser dua kali saat dihapus.
func periksaTidakAdaRuleKembar(t *testing.T) {
	t.Helper()
	st, err := ufwStatus()
	if err != nil {
		t.Fatalf("ufw status: %v", err)
	}
	lihat := map[string]int{}
	for _, r := range st.Rules {
		if !strings.EqualFold(r.Action, "allow") {
			continue
		}
		lihat[kunciPortKomponen("", r.Port, r.Proto, sumberRule(r.From))]++
	}
	for k, n := range lihat {
		if n > 1 {
			t.Errorf("rule kembar untuk bentuk %s: %d baris", k, n)
		}
	}
}

func sinkronPortKomponenUji(t *testing.T) {
	t.Helper()
	if err := sinkronkanPortKomponen(); err != nil {
		t.Fatalf("sinkronkanPortKomponen: %v", err)
	}
}

func unitUji(nama string) string { return "/run/systemd/system/" + nama + ".service" }

// jalankanUnitUji membuat unit uji dan menyalakannya (atau menghentikannya).
// Unit-nya oneshot + RemainAfterExit supaya "aktif" berarti "sudah dijalankan"
// tanpa perlu daemon yang benar-benar hidup.
func jalankanUnitUji(t *testing.T, nama, aksi string) {
	t.Helper()
	if aksi == "start" {
		isi := "[Unit]\nDescription=Unit uji reconciler port komponen (dibuat test E2E)\n\n" +
			"[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/bin/true\n"
		if err := os.WriteFile(unitUji(nama), []byte(isi), 0o644); err != nil {
			t.Fatalf("tulis unit %s: %v", unitUji(nama), err)
		}
		// Unit yang baru ditulis hanya dikenal systemd sesudah daemon-reload.
		if _, err := run("systemctl", "daemon-reload"); err != nil {
			t.Fatalf("daemon-reload: %v", err)
		}
	}
	if _, err := run("systemctl", aksi, nama); err != nil {
		t.Fatalf("systemctl %s %s: %v", aksi, nama, err)
	}
}

func jalankanDiam(nama string, args ...string) error {
	_, err := run(nama, args...)
	return err
}

// hapusRuleUji membuang rule uji apa pun bentuknya, supaya uji bisa dijalankan
// berulang dan tidak meninggalkan sisa.
func hapusRuleUji(t *testing.T, port string) {
	t.Helper()
	for _, dari := range []string{"", subnetLokal()} {
		_ = ufwHapusRule(aturanPort(portKomponen{Port: port, Proto: "tcp"}, dari))
	}
}

// labelRuleUfw mengembalikan label pemilik rule untuk port TCP itu.
func labelRuleUfw(t *testing.T, port string) string {
	t.Helper()
	for _, r := range ruleUfwUntuk(t, port) {
		return r.Comment
	}
	return ""
}

// fromRuleUfw mengembalikan cakupan sumber rule untuk port TCP itu.
func fromRuleUfw(t *testing.T, port string) string {
	t.Helper()
	for _, r := range ruleUfwUntuk(t, port) {
		return r.From
	}
	return ""
}

func ruleUfwUntuk(t *testing.T, port string) []helperproto.UfwRule {
	t.Helper()
	st, err := ufwStatus()
	if err != nil {
		t.Fatalf("ufw status: %v", err)
	}
	var out []helperproto.UfwRule
	for _, r := range st.Rules {
		if r.Port == port && strings.EqualFold(r.Action, "allow") {
			out = append(out, r)
		}
	}
	return out
}

// bacaBerkas menyimpan isi berkas sebelum uji menyentuhnya.
func bacaBerkas(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// pulihkanBerkas mengembalikan berkas catatan seperti sebelum uji — helper yang
// sedang jalan di mesin ini memakai berkas yang sama.
func pulihkanBerkas(t *testing.T, path string, isi []byte, ada bool) {
	t.Helper()
	if !ada {
		_ = os.Remove(path)
		return
	}
	if err := os.WriteFile(path, isi, 0o600); err != nil {
		t.Errorf("pulihkan %s: %v", path, err)
	}
}
