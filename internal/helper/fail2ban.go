package helper

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// fail2ban punya dua sisi yang harus dibaca terpisah:
//
//   - konfigurasi jail (jail.local) — apa yang DIMINTA aktif
//   - status runtime (`fail2ban-client status <jail>`) — apa yang BENAR-BENAR
//     jalan, berikut IP yang sedang diblokir
//
// Menampilkan satu saja menyesatkan: jail yang enabled di file tapi gagal
// start (log path tidak ada, misalnya) akan terlihat aman padahal tidak
// memblokir apa pun.
var (
	jailLocalPath = "/etc/fail2ban/jail.local"
	jailDDir      = "/etc/fail2ban/jail.d"
)

const (
	f2bTanda   = "# lindash-fail2ban"
	f2bDefBan  = "1h"
	f2bDefFind = "10m"
	f2bDefTry  = 5
)

var (
	jailNamaRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	durasiRe   = regexp.MustCompile(`^[0-9]+[smhdw]?$`)
	ipRe2      = regexp.MustCompile(`^[0-9a-fA-F:.]+(/[0-9]{1,3})?$`)
	sectionRe  = regexp.MustCompile(`^\[([^\]]+)\]$`)
	// (?m) WAJIB: tanpa itu `$` hanya cocok di ujung seluruh teks, sedangkan
	// baris ini selalu diikuti baris lain (atau newline penutup) — polanya tidak
	// pernah cocok dan jail yang jelas jalan dilaporkan "belum jalan".
	statusJail  = regexp.MustCompile(`(?m)Jail list:\s*(.*)$`)
	bannedIPRe  = regexp.MustCompile(`(?m)Banned IP list:\s*(.*)$`)
	currBanRe   = regexp.MustCompile(`Currently banned:\s*(\d+)`)
	totalBanRe  = regexp.MustCompile(`Total banned:\s*(\d+)`)
	currFailRe  = regexp.MustCompile(`Currently failed:\s*(\d+)`)
	totalFailRe = regexp.MustCompile(`Total failed:\s*(\d+)`)
)

func fail2banList() ([]helperproto.Fail2banJail, error) {
	if !installed("fail2ban-client") {
		return []helperproto.Fail2banJail{}, nil
	}
	konfig := bacaJailLocal()
	aktif := jailAktif()

	// Gabungkan: jail yang tertulis di jail.local + jail yang sedang jalan
	// (bisa berasal dari jail.conf bawaan atau file lain milik admin).
	urut := []string{}
	lihat := map[string]bool{}
	for _, j := range konfig {
		urut = append(urut, j.Name)
		lihat[j.Name] = true
	}
	for _, n := range aktif {
		if !lihat[n] {
			urut = append(urut, n)
			lihat[n] = true
		}
	}

	out := []helperproto.Fail2banJail{}
	for _, nama := range urut {
		j := helperproto.Fail2banJail{Name: nama, External: true}
		for _, k := range konfig {
			if k.Name == nama {
				j = k
				break
			}
		}
		for _, a := range aktif {
			if a == nama {
				j.Running = true
			}
		}
		if j.Running {
			isiStatusJail(&j)
			// Jail yang tidak ada di jail.local (mis. dinyalakan
			// jail.d/defaults-debian.conf) tidak punya nilai apa pun dari file
			// panel. Menampilkan "maxretry 0 · bantime -" untuk jail yang jelas
			// bekerja itu menyesatkan, jadi nilai efektifnya ditanyakan
			// langsung ke fail2ban.
			if j.External {
				isiKonfigEfektif(&j)
			}
		}
		out = append(out, j)
	}
	return out, nil
}

// bacaJailLocal membaca section jail di jail.local. Section yang didahului
// penanda panel dianggap milik panel; sisanya read-only.
func bacaJailLocal() []helperproto.Fail2banJail {
	b, err := os.ReadFile(jailLocalPath)
	if err != nil {
		return nil
	}
	var out []helperproto.Fail2banJail
	var cur *helperproto.Fail2banJail
	punyaPanel := false
	simpan := func() {
		if cur != nil && cur.Name != "DEFAULT" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t == f2bTanda {
			punyaPanel = true
			continue
		}
		if m := sectionRe.FindStringSubmatch(t); m != nil {
			simpan()
			cur = &helperproto.Fail2banJail{
				Name: m[1], External: !punyaPanel,
				MaxRetry: f2bDefTry, BanTime: f2bDefBan, FindTime: f2bDefFind,
			}
			punyaPanel = false
			continue
		}
		if cur == nil || t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, val, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "enabled":
			cur.Enabled = val == "true"
		case "maxretry":
			if n, err := strconv.Atoi(val); err == nil {
				cur.MaxRetry = n
			}
		case "bantime":
			cur.BanTime = val
		case "findtime":
			cur.FindTime = val
		case "port":
			cur.Port = val
		}
	}
	simpan()
	return out
}

func jailAktif() []string {
	res, err := run("fail2ban-client", "status")
	if err != nil {
		return nil
	}
	m := statusJail.FindStringSubmatch(res.Stdout)
	if m == nil {
		return nil
	}
	var out []string
	for _, n := range strings.Split(m[1], ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func isiStatusJail(j *helperproto.Fail2banJail) {
	res, err := run("fail2ban-client", "status", j.Name)
	if err != nil {
		return
	}
	angka := func(re *regexp.Regexp) int {
		if m := re.FindStringSubmatch(res.Stdout); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}
	j.CurrentlyBanned = angka(currBanRe)
	j.TotalBanned = angka(totalBanRe)
	j.CurrentlyFailed = angka(currFailRe)
	j.TotalFailed = angka(totalFailRe)
	if m := bannedIPRe.FindStringSubmatch(res.Stdout); m != nil {
		for _, ip := range strings.Fields(m[1]) {
			j.BannedIPs = append(j.BannedIPs, ip)
		}
	}
}

// isiKonfigEfektif menanyakan nilai yang benar-benar dipakai fail2ban untuk
// jail ini. `fail2ban-client get <jail> bantime` mengembalikan detik, jadi
// dikembalikan ke bentuk ringkas (600 → 10m) supaya sama dengan yang diketik
// user di form.
func isiKonfigEfektif(j *helperproto.Fail2banJail) {
	if v := getJailInt(j.Name, "maxretry"); v > 0 {
		j.MaxRetry = v
	}
	if v := getJailInt(j.Name, "bantime"); v > 0 {
		j.BanTime = detikRingkas(v)
	}
	if v := getJailInt(j.Name, "findtime"); v > 0 {
		j.FindTime = detikRingkas(v)
	}
	j.Enabled = true // jail yang dimuat fail2ban pasti enabled di suatu file
}

func getJailInt(jail, kunci string) int {
	res, err := run("fail2ban-client", "get", jail, kunci)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil {
		return 0
	}
	return n
}

func detikRingkas(d int) string {
	switch {
	case d%86400 == 0:
		return strconv.Itoa(d/86400) + "d"
	case d%3600 == 0:
		return strconv.Itoa(d/3600) + "h"
	case d%60 == 0:
		return strconv.Itoa(d/60) + "m"
	default:
		return strconv.Itoa(d) + "s"
	}
}

func fail2banSave(j helperproto.Fail2banJail) error {
	if !installed("fail2ban-client") {
		return errKode(helperproto.ErrBelumTerpasang, "fail2ban belum terpasang — pasang dulu lewat Components")
	}
	if !jailNamaRe.MatchString(j.Name) {
		return errInvalid("nama jail tidak valid")
	}
	if j.MaxRetry < 1 || j.MaxRetry > 100 {
		return errInvalid("maxretry harus 1–100")
	}
	for _, d := range []string{j.BanTime, j.FindTime} {
		if d != "" && !durasiRe.MatchString(d) {
			return errInvalid("durasi %q tidak valid — pakai format seperti 10m, 1h, 1d", d)
		}
	}
	if j.Port != "" && !regexp.MustCompile(`^[a-z0-9,:-]+$`).MatchString(j.Port) {
		return errInvalid("port %q tidak valid", j.Port)
	}
	lama := bacaJailLocal()
	for _, x := range lama {
		if x.Name == j.Name && x.External {
			return errInvalid("jail %q didefinisikan di luar panel — ubah filenya sendiri", j.Name)
		}
	}
	if err := tulisJailLocal(j.Name, susunJail(j)); err != nil {
		return err
	}
	return muatUlangFail2ban()
}

func susunJail(j helperproto.Fail2banJail) string {
	if j.BanTime == "" {
		j.BanTime = f2bDefBan
	}
	if j.FindTime == "" {
		j.FindTime = f2bDefFind
	}
	var b strings.Builder
	b.WriteString(f2bTanda + "\n[" + j.Name + "]\n")
	b.WriteString("enabled = " + map[bool]string{true: "true", false: "false"}[j.Enabled] + "\n")
	b.WriteString("maxretry = " + strconv.Itoa(j.MaxRetry) + "\n")
	b.WriteString("bantime = " + j.BanTime + "\n")
	b.WriteString("findtime = " + j.FindTime + "\n")
	if j.Port != "" {
		b.WriteString("port = " + j.Port + "\n")
	}
	return b.String()
}

// fail2banDelete membuang jail sepenuhnya: section milik panel di jail.local
// DAN stanza dengan nama sama di /etc/fail2ban/jail.d/*.conf.
//
// Membuang jail.local saja tidak cukup — Debian/Ubuntu menyalakan [sshd] lewat
// jail.d/defaults-debian.conf, jadi jail-nya kembali hidup dengan nilai bawaan
// dan tetap muncul di daftar. Yang dihapus HANYA stanza jail bersangkutan;
// section [DEFAULT] (banaction, backend) dan jail lain di file yang sama tidak
// disentuh, karena membuang seluruh file akan mematikan nftables/journal
// backend untuk semua jail.
func fail2banDelete(nama string) error {
	if !jailNamaRe.MatchString(nama) {
		return errInvalid("nama jail tidak valid")
	}
	if err := tulisJailLocal(nama, ""); err != nil {
		return err
	}
	if err := hapusStanzaJailD(nama); err != nil {
		return err
	}
	return muatUlangFail2ban()
}

// hapusStanzaJailD membuang stanza [nama] dari setiap berkas di jail.d.
// Berkas yang diubah dicadangkan sekali ke <berkas>.lindash.bak — isinya milik
// paket fail2ban (dpkg conffile), jadi perubahan harus bisa dikembalikan.
func hapusStanzaJailD(nama string) error {
	entries, err := os.ReadDir(jailDDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		path := filepath.Join(jailDDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		baru, berubah := buangStanza(string(b), nama)
		if !berubah {
			continue
		}
		if _, err := os.Stat(path + ".lindash.bak"); os.IsNotExist(err) {
			_ = os.WriteFile(path+".lindash.bak", b, 0o644)
		}
		tmp := path + ".lindash-tmp"
		if err := os.WriteFile(tmp, []byte(baru), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	return nil
}

// buangStanza menghapus satu section bernama `nama` beserta isinya sampai
// header section berikutnya.
func buangStanza(isi, nama string) (string, bool) {
	var keluar []string
	lewati := false
	berubah := false
	for _, line := range strings.Split(isi, "\n") {
		t := strings.TrimSpace(line)
		if m := sectionRe.FindStringSubmatch(t); m != nil {
			lewati = m[1] == nama
			if lewati {
				berubah = true
				// Buang baris kosong penutup sebelum header supaya tidak
				// menumpuk baris kosong setiap kali menghapus.
				for len(keluar) > 0 && strings.TrimSpace(keluar[len(keluar)-1]) == "" {
					keluar = keluar[:len(keluar)-1]
				}
				continue
			}
		}
		if lewati {
			continue
		}
		keluar = append(keluar, line)
	}
	teks := strings.Join(keluar, "\n")
	if !strings.HasSuffix(teks, "\n") {
		teks += "\n"
	}
	return teks, berubah
}

// fail2banUnban melepas satu IP dari jail. Ini aksi paling sering dibutuhkan
// saat admin sendiri yang salah ketik password dan ikut terblokir.
func fail2banUnban(jail, ip string) error {
	if !jailNamaRe.MatchString(jail) {
		return errInvalid("nama jail tidak valid")
	}
	if !ipRe2.MatchString(ip) {
		return errInvalid("alamat IP tidak valid")
	}
	_, err := run("fail2ban-client", "set", jail, "unbanip", ip)
	return err
}

func muatUlangFail2ban() error {
	if _, err := run("fail2ban-client", "reload"); err != nil {
		return errInvalid("fail2ban menolak konfigurasi: %v", err)
	}
	return nil
}

// tulisJailLocal mengganti satu section milik panel di jail.local.
// isi kosong = hapus section itu. Section milik admin disalin apa adanya.
func tulisJailLocal(nama, isi string) error {
	b, err := os.ReadFile(jailLocalPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	baris := strings.Split(string(b), "\n")
	var keluar []string
	ketemu := false
	lewati := false
	for i := 0; i < len(baris); i++ {
		t := strings.TrimSpace(baris[i])
		if m := sectionRe.FindStringSubmatch(t); m != nil {
			lewati = m[1] == nama
			if lewati {
				ketemu = true
				// Penanda panel berada tepat sebelum header section.
				for len(keluar) > 0 && strings.TrimSpace(keluar[len(keluar)-1]) == f2bTanda {
					keluar = keluar[:len(keluar)-1]
				}
				if isi != "" {
					keluar = append(keluar, strings.Split(strings.TrimRight(isi, "\n"), "\n")...)
				}
				continue
			}
		}
		if lewati {
			continue
		}
		keluar = append(keluar, baris[i])
	}
	if !ketemu && isi != "" {
		for len(keluar) > 0 && strings.TrimSpace(keluar[len(keluar)-1]) == "" {
			keluar = keluar[:len(keluar)-1]
		}
		if len(keluar) > 0 {
			keluar = append(keluar, "")
		}
		keluar = append(keluar, strings.Split(strings.TrimRight(isi, "\n"), "\n")...)
		keluar = append(keluar, "")
	}
	teks := strings.Join(keluar, "\n")
	if !strings.HasSuffix(teks, "\n") {
		teks += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(jailLocalPath), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(jailLocalPath), ".jail.local.lindash")
	if err := os.WriteFile(tmp, []byte(teks), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, jailLocalPath)
}

// pastikanJailBawaan menyalakan jail yang jelas berguna begitu fail2ban
// terpasang, supaya user tidak perlu menambahkannya sendiri.
//
// Isinya hanya sshd, dan itu bukan pilihan sembarangan: fail2ban 1.1 tidak
// menyediakan filter untuk satu pun komponen di katalog panel — samba, nfs,
// avahi, dan 9router semuanya tidak punya, dan tidak ada jail bawaan di
// jail.conf yang cocok dengan mereka. Jail untuk komponen itu akan enabled di
// berkas tapi gagal dimuat fail2ban, yaitu persis kondisi "terlihat aman
// padahal tidak memblokir apa pun" yang jadi alasan berkas ini membedakan
// konfigurasi dari status runtime.
//
// Jail yang sudah ada TIDAK ditimpa, dan yang sudah dihapus user TIDAK dibuat
// ulang: panel membuatnya sekali saat fail2ban dipasang, sesudah itu daftar
// jail sepenuhnya milik user.
func pastikanJailBawaan() {
	if !installed("fail2ban-client") {
		return
	}
	pastikanJailSSH()
	// Samba tidak punya filter bawaan di fail2ban, jadi filternya dipasang
	// panel. Hanya berguna kalau smbd memang ada.
	//
	// lookBinary, bukan installed: smbd dipasang ke /usr/sbin, yang tidak
	// selalu ada di PATH proses daemon — exec.LookPath saja akan melaporkan
	// Samba "belum terpasang" padahal jelas ada (lihat komentar lookBinary).
	if _, ada := lookBinary("smbd"); ada {
		if err := pastikanJailSamba(); err != nil {
			log.Printf("fail2ban: gagal menyiapkan jail samba: %v", err)
		}
	}
}

func pastikanJailSSH() {
	for _, j := range bacaJailLocal() {
		if j.Name == "sshd" {
			return
		}
	}
	// maxretry 5 dengan ban 1 jam: cukup ketat untuk menahan brute force,
	// cukup longgar untuk tidak mengunci admin yang salah ketik password.
	err := fail2banSave(helperproto.Fail2banJail{
		Name: "sshd", Enabled: true, MaxRetry: 5,
		BanTime: f2bDefBan, FindTime: f2bDefFind,
	})
	if err != nil {
		log.Printf("fail2ban: gagal menyalakan jail sshd: %v", err)
	}
}

const f2bFilterSamba = "/etc/fail2ban/filter.d/lindash-samba.conf"

// jailDDir adalah var, jadi ini ikut var — bukan const.
var f2bJailSamba = jailDDir + "/lindash-samba.conf"

// isiFilterSamba mencocokkan baris audit autentikasi Samba, yang bentuknya:
//
//	Auth: [SMB2,(null)] user [MINIPC]\[budi] at [...] with [NTLMv2]
//	  status [NT_STATUS_WRONG_PASSWORD] workstation [PC-BUDI]
//	  remote host [ipv4:192.168.2.77:51422] became [...]
//
// Baris ini hanya muncul kalau smb.conf sudah disiapkan
// pastikanGlobalAuditSamba — tanpa itu berkas log tidak memuat satu pun
// NT_STATUS kegagalan dan jail ini tidak akan pernah mem-ban siapa pun.
//
// ponytail: hanya alamat ipv4 yang dicocokkan. Samba menulis peer ipv6 sebagai
// "ipv6:fe80::1%eth0:445", dan memisahkan alamat dari port di bentuk itu butuh
// pola yang jauh lebih ruwet daripada nilainya untuk jaringan rumah/kantor
// kecil yang jadi sasaran panel ini. Jalan naiknya: tambahkan satu baris
// failregex kedua khusus ipv6 kalau ada deployment yang memakainya.
const isiFilterSamba = `# Dikelola linux-dashboard. Perubahan manual akan tertimpa.
[Definition]
failregex = ^\s*Auth: .* status \[NT_STATUS_(?:LOGON_FAILURE|WRONG_PASSWORD|NO_SUCH_USER|ACCOUNT_DISABLED|ACCOUNT_LOCKED_OUT|ACCOUNT_EXPIRED|PASSWORD_EXPIRED|PASSWORD_MUST_CHANGE)\] .* remote host \[ipv4:<HOST>:\d+\]
ignoreregex =
`

// isiJailSamba memuat bagian yang tidak bisa diungkapkan lewat form panel:
// filter mana yang dipakai dan berkas log mana yang dibaca. Nilai yang memang
// diatur user — enabled, maxretry, bantime, findtime — sengaja TIDAK ditulis di
// sini melainkan di jail.local lewat fail2banSave, supaya jail ini bisa disunting
// dari halaman Fail2ban seperti jail lain dan suntingan itu menang atas berkas ini.
//
// backend ditulis eksplisit: sebagian distro menyetel backend = systemd di
// [DEFAULT], dan dengan backend itu logpath diabaikan sepenuhnya — jail akan
// terlihat jalan tapi tidak pernah membaca log Samba.
const isiJailSamba = `# Dikelola linux-dashboard. Perubahan manual akan tertimpa.
[samba]
filter = lindash-samba
backend = auto
logpath = /var/log/samba/log.*
port = 139,445
`

// pastikanJailSamba memasang filter Samba milik panel dan menyalakan jailnya.
// Jail yang sudah tercatat di jail.local tidak disentuh: sesudah dibuat sekali,
// isinya milik user — termasuk kalau user menghapusnya dari halaman Fail2ban.
func pastikanJailSamba() error {
	for _, j := range bacaJailLocal() {
		if j.Name == "samba" {
			return nil
		}
	}
	// smb.conf disiapkan lebih dulu: jail yang menyala di atas log yang tidak
	// memuat kegagalan apa pun adalah jail yang terlihat aman tanpa memblokir
	// apa pun, dan itu lebih buruk daripada tidak ada jail sama sekali.
	if err := pastikanGlobalAuditSamba(); err != nil {
		return err
	}
	if err := os.WriteFile(f2bFilterSamba, []byte(isiFilterSamba), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(f2bJailSamba, []byte(isiJailSamba), 0o644); err != nil {
		return err
	}
	return fail2banSave(helperproto.Fail2banJail{
		Name: "samba", Enabled: true, MaxRetry: f2bDefTry,
		BanTime: f2bDefBan, FindTime: f2bDefFind, Port: "139,445",
	})
}
