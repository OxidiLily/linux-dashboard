package helper

import (
	"strings"
	"testing"
)

// ---- label pemilik rule ----

func TestPisahKomentar(t *testing.T) {
	kasus := []struct {
		baris    string
		sumber   string
		komentar string
	}{
		{"Anywhere", "Anywhere", ""},
		{"Anywhere # Samba", "Anywhere", "Samba"},
		{"192.168.2.0/24 # CUPS", "192.168.2.0/24", "CUPS"},
		{"Anywhere  #  Docker: cctv-agentdvr-1", "Anywhere", "Docker: cctv-agentdvr-1"},
		{"", "", ""},
	}
	for _, k := range kasus {
		sumber, komentar := pisahKomentar(k.baris)
		if sumber != k.sumber || komentar != k.komentar {
			t.Errorf("pisahKomentar(%q) = %q, %q; mau %q, %q",
				k.baris, sumber, komentar, k.sumber, k.komentar)
		}
	}
}

func TestParseAddedRuleDenganLabel(t *testing.T) {
	r, ok := parseAddedRule("ufw allow 22/tcp comment 'SSH'")
	if !ok {
		t.Fatal("baris dengan label harus terbaca")
	}
	if r.Port != "22" || r.Proto != "tcp" || r.From != "" || r.Comment != "SSH" {
		t.Errorf("rule salah baca: %+v", r)
	}

	r, ok = parseAddedRule("ufw allow from 192.168.2.0/24 to any port 631 proto tcp comment 'CUPS'")
	if !ok {
		t.Fatal("baris ber-scope dengan label harus terbaca")
	}
	if r.From != "192.168.2.0/24" || r.Port != "631" || r.Proto != "tcp" || r.Comment != "CUPS" {
		t.Errorf("rule ber-scope salah baca: %+v", r)
	}

	// Label yang isinya memuat spasi tetap utuh.
	r, _ = parseAddedRule("ufw allow 21/tcp comment 'panel linux-dashboard'")
	if r.Comment != "panel linux-dashboard" {
		t.Errorf("label berspasi salah baca: %q", r.Comment)
	}
}

func TestValidKomentar(t *testing.T) {
	baik := []string{"", "SSH", "Samba", "Avahi (mDNS)", "Docker: cctv-agentdvr-1", "9router"}
	for _, s := range baik {
		if err := validKomentar(s); err != nil {
			t.Errorf("label %q seharusnya diterima: %v", s, err)
		}
	}
	buruk := []string{
		"a'b",                   // tanda kutip menutup label di tengah perintah
		`x"y`,                   // kutip ganda
		"baris\nbaru",           // baris baru
		"titik;perintah",        // pemisah perintah
		strings.Repeat("a", 61), // terlalu panjang
	}
	for _, s := range buruk {
		if err := validKomentar(s); err == nil {
			t.Errorf("label %q seharusnya ditolak", s)
		}
	}
}

func TestLabelDocker(t *testing.T) {
	if d := labelDocker("cctv-agentdvr-1"); d != "Docker: cctv-agentdvr-1" {
		t.Errorf("label container biasa: %q", d)
	}
	// Nama container datang dari luar panel: karakter yang tidak dikenal ufw
	// diganti, dan namanya dipotong supaya labelnya tidak melebihi batas ufw.
	if d := labelDocker("aneh$sekali`nama"); strings.ContainsAny(d, "$`") {
		t.Errorf("karakter aneh harus diganti: %q", d)
	}
	if d := labelDocker(strings.Repeat("x", 200)); len(d) > 60 {
		t.Errorf("label harus dipotong, panjangnya %d", len(d))
	}
	if err := validKomentar(labelDocker(strings.Repeat("x", 200))); err != nil {
		t.Errorf("label hasil potongan harus tetap valid: %v", err)
	}
	if d := labelDocker(""); d != "Docker" {
		t.Errorf("nama kosong harus jadi label netral, dapat %q", d)
	}
}

// ---- reconciler port komponen ----

// Komponen yang layanannya hidup harus punya rule, lengkap dengan label
// pemiliknya, dan putaran berikutnya tidak boleh menambah rule kembar.
func TestSinkronKomponenMembukaPortSaatLayananHidup(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setTerpasang("9router", true)
	u.setUnit("9router", true, true)

	u.sinkron()

	u.harusSama("izin dibuka", u.izinDibuka(), []string{"20128/tcp comment 9router"})
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"20128/tcp|9router"})

	st := u.state()
	if len(st.Rules) != 1 || st.Rules[0].Port != "20128" || st.Rules[0].Komponen != "9router" || !st.Rules[0].Dibuat {
		t.Fatalf("catatan rule salah: %+v", st.Rules)
	}

	// Putaran kedua dengan keadaan yang sama tidak menambah apa pun: `ufw allow`
	// yang sama berulang kali bukan bencana, tapi menandakan reconciler tidak
	// mengenali pekerjaannya sendiri.
	u.sinkron()
	u.harusSama("izin dibuka pada putaran kedua", u.izinDibuka(), []string{"20128/tcp comment 9router"})
}

// Layanan yang unitnya ada tapi tidak jalan: port-nya dicabut, karena port
// terbuka untuk layanan yang mati adalah lubang tanpa gunanya.
func TestSinkronKomponenMencabutSaatLayananMati(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "445", Proto: "tcp", Komponen: "samba", Dibuat: true},
	}})
	u.setRuleUfw("445/tcp", "Samba")
	u.setTerpasang("samba", true)
	u.setUnit("smbd", true, false)

	u.sinkron()

	u.harusSama("izin dicabut", u.izinDicabut(), []string{"445/tcp"})
	u.harusKosong("rule di ufw", u.ruleUfw())
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan harus dibuang sesudah layanan mati, isinya %+v", st.Rules)
	}
}

// Komponen yang binary-nya sudah tidak ada: seluruh rule miliknya dicabut —
// termasuk yang cakupannya dibatasi subnet lokal. 631/tcp yang tertinggal
// sesudah paket CUPS dihapus adalah bentuk nyata kasus ini.
func TestSinkronKomponenMencabutSaatKomponenTidakAda(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "631", Proto: "tcp", Dari: "192.168.2.0/24", Komponen: "print-server", Dibuat: true},
	}})
	u.setRuleUfw("from 192.168.2.0/24 to any port 631 proto tcp", "CUPS")

	u.sinkron()

	u.harusSama("izin dicabut", u.izinDicabut(), []string{"from 192.168.2.0/24 to any port 631 proto tcp"})
	u.harusKosong("rule di ufw", u.ruleUfw())
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan harus kosong, isinya %+v", st.Rules)
	}
}

// Rule lama buatan panel (sebelum label ada) tidak dibiarkan tanpa pemilik:
// labelnya ditulis, dan cakupannya dipertahankan apa adanya — merapikan label
// tidak boleh berarti melonggarkan aturan yang sengaja dibatasi ke subnet lokal.
func TestSinkronKomponenMelabeliRuleLamaTanpaMelonggarkanCakupan(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setRuleUfw("from 192.168.2.0/24 to any port 631 proto tcp", "")
	u.setTerpasang("print-server", true)
	u.setUnit("cups", true, true)

	u.sinkron()

	// Rule yang sama diperbarui di tempat, bukan dihapus lalu ditulis ulang:
	// tidak ada jeda saat portnya tertutup, dan tidak ada rule kembar.
	u.harusSama("izin dibuka", u.izinDibuka(), []string{"from 192.168.2.0/24 to any port 631 proto tcp comment CUPS"})
	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"from 192.168.2.0/24 to any port 631 proto tcp|CUPS"})
	if st := u.state(); len(st.Rules) != 1 || st.Rules[0].Dari != "192.168.2.0/24" {
		t.Errorf("catatan harus mempertahankan cakupan lokal: %+v", st.Rules)
	}
}

// Rule yang dihapus user sendiri dari halaman Firewall tidak dibuat ulang
// selama layanannya masih hidup, tapi keadaan layanannya tetap diingat: begitu
// layanan itu mati dan hidup lagi, portnya dibuka kembali seperti seharusnya.
func TestSinkronKomponenMenghormatiPenghapusanUser(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "20128", Proto: "tcp", Komponen: "9router", Diabaikan: true},
	}})
	u.setTerpasang("9router", true)
	u.setUnit("9router", true, true)

	u.sinkron()
	u.harusKosong("izin dibuka", u.izinDibuka())

	// Layanan dimatikan: catatan penanda ikut dibuang, bukan disimpan.
	u.setUnit("9router", true, false)
	u.sinkron()
	if st := u.state(); len(st.Rules) != 0 {
		t.Fatalf("catatan harus dibuang saat layanan mati, isinya %+v", st.Rules)
	}

	// Layanan hidup lagi: portnya dibuka lagi tanpa warisan penanda lama.
	u.setUnit("9router", true, true)
	u.sinkron()
	u.harusSama("izin dibuka sesudah layanan hidup lagi", u.izinDibuka(), []string{"20128/tcp comment 9router"})
}

// Nama unit yang tidak bisa dipercaya bukan alasan mencabut izin: komponennya
// ada, dan mungkin saja unitnya bernama lain. Arah gagalnya harus "biarkan".
func TestSinkronKomponenUnitTidakDikenalTidakDicabut(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "20128", Proto: "tcp", Komponen: "9router", Dibuat: true},
	}})
	u.setRuleUfw("20128/tcp", "9router")
	u.setTerpasang("9router", true)

	u.sinkron()

	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"20128/tcp|9router"})
	if st := u.state(); len(st.Rules) != 1 {
		t.Errorf("catatan harus dipertahankan, isinya %+v", st.Rules)
	}
}

// Catatan yang hilang tidak boleh membuat izin layanan yang sudah mati tetap
// terbuka — tapi hanya rule BERLABEL kita yang boleh dicabut atas dasar ini.
func TestSinkronKomponenMencabutRuleBerlabelTanpaCatatan(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setRuleUfw("20128/tcp", "9router")

	u.sinkron()

	u.harusSama("izin dicabut", u.izinDicabut(), []string{"20128/tcp"})
	u.harusKosong("rule di ufw", u.ruleUfw())
}

// Rule lama tanpa label milik komponen yang sudah tidak ada ikut dicabut: 631/tcp
// yang tertinggal sesudah paket CUPS dihapus adalah bentuk nyatanya, dan tanpa
// pencabutan itu izinnya bertahan selamanya.
func TestSinkronKomponenMencabutRuleLamaTanpaLabel(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setRuleUfw("631/tcp", "")

	u.sinkron()

	u.harusSama("izin dicabut", u.izinDicabut(), []string{"631/tcp"})
	u.harusKosong("rule di ufw", u.ruleUfw())
}

// ...tapi TIDAK kalau portnya sedang dipublikasikan container yang jalan: port
// yang dideklarasikan komponen bisa juga dipakai layanan lain (443 milik
// Stalwart vs reverse proxy di container), dan mencabutnya berarti menutup
// layanan yang justru sedang melayani.
func TestSinkronKomponenTidakMencabutRuleYangDipakaiContainer(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setRuleUfw("443/tcp", "")
	u.setDockerTerbit("443", "tcp")

	u.sinkron()

	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"443/tcp|"})
}

// Juga tidak kalau jawabannya tidak bisa dipastikan: docker yang gagal dibaca
// bukan bukti bahwa tidak ada yang memakai portnya.
func TestSinkronKomponenTidakMencabutRuleSaatDockerTidakTerbaca(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setRuleUfw("631/tcp", "")
	u.setDockerGagal()

	u.sinkron()

	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"631/tcp|"})
}

// Rule yang cakupannya dibatasi ke alamat lain adalah tulisan user, bukan
// bentuk yang pernah ditulis panel — dan tidak boleh ikut dicabut.
func TestSinkronKomponenTidakMencabutRuleBerScopeUser(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	// 203.0.113.0/24 adalah blok dokumentasi (TEST-NET-3): tidak akan pernah
	// jadi subnet interface mana pun, jadi cakupan ini pasti "bukan bentuk panel".
	u.setRuleUfw("from 203.0.113.0/24 to any port 631 proto tcp", "")

	u.sinkron()

	u.harusKosong("izin dicabut", u.izinDicabut())
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"from 203.0.113.0/24 to any port 631 proto tcp|"})
}

// ufw yang tidak ada bukan kesalahan: firewall memang opsional.
func TestSinkronKomponenTanpaUfw(t *testing.T) {
	if _, ada := lookBinarySistem("ufw"); ada {
		t.Skip("ufw terpasang di mesin ini — jalur 'ufw belum ada' tidak bisa diuji di sini")
	}
	u := siapkanUjiKomponenPort(t)
	u.hapusUfw()
	u.setTerpasang("9router", true)
	u.setUnit("9router", true, true)

	u.sinkron()

	u.harusKosong("izin dibuka", u.izinDibuka())
	u.harusKosong("izin dicabut", u.izinDicabut())
}

// Mencopot komponen mencabut kedua bentuk rule (berlabel dan lama tanpa label),
// dan membersihkan catatannya supaya panel tidak mengingat port yang bukan
// urusannya lagi.
func TestHapusPortKomponen(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "445", Proto: "tcp", Komponen: "samba", Dibuat: true},
		{Port: "139", Proto: "tcp", Komponen: "samba", Dibuat: true},
	}})
	u.setRuleUfw("445/tcp", "Samba")

	hapusPortKomponen(components["samba"])

	dicabut := u.izinDicabut()
	harusMemuat(t, "izin dicabut", dicabut, "445/tcp", "from 192.168.2.0/24 to any port 445 proto tcp")
	if st := u.state(); len(st.Rules) != 0 {
		t.Errorf("catatan samba harus dibuang seluruhnya, isinya %+v", st.Rules)
	}
}

// Rule yang hilang di luar panel (`ufw reset`, atau dihapus dari terminal) harus
// kembali selama layanannya memang hidup — itu yang membedakan reconciler ini
// dari pendaftaran sekali saat pemasangan.
func TestSinkronKomponenMembukaKembaliSaatRuleHilangDiLuarPanel(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "20128", Proto: "tcp", Komponen: "9router", Dibuat: true},
	}})
	u.setTerpasang("9router", true)
	u.setUnit("9router", true, true)

	u.sinkron()

	u.harusSama("izin dibuka", u.izinDibuka(), []string{"20128/tcp comment 9router"})
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"20128/tcp|9router"})
}

// Rule lama tanpa label tetap dilabeli meski catatan panel sudah ada untuk port
// itu: catatan biasa bukan alasan membiarkan rule tanpa pemilik.
func TestSinkronKomponenMelabeliRuleLamaWalauSudahTercatat(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "20128", Proto: "tcp", Komponen: "9router", Dibuat: true},
	}})
	u.setRuleUfw("from 192.168.2.0/24 to any port 20128 proto tcp", "")
	u.setTerpasang("9router", true)
	u.setUnit("9router", true, true)

	u.sinkron()

	u.harusSama("izin dibuka", u.izinDibuka(), []string{"from 192.168.2.0/24 to any port 20128 proto tcp comment 9router"})
	u.harusSama("rule di ufw", u.ruleUfw(), []string{"from 192.168.2.0/24 to any port 20128 proto tcp|9router"})
}

// Rule yang dihapus lewat halaman Firewall ditandai supaya reconciler tidak
// membuatnya ulang — penandanya bekerja untuk port komponen maupun container.
func TestTandaiPortKomponenDihapus(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.tulisState(statePortKomponen{Rules: []entriStatePortKomponen{
		{Port: "20128", Proto: "tcp", Komponen: "9router", Dibuat: true},
	}})

	tandaiPortKomponenDihapus("20128", "tcp")

	st := u.state()
	if len(st.Rules) != 1 || !st.Rules[0].Diabaikan || st.Rules[0].Dibuat {
		t.Fatalf("catatan harus ditandai diabaikan: %+v", st.Rules)
	}
}

// Hapus rule lewat halaman Firewall saat ufw nonaktif memakai bentuk rule apa
// adanya, dan bentuk itu memuat label ("... comment 'SSH'"). Penghapusannya
// harus tetap jalan: token berlabel tidak boleh membuat perintahnya ditolak.
func TestUfwDeleteSpecBerlabel(t *testing.T) {
	u := siapkanUjiKomponenPort(t)
	u.setUfwNonaktif()
	u.setRuleUfw("22/tcp", "SSH")

	if err := ufwDelete("", "allow 22/tcp comment 'SSH'"); err != nil {
		t.Fatalf("hapus rule berlabel: %v", err)
	}
	u.harusKosong("rule di ufw", u.ruleUfw())
	harusMemuat(t, "panggilan hapus", u.izinDicabut(), "22/tcp")
}

func harusMemuat(t *testing.T, apa string, daftar []string, mau ...string) {
	t.Helper()
	for _, m := range mau {
		ada := false
		for _, d := range daftar {
			if d == m {
				ada = true
				break
			}
		}
		if !ada {
			t.Errorf("%s: %q tidak ada di %v", apa, m, daftar)
		}
	}
}
