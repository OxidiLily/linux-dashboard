package helper

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pendaftaran port komponen ke firewall.
//
// ufw dipasang dengan DEFAULT_INPUT_POLICY=DROP: begitu firewall dinyalakan,
// setiap layanan yang portnya belum punya aturan allow langsung tidak bisa
// dihubungi — termasuk layanan yang sudah dipakai sehari-hari sebelum firewall
// ada. Samba yang tiba-tiba tidak bisa diakses setelah user menyalakan
// firewall dari halaman Settings → Firewall adalah kegagalan yang sulit
// dilacak, karena tidak ada yang berubah di sisi Samba-nya.
//
// Karena itu port didaftarkan LEBIH DULU, saat komponennya dipasang, bukan
// saat firewallnya menyala: `ufw allow` tetap tersimpan di /etc/ufw/user.rules
// meskipun ufw sedang nonaktif, jadi aturannya sudah siap dan menyalakan
// firewall tidak memutus apa pun.

// portKomponen adalah satu port masuk yang dibutuhkan sebuah komponen supaya
// bisa dipakai dari LAN.
type portKomponen struct {
	Port  string // "445", atau rentang gaya ufw "137:138"
	Proto string // tcp | udp
	Guna  string // keterangan singkat, dipakai di pesan log
}

// denganPort menandai port masuk yang dibutuhkan komponen — pasangan `wajib`,
// dipakai supaya entri di katalog tetap terbaca satu baris.
func denganPort(c *component, ps ...portKomponen) *component {
	c.ports = ps
	return c
}

// subnetLokal mengembalikan subnet interface yang memegang default route,
// mis. "192.168.2.0/24". Port komponen diizinkan hanya dari sana, bukan dari
// mana saja: 445/tcp yang terbuka ke internet adalah target pemindaian yang
// ramai, dan komponen di panel ini semuanya layanan LAN. Kembalian "" berarti
// subnet tidak bisa ditentukan.
func subnetLokal() string {
	// net.ParseCIDR sekalian memberi alamat jaringannya:
	// "192.168.2.11/24" → 192.168.2.0/24. Tidak perlu menghitung mask sendiri.
	_, jaringan, err := net.ParseCIDR(alamatLokalCIDR())
	if err != nil {
		return ""
	}
	return jaringan.String()
}

// alamatLokalCIDR mengembalikan alamat IPv4 interface yang memegang default
// route berikut prefiksnya, mis. "192.168.2.11/24". Kembalian "" berarti tidak
// bisa ditentukan.
//
// Alamatnya dibaca dari `ip addr`, BUKAN dari field `src` keluaran `ip route`:
// route statis yang ditulis tanpa `src` (bentuk yang dipakai cloud-init dan
// netplan di banyak image server) tidak punya field itu sama sekali, dan
// pembacaan yang bergantung padanya diam-diam mengembalikan kosong di mesin
// yang jaringannya justru normal.
func alamatLokalCIDR() string {
	dev := ""
	if res, err := run("ip", "-o", "-4", "route", "show", "to", "default"); err == nil {
		f := strings.Fields(res.Stdout)
		for i, t := range f {
			if t == "dev" && i+1 < len(f) {
				dev = f[i+1]
				break
			}
		}
	}
	if dev == "" {
		return ""
	}
	res, err := run("ip", "-o", "-4", "addr", "show", "dev", dev)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		for i, t := range f {
			if t == "inet" && i+1 < len(f) {
				return f[i+1]
			}
		}
	}
	return ""
}

// aturanPort menyusun rule ufw untuk satu port komponen. Sumber dibiarkan
// kosong (= Anywhere) kalau subnet tidak terdeteksi: lebih longgar dari yang
// diinginkan, tapi jauh lebih baik daripada layanan yang mati diam-diam
// begitu firewall dinyalakan.
func aturanPort(p portKomponen, dari string) helperproto.UfwRule {
	return helperproto.UfwRule{Action: "allow", Port: p.Port, Proto: p.Proto, From: dari}
}

// aturanPortKomponen adalah aturanPort plus label pemiliknya. Label inilah yang
// membuat `ufw status` menjawab "port ini punya siapa" tanpa perlu dibaca dari
// kode: "445/tcp ALLOW IN Anywhere # Samba".
func aturanPortKomponen(p portKomponen, dari, label string) helperproto.UfwRule {
	r := aturanPort(p, dari)
	r.Comment = label
	return r
}

// labelPortKomponen mengembalikan nama pemilik yang ditulis di rule. Dikosongkan
// berarti pakai Name komponennya.
func labelPortKomponen(c *component) string {
	if c.Label != "" {
		return c.Label
	}
	return c.Name
}

// daftarkanPortKomponen meminta penyelarasan port komponen secepatnya, bukan
// menulis aturannya sendiri: keputusan "port ini pantas dibuka atau tidak"
// berada di satu tempat saja — reconciler di komponenport.go, yang memeriksa
// apakah layanannya benar-benar hidup. Memasang komponen yang service-nya belum
// jalan tidak boleh membuka portnya lebih dulu.
func daftarkanPortKomponen(c *component) {
	if len(c.ports) == 0 {
		return
	}
	picuSinkronPortKomponen()
}

// hapusPortKomponen membuang aturan yang dibuat panel untuk komponen ini, supaya
// mencopot komponen tidak meninggalkan lubang di firewall untuk layanan yang
// sudah tidak ada. Dua bentuk ikut dicabut: Anywhere dan yang terbatas subnet
// lokal — keduanya pernah ditulis panel pada versi yang berbeda.
func hapusPortKomponen(c *component) {
	if len(c.ports) == 0 {
		return
	}
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	label := labelPortKomponen(c)
	dari := subnetLokal()
	for _, p := range c.ports {
		if err := ufwHapusRule(aturanPortKomponen(p, "", label)); err != nil {
			log.Printf("firewall: gagal mencabut izin %s/%s untuk %s: %v",
				p.Port, p.Proto, c.Name, err)
		}
		if dari != "" {
			_ = ufwHapusRule(aturanPortKomponen(p, dari, label))
		}
		// Rule lama tanpa label ikut dibersihkan: menghapus rule ufw tidak
		// membutuhkan labelnya, jadi bentuk mana pun yang cocok akan tercabut.
		_ = ufwHapusRule(aturanPort(p, ""))
		if dari != "" {
			_ = ufwHapusRule(aturanPort(p, dari))
		}
	}
	lupakanPortKomponen(c.Name)
}

// daftarkanPortSemuaKomponen menyelaraskan port seluruh komponen dengan
// kenyataan layanannya, lalu memastikan akses admin. Dipanggil saat helper
// mulai dan sebelum ufw dinyalakan: komponen yang sudah ada duluan tidak pernah
// sempat mendaftar, dan justru merekalah yang paling mungkin sedang dipakai saat
// firewall dinyalakan.
func daftarkanPortSemuaKomponen() {
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	if err := sinkronkanPortKomponen(); err != nil {
		log.Printf("firewall: port komponen tidak bisa diselaraskan: %v", err)
	}
	pastikanAksesAdmin()
}

// pastikanAksesAdmin mendaftarkan port SSH dan panel saja, dipanggil tepat
// sebelum firewall dinyalakan.
//
// Yang TIDAK dilakukannya sama pentingnya: port komponen sengaja tidak
// didaftarkan ulang di sini. Aturan yang pernah dibuat panel lalu dihapus user
// adalah keputusan user, dan menyalakan firewall bukan alasan untuk
// mengembalikannya — reconciler yang menambahkan kembali port komponen saat
// layanannya memang hidup. Hanya akses admin yang tetap dipaksakan, karena
// kehilangan itu berarti kehilangan mesinnya.
func pastikanAksesAdmin() {
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	for _, p := range portAksesAdmin() {
		// Labelnya sudah ada di katalog akses admin ("SSH", "panel
		// linux-dashboard") dan sekaligus jadi penanda pemiliknya di ufw.
		if err := ufwAdd(aturanPortKomponen(p, "", p.Guna)); err != nil {
			log.Printf("firewall: gagal mengizinkan %s/%s (%s): %v",
				p.Port, p.Proto, p.Guna, err)
		}
	}
}

// portAksesAdmin mengembalikan port yang tidak boleh ikut tertutup saat
// firewall dinyalakan: SSH dan panel ini sendiri.
//
// Ini bukan port komponen, tapi dipasang lewat jalur yang sama karena
// akibatnya paling parah. Menyalakan ufw tanpa keduanya mengunci pemilik dari
// mesinnya sendiri, dan satu-satunya jalan kembali adalah konsol fisik —
// sesuatu yang sering tidak ada di VPS atau VM Proxmox. Halaman Firewall
// selama ini hanya menampilkan kalimat peringatan untuk ini; kalimat tidak
// menahan siapa pun yang menekan tombolnya.
func portAksesAdmin() []portKomponen {
	out := []portKomponen{}
	for _, p := range portSSH() {
		out = append(out, portKomponen{p, "tcp", "SSH"})
	}
	if p := portPanel(); p != "" {
		out = append(out, portKomponen{p, "tcp", "panel linux-dashboard"})
	}
	return out
}

// portSSH membaca setiap `Port N` di sshd_config dan sshd_config.d/*.conf. Kosong
// berarti sshd memakai bawaannya, 22 — baris Port memang biasanya tidak ditulis sama sekali.
func portSSH() []string {
	var out []string
	baca := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Fields(line)
			if len(f) >= 2 && strings.EqualFold(f[0], "Port") && portRe.MatchString(f[1]) {
				out = append(out, f[1])
			}
		}
	}
	baca("/etc/ssh/sshd_config")
	if entries, err := os.ReadDir("/etc/ssh/sshd_config.d"); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
				baca(filepath.Join("/etc/ssh/sshd_config.d", e.Name()))
			}
		}
	}
	if len(out) == 0 {
		out = []string{"22"}
	}
	return out
}

// portPanel membaca port yang dipakai web panel dari DASHBOARD_LISTEN.
// EnvironmentFile menang atas Environment= di unit, jadi /etc/default dibaca
// lebih dulu — urutan yang sama dengan yang dipakai systemd.
func portPanel() string {
	for _, berkas := range []string{
		"/etc/default/linux-dashboard",
		"/etc/systemd/system/linux-dashboard-web.service",
	} {
		b, err := os.ReadFile(berkas)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || !strings.Contains(line, "DASHBOARD_LISTEN") {
				continue
			}
			_, nilai, ok := strings.Cut(line, "DASHBOARD_LISTEN=")
			if !ok {
				continue
			}
			nilai = strings.Trim(strings.TrimSpace(nilai), `"'`)
			// Bentuknya "host:port" (0.0.0.0:1122) atau ":1122".
			if i := strings.LastIndex(nilai, ":"); i >= 0 {
				nilai = nilai[i+1:]
			}
			if portRe.MatchString(nilai) {
				return nilai
			}
		}
	}
	return ""
}
