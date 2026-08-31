package helper

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var (
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	serviceRe  = regexp.MustCompile(`^[a-zA-Z0-9@._\-]+$`)
	portRe     = regexp.MustCompile(`^[0-9]{1,5}(:[0-9]{1,5})?$`)
	ipRe       = regexp.MustCompile(`^[0-9a-fA-F:.]+(/[0-9]{1,3})?$`)
)

func setHostname(name string) error {
	// Whitelist, bukan blacklist: hanya bentuk hostname RFC-1123 yang lolos.
	if !hostnameRe.MatchString(name) {
		return errInvalid("hostname tidak valid")
	}
	if _, err := run("hostnamectl", "set-hostname", name); err != nil {
		return err
	}
	// Jaga /etc/hosts tetap konsisten supaya resolusi nama lokal tidak pecah.
	if b, err := os.ReadFile("/etc/hosts"); err == nil {
		lines := strings.Split(string(b), "\n")
		found := false
		for i, l := range lines {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "127.0.1.1" {
				lines[i] = "127.0.1.1\t" + name
				found = true
			}
		}
		if !found {
			lines = append(lines, "127.0.1.1\t"+name)
		}
		_ = os.WriteFile("/etc/hosts", []byte(strings.Join(lines, "\n")), 0o644)
	}
	return nil
}

// setDNS menulis nameserver. systemd-resolved (default Ubuntu) mengelola
// /etc/resolv.conf sendiri, jadi kalau aktif kita pakai `resolvectl` —
// menulis file langsung akan tertimpa saat resolved restart.
func setDNS(servers []string) error {
	if len(servers) == 0 {
		return errInvalid("minimal satu nameserver")
	}
	for _, s := range servers {
		if !ipRe.MatchString(s) {
			return errInvalid("alamat DNS tidak valid: %s", s)
		}
	}
	if _, err := run("systemctl", "is-active", "--quiet", "systemd-resolved"); err == nil {
		iface, err := defaultInterface()
		if err != nil {
			return err
		}
		args := append([]string{"dns", iface}, servers...)
		_, err = run("resolvectl", args...)
		return err
	}
	var b strings.Builder
	b.WriteString("# dikelola oleh linux-dashboard\n")
	for _, s := range servers {
		fmt.Fprintf(&b, "nameserver %s\n", s)
	}
	return os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644)
}

func defaultInterface() (string, error) {
	res, err := run("ip", "-o", "route", "get", "1.1.1.1")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(res.Stdout)
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", errInvalid("interface default tidak ditemukan")
}

func processOwner(pid int) (int, error) {
	var st syscall.Stat_t
	if err := syscall.Stat("/proc/"+strconv.Itoa(pid), &st); err != nil {
		return 0, err
	}
	return int(st.Uid), nil
}

func killAsRoot(pid, sig int) error {
	if err := syscall.Kill(pid, syscall.Signal(sig)); err != nil {
		return &helperErr{code: helperproto.ErrInternal, msg: err.Error()}
	}
	return nil
}

var allowedServiceActions = map[string]bool{
	"start": true, "stop": true, "restart": true, "reload": true,
	"enable": true, "disable": true,
}

func serviceAction(args helperproto.ServiceArgs) error {
	if !serviceRe.MatchString(args.Name) {
		return errInvalid("nama service tidak valid")
	}
	if !allowedServiceActions[args.Action] {
		return errInvalid("aksi service tidak diizinkan")
	}
	// systemctl start normal menunggu seluruh dependency job selesai;
	// kalau salah satu gagal (mis. network-online.target tidak ready di
	// WSL/container), start ikut "dependency job failed" dan unit tidak
	// nyala padahal unit-nya sendiri sehat. --no-block melepas barrier
	// dependency dan mengembalikan lebih cepat — start tetap diproses
	// oleh systemd, hanya tidak ditunggu di sini.
	args2 := append([]string{args.Action}, args.Name)
	if args.Action == "start" {
		args2 = append(args2, "--no-block")
	}
	if _, err := run("systemctl", args2...); err != nil {
		// systemctl hanya menulis ke stderr-nya sendiri, dan teks itu tidak
		// selalu masuk ke ExecResult (bergantung versi & jenis kegagalan).
		// Tarik pesan diagnostik dari journal agar UI menampilkan alasan
		// yang sebenarnya — "Job for x failed because..." jauh lebih berguna
		// daripada baris kosong.
		detail := systemctlDiagnose(args.Name, args.Action)
		if detail == "" {
			return err
		}
		return &helperErr{code: helperproto.ErrInternal, msg: detail}
	}
	// --- start: pastikan unit benar-benar hidup ---
	//
	// --no-block membuat systemctl selalu keluar dengan status 0: ia cuma
	// menitipkan job, tidak menunggu hasilnya. Tanpa langkah ini, unit yang
	// gagal start (smartd tanpa perangkat yang bisa dipantau adalah kasus
	// paling sering) dilaporkan sukses, halaman memuat ulang, dan badge-nya
	// tetap "Nonaktif" — dari sisi user tombolnya terlihat tidak melakukan
	// apa-apa. Tunggu sebentar, lalu laporkan alasan sebenarnya.
	if args.Action == "start" {
		if err := tungguUnitAktif(args.Name); err != nil {
			return err
		}
	}
	return nil
}

// tungguUnitAktif menunggu unit menjadi active sampai batas waktu pendek.
// Batasnya sengaja kecil: yang ditunggu hanya transisi activating → active,
// bukan seluruh rantai dependency (itu yang dilepas oleh --no-block).
func tungguUnitAktif(unit string) error {
	const batas = 4 * time.Second
	const jeda = 250 * time.Millisecond
	tenggat := time.Now().Add(batas)
	for {
		if _, err := run("systemctl", "is-active", "--quiet", unit); err == nil {
			return nil
		}
		if time.Now().After(tenggat) {
			break
		}
		time.Sleep(jeda)
	}

	aktif, hasil, jenis := keadaanUnit(unit)
	switch {
	// Masih naik setelah batas waktu bukan kegagalan — service berat (docker,
	// smbd di mesin kecil) memang butuh lebih lama. Melaporkannya sebagai
	// error justru salah; UI akan menampilkan status sebenarnya saat refresh.
	case aktif == "activating", aktif == "reloading":
		return nil
	// Unit oneshot yang selesai dengan sukses memang berakhir di "inactive".
	case jenis == "oneshot" && hasil == "success":
		return nil
	}

	pesan := strings.TrimSpace(diagnosaJurnal(unit))
	if pesan == "" {
		pesan = "service " + unit + " tidak menyala setelah dijalankan — periksa `systemctl status " + unit + "`"
	}
	return &helperErr{code: helperproto.ErrInternal, msg: pesan}
}

// keadaanUnit membaca ActiveState, Result, dan Type dalam satu panggilan.
//
// Sengaja TANPA --value: systemd mencetak properti dalam urutannya sendiri,
// bukan urutan flag -p yang diminta (`-p ActiveState -p Result -p Type` di
// mesin ini keluar sebagai ActiveState, Type, Result). Membaca posisi baris
// berarti menukar Type dengan Result — jadi tiap baris dibaca sebagai
// pasangan kunci=nilai.
func keadaanUnit(unit string) (aktif, hasil, jenis string) {
	res, err := run("systemctl", "show", "-p", "ActiveState", "-p", "Result", "-p", "Type", unit)
	if err != nil {
		return "", "", ""
	}
	for _, baris := range strings.Split(res.Stdout, "\n") {
		kunci, nilai, ada := strings.Cut(strings.TrimSpace(baris), "=")
		if !ada {
			continue
		}
		switch kunci {
		case "ActiveState":
			aktif = nilai
		case "Result":
			hasil = nilai
		case "Type":
			jenis = nilai
		}
	}
	return aktif, hasil, jenis
}

// diagnosaJurnal mengambil beberapa baris journal terakhir milik unit.
func diagnosaJurnal(unit string) string {
	j, err := run("journalctl", "-u", unit, "-n", "12", "-o", "cat", "--no-pager")
	if err != nil {
		return ""
	}
	return j.Stdout
}

// systemctlDiagnose mengambil 20 baris terakhir journal untuk unit ini saat
// systemctl gagal — root cause biasanya ada di situ (dependency missing,
// path biner salah, dsb). Dipakai untuk wireguard, qemu-guest-agent, dan
// service lain yang sering gagal start tanpa pesan jelas.
func systemctlDiagnose(unit, action string) string {
	res, err := run("systemctl", action, unit)
	if err == nil {
		// Unit ternyata jalan sekarang — baris terakhir dari journal
		// mungkin menjelaskan mengapa start sebelumnya gagal.
		if j, e := run("journalctl", "-u", unit, "-n", "5", "-o", "cat", "--no-pager"); e == nil {
			return strings.TrimSpace(j.Stdout)
		}
		return ""
	}
	// Bentuk pesan yang sama yang dilihat user di terminal: baris systemctl
	// + 20 baris journal terakhir. Dipotong supaya tidak memenuhi toast.
	msg := strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout, err.Error()))
	if msg == "" {
		msg = err.Error()
	}
	if j, e := run("journalctl", "-u", unit, "-n", "20", "-o", "cat", "--no-pager"); e == nil {
		js := strings.TrimSpace(j.Stdout)
		if js != "" {
			return msg + "\n" + js
		}
	}
	return msg
}

// ---- ufw ----

// Baris `ufw status numbered` berbentuk: "[ 1] 22/tcp   ALLOW IN   Anywhere"
var ufwLineRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(\S+)\s+(ALLOW|DENY|REJECT|LIMIT)\s+(IN|OUT)\s+(.*)$`)

type UfwStatus struct {
	Enabled bool                  `json:"enabled"`
	Rules   []helperproto.UfwRule `json:"rules"`
}

func ufwStatus() (UfwStatus, error) {
	res, err := run("ufw", "status", "numbered")
	if err != nil {
		return UfwStatus{}, err
	}
	st := UfwStatus{Enabled: strings.Contains(res.Stdout, "Status: active")}
	for _, line := range strings.Split(res.Stdout, "\n") {
		m := ufwLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		port, proto, _ := strings.Cut(m[2], "/")
		st.Rules = append(st.Rules, helperproto.UfwRule{
			Num:    m[1],
			Action: strings.ToLower(m[3]),
			Port:   port,
			Proto:  proto,
			From:   strings.TrimSpace(m[5]),
			Raw:    strings.TrimSpace(line),
		})
	}
	// Saat ufw nonaktif, `ufw status numbered` hanya mencetak "Status: inactive"
	// tanpa satu pun rule -- padahal rule yang sudah disimpan tetap ada di
	// /etc/ufw/user.rules. Tanpa fallback ini halaman Firewall tampak kosong
	// setelah user menyimpan rule.
	if !st.Enabled {
		if added, err := run("ufw", "show", "added"); err == nil {
			for _, line := range strings.Split(added.Stdout, "\n") {
				if r, ok := parseAddedRule(strings.TrimSpace(line)); ok {
					st.Rules = append(st.Rules, r)
				}
			}
		}
	}
	return st, nil
}

// parseAddedRule membaca satu baris `ufw show added`, mis.
// "ufw allow 22/tcp" atau "ufw allow from 10.0.0.0/8 to any port 22 proto tcp".
// Rule dari sini tidak punya nomor (lihat UfwDeleteArgs.Spec).
func parseAddedRule(line string) (helperproto.UfwRule, bool) {
	f := strings.Fields(line)
	if len(f) < 3 || f[0] != "ufw" {
		return helperproto.UfwRule{}, false
	}
	r := helperproto.UfwRule{Action: strings.ToLower(f[1]), Raw: strings.Join(f[1:], " ")}
	rest := f[2:]
	if rest[0] == "from" {
		r.From = rest[1]
		for i, t := range rest {
			if i+1 >= len(rest) {
				break
			}
			switch t {
			case "port":
				r.Port = rest[i+1]
			case "proto":
				r.Proto = rest[i+1]
			}
		}
	} else {
		r.Port, r.Proto, _ = strings.Cut(rest[0], "/")
	}
	return r, r.Port != ""
}

// ufwUpdate mengganti rule: ufw tidak punya "edit", jadi rule lama dihapus
// lalu rule baru ditambahkan. Rule baru divalidasi DULU supaya rule lama tidak
// terlanjur hilang gara-gara input yang ditolak.
func ufwUpdate(args helperproto.UfwUpdateArgs) error {
	if err := validUfwRule(args.Rule); err != nil {
		return err
	}
	if err := ufwDelete(args.Num, args.Spec); err != nil {
		return err
	}
	return ufwAdd(args.Rule)
}

func validUfwRule(r helperproto.UfwRule) error {
	if r.Action != "allow" && r.Action != "deny" {
		return errInvalid("aksi firewall harus allow atau deny")
	}
	if !portRe.MatchString(r.Port) {
		return errInvalid("port tidak valid")
	}
	if r.From != "" && r.From != "any" && !ipRe.MatchString(r.From) {
		return errInvalid("alamat sumber tidak valid")
	}
	switch r.Proto {
	case "", "any", "tcp", "udp":
	default:
		return errInvalid("protokol tidak valid")
	}
	return nil
}

// ufwArgs menyusun argumen ufw untuk satu rule. Dipakai bersama oleh ufwAdd
// dan ufwHapusRule supaya bentuk yang ditulis dan bentuk yang dihapus tidak
// pernah berbeda — `ufw delete` mencocokkan rule apa adanya, jadi satu spasi
// atau urutan kata yang lain saja sudah membuat penghapusan gagal diam-diam.
func ufwArgs(r helperproto.UfwRule) ([]string, error) {
	if r.Action != "allow" && r.Action != "deny" {
		return nil, errInvalid("aksi firewall harus allow atau deny")
	}
	if !portRe.MatchString(r.Port) {
		return nil, errInvalid("port tidak valid")
	}
	args := []string{r.Action}
	if r.From != "" && r.From != "any" {
		if !ipRe.MatchString(r.From) {
			return nil, errInvalid("alamat sumber tidak valid")
		}
		args = append(args, "from", r.From, "to", "any", "port", r.Port)
	} else {
		args = append(args, r.Port)
	}
	switch r.Proto {
	case "tcp", "udp":
		if r.From != "" && r.From != "any" {
			args = append(args, "proto", r.Proto)
		} else {
			args[len(args)-1] = r.Port + "/" + r.Proto
		}
	case "", "any":
	default:
		return nil, errInvalid("protokol tidak valid")
	}
	return args, nil
}

func ufwAdd(r helperproto.UfwRule) error {
	args, err := ufwArgs(r)
	if err != nil {
		return err
	}
	_, err = run("ufw", args...)
	return err
}

// ufwHapusRule membuang rule berdasarkan bentuknya, bukan nomornya. Nomor rule
// hanyalah posisi dalam daftar dan ikut bergeser setiap kali ada rule lain yang
// dihapus, jadi menghapus port komponen lewat nomor berisiko mengenai rule
// milik user.
func ufwHapusRule(r helperproto.UfwRule) error {
	args, err := ufwArgs(r)
	if err != nil {
		return err
	}
	_, err = run("ufw", append([]string{"--force", "delete"}, args...)...)
	return err
}

var ufwSpecTokenRe = regexp.MustCompile(`^[A-Za-z0-9./:_-]+$`)

func ufwDelete(num, spec string) error {
	if spec != "" {
		args := []string{"--force", "delete"}
		for _, tok := range strings.Fields(spec) {
			if !ufwSpecTokenRe.MatchString(tok) {
				return errInvalid("spec rule tidak valid")
			}
			args = append(args, tok)
		}
		if len(args) < 3 {
			return errInvalid("spec rule kosong")
		}
		_, err := run("ufw", args...)
		return err
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return errInvalid("nomor rule tidak valid")
	}
	_, err = run("ufw", "--force", "delete", strconv.Itoa(n))
	return err
}

func ufwToggle(enable bool) error {
	// Kebijakan masuk bawaan ufw adalah DROP, jadi menyalakan firewall memutus
	// setiap layanan yang portnya belum diizinkan — termasuk SSH dan panel ini
	// sendiri. Keduanya dipastikan SEBELUM firewall menyala, bukan sesudah.
	if enable {
		pastikanAksesAdmin()
	}
	// "ufw enable" interaktif (konfirmasi "Command may disrupt existing ssh
	// connections") -- pakai --force supaya tidak menggantung.
	verb := "disable"
	if enable {
		verb = "enable"
	}
	_, err := run("ufw", "--force", verb)
	return err
}
