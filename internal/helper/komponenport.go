package helper

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Reconciler port komponen.
//
// Port komponen sudah didaftarkan saat komponennya dipasang (portkomponen.go),
// dan selama ini itu satu-satunya saat aturannya disentuh. Akibatnya dua arah
// sama-sama salah: izin firewall untuk layanan yang sudah mati atau dicopot
// tetap terbuka (631/tcp milik CUPS yang paketnya sudah tidak ada adalah contoh
// nyata di mesin ini), sedangkan layanan yang hidup tapi aturannya hilang —
// sesudah `ufw reset`, atau karena rulenya dihapus saat membersihkan daftar —
// tetap tertutup dari LAN sampai ada orang yang menyadarinya.
//
// Berkas ini menutup keduanya. Setiap putaran, port yang dideklarasikan setiap
// komponen diselaraskan dengan kenyataan layanannya, dan setiap rule diberi
// label pemiliknya supaya `ufw status` menjawab "port ini punya siapa" tanpa
// perlu dibaca dari kode:
//
//	[10] 22/tcp    ALLOW IN  Anywhere  # SSH
//	[ 6] 445/tcp   ALLOW IN  Anywhere  # Samba
//
// Arah gagal selalu "biarkan seperti apa adanya": keadaan yang tidak bisa
// dipastikan tidak pernah mengubah firewall. Hanya unit systemd yang JELAS
// tidak aktif, atau komponen yang binary-nya sudah tidak ada, yang membuat
// rule-nya dicabut.
const (
	versiStatePortKomponen = 1
	// Jeda yang sama dengan reconciler port container: keduanya dibaca sebagai
	// satu kebijakan — paling lama setengah menit, firewall menyesuaikan diri
	// dengan kenyataan layanan di mesin.
	jedaPortKomponen = 30 * time.Second
)

var pathStatePortKomponen = "/var/lib/linux-dashboard/komponen-ports.json"

// entriStatePortKomponen adalah satu rule yang dipegang panel untuk sebuah
// komponen. Sumber (Dari) ikut dicatat karena satu port bisa terdaftar lebih
// dari sekali dengan cakupan berbeda — tiap bentuk punya riwayat pencabutan
// sendiri, dan mencabut yang salah berarti menutup cakupan yang bukan miliknya.
type entriStatePortKomponen struct {
	Port     string `json:"port"`
	Proto    string `json:"proto"`
	Dari     string `json:"dari,omitempty"`
	Komponen string `json:"komponen,omitempty"`
	// Dibuat menandai rule ini dibuat PANEL; hanya rule dengan Dibuat=true yang
	// boleh dicabut kembali.
	Dibuat bool `json:"dibuat"`
	// Diabaikan menandai user menghapus rule ini sendiri lewat Settings →
	// Firewall. Selama layanannya masih hidup, aturannya tidak dibuat ulang.
	Diabaikan bool `json:"diabaikan,omitempty"`
}

type statePortKomponen struct {
	Version int                      `json:"version"`
	Rules   []entriStatePortKomponen `json:"rules"`
}

var (
	muPortKomponen     sync.Mutex
	sinyalPortKomponen = make(chan struct{}, 1)
	// statusUnitLayanan dijadikan variabel supaya test bisa menyatakan sendiri
	// unit mana yang ada dan hidup. Tanpa itu, test port komponen bergantung
	// pada unit systemd yang kebetulan terpasang di mesin yang menjalankannya.
	statusUnitLayanan = statusUnitSungguhan
)

// statusUnitSungguhan menjawab apakah unit systemd komponen ada dan sedang
// jalan.
//
// "Ada" dipisahkan dari "jalan" karena akibatnya berbeda jauh: unit yang tidak
// ketemu berarti nama unitnya tidak bisa dipercaya (jangan sentuh apa pun —
// mungkin saja unitnya bernama lain), sedangkan unit yang ada tapi tidak jalan
// berarti layanannya memang mati.
func statusUnitSungguhan(unit string) (ada, aktif bool) {
	if unit == "" {
		return false, false
	}
	// LoadState membedakan "unit ini ada" dari "unit ini jalan": `systemctl
	// is-active` menjawab tidak-jalan untuk keduanya, jadi ia tidak bisa dipakai
	// sendiri di sini.
	res, err := run("systemctl", "show", "-p", "LoadState", "--value", unit)
	if err != nil || strings.TrimSpace(res.Stdout) != "loaded" {
		return false, false
	}
	_, err = run("systemctl", "is-active", "--quiet", unit)
	return true, err == nil
}

// sumberRule menormalkan kolom sumber ufw kembali ke bentuk yang dipakai helper:
// "Anywhere" berarti tanpa batas sumber. Tanpa normalisasi ini, rule hasil
// pembacaan `ufw status` tidak bisa ditulis ulang — "Anywhere" bukan alamat.
func sumberRule(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "anywhere", "any", "anywhere (v6)":
		return ""
	}
	return s
}

// kunciPortKomponen adalah identitas satu rule di catatan: komponen, port, dan
// cakupannya.
func kunciPortKomponen(nama, port, proto, dari string) string {
	return nama + "|" + port + "/" + proto + "|" + dari
}

// picuSinkronPortKomponen meminta penyelarasan secepatnya, dipakai sesudah
// tindakan yang mengubah keadaan komponen (pasang, copot, service dinyalakan).
// Non-blocking: satu sinyal yang tertunda sudah cukup, karena pengawasnya
// menyelaraskan seluruh keadaan, bukan hanya komponen yang barusan disentuh.
func picuSinkronPortKomponen() {
	select {
	case sinyalPortKomponen <- struct{}{}:
	default:
	}
}

// pengawasPortKomponen menyelaraskan port komponen dengan ufw secara berkala.
func pengawasPortKomponen() {
	t := time.NewTicker(jedaPortKomponen)
	defer t.Stop()
	var galatTerakhir string
	for {
		if err := sinkronkanPortKomponen(); err != nil {
			// Kegagalan yang berulang setiap putaran hanya dicatat saat pesannya
			// berubah: log yang menuliskan kalimat sama ribuan kali sehari
			// berhenti dibaca orang.
			if p := err.Error(); p != galatTerakhir {
				log.Printf("firewall: sinkronisasi port komponen dilewati: %v", err)
				galatTerakhir = p
			}
		} else if galatTerakhir != "" {
			log.Printf("firewall: sinkronisasi port komponen berjalan kembali")
			galatTerakhir = ""
		}
		select {
		case <-t.C:
		case <-sinyalPortKomponen:
		}
	}
}

// sinkronkanPortKomponen menyelaraskan seluruh port komponen dengan kenyataan
// layanannya.
func sinkronkanPortKomponen() error {
	muPortKomponen.Lock()
	defer muPortKomponen.Unlock()
	return sinkronPortKomponenTerkunci()
}

// komponenTerpasang dijadikan variabel supaya test bisa menentukan sendiri
// komponen mana yang dianggap ada di mesin: `lookBinary` selalu menemukan binary
// sungguhan, jadi test yang bergantung padanya diam-diam berubah perilaku
// mengikuti apa yang kebetulan terpasang di mesin yang menjalankannya.
var komponenTerpasang = komponenTerpasangSungguhan

// komponenTerpasangSungguhan menjawab apakah komponen ini benar-benar ada di
// mesin. Komponen yang mendefinisikan sendiri `terpasang` dipercaya (binary-nya
// bukan binary biasa, mis. stack container), sisanya dicari lewat PATH.
func komponenTerpasangSungguhan(c *component) bool {
	if c.terpasang != nil {
		return c.terpasang()
	}
	_, ok := lookBinary(c.Binary)
	return ok
}

// komponenBernama mencari komponen di katalog.
func komponenBernama(nama string) *component {
	for _, c := range components {
		if c.Name == nama {
			return c
		}
	}
	return nil
}

// sinkronPortKomponenTerkunci adalah isi penyelarasan. Pemanggil sudah memegang
// muPortKomponen.
func sinkronPortKomponenTerkunci() error {
	// Tidak ada ufw = tidak ada tempat mendaftar. Bukan kesalahan: firewall
	// memang opsional.
	if _, ada := lookBinary("ufw"); !ada {
		return nil
	}
	// Keadaan firewall dibaca SEKALI untuk seluruh putaran: pertanyaan "rule ini
	// sudah ada?" tidak perlu memanggil ufw lagi per port.
	status, err := ufwStatus()
	if err != nil {
		// Tanpa daftar rule yang bisa dipercaya tidak ada satu pun keputusan
		// yang boleh diambil: menambah bisa membuat rule kembar, mencabut bisa
		// mengenai rule yang justru sedang dipakai.
		return err
	}
	adaRule := map[string][]helperproto.UfwRule{}
	for _, r := range status.Rules {
		if r.Action != "allow" {
			continue
		}
		adaRule[r.Port+"/"+r.Proto] = append(adaRule[r.Port+"/"+r.Proto], r)
	}

	st := bacaStatePortKomponen()
	hasil := []entriStatePortKomponen{}
	diproses := map[string]bool{}
	for _, c := range components {
		if len(c.ports) == 0 {
			continue
		}
		label := labelPortKomponen(c)
		terpasang := komponenTerpasang(c)
		unitAda, unitAktif := statusUnitLayanan(c.Service)
		// "Tidak bisa dipastikan mati" berarti jangan dicabut. Dua keadaan yang
		// lolos: unitnya tidak ketemu (nama unitnya tidak bisa dipercaya) dan
		// unitnya hidup. Komponen yang tidak terpasang tidak lolos: tidak ada
		// layanan yang bisa memakai portnya.
		mau := terpasang && (unitAktif || !unitAda)
		diproses[c.Name] = true
		for _, p := range c.ports {
			kandidat := adaRule[p.Port+"/"+p.Proto]
			if mau {
				hasil = append(hasil, pastikanPortKomponen(c, label, p, kandidat, st)...)
			} else {
				hasil = append(hasil, cabutPortKomponen(c, label, p, kandidat, st)...)
			}
		}
	}
	// Catatan milik komponen yang sudah tidak ada di katalog dibiarkan apa
	// adanya: rule-nya tidak boleh dicabut oleh kode yang tidak lagi tahu apa
	// itu, dan menghapus catatannya sama dengan melupakannya selamanya.
	for _, e := range st.Rules {
		if !diproses[e.Komponen] {
			hasil = append(hasil, e)
		}
	}
	return simpanStateKomponenJikaBerubah(st, hasil)
}

// pastikanPortKomponen memastikan satu port komponen punya rule berlabel, tanpa
// pernah melonggarkan cakupan yang sudah diatur.
func pastikanPortKomponen(c *component, label string, p portKomponen, kandidat []helperproto.UfwRule, st statePortKomponen) []entriStatePortKomponen {
	punyaLabel := []helperproto.UfwRule{}
	tanpaLabel := []helperproto.UfwRule{}
	for _, r := range kandidat {
		switch r.Comment {
		case label:
			punyaLabel = append(punyaLabel, r)
		case "":
			tanpaLabel = append(tanpaLabel, r)
		}
	}
	// Rule berlabel kita sudah ada. Ini yang menang atas catatan: rule yang
	// hidup adalah kenyataan, jadi penanda "diabaikan" dari penghapusan
	// sebelumnya tidak berlaku lagi.
	if len(punyaLabel) > 0 {
		hasil := make([]entriStatePortKomponen, 0, len(punyaLabel))
		for _, r := range punyaLabel {
			hasil = append(hasil, entriPort(c, p, sumberRule(r.From), true))
		}
		return hasil
	}
	// Port ini pernah dihapus user sendiri lewat halaman Firewall. Aturannya
	// TIDAK dibuat ulang selama layanannya masih hidup: menghapusnya adalah
	// keputusan user, bukan kesalahan yang perlu diperbaiki otomatis.
	if adaCatatanDiabaikan(st, c.Name, p) {
		return catatanKomponen(st, c.Name, p)
	}
	// Rule lama tanpa label — dibuat panel versi sebelum label ada. Dilabeli
	// tanpa mengubah cakupannya: `ufw allow` dengan bentuk yang sama
	// MEMPERBARUI rule itu di tempat (ufw mencetak "Rule updated", diuji
	// langsung di mesin ini), jadi tidak ada hapus-tambah, tidak ada jeda saat
	// portnya tertutup, dan aturan yang sengaja dibatasi ke subnet lokal tidak
	// ikut dilonggarkan.
	hasil := make([]entriStatePortKomponen, 0, len(tanpaLabel))
	for _, r := range tanpaLabel {
		dari := sumberRule(r.From)
		if err := ufwAdd(aturanPortKomponen(p, dari, label)); err != nil {
			log.Printf("firewall: gagal memberi label %s/%s (%s): %v", p.Port, p.Proto, label, err)
			continue
		}
		hasil = append(hasil, entriPort(c, p, dari, true))
	}
	if len(hasil) > 0 {
		return hasil
	}
	// Belum ada apa pun: buka aturannya.
	if err := ufwAdd(aturanPortKomponen(p, "", label)); err != nil {
		log.Printf("firewall: gagal mengizinkan %s/%s (%s) untuk %s: %v",
			p.Port, p.Proto, p.Guna, c.Name, err)
		// Tidak dicatat: catatan tanpa rule membuat putaran berikutnya
		// menganggap aturannya sudah ada.
		return nil
	}
	return []entriStatePortKomponen{entriPort(c, p, "", true)}
}

// cabutPortKomponen mencabut rule milik panel untuk satu port komponen yang
// layanannya sudah tidak ada atau sudah mati.
func cabutPortKomponen(c *component, label string, p portKomponen, kandidat []helperproto.UfwRule, st statePortKomponen) []entriStatePortKomponen {
	dicabut := map[string]bool{}
	sisa := []entriStatePortKomponen{}
	for _, e := range st.Rules {
		if e.Komponen != c.Name || e.Port != p.Port || e.Proto != p.Proto {
			continue
		}
		// Dicek pakai aturan port, bukan kunci catatan apa adanya: proto boleh
		// tertulis "any" di satu sisi dan "tcp" di sisi lain.
		if !e.Dibuat || e.Diabaikan {
			// Tidak ada yang perlu dicabut, dan catatannya dibuang (bukan
			// disimpan) supaya layanan yang hidup lagi membuka portnya kembali
			// tanpa warisan penanda dari kehidupan sebelumnya.
			continue
		}
		if err := ufwHapusRule(aturanPortKomponen(p, e.Dari, label)); err != nil {
			log.Printf("firewall: gagal mencabut izin %s/%s untuk %s: %v",
				p.Port, p.Proto, c.Name, err)
			sisa = append(sisa, e)
			continue
		}
		dicabut[e.Dari] = true
	}
	// Rule berlabel kita yang tidak lagi punya catatan (berkas catatannya
	// hilang) juga dicabut: izin untuk layanan yang sudah mati tidak boleh tetap
	// terbuka hanya karena catatannya lenyap.
	for _, r := range kandidat {
		if r.Comment != label {
			continue
		}
		if dari := sumberRule(r.From); !dicabut[dari] {
			_ = ufwHapusRule(aturanPort(p, dari))
		}
	}
	// Rule lama tanpa label milik komponen ini juga dicabut — kalau tidak,
	// izin layanan yang paketnya sudah dihapus (631/tcp sesudah CUPS dicopot)
	// akan bertahan selamanya, dan justru itulah yang paling sering terjadi
	// pada mesin yang sudah lama dipakai.
	//
	// Dua pengaman dipasang sebelum menyentuhnya, karena rule tanpa label tidak
	// bisa dipastikan milik panel:
	//   - cakupannya harus Anywhere atau subnet lokal, bentuk yang memang
	//     ditulis panel; rule yang dibatasi ke alamat lain milik user;
	//   - portnya tidak sedang dipublikasikan container yang jalan. Port yang
	//     dideklarasikan komponen bisa juga dipakai container (443 milik
	//     Stalwart vs reverse proxy di container), dan mencabutnya berarti
	//     menutup layanan yang justru sedang melayani.
	dariLokal := subnetLokal()
	for _, r := range kandidat {
		if r.Comment != "" {
			continue
		}
		dari := sumberRule(r.From)
		if dari != "" && dari != dariLokal {
			continue
		}
		if dipakai, pasti := portTerbitDocker(p.Port, p.Proto); !pasti || dipakai {
			continue
		}
		_ = ufwHapusRule(aturanPort(p, dari))
	}
	return sisa
}

// portTerbitDocker menjawab apakah sebuah port host sedang dipublikasikan
// container yang jalan.
//
// Kembalian kedua berarti "jawabannya bisa dipastikan". Pembacaan docker yang
// gagal BUKAN "tidak ada yang memakai": tanpa kepastian itu, satu kegagalan
// docker sudah cukup untuk menghapus rule yang menopang layanan lain.
func portTerbitDocker(port, proto string) (dipakai, pasti bool) {
	if _, ada := lookBinary("docker"); !ada {
		// Tidak ada docker = tidak ada container yang bisa memakainya.
		return false, true
	}
	daftar, err := portDockerBerjalan()
	if err != nil && !errors.Is(err, errDockerTidakLengkap) {
		return false, false
	}
	lengkap := err == nil
	for _, p := range daftar {
		if p.Port == port && protoCocokDengan(p.Proto, proto) {
			return true, true
		}
	}
	// Daftarnya tidak lengkap dan portnya tidak ketemu: bisa jadi container
	// yang memakainya justru yang gagal dibaca.
	return false, lengkap
}

func entriPort(c *component, p portKomponen, dari string, dibuat bool) entriStatePortKomponen {
	return entriStatePortKomponen{
		Port: p.Port, Proto: p.Proto, Dari: dari, Komponen: c.Name, Dibuat: dibuat,
	}
}

// adaCatatanDiabaikan menjawab apakah port ini pernah dihapus user sendiri lewat
// halaman Firewall. Catatan biasa (rule yang memang dipegang panel) sengaja TIDAK
// menahan pembuatan ulang: rule yang hilang di luar panel — `ufw reset`, atau
// dihapus dari terminal — harus kembali selama layanannya memang hidup, dan
// itulah yang diminta dari reconciler ini.
func adaCatatanDiabaikan(st statePortKomponen, nama string, p portKomponen) bool {
	for _, e := range st.Rules {
		if e.Komponen == nama && e.Port == p.Port && protoCocokDengan(e.Proto, p.Proto) && e.Diabaikan {
			return true
		}
	}
	return false
}

// catatanKomponen mempertahankan catatan lama sebuah port apa adanya — dipakai
// saat port-nya tidak boleh dibuka kembali karena user sendiri yang
// menghapusnya.
func catatanKomponen(st statePortKomponen, nama string, p portKomponen) []entriStatePortKomponen {
	var out []entriStatePortKomponen
	for _, e := range st.Rules {
		if e.Komponen == nama && e.Port == p.Port && protoCocokDengan(e.Proto, p.Proto) {
			out = append(out, e)
		}
	}
	return out
}

// tandaiPortKomponenDihapus mencatat bahwa user menghapus rule ini sendiri dari
// halaman Firewall, supaya reconciler tidak membuatnya ulang selama layanannya
// masih hidup.
func tandaiPortKomponenDihapus(port, proto string) {
	muPortKomponen.Lock()
	defer muPortKomponen.Unlock()
	st := bacaStatePortKomponen()
	ubah := false
	for i := range st.Rules {
		e := &st.Rules[i]
		if e.Port != port || !protoCocokDengan(e.Proto, proto) {
			continue
		}
		if !e.Diabaikan || e.Dibuat {
			e.Diabaikan = true
			e.Dibuat = false
			ubah = true
		}
	}
	if !ubah {
		return
	}
	if err := tulisStatePortKomponen(st); err != nil {
		log.Printf("firewall: gagal mencatat penghapusan rule %s/%s: %v", port, proto, err)
	}
}

// tandaiPortDihapusUser memberitahukan SEMUA reconciler bahwa satu rule dihapus
// dari halaman Firewall, supaya tidak ada yang membuatnya ulang: port container
// maupun port komponen.
func tandaiPortDihapusUser(port, proto string) {
	tandaiPortDockerDihapus(port, proto)
	tandaiPortKomponenDihapus(port, proto)
}

// lupakanPortKomponen membuang seluruh catatan milik satu komponen. Dipakai saat
// komponennya dicopot: mulai sekarang port itu bukan urusan panel lagi.
func lupakanPortKomponen(nama string) {
	muPortKomponen.Lock()
	defer muPortKomponen.Unlock()
	st := bacaStatePortKomponen()
	var sisa []entriStatePortKomponen
	for _, e := range st.Rules {
		if e.Komponen != nama {
			sisa = append(sisa, e)
		}
	}
	st.Rules = sisa
	if err := tulisStatePortKomponen(st); err != nil {
		log.Printf("firewall: gagal membersihkan catatan port %s: %v", nama, err)
	}
}

// simpanStateKomponenJikaBerubah hanya menulis berkas kalau isinya memang
// berbeda: reconciler berjalan tiap setengah menit, dan menulis ulang berkas
// yang sama ribuan kali sehari tidak memberi keterangan apa pun.
func simpanStateKomponenJikaBerubah(lama statePortKomponen, hasil []entriStatePortKomponen) error {
	if kunciStateKomponen(lama.Rules) == kunciStateKomponen(hasil) {
		return nil
	}
	return tulisStatePortKomponen(statePortKomponen{Rules: hasil})
}

// kunciStateKomponen adalah bentuk yang bisa dibandingkan dari daftar catatan.
func kunciStateKomponen(daftar []entriStatePortKomponen) string {
	k := make([]string, 0, len(daftar))
	for _, e := range daftar {
		k = append(k, kunciPortKomponen(e.Komponen, e.Port, e.Proto, e.Dari)+
			"|dibuat="+boolStr(e.Dibuat)+"|diabaikan="+boolStr(e.Diabaikan))
	}
	sort.Strings(k)
	return strings.Join(k, "\n")
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// bacaStatePortKomponen membaca catatan rule reconciler. Berkas yang hilang
// atau rusak diperlakukan sebagai catatan kosong — akibatnya paling buruk rule
// yang sudah ada dianggap milik pihak lain dan tidak pernah dicabut, bukan rule
// milik user yang terhapus.
func bacaStatePortKomponen() statePortKomponen {
	st := statePortKomponen{Version: versiStatePortKomponen}
	b, err := os.ReadFile(pathStatePortKomponen)
	if err != nil {
		return st
	}
	var dibaca statePortKomponen
	if err := json.Unmarshal(b, &dibaca); err != nil {
		log.Printf("firewall: catatan port komponen %s tidak terbaca (%v) — dimulai dari kosong",
			pathStatePortKomponen, err)
		return st
	}
	if dibaca.Version == 0 {
		dibaca.Version = versiStatePortKomponen
	}
	return dibaca
}

func tulisStatePortKomponen(st statePortKomponen) error {
	st.Version = versiStatePortKomponen
	if st.Rules == nil {
		st.Rules = []entriStatePortKomponen{}
	}
	sort.Slice(st.Rules, func(i, j int) bool {
		a, b := st.Rules[i], st.Rules[j]
		if a.Komponen != b.Komponen {
			return a.Komponen < b.Komponen
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Proto < b.Proto
	})
	return tulisJSONAtomik(pathStatePortKomponen, st)
}

// tulisJSONAtomik menulis catatan lewat berkas sementara + rename, supaya helper
// yang mati di tengah penulisan tidak meninggalkan JSON setengah jadi yang
// membuat seluruh rule kehilangan pemiliknya.
func tulisJSONAtomik(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	sementara := path + ".baru"
	if err := os.WriteFile(sementara, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(sementara, path); err != nil {
		_ = os.Remove(sementara)
		return err
	}
	return nil
}
