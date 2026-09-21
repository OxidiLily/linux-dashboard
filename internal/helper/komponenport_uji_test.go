package helper

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---- perkakas uji reconciler port komponen ----

// unitTiruan adalah jawaban tiruan atas "unit ini ada dan hidup?" — namanya
// dibedakan dari keadaanUnit() di system.go yang membaca keadaan sungguhan.
type unitTiruan struct {
	ada   bool
	aktif bool
}

// ujiKomponenPort menyiapkan binary `ufw` tiruan plus jawaban tiruan atas
// pertanyaan tentang komponen dan unit systemd-nya.
//
// `ufw` tiruannya MENGINGAT label dan cakupan rule (`spec|label`), karena
// seluruh pekerjaan reconciler ini adalah membedakan rule berlabel milik panel
// dari rule lama tanpa label. Tiruan yang membuang labelnya akan membuat test
// mengukur perilaku yang berbeda dari yang berjalan di mesin sungguhan.
//
// Dua hal yang tidak bisa ditirukan lewat PATH dijadikan variabel: pertanyaan
// "komponen ini terpasang?" dan "unit ini ada dan hidup?". Keduanya bergantung
// pada isi mesin yang menjalankan test — `lookBinary` selalu menemukan binary
// sungguhan, dan unit systemd yang kebetulan terpasang di mesin pengembang tidak
// sama dengan di mesin lain.
type ujiKomponenPort struct {
	t         *testing.T
	dir       string
	ufwLog    string
	unit      map[string]unitTiruan
	terpasang map[string]bool
}

func siapkanUjiKomponenPort(t *testing.T) *ujiKomponenPort {
	t.Helper()
	dir := t.TempDir()
	u := &ujiKomponenPort{
		t:         t,
		dir:       dir,
		ufwLog:    filepath.Join(dir, "ufw.log"),
		unit:      map[string]unitTiruan{},
		terpasang: map[string]bool{},
	}

	lamaPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+lamaPath)
	lamaExec := pathExec
	pathExec = dir + ":" + lamaExec
	lamaState := pathStatePortKomponen
	pathStatePortKomponen = filepath.Join(dir, "komponen-ports.json")
	lamaStatus := statusUnitLayanan
	statusUnitLayanan = func(unit string) (bool, bool) {
		if k, ok := u.unit[unit]; ok {
			return k.ada, k.aktif
		}
		// Default: unit tidak dikenal — bentuk yang paling aman, karena
		// reconciler tidak boleh menyimpulkan apa pun darinya.
		return false, false
	}
	lamaTerpasang := komponenTerpasang
	komponenTerpasang = func(c *component) bool { return u.terpasang[c.Name] }
	t.Cleanup(func() {
		pathExec = lamaExec
		pathStatePortKomponen = lamaState
		statusUnitLayanan = lamaStatus
		komponenTerpasang = lamaTerpasang
	})

	u.tulisSkrip("ufw", skripUfwKomponen(dir, u.ufwLog))
	// Docker tiruan ikut disiapkan: reconciler port komponen menanyanya sebelum
	// mencabut rule lama tanpa label. Default-nya jalan tanpa container, jadi
	// pertanyaannya terjawab "tidak ada yang memakainya".
	u.tulisSkrip("docker", `#!/bin/sh
case "$1" in
  ps)      cat "`+dir+`/ps.txt" ;;
  inspect) cat "`+dir+`/inspect.txt" ;;
esac
`)
	u.setDockerKosong()
	u.tulis("mode.txt", "active\n")
	u.tulis("rules.txt", "")
	return u
}

// setDockerKosong menyatakan docker jalan tanpa container berjalan.
func (u *ujiKomponenPort) setDockerKosong() {
	u.t.Helper()
	u.tulis("ps.txt", "")
	u.tulis("inspect.txt", "")
}

// setDockerTerbit menyatakan sebuah container berjalan dan mempublikasikan port
// host itu — bentuk yang membuat rule tanpa label bukan milik komponen, karena
// ada layanan lain yang memakainya.
func (u *ujiKomponenPort) setDockerTerbit(hostPort, proto string) {
	u.t.Helper()
	u.tulis("ps.txt", "id1\n")
	u.tulis("inspect.txt", barisInspect("lain", "running", "", "bridge", map[string][]bindingPortDocker{
		"80/" + proto: ikatanDocker("0.0.0.0", hostPort),
	})+"\n")
}

// setDockerGagal menyatakan pembacaan docker gagal (daemon mati, socket tidak
// bisa dihubungi).
func (u *ujiKomponenPort) setDockerGagal() {
	u.t.Helper()
	_ = os.Remove(filepath.Join(u.dir, "ps.txt"))
	_ = os.Remove(filepath.Join(u.dir, "inspect.txt"))
}

// skripUfwKomponen adalah ufw tiruan: `allow` menyimpan bentuk rule beserta
// labelnya, `status`/`show added` membacanya kembali dalam bentuk yang sama
// dengan keluaran ufw sungguhan, dan `--force delete` mencocokkan rule TANPA
// labelnya — persis seperti ufw sungguhan (diuji langsung di mesin ini).
func skripUfwKomponen(dir, log string) string {
	return `#!/bin/sh
R="` + dir + `/rules.txt"
touch "$R"
printf '%s\n' "$*" >> "` + log + `"
case "$1" in
  status)
    if [ "$(cat "` + dir + `/mode.txt")" = "inactive" ]; then
      echo "Status: inactive"
      exit 0
    fi
    echo "Status: active"
    echo
    echo "     To                         Action      From"
    echo "     --                         ------      ----"
    i=0
    while IFS= read -r r || [ -n "$r" ]; do
      [ -n "$r" ] || continue
      i=$((i+1))
      spec=${r%%|*}
      label=${r#*|}
      set -- $spec
      if [ "$1" = "from" ]; then
        baris="$6/$8 ALLOW IN $2"
      else
        baris="$1 ALLOW IN Anywhere"
      fi
      if [ -n "$label" ]; then
        echo "[$i] $baris # $label"
      else
        echo "[$i] $baris"
      fi
    done < "$R"
    ;;
  show)
    echo "Added user rules (see 'ufw status' for running firewall):"
    while IFS= read -r r || [ -n "$r" ]; do
      [ -n "$r" ] || continue
      spec=${r%%|*}
      label=${r#*|}
      if [ -n "$label" ]; then
        echo "ufw allow $spec comment '$label'"
      else
        echo "ufw allow $spec"
      fi
    done < "$R"
    ;;
  allow)
    shift
    label=""
    spec=""
    while [ $# -gt 0 ]; do
      if [ "$1" = "comment" ]; then shift; label="$1"; else spec="$spec $1"; fi
      shift
    done
    spec=$(echo $spec)
    if grep -qF "$spec|" "$R" 2>/dev/null; then
      awk -F'|' -v c="$spec" -v l="$label" 'BEGIN{OFS="|"} { if ($1 == c) { print c, l } else { print } }' "$R" > "$R.tmp"
      mv "$R.tmp" "$R"
    else
      echo "$spec|$label" >> "$R"
    fi
    ;;
  --force)
    [ "$2" = "delete" ] || exit 0
    shift 2
    [ "$1" = "allow" ] && shift
    cari=""
    while [ $# -gt 0 ]; do
      if [ "$1" = "comment" ]; then shift 2; continue; fi
      cari="$cari $1"
      shift
    done
    cari=$(echo $cari)
    awk -F'|' -v c="$cari" '$1 != c' "$R" > "$R.tmp"
    mv "$R.tmp" "$R"
    ;;
esac
`
}

func (u *ujiKomponenPort) tulis(nama, isi string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(isi), 0o644); err != nil {
		u.t.Fatalf("tulis %s: %v", nama, err)
	}
}

func (u *ujiKomponenPort) tulisSkrip(nama, isi string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(isi), 0o755); err != nil {
		u.t.Fatalf("tulis skrip %s: %v", nama, err)
	}
}

// setTerpasang menyatakan komponen ini ada di mesin (binary-nya ada).
func (u *ujiKomponenPort) setTerpasang(nama string, v bool) {
	u.terpasang[nama] = v
}

// setUnit menyatakan unit systemd komponen ada dan hidup (atau tidak).
func (u *ujiKomponenPort) setUnit(unit string, ada, aktif bool) {
	u.unit[unit] = unitTiruan{ada: ada, aktif: aktif}
}

// setRuleUfw menaruh satu rule di firewall tiruan apa adanya: bentuknya
// ("445/tcp" atau "from 192.168.2.0/24 to any port 631 proto tcp") beserta
// labelnya (kosong = rule lama tanpa label).
func (u *ujiKomponenPort) setRuleUfw(spec, label string) {
	u.t.Helper()
	u.tulis("rules.txt", spec+"|"+label+"\n")
}

// tambahRuleUfw menambahkan rule tanpa menghapus yang sudah ada.
func (u *ujiKomponenPort) tambahRuleUfw(spec, label string) {
	u.t.Helper()
	b := bacaMentah(u.t, filepath.Join(u.dir, "rules.txt"))
	u.tulis("rules.txt", b+spec+"|"+label+"\n")
}

// setUfwNonaktif menyatakan ufw terpasang tapi belum dinyalakan.
func (u *ujiKomponenPort) setUfwNonaktif() {
	u.t.Helper()
	u.tulis("mode.txt", "inactive\n")
}

// hapusUfw menghilangkan binary ufw tiruan — bentuk mesin yang belum pernah
// memasang firewall.
func (u *ujiKomponenPort) hapusUfw() {
	u.t.Helper()
	_ = os.Remove(filepath.Join(u.dir, "ufw"))
}

// tulisState menulis catatan reconciler langsung, tanpa lewat penyelarasan.
func (u *ujiKomponenPort) tulisState(st statePortKomponen) {
	u.t.Helper()
	if err := tulisStatePortKomponen(st); err != nil {
		u.t.Fatalf("tulis catatan: %v", err)
	}
}

func (u *ujiKomponenPort) sinkron() {
	u.t.Helper()
	if err := sinkronkanPortKomponen(); err != nil {
		u.t.Fatalf("sinkronkanPortKomponen: %v", err)
	}
}

// panggilanUfw mengembalikan setiap baris argumen yang diterima ufw tiruan.
func (u *ujiKomponenPort) panggilanUfw() []string {
	u.t.Helper()
	b, err := os.ReadFile(u.ufwLog)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// izinDibuka adalah panggilan yang MENAMBAH rule, apa adanya — termasuk
// labelnya, karena label itulah yang diuji di sini.
func (u *ujiKomponenPort) izinDibuka() []string {
	var out []string
	for _, p := range u.panggilanUfw() {
		if strings.HasPrefix(p, "allow ") {
			out = append(out, strings.TrimPrefix(p, "allow "))
		}
	}
	return out
}

// izinDicabut adalah panggilan yang MENGHAPUS rule.
func (u *ujiKomponenPort) izinDicabut() []string {
	var out []string
	for _, p := range u.panggilanUfw() {
		if strings.HasPrefix(p, "--force delete allow ") {
			out = append(out, strings.TrimPrefix(p, "--force delete allow "))
		}
	}
	return out
}

// ruleUfw adalah isi firewall tiruan sesudah semua panggilan di atas, dalam
// bentuk "spec|label".
func (u *ujiKomponenPort) ruleUfw() []string {
	u.t.Helper()
	var out []string
	for _, r := range strings.Split(bacaMentah(u.t, filepath.Join(u.dir, "rules.txt")), "\n") {
		if strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	return out
}

func (u *ujiKomponenPort) state() statePortKomponen { return bacaStatePortKomponen() }

// harusSama membandingkan dua daftar tanpa memperhatikan urutan.
func (u *ujiKomponenPort) harusSama(apa string, dapat, mau []string) {
	u.t.Helper()
	d := append([]string(nil), dapat...)
	m := append([]string(nil), mau...)
	sort.Strings(d)
	sort.Strings(m)
	if strings.Join(d, "\u0000") != strings.Join(m, "\u0000") {
		u.t.Errorf("%s: dapat %v, mau %v", apa, dapat, mau)
	}
}

func (u *ujiKomponenPort) harusKosong(apa string, daftar []string) {
	u.t.Helper()
	if len(daftar) != 0 {
		u.t.Errorf("%s: harus kosong, ternyata %v", apa, daftar)
	}
}
