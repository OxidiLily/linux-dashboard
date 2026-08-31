package helper

import (
	"log"
	"net"
	"os"
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
			if t != "inet" || i+1 >= len(f) {
				continue
			}
			// net.ParseCIDR sekalian memberi alamat jaringannya:
			// "192.168.2.11/24" → 192.168.2.0/24. Tidak perlu menghitung
			// mask sendiri.
			if _, jaringan, err := net.ParseCIDR(f[i+1]); err == nil {
				return jaringan.String()
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

// daftarkanPortKomponen menambahkan aturan allow untuk setiap port komponen.
// Aman dipanggil berulang: `ufw allow` yang sudah ada dilewati ufw sendiri.
func daftarkanPortKomponen(c *component) {
	if len(c.ports) == 0 {
		return
	}
	// Tidak ada ufw = tidak ada tempat mendaftar. Bukan kesalahan: firewall
	// memang opsional, dan saat ufw dipasang nanti seluruh port komponen yang
	// terpasang didaftarkan sekaligus lewat daftarkanPortSemuaKomponen.
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	daftarkanPortLangsung(c.Name, c.ports, subnetLokal())
}

// daftarkanPortLangsung menulis aturan allow untuk sekumpulan port. `dari`
// kosong berarti Anywhere.
func daftarkanPortLangsung(nama string, ports []portKomponen, dari string) {
	for _, p := range ports {
		// Kegagalan mendaftar TIDAK membatalkan instalasi komponen: paketnya
		// sudah terpasang dan jalan, dan ufw yang belum aktif tidak memblokir
		// apa pun hari ini. Yang tersisa hanya catatan supaya bisa ditelusuri.
		if err := ufwAdd(aturanPort(p, dari)); err != nil {
			log.Printf("firewall: gagal mengizinkan %s/%s (%s) untuk %s: %v",
				p.Port, p.Proto, p.Guna, nama, err)
		}
	}
}

// hapusPortKomponen membuang aturan yang dibuat daftarkanPortKomponen, supaya
// mencopot komponen tidak meninggalkan lubang di firewall untuk layanan yang
// sudah tidak ada.
func hapusPortKomponen(c *component) {
	if len(c.ports) == 0 {
		return
	}
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	dari := subnetLokal()
	for _, p := range c.ports {
		if err := ufwHapusRule(aturanPort(p, dari)); err != nil {
			log.Printf("firewall: gagal mencabut izin %s/%s untuk %s: %v",
				p.Port, p.Proto, c.Name, err)
		}
	}
}

// daftarkanPortSemuaKomponen mendaftarkan port setiap komponen yang benar-benar
// terpasang. Dipanggil sebelum ufw dinyalakan dan setelah ufw sendiri dipasang:
// komponen yang sudah ada duluan tidak pernah sempat mendaftar, dan justru
// merekalah yang paling mungkin sedang dipakai saat firewall dinyalakan.
func daftarkanPortSemuaKomponen() {
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	// Subnet dihitung sekali di sini, bukan per komponen: menentukannya butuh
	// dua panggilan `ip` dan jawabannya sama untuk semuanya.
	dari := subnetLokal()
	for _, c := range components {
		if len(c.ports) == 0 {
			continue
		}
		terpasang := false
		if c.terpasang != nil {
			terpasang = c.terpasang()
		} else if _, ok := lookBinary(c.Binary); ok {
			terpasang = true
		}
		if terpasang {
			daftarkanPortLangsung(c.Name, c.ports, dari)
		}
	}
	// SSH dan panel bukan komponen, tapi merekalah yang paling mahal kalau
	// ikut tertutup. Sumbernya sengaja Anywhere, bukan subnet lokal seperti
	// port komponen: membatasi keduanya ke LAN justru menciptakan penguncian
	// yang mau dicegah di sini — admin yang masuk lewat WAN, VPN, atau
	// Tailscale datang dari subnet yang lain.
	daftarkanPortLangsung("akses admin", portAksesAdmin(), "")
}

// pastikanAksesAdmin mendaftarkan port SSH dan panel saja, dipanggil tepat
// sebelum firewall dinyalakan.
//
// Yang TIDAK dilakukannya sama pentingnya: port komponen sengaja tidak
// didaftarkan ulang di sini. Aturan yang pernah dibuat panel lalu dihapus user
// adalah keputusan user, dan menyalakan firewall bukan alasan untuk
// mengembalikannya — panel mendaftarkan sekali saat komponennya dipasang,
// sesudah itu daftar rule sepenuhnya milik user. Hanya akses admin yang tetap
// dipaksakan, karena kehilangan itu berarti kehilangan mesinnya.
func pastikanAksesAdmin() {
	if _, ada := lookBinary("ufw"); !ada {
		return
	}
	daftarkanPortLangsung("akses admin", portAksesAdmin(), "")
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

// portSSH membaca setiap `Port N` di sshd_config. Kosong berarti sshd memakai
// bawaannya, 22 — baris Port memang biasanya tidak ditulis sama sekali.
func portSSH() []string {
	var out []string
	b, err := os.ReadFile("/etc/ssh/sshd_config")
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && strings.EqualFold(f[0], "Port") && portRe.MatchString(f[1]) {
				out = append(out, f[1])
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
