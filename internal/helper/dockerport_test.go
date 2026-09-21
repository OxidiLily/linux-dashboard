package helper

import (
	"strings"
	"testing"
)

func TestPortDariBarisInspect(t *testing.T) {
	// Container biasa: satu port container diikat ke 0.0.0.0 DAN :: (v4 + v6).
	// Keduanya port host yang sama, jadi rule ufw-nya satu.
	baris := barisInspect("web", "running", "healthy", "bridge_default", map[string][]bindingPortDocker{
		"80/tcp":   append(ikatanDocker("0.0.0.0", "8085"), ikatanDocker("::", "8085")...),
		"3478/udp": ikatanDocker("0.0.0.0", "3478"),
		// Tidak dipublikasikan ke host: tidak pernah jadi rule.
		"443/tcp": nil,
		// Hanya loopback: paket dari LAN tidak pernah sampai ke sini.
		"9000/tcp": ikatanDocker("127.0.0.1", "9000"),
	})
	port, ok := portDariBarisInspect(baris)
	if !ok {
		t.Fatalf("baris container berjalan harus terbaca")
	}
	var dapat []string
	for _, p := range port {
		dapat = append(dapat, p.Container+" "+p.Port+"/"+p.Proto)
	}
	if len(dapat) != 2 {
		t.Fatalf("port yang terbaca %v, mau tepat 2 (8085/tcp dan 3478/udp)", dapat)
	}
	for _, mau := range []string{"web 8085/tcp", "web 3478/udp"} {
		ada := false
		for _, d := range dapat {
			if d == mau {
				ada = true
			}
		}
		if !ada {
			t.Errorf("port %q tidak ikut terbaca (dapat %v)", mau, dapat)
		}
	}
}

func TestPortDariBarisInspectKasusTepi(t *testing.T) {
	kasus := []struct {
		nama   string
		baris  string
		ok     bool
		jumlah int
	}{
		{"container berhenti", barisInspect("mati", "exited", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 0},
		{"container mati permanen", barisInspect("dead", "dead", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 0},
		{"container belum pernah jalan", barisInspect("created", "created", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 0},
		// docker ps menampilkan keduanya sebagai "Up"; port host-nya masih
		// dipegang, jadi izinnya tidak dicabut dan tidak dibuka-tutup berulang.
		{"container dijeda", barisInspect("paused", "paused", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 1},
		{"container sedang restart", barisInspect("restart", "restarting", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 1},
		{"network host", barisInspect("host", "running", "", "host", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", "80"),
		}), true, 0},
		{"tanpa port", barisInspect("polos", "running", "", "bridge", nil), true, 0},
		{"protokol tak dikenal", barisInspect("sctp", "running", "", "bridge", map[string][]bindingPortDocker{
			"80/sctp": ikatanDocker("0.0.0.0", "8080"),
		}), true, 0},
		{"host port kosong", barisInspect("kosong", "running", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("0.0.0.0", ""),
		}), true, 0},
		{"host ip kosong = 0.0.0.0", barisInspect("kosongip", "running", "", "bridge", map[string][]bindingPortDocker{
			"80/tcp": ikatanDocker("", "8080"),
		}), true, 1},
		{"baris sampah", "bukan json\t\t\t\t", false, 0},
		{"baris kosong", "   ", false, 0},
	}
	for _, k := range kasus {
		port, ok := portDariBarisInspect(k.baris)
		if ok != k.ok || len(port) != k.jumlah {
			t.Errorf("%s: dapat ok=%v jumlah=%d, mau ok=%v jumlah=%d", k.nama, ok, len(port), k.ok, k.jumlah)
		}
	}
}

func TestHostTerbukaDariLuar(t *testing.T) {
	buka := []string{"", "0.0.0.0", "::", "[::]", "192.168.2.11"}
	tutup := []string{"127.0.0.1", "127.0.0.53", "::1", "localhost"}
	for _, ip := range buka {
		if !hostTerbukaDariLuar(ip) {
			t.Errorf("host %q harus dianggap bisa dihubungi dari luar", ip)
		}
	}
	for _, ip := range tutup {
		if hostTerbukaDariLuar(ip) {
			t.Errorf("host %q hanya loopback, tidak boleh dianggap terbuka", ip)
		}
	}
}

// Container yang jalan membuka portnya, dan yang berhenti menutupnya kembali.
// Ini inti permintaannya, jadi diperiksa sampai perintah ufw yang dijalankan.
func TestSinkronBukaDanCabutPortContainer(t *testing.T) {
	u := siapkanUjiDockerPort(t)

	u.setDockerBerjalan(
		barisInspect("cctv", "running", "healthy", "cctv_default", map[string][]bindingPortDocker{
			"3478/tcp": ikatanDocker("0.0.0.0", "3478"),
			"3478/udp": ikatanDocker("0.0.0.0", "3478"),
			"8090/tcp": ikatanDocker("0.0.0.0", "8090"),
		}),
		barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
			"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
		}),
	)
	u.sinkron()

	u.harusSama("izin dibuka", u.izinDibuka(), []string{"3478/tcp", "3478/udp", "8090/tcp", "8085/tcp"})
	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"3478/tcp", "3478/udp", "8090/tcp", "8085/tcp"})
	// Label pemilik ikut ditulis: yang membaca `ufw status` di mesin ini harus
	// tahu port itu milik container mana tanpa membuka catatan panel.
	u.harusSama("label rule", u.labelDibuka(), []string{
		"Docker: cctv", "Docker: cctv", "Docker: cctv", "Docker: pdf",
	})

	st := u.state()
	if len(st.Rules) != 4 {
		t.Fatalf("catatan harus memuat 4 rule, isinya %+v", st.Rules)
	}
	for _, e := range st.Rules {
		if !e.Dibuat {
			t.Errorf("rule %s/%s dibuat panel, harus tercatat sebagai milik panel: %+v", e.Port, e.Proto, e)
		}
	}

	// Putaran kedua dengan keadaan yang sama tidak boleh menambah apa pun:
	// `ufw allow` yang sama berulang kali bukan bencana, tapi menandakan
	// reconciler tidak mengenali pekerjaannya sendiri.
	u.sinkron()
	u.harusSama("izin dibuka pada putaran kedua", u.izinDibuka(),
		[]string{"3478/tcp", "3478/udp", "8090/tcp", "8085/tcp"})

	// Container berhenti: docker tidak lagi melaporkannya berjalan, dan port
	// host-nya ikut lepas — izinnya tidak boleh tertinggal.
	u.setDockerBerjalan()
	u.sinkron()
	u.harusSama("izin dicabut setelah container berhenti", u.izinDicabut(),
		[]string{"3478/tcp", "3478/udp", "8090/tcp", "8085/tcp"})
	u.harusKosong("rule di ufw setelah container berhenti", u.ruleUfw())
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan harus kosong setelah semua container berhenti, isinya %+v", st.Rules)
	}
}

// Rule yang sudah ada sebelum reconciler melihatnya BUKAN milik panel: jangan
// ditambah, jangan pernah dicabut.
func TestSinkronTidakMenyentuhRuleMilikUser(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.setUfwAktif("8085/tcp")
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()
	u.harusSama("izin dibuka", u.izinDibuka(), nil)
	st := u.state()
	if len(st.Rules) != 1 || st.Rules[0].Dibuat {
		t.Fatalf("rule yang sudah ada harus dicatat tanpa kepemilikan, isinya %+v", st.Rules)
	}

	// Container berhenti — rule user harus tetap tinggal.
	u.setDockerBerjalan()
	u.sinkron()
	u.harusSama("izin dicabut", u.izinDicabut(), nil)
	u.harusSama("rule user di ufw", u.ruleUfw(), []string{"8085/tcp"})
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan harus bersih lagi, isinya %+v", st.Rules)
	}
}

// Docker yang tidak bisa dijawab bukan bukti tidak ada container yang jalan.
func TestSinkronDiamSaatDockerTidakTerjawab(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()
	if st := u.state(); len(st.Rules) != 1 || !st.Rules[0].Dibuat {
		t.Fatalf("prasyarat: rule 8085/tcp harus tercatat lebih dulu, isinya %+v", st.Rules)
	}

	u.dockerMati()
	if err := sinkronkanPortDocker(); err == nil {
		t.Fatalf("docker yang gagal harus dilaporkan sebagai error, bukan dianggap tidak ada container")
	}
	u.harusSama("izin dicabut", u.izinDicabut(), nil)
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"8085/tcp"})
	st := u.state()
	if len(st.Rules) != 1 || !st.Rules[0].Dibuat || st.Rules[0].Port != "8085" {
		t.Errorf("catatan tidak boleh berubah saat docker tidak terjawab, isinya %+v", st.Rules)
	}
}

// Port yang sudah dideklarasikan komponen bukan urusan reconciler, walaupun
// container mempublikasikannya.
func TestSinkronMelewatiPortKomponen(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.setUfwAktif()
	u.setDockerBerjalan(barisInspect("campur", "running", "", "bridge", map[string][]bindingPortDocker{
		"445/tcp":   ikatanDocker("0.0.0.0", "445"),   // samba
		"22/tcp":    ikatanDocker("0.0.0.0", "22"),    // akses admin (SSH)
		"20128/tcp": ikatanDocker("0.0.0.0", "20128"), // 9router
		"5055/tcp":  ikatanDocker("0.0.0.0", "5055"),  // arkon
		"19999/tcp": ikatanDocker("0.0.0.0", "19999"), // tidak diklaim siapa pun
	}))
	u.sinkron()

	for _, d := range u.izinDibuka() {
		switch d {
		case "445/tcp", "22/tcp", "20128/tcp", "5055/tcp":
			t.Errorf("port %s dideklarasikan komponen/akses admin, tidak boleh dibuka reconciler", d)
		}
	}
	u.harusSama("izin dibuka", u.izinDibuka(), []string{"19999/tcp"})
}

// Daftar container yang tidak bisa dipastikan lengkap tidak boleh dipakai
// untuk MENCABUT izin: container yang gagal dibaca tidak terlihat di daftar,
// dan memperlakukannya sebagai "tidak jalan" menutup port layanan yang mungkin
// masih hidup. Menambah izin tetap aman, jadi itu tetap dikerjakan.
func TestSinkronTidakMencabutSaatDaftarContainerTidakLengkap(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	if err := tulisStatePortDocker(statePortDocker{Rules: []entriStatePortDocker{
		{Port: "29998", Proto: "tcp", Container: "hilang", Dibuat: true},
	}}); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}
	u.setUfwAktif("29998/tcp")
	u.setDockerTidakLengkap(t)

	if err := sinkronkanPortDocker(); err == nil {
		t.Errorf("daftar container yang tidak lengkap harus dilaporkan sebagai error")
	}
	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("izin dibuka", u.izinDibuka(), []string{"29999/tcp"})
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"29998/tcp", "29999/tcp"})

	st := u.state()
	ada := false
	for _, e := range st.Rules {
		if e.Port == "29998" {
			ada = true
		}
	}
	if !ada {
		t.Errorf("catatan container yang tidak terbaca harus dipertahankan, isinya %+v", st.Rules)
	}
}

// Penghapusan lewat halaman Firewall harus tercatat, bukan sekadar rule-nya
// hilang: tanpa catatan itu reconciler membuatnya ulang pada putaran
// berikutnya, dan panel tampak mengembalikan apa yang baru saja user hapus.
func TestUfwDeleteMenandaiRulePortDocker(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	if err := tulisStatePortDocker(statePortDocker{Rules: []entriStatePortDocker{
		{Port: "8085", Proto: "tcp", Container: "pdf", Dibuat: true},
	}}); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}

	// Bentuk pertama: ufw nonaktif, rule tidak bernomor dan dihapus lewat spec.
	u.setUfwNonaktif("8085/tcp")
	if err := ufwDelete("", "allow 8085/tcp"); err != nil {
		t.Fatalf("ufwDelete(spec): %v", err)
	}
	st := u.state()
	if len(st.Rules) != 1 || !st.Rules[0].Diabaikan || st.Rules[0].Dibuat {
		t.Fatalf("penghapusan lewat spec harus tercatat dan melepas kepemilikan: %+v", st.Rules)
	}

	// Bentuk kedua: ufw aktif, rule dihapus lewat nomornya. Nomor hanyalah
	// posisi — bentuk rule-nya yang harus dicari lebih dulu.
	if err := tulisStatePortDocker(statePortDocker{Rules: []entriStatePortDocker{
		{Port: "8085", Proto: "tcp", Container: "pdf", Dibuat: true},
	}}); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}
	u.setUfwAktif("8085/tcp")
	if err := ufwDelete("1", ""); err != nil {
		t.Fatalf("ufwDelete(num): %v", err)
	}
	st = u.state()
	if len(st.Rules) != 1 || !st.Rules[0].Diabaikan {
		t.Fatalf("penghapusan lewat nomor harus tercatat: %+v", st.Rules)
	}

	// Dan sesudah itu reconciler tidak boleh membuatnya ulang walaupun
	// container-nya masih memakai port itu.
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()
	u.harusKosong("izin dibuka", u.izinDibuka())
}

// Rule yang dihapus user lewat panel tidak boleh dibuat ulang selama
// container-nya masih memakai port itu.
func TestSinkronMenghormatiPenghapusanUser(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	if err := tulisStatePortDocker(statePortDocker{Rules: []entriStatePortDocker{
		{Port: "8085", Proto: "tcp", Container: "pdf", Dibuat: false, Diabaikan: true},
	}}); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}

	u.setUfwAktif()
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()

	u.harusSama("izin dibuka", u.izinDibuka(), nil)
	if len(u.state().Rules) != 1 {
		t.Errorf("catatan penghapusan user harus dipertahankan, isinya %+v", u.state().Rules)
	}
}

// Rule milik panel yang hilang di luar panel (ufw reset, mesin dipulihkan dari
// cadangan) dipasang lagi — container-nya masih jalan dan portnya masih
// dipublikasikan.
func TestSinkronMemasangUlangSetelahRuleHilang(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	if err := tulisStatePortDocker(statePortDocker{Rules: []entriStatePortDocker{
		{Port: "8085", Proto: "tcp", Container: "pdf", Dibuat: true},
	}}); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}

	u.setUfwAktif() // `ufw reset`: tidak ada rule sama sekali
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()
	u.harusSama("izin dibuka", u.izinDibuka(), []string{"8085/tcp"})
}

// Mencopot komponen docker mencabut seluruh rule yang pernah dibuat reconciler.
func TestCabutSemuaPortDocker(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
		"8080/udp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.sinkron()
	u.harusSama("izin dibuka", u.izinDibuka(), []string{"8085/tcp", "8085/udp"})

	cabutSemuaPortDocker()
	u.harusSama("izin dicabut", u.izinDicabut(), []string{"8085/tcp", "8085/udp"})
	u.harusKosong("rule di ufw", u.ruleUfw())
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan harus kosong, isinya %+v", st.Rules)
	}
}

// ufw belum terpasang: tidak ada tempat mendaftar, dan tidak ada yang boleh
// gagal. Saat ufw dipasang nanti, putaran berikutnya yang mendaftarkannya.
//
// Dilewati di mesin yang ufw-nya benar-benar terpasang: lookBinary punya
// cadangan ke direktori sistem, jadi binary ufw sungguhan tetap ditemukan
// walaupun direktori tiruan dikosongkan.
func TestSinkronTanpaUfw(t *testing.T) {
	if _, ada := lookBinarySistem("ufw"); ada {
		t.Skip("ufw terpasang di mesin ini — jalur 'ufw belum ada' tidak bisa diuji di sini")
	}
	u := siapkanUjiDockerPort(t)
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	u.hapusUfw()
	u.sinkron()
	u.harusSama("izin dibuka", u.izinDibuka(), nil)
}

// Catatan yang rusak diperlakukan sebagai catatan kosong — bukan alasan untuk
// gagal, dan bukan alasan untuk mencabut rule siapa pun.
func TestSinkronCatatanRusak(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.tulis("docker-ports.json", "{bukan json")
	u.setUfwAktif("9999/tcp")
	u.setDockerBerjalan()
	u.sinkron()
	u.harusSama("izin dicabut", u.izinDicabut(), nil)
}

// Keluaran `ufw status numbered` yang tidak bisa dijawab juga menghentikan
// penyelarasan: tanpa daftar rule, tidak ada yang bisa diputuskan dengan aman.
func TestSinkronDiamSaatUfwTidakTerjawab(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	u.ufwGagal()
	u.setDockerBerjalan(barisInspect("pdf", "running", "", "pdf_default", map[string][]bindingPortDocker{
		"8080/tcp": ikatanDocker("0.0.0.0", "8085"),
	}))
	if err := sinkronkanPortDocker(); err == nil {
		t.Fatalf("ufw status yang gagal harus dilaporkan sebagai error")
	}
	u.harusSama("izin dibuka", u.izinDibuka(), nil)
}

func TestDockerUbahKeadaan(t *testing.T) {
	ubah := [][]string{
		{"start"}, {"stop"}, {"restart"}, {"rm"},
		{"compose", "up"}, {"compose", "down"},
		{"compose", "-f", "/opt/x/docker-compose.yml", "up", "-d"},
		{"compose", "-f", "/opt/x/docker-compose.yml", "--env-file", "/opt/x/.env", "restart"},
		{"compose", "-p", "supabase", "stop"},
	}
	diam := [][]string{
		{"ps"}, {"logs"}, {"inspect"}, {"stats"}, {"version"}, {"info"},
		{"compose", "ps"}, {"compose", "logs"}, {"compose", "config"},
		{"compose", "-f", "/opt/x/docker-compose.yml", "pull"},
		{"image"}, {"volume"}, {"system"},
	}
	for _, a := range ubah {
		if !dockerUbahKeadaan(a[0], a[1:]) {
			t.Errorf("%v mengubah keadaan container, harus dianggap perubahan", a)
		}
	}
	for _, a := range diam {
		if dockerUbahKeadaan(a[0], a[1:]) {
			t.Errorf("%v hanya membaca, tidak boleh memicu penyelarasan", a)
		}
	}
}

func TestComposeSub(t *testing.T) {
	kasus := []struct {
		args []string
		sub  string
	}{
		{[]string{"up", "-d"}, "up"},
		{[]string{"-f", "/a/b.yml", "down"}, "down"},
		{[]string{"--file", "/a/b.yml", "--env-file", "/a/.env", "ps"}, "ps"},
		{[]string{"-d", "up"}, "up"},
		{[]string{"-f", "/a/b.yml"}, ""},
	}
	for _, k := range kasus {
		sub, err := composeSub(k.args)
		if err != nil {
			t.Errorf("composeSub(%v): %v", k.args, err)
			continue
		}
		if sub != k.sub {
			t.Errorf("composeSub(%v) = %q, mau %q", k.args, sub, k.sub)
		}
	}
	// Opsi yang kehilangan nilainya harus ditolak, bukan dilewati diam-diam:
	// tanpanya `-f` akan dianggap subcommand dan validasinya bocor.
	if _, err := composeSub([]string{"-f"}); err == nil {
		t.Errorf("composeSub(-f) tanpa nilai harus gagal")
	}
	if err := checkComposeArgs([]string{"-f", "/a/b.yml", "prune"}); err == nil {
		t.Errorf("subcommand compose yang tidak diizinkan harus tetap ditolak")
	}
	if err := checkComposeArgs([]string{"-f", "/a/b.yml", "up", "-d"}); err != nil {
		t.Errorf("compose up harus tetap diizinkan: %v", err)
	}
}

func TestProtoCocokDengan(t *testing.T) {
	if !protoCocokDengan("tcp", "tcp") || !protoCocokDengan("udp", "udp") {
		t.Errorf("proto yang sama harus cocok")
	}
	if protoCocokDengan("tcp", "udp") {
		t.Errorf("tcp bukan udp")
	}
	// Rule tanpa proto berlaku untuk keduanya.
	if !protoCocokDengan("tcp", "") || !protoCocokDengan("udp", "any") {
		t.Errorf("rule tanpa proto berlaku untuk tcp dan udp")
	}
}

func TestStatePortDockerRoundTrip(t *testing.T) {
	u := siapkanUjiDockerPort(t)
	mau := statePortDocker{Rules: []entriStatePortDocker{
		{Port: "8090", Proto: "tcp", Container: "cctv", Dibuat: true},
		{Port: "3478", Proto: "udp", Container: "cctv"},
		{Port: "8085", Proto: "tcp", Container: "pdf", Diabaikan: true},
	}}
	if err := tulisStatePortDocker(mau); err != nil {
		t.Fatalf("tulis catatan: %v", err)
	}
	dapat := u.state()
	if dapat.Version != versiStatePortDocker {
		t.Errorf("versi catatan %d, mau %d", dapat.Version, versiStatePortDocker)
	}
	if len(dapat.Rules) != 3 {
		t.Fatalf("catatan kembali %d entri, mau 3: %+v", len(dapat.Rules), dapat.Rules)
	}
	// Urutan ditulis terurut supaya diff-nya terbaca.
	if dapat.Rules[0].Port != "3478" || dapat.Rules[1].Port != "8085" || dapat.Rules[2].Port != "8090" {
		t.Errorf("urutan catatan tidak terurut: %+v", dapat.Rules)
	}
	harusUrut(t, "urutan catatan",
		[]string{dapat.Rules[0].Proto, dapat.Rules[1].Proto, dapat.Rules[2].Proto},
		[]string{"udp", "tcp", "tcp"})
	if !strings.Contains(bacaMentah(t, pathStatePortDocker), "\"diabaikan\": true") {
		t.Errorf("penanda penghapusan user tidak ikut tertulis")
	}
}
