package helper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---- perkakas uji ----

// ujiDockerPort menyiapkan binary `docker` dan `ufw` tiruan di PATH plus
// berkas catatan reconciler di direktori sementara.
//
// `docker` tiruan hanya mencetak berkas yang ditulis test (ps.txt,
// inspect.txt): setiap langkah uji menyatakan sendiri container mana yang
// sedang berjalan, jadi tidak ada logika shell yang perlu dipercaya.
//
// `ufw` tiruan sebaliknya MENYIMPAN rule-nya di rules.txt dan melayani
// `status`/`show added`/`allow`/`--force delete` dari sana. Itu perlu: sesudah
// reconciler menambah atau mencabut rule, ufw sungguhan ikut berubah, dan
// reconciler yang memeriksa hasil pekerjaannya sendiri hanya bisa diuji kalau
// tiruannya berubah juga. Tanpa itu setiap putaran tampak seperti rule yang
// hilang di luar panel, dan yang terukur bukan lagi perilaku yang sebenarnya.
type ujiDockerPort struct {
	t         *testing.T
	dir       string
	ufwLog    string
	dockerLog string
}

func siapkanUjiDockerPort(t *testing.T) *ujiDockerPort {
	t.Helper()
	dir := t.TempDir()
	u := &ujiDockerPort{
		t:         t,
		dir:       dir,
		ufwLog:    filepath.Join(dir, "ufw.log"),
		dockerLog: filepath.Join(dir, "docker.log"),
	}

	// PATH proses: dipakai lookBinary/exec.LookPath untuk menemukan tiruannya.
	lamaPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+lamaPath)
	// PATH anak proses: run() memasang PATH-nya sendiri, jadi harus diarahkan
	// ke direktori yang sama.
	lamaExec := pathExec
	pathExec = dir + ":" + lamaExec
	lamaState := pathStatePortDocker
	pathStatePortDocker = filepath.Join(dir, "docker-ports.json")
	t.Cleanup(func() { pathExec = lamaExec; pathStatePortDocker = lamaState })

	u.tulisSkrip("docker", `#!/bin/sh
printf '%s\n' "$*" >> "`+u.dockerLog+`"
case "$1" in
  ps)      cat "`+dir+`/ps.txt" ;;
  inspect)
    G="`+dir+`/inspect-gagal.txt"
    if [ -f "$G" ] && echo "$*" | grep -qF -- "$(cat "$G")"; then exit 1; fi
    cat "`+dir+`/inspect.txt" ;;
esac
`)
	u.tulisSkrip("ufw", `#!/bin/sh
R="`+dir+`/rules.txt"
[ -f "`+dir+`/gagal.txt" ] && exit 1
touch "$R"
printf '%s\n' "$*" >> "`+u.ufwLog+`"
case "$1" in
  status)
    if [ "$(cat "`+dir+`/mode.txt")" = "inactive" ]; then
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
      printf '[%d] %s ALLOW IN Anywhere\n' "$i" "$r"
    done < "$R"
    ;;
  show)
    echo "Added user rules (see 'ufw status' for running firewall):"
    while IFS= read -r r || [ -n "$r" ]; do
      [ -n "$r" ] && echo "ufw allow $r"
    done < "$R"
    ;;
  allow)
    grep -qxF "${2:-}" "$R" 2>/dev/null || echo "${2:-}" >> "$R"
    ;;
  --force)
    [ "${2:-}" = "delete" ] || exit 0
    shift 2
    [ "${1:-}" = "allow" ] && shift
    grep -vxF "$*" "$R" > "$R.tmp" 2>/dev/null
    mv "$R.tmp" "$R"
    ;;
esac
`)
	u.setUfwAktif()
	u.setDockerBerjalan()
	return u
}

func (u *ujiDockerPort) tulis(nama, isi string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(isi), 0o644); err != nil {
		u.t.Fatalf("tulis %s: %v", nama, err)
	}
}

func (u *ujiDockerPort) tulisSkrip(nama, isi string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(isi), 0o755); err != nil {
		u.t.Fatalf("tulis skrip %s: %v", nama, err)
	}
}

// dockerMati membuat `docker ps` gagal — bentuk yang sama dengan daemon yang
// belum siap atau socket yang tidak bisa dihubungi.
func (u *ujiDockerPort) dockerMati() {
	u.t.Helper()
	_ = os.Remove(filepath.Join(u.dir, "ps.txt"))
}

// setDockerBerjalan menyatakan container yang sedang jalan beserta seluruh
// publikasi portnya.
func (u *ujiDockerPort) setDockerBerjalan(baris ...string) {
	u.t.Helper()
	u.tulis("ps.txt", "id1\n")
	if len(baris) == 0 {
		u.tulis("inspect.txt", "")
		return
	}
	u.tulis("inspect.txt", strings.Join(baris, "\n")+"\n")
}

// setDockerTidakLengkap menyatakan 21 container yang berjalan dengan batch
// `docker inspect` kedua sengaja gagal. Daftarnya jadi tidak bisa dipastikan
// lengkap: satu-satunya container berport ada di batch pertama.
func (u *ujiDockerPort) setDockerTidakLengkap(t *testing.T) {
	t.Helper()
	u.tulis("inspect-gagal.txt", "id21")
	baris := []string{barisInspect("baru", "running", "", "bridge", map[string][]bindingPortDocker{
		"80/tcp": ikatanDocker("0.0.0.0", "29999"),
	})}
	for i := 2; i <= 21; i++ {
		baris = append(baris, barisInspect("polos", "running", "", "bridge", nil))
	}
	u.tulis("inspect.txt", strings.Join(baris, "\n")+"\n")
	ids := make([]string, 0, 21)
	for i := 1; i <= 21; i++ {
		ids = append(ids, "id"+strconv.Itoa(i))
	}
	u.tulis("ps.txt", strings.Join(ids, "\n")+"\n")
}

// setUfwAktif menyatakan ufw yang menyala dengan rule awal yang diberikan,
// sebagai kolom "To" (mis. "19999/tcp"). Kosong berarti tidak ada rule.
func (u *ujiDockerPort) setUfwAktif(rule ...string) {
	u.t.Helper()
	u.tulis("mode.txt", "active\n")
	u.tulis("rules.txt", barisRule(rule))
}

// setUfwNonaktif menyatakan ufw yang terpasang tapi belum dinyalakan: rule-nya
// hanya terlihat lewat `ufw show added`, tanpa nomor.
func (u *ujiDockerPort) setUfwNonaktif(rule ...string) {
	u.t.Helper()
	u.tulis("mode.txt", "inactive\n")
	u.tulis("rules.txt", barisRule(rule))
}

// barisRule menutup daftar dengan baris baru: berkas yang baris terakhirnya
// tidak diakhiri newline membuat `while read` di tiruan ufw berhenti sebelum
// memproses baris itu, dan rule yang sebenarnya ada jadi tidak terlihat.
func barisRule(rule []string) string {
	if len(rule) == 0 {
		return ""
	}
	return strings.Join(rule, "\n") + "\n"
}

// hapusUfw menghilangkan binary ufw tiruan — bentuk yang sama dengan mesin yang
// belum pernah memasang firewall.
func (u *ujiDockerPort) hapusUfw() {
	u.t.Helper()
	_ = os.Remove(filepath.Join(u.dir, "ufw"))
}

// ufwGagal membuat setiap panggilan ufw keluar dengan status bukan nol —
// bentuk yang sama dengan ufw yang gagal dijalankan.
func (u *ujiDockerPort) ufwGagal() {
	u.t.Helper()
	u.tulis("gagal.txt", "1\n")
}

func (u *ujiDockerPort) sinkron() {
	u.t.Helper()
	if err := sinkronkanPortDocker(); err != nil {
		u.t.Fatalf("sinkronkanPortDocker: %v", err)
	}
}

func (u *ujiDockerPort) bacaLog(path string) []string {
	u.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// panggilanUfw mengembalikan setiap baris argumen yang diterima ufw tiruan.
func (u *ujiDockerPort) panggilanUfw() []string { return u.bacaLog(u.ufwLog) }

// izinDibuka adalah panggilan yang MENAMBAH rule, tanpa label pemiliknya.
// Sebagian besar test membandingkan bentuk ini: yang diuji adalah rule mana yang
// dibuka, bukan teks labelnya — labelnya diperiksa lewat labelDibuka.
func (u *ujiDockerPort) izinDibuka() []string {
	var out []string
	for _, p := range u.panggilanUfw() {
		if strings.HasPrefix(p, "allow ") {
			out = append(out, tanpaLabel(strings.TrimPrefix(p, "allow ")))
		}
	}
	return out
}

// labelDibuka adalah label pemilik yang ikut ditulis pada setiap penambahan
// rule, dalam urutan penulisannya.
func (u *ujiDockerPort) labelDibuka() []string {
	var out []string
	for _, p := range u.panggilanUfw() {
		if !strings.HasPrefix(p, "allow ") {
			continue
		}
		if i := strings.Index(p, " comment "); i >= 0 {
			out = append(out, p[i+len(" comment "):])
		}
	}
	return out
}

// tanpaLabel membuang bagian "comment <label>" dari satu bentuk rule.
func tanpaLabel(spec string) string {
	if i := strings.Index(spec, " comment "); i >= 0 {
		return spec[:i]
	}
	return spec
}

// izinDicabut adalah panggilan yang MENGHAPUS rule.
func (u *ujiDockerPort) izinDicabut() []string {
	var out []string
	for _, p := range u.panggilanUfw() {
		if strings.HasPrefix(p, "--force delete allow ") {
			out = append(out, strings.TrimPrefix(p, "--force delete allow "))
		}
	}
	return out
}

// ruleUfw adalah isi daftar rule ufw tiruan sesudah semua panggilan di atas.
func (u *ujiDockerPort) ruleUfw() []string {
	var out []string
	for _, r := range u.bacaLog(filepath.Join(u.dir, "rules.txt")) {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func (u *ujiDockerPort) state() statePortDocker { return bacaStatePortDocker() }

// bacaMentah membaca berkas apa adanya — dipakai untuk memeriksa bentuk JSON
// catatan, bukan hanya hasil pembacaannya.
func bacaMentah(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baca %s: %v", path, err)
	}
	return string(b)
}

// barisInspect menyusun satu baris keluaran `docker inspect --format` yang
// dipakai reconciler. Bentuk ini kontrak antara formatInspectDocker dan
// portDariBarisInspect: kalau template-nya berubah, di sini tempat menyesuaikan.
func barisInspect(nama, status, health, jaringan string, ports map[string][]bindingPortDocker) string {
	js := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	hp := "null"
	if health != "" {
		hp = js(health)
	}
	p := "null"
	if ports != nil {
		p = js(ports)
	}
	return strings.Join([]string{js(status), hp, p, js(jaringan), js("/" + nama)}, "\t")
}

// ikatanDocker membangun satu daftar ikatan host untuk satu port container.
func ikatanDocker(hostIP, hostPort string) []bindingPortDocker {
	return []bindingPortDocker{{HostIP: hostIP, HostPort: hostPort}}
}

// harusSama membandingkan dua daftar rule tanpa memperhatikan urutan: urutan
// penambahan dan pencabutan mengikuti iterasi map, jadi urutannya memang tidak
// dijanjikan — yang dijanjikan hanya isinya.
func (u *ujiDockerPort) harusSama(apa string, dapat, mau []string) {
	u.t.Helper()
	d := append([]string(nil), dapat...)
	m := append([]string(nil), mau...)
	sort.Strings(d)
	sort.Strings(m)
	if strings.Join(d, ",") != strings.Join(m, ",") {
		u.t.Errorf("%s: dapat %v, mau %v", apa, dapat, mau)
	}
}

// harusUrut membandingkan dua daftar dengan urutan dipertahankan.
func harusUrut(t *testing.T, apa string, dapat, mau []string) {
	t.Helper()
	if strings.Join(dapat, ",") != strings.Join(mau, ",") {
		t.Errorf("%s: dapat %v, mau %v", apa, dapat, mau)
	}
}

func (u *ujiDockerPort) harusKosong(apa string, daftar []string) {
	u.t.Helper()
	if len(daftar) != 0 {
		u.t.Errorf("%s: harus kosong, ternyata %v", apa, daftar)
	}
}
