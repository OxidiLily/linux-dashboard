package helper

import (
	"net"
	"regexp"
	"slices"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Konfigurasi IP per interface lewat netplan (Ubuntu). Semua tulisan memakai
// `netplan set`, yang menulis ke berkas ASAL tempat interface itu didefinisikan
// (netplan ≥ 0.105). Overlay 90-linux-dashboard.yaml sengaja TIDAK dipakai:
// daftar `addresses` antar berkas DIGABUNG oleh netplan, bukan ditimpa, jadi IP
// lama akan tetap terpasang di samping yang baru (diuji lewat
// `netplan generate --root-dir`).
//
// Pembacaannya dari SATU `netplan get all` yang dipecah sendiri baris per
// baris: berkas /etc/netplan bermode 0600, panel tidak punya pustaka YAML, dan
// tiap panggilan netplan memakan ~1,5 detik (Python + libnetplan) — `get` per
// kunci membuat modal terbuka 10 detik. Alasan yang sama membuat `set` hanya
// dipanggil untuk kunci yang nilainya berubah.

var ifaceRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

// netplanExtra ditambahkan ke setiap panggilan netplan — test mengisinya
// dengan `--root-dir` supaya seluruh alur bisa dijalankan tanpa root.
var netplanExtra []string

func netplan(args ...string) (helperproto.ExecResult, error) {
	return run("netplan", append(args, netplanExtra...)...)
}

// netplanGet mengembalikan keluaran `netplan get` untuk satu kunci, atau ""
// kalau kuncinya tidak ada (netplan mencetak "null").
func netplanGet(key string) (string, error) {
	res, err := netplan("get", key)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "null" {
		return "", nil
	}
	return out, nil
}

func indentasi(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// bagianNetplan mencari seksi (ethernets/bridges/bonds/vlans/wifis) yang
// memuat interface di keluaran `netplan get all` — seksi di indentasi 2, nama
// interface di indentasi 4 — dan mengembalikan blok interface itu, dipetakan
// per kunci indentasi 6: nilai skalar, atau baris-baris bersarang di bawahnya
// (list `- x` netplan ditulis sejajar kuncinya).
func bagianNetplan(all, iface string) (string, map[string]string) {
	seksi, blok := "", map[string]string(nil)
	kunci := ""
	for _, line := range strings.Split(all, "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		indent := indentasi(line)
		isi := strings.TrimSpace(line)
		if blok != nil {
			switch {
			case indent > 6 || indent == 6 && strings.HasPrefix(isi, "- "):
				blok[kunci] += line[6:] + "\n"
				continue
			case indent == 6:
				k, v, _ := strings.Cut(isi, ":")
				kunci = k
				blok[kunci] = strings.TrimSpace(v)
				continue
			default:
				return seksi, blok
			}
		}
		nama := strings.TrimSuffix(isi, ":")
		switch indent {
		case 2:
			seksi = nama
		case 4:
			if nama == iface && seksi != "" && seksi != "version" {
				blok = map[string]string{}
			}
		}
	}
	return seksi, blok
}

// bacaDaftar mengubah keluaran list `netplan get` (`- "x"` per baris) jadi
// slice string tanpa tanda kutip.
func bacaDaftar(out string) []string {
	var v []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "- "); ok {
			v = append(v, strings.Trim(after, `"`))
		}
	}
	return v
}

// ruteEntri adalah satu entri `routes:` sebagai urutan pasangan kunci→nilai
// mentah, supaya bisa ditulis balik apa adanya (metric, table, on-link, dst.)
// tanpa perlu memahami setiap kuncinya.
type ruteEntri [][2]string

func (r ruteEntri) nilai(k string) string {
	for _, kv := range r {
		if kv[0] == k {
			return strings.Trim(kv[1], `"`)
		}
	}
	return ""
}

func (r ruteEntri) bawaan() bool {
	switch r.nilai("to") {
	case "default", "0.0.0.0/0", "::/0":
		return true
	}
	return false
}

// bacaRute membaca keluaran `netplan get ...routes`:
//
//   - to: "default"
//     via: "192.168.2.1"
func bacaRute(out string) []ruteEntri {
	var v []ruteEntri
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		baru := false
		if after, ok := strings.CutPrefix(line, "- "); ok {
			line, baru = after, true
		}
		k, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if baru {
			v = append(v, nil)
		}
		if len(v) == 0 {
			continue
		}
		v[len(v)-1] = append(v[len(v)-1], [2]string{strings.TrimSpace(k), strings.TrimSpace(val)})
	}
	return v
}

// flowYAML menulis daftar entri rute sebagai flow sequence YAML untuk argumen
// `netplan set`. Nilai mentah dari `netplan get` sudah valid YAML.
func flowYAML(rutes []ruteEntri) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, r := range rutes {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for j, kv := range r {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`"` + kv[0] + `":` + kv[1])
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

func flowKutip(v []string) string {
	q := make([]string, len(v))
	for i, s := range v {
		q[i] = `"` + s + `"`
	}
	return "[" + strings.Join(q, ",") + "]"
}

func ipv6(s string) bool { return strings.Contains(s, ":") }

func ifaceConfigGet(iface string) (helperproto.IfaceConfig, error) {
	c := helperproto.IfaceConfig{Iface: iface, Addrs4: []string{}, Addrs6: []string{}}
	if !ifaceRe.MatchString(iface) {
		return c, errInvalid("nama interface tidak valid")
	}
	all, err := netplanGet("all")
	if err != nil {
		return c, err
	}
	_, blok := bagianNetplan(all, iface)
	if blok == nil {
		c.IPv4, c.IPv6 = "off", "off"
		return c, nil
	}
	c.Managed = true
	for _, a := range bacaDaftar(blok["addresses"]) {
		if ipv6(a) {
			c.Addrs6 = append(c.Addrs6, a)
		} else {
			c.Addrs4 = append(c.Addrs4, a)
		}
	}
	for _, r := range bacaRute(blok["routes"]) {
		if !r.bawaan() {
			continue
		}
		if via := r.nilai("via"); ipv6(via) {
			c.Gw6 = via
		} else {
			c.Gw4 = via
		}
	}
	dhcp4, dhcp6, ra := blok["dhcp4"] == "true", blok["dhcp6"] == "true", blok["accept-ra"]
	switch {
	case dhcp4:
		c.IPv4 = "dhcp"
	case len(c.Addrs4) > 0:
		c.IPv4 = "static"
	default:
		c.IPv4 = "off"
	}
	switch {
	case len(c.Addrs6) > 0:
		c.IPv6 = "static"
	case dhcp6 || ra != "false":
		c.IPv6 = "auto" // tanpa kunci pun kernel menerima RA (SLAAC)
	default:
		c.IPv6 = "off"
	}
	return c, nil
}

func cekCIDR(list []string, v6 bool) error {
	for _, a := range list {
		ip, _, err := net.ParseCIDR(a)
		if err != nil || (ip.To4() == nil) != v6 {
			return errInvalid("alamat IP tidak valid: %s", a)
		}
	}
	return nil
}

func cekGateway(gw string, v6 bool) error {
	if gw == "" {
		return nil
	}
	if ip := net.ParseIP(gw); ip == nil || (ip.To4() == nil) != v6 {
		return errInvalid("gateway tidak valid: %s", gw)
	}
	return nil
}

// ifaceConfigSet menulis konfigurasi lewat `netplan set`, memvalidasinya
// dengan `netplan generate`, lalu `netplan apply`. Kalau generate menolak,
// konfigurasi lama ditulis kembali supaya berkas netplan tidak tertinggal
// dalam keadaan rusak.
//
// ponytail: apply langsung, tanpa `netplan try`. Alamat yang salah pada
// interface yang dipakai panel = terkunci sampai dibetulkan lewat konsol VM.
// Jalan naiknya: `netplan try --timeout` di unit transient + endpoint
// konfirmasi dari alamat baru.
func ifaceConfigSet(c helperproto.IfaceConfig) error {
	if !ifaceRe.MatchString(c.Iface) {
		return errInvalid("nama interface tidak valid")
	}
	if !slices.Contains([]string{"dhcp", "static", "off"}, c.IPv4) ||
		!slices.Contains([]string{"auto", "static", "off"}, c.IPv6) {
		return errInvalid("mode IP tidak dikenal")
	}
	if c.IPv4 == "static" && len(c.Addrs4) == 0 || c.IPv6 == "static" && len(c.Addrs6) == 0 {
		return errInvalid("mode statis butuh minimal satu alamat")
	}
	if err := cekCIDR(c.Addrs4, false); err != nil {
		return err
	}
	if err := cekCIDR(c.Addrs6, true); err != nil {
		return err
	}
	if err := cekGateway(c.Gw4, false); err != nil {
		return err
	}
	if err := cekGateway(c.Gw6, true); err != nil {
		return err
	}
	lama, err := ifaceConfigGet(c.Iface)
	if err != nil {
		return err
	}
	if !lama.Managed {
		return errInvalid("interface %s tidak dikelola netplan", c.Iface)
	}
	if err := tulisNetplan(c); err != nil {
		return err
	}
	if _, err := netplan("generate"); err != nil {
		_ = tulisNetplan(lama)
		return err
	}
	_, err = netplan("apply")
	return err
}

// tulisNetplan menormalkan mode (alamat/gateway hanya berarti pada mode
// static) lalu memanggil `netplan set` untuk tiap kunci yang berbeda dari
// keadaan sekarang.
func tulisNetplan(c helperproto.IfaceConfig) error {
	if c.IPv4 != "static" {
		c.Addrs4, c.Gw4 = nil, ""
	}
	if c.IPv6 != "static" {
		c.Addrs6, c.Gw6 = nil, ""
	}
	all, err := netplanGet("all")
	if err != nil {
		return err
	}
	seksi, blok := bagianNetplan(all, c.Iface)
	if blok == nil {
		return errInvalid("interface %s tidak dikelola netplan", c.Iface)
	}
	k := seksi + "." + c.Iface + "."
	// Rute non-default milik user dipertahankan apa adanya; hanya rute default
	// yang dikelola halaman ini.
	ruteLama := bacaRute(blok["routes"])
	var rutes []ruteEntri
	for _, r := range ruteLama {
		if !r.bawaan() {
			rutes = append(rutes, r)
		}
	}
	for _, gw := range []string{c.Gw4, c.Gw6} {
		if gw != "" {
			rutes = append(rutes, ruteEntri{{"to", `"default"`}, {"via", `"` + gw + `"`}})
		}
	}
	addrs := append(append([]string{}, c.Addrs4...), c.Addrs6...)
	var set [][2]string
	// List: `set` pada list = tambah, bukan ganti — kosongkan dulu.
	if lama := bacaDaftar(blok["addresses"]); !slices.Equal(lama, addrs) {
		if len(lama) > 0 {
			set = append(set, [2]string{"addresses", "null"})
		}
		if len(addrs) > 0 {
			set = append(set, [2]string{"addresses", flowKutip(addrs)})
		}
	}
	if flowYAML(ruteLama) != flowYAML(rutes) {
		if len(ruteLama) > 0 {
			set = append(set, [2]string{"routes", "null"})
		}
		if len(rutes) > 0 {
			set = append(set, [2]string{"routes", flowYAML(rutes)})
		}
	}
	// Skalar: "" = kunci tidak ada = bawaan netplan. auto pada IPv6 berarti
	// bawaan itu sendiri (kernel menerima RA, SLAAC), jadi kuncinya dihapus.
	for _, kv := range [][2]string{
		{"dhcp4", pilih(c.IPv4 == "dhcp", "true", "")},
		{"dhcp6", pilih(c.IPv6 == "auto", "", "false")},
		{"accept-ra", pilih(c.IPv6 == "auto", "", "false")},
		{"link-local", pilih(c.IPv6 == "off", "[]", "")},
	} {
		lama := strings.TrimSpace(blok[kv[0]])
		if kv[1] == "" && lama == "false" && kv[0] == "dhcp4" {
			continue // dhcp4: false ≡ tidak ada
		}
		if lama != kv[1] {
			set = append(set, [2]string{kv[0], pilih(kv[1] == "", "null", kv[1])})
		}
	}
	for _, kv := range set {
		if _, err := netplan("set", k+kv[0]+"="+kv[1]); err != nil {
			return err
		}
	}
	return nil
}

func pilih(cond bool, ya, tidak string) string {
	if cond {
		return ya
	}
	return tidak
}
