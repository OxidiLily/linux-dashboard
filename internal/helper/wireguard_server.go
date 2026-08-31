package helper

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// WireGuard mode server: panel yang menyiapkan config, kunci, NAT, dan daftar
// klien — lawan dari mode klien yang cuma menempel config jadi.
//
// Sumber kebenarannya tetap berkas `/etc/wireguard/<iface>.conf` itu sendiri,
// bukan basis data panel. Config yang dibuat orang di luar panel karena itu
// ikut terbaca dan bisa dikelola dari sini; panel tidak pernah menimpanya
// diam-diam (lihat wgServerInit).
//
// Private key klien TIDAK disimpan di server: yang masuk config server hanya
// public key-nya, persis seperti WireGuard dirancang. Konsekuensinya config
// klien hanya bisa ditampilkan sekali saat dibuat — kalau hilang, klien itu
// dibuat ulang.

const wgNamaTanda = "# klien: "

var (
	wgNamaRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,31}$`)
	wgMetaRe = regexp.MustCompile(`^# lindash-wg: .*endpoint=(\S+)`)
	// Endpoint boleh FQDN bertitik (vpn.contoh.com), beda dengan hostnameRe di
	// system.go yang hanya menerima satu label karena dipakai untuk hostname
	// mesin. Alamat IP diperiksa terpisah lewat net.ParseIP.
	wgEndpointRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
)

// wgEndpointValid menerima IP maupun hostname/FQDN yang dituju klien.
func wgEndpointValid(v string) bool {
	if v == "" || len(v) > 253 {
		return false
	}
	return net.ParseIP(v) != nil || wgEndpointRe.MatchString(v)
}

func wgConfPath(iface string) string { return "/etc/wireguard/" + iface + ".conf" }

// wgServerInfo membaca keadaan sistem apa adanya: ada tidaknya config, apakah
// ia config server (punya ListenPort), dan daftar peer-nya.
func wgServerInfo() helperproto.WGServerInfo {
	iface := wgInterfaceTerkonfigurasi()
	info := helperproto.WGServerInfo{Iface: iface, Peers: []helperproto.WGPeer{}}

	b, err := os.ReadFile(wgConfPath(iface))
	if err != nil {
		return info
	}
	info.Ada = true

	var peer *helperproto.WGPeer
	namaBerikut := ""
	for _, baris := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(baris)
		switch {
		case strings.HasPrefix(t, wgNamaTanda):
			namaBerikut = strings.TrimSpace(strings.TrimPrefix(t, wgNamaTanda))
		case wgMetaRe.MatchString(t):
			info.Endpoint = wgMetaRe.FindStringSubmatch(t)[1]
		case strings.EqualFold(t, "[Peer]"):
			info.Peers = append(info.Peers, helperproto.WGPeer{Nama: namaBerikut})
			peer = &info.Peers[len(info.Peers)-1]
			namaBerikut = ""
		case strings.EqualFold(t, "[Interface]"):
			peer = nil
		}
		kunci, nilai, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		kunci, nilai = strings.TrimSpace(kunci), strings.TrimSpace(nilai)
		switch {
		case peer == nil && strings.EqualFold(kunci, "ListenPort"):
			info.Port, _ = strconv.Atoi(nilai)
			info.Server = info.Port > 0
		case peer == nil && strings.EqualFold(kunci, "Address"):
			// Address server berbentuk 10.8.0.1/24 — subnet tunnel diambil
			// dari sana supaya config buatan luar panel pun terbaca benar.
			if _, jaringan, err := net.ParseCIDR(nilai); err == nil {
				info.Subnet = jaringan.String()
			}
		case peer != nil && strings.EqualFold(kunci, "PublicKey"):
			peer.PublicKey = nilai
		case peer != nil && strings.EqualFold(kunci, "AllowedIPs"):
			peer.IP = strings.TrimSuffix(strings.Split(nilai, ",")[0], "/32")
		}
	}
	wgIsiStatistik(iface, info.Peers)
	return info
}

// wgIsiStatistik melengkapi peer dengan handshake & transfer dari interface
// yang sedang hidup. Config saja tidak tahu klien mana yang benar-benar aktif.
func wgIsiStatistik(iface string, peers []helperproto.WGPeer) {
	res, err := run("wg", "show", iface, "dump")
	if err != nil {
		return
	}
	for _, baris := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(baris, "\t")
		if len(f) < 6 {
			continue
		}
		for i := range peers {
			if peers[i].PublicKey != f[0] {
				continue
			}
			if detik, err := strconv.ParseInt(f[4], 10, 64); err == nil && detik > 0 {
				peers[i].Handshake = strconv.FormatInt(detik, 10)
			}
			peers[i].Transfer = f[5] + "/" + f[6]
		}
	}
}

func wgServerInit(args helperproto.WGServerArgs) (helperproto.WGServerInfo, error) {
	if !installed("wg-quick") {
		return helperproto.WGServerInfo{}, errInvalid("WireGuard belum terpasang — install dulu lewat Components")
	}
	ip, jaringan, err := net.ParseCIDR(strings.TrimSpace(args.Subnet))
	if err != nil || ip.To4() == nil {
		return helperproto.WGServerInfo{}, errInvalid("subnet tidak valid — pakai bentuk CIDR IPv4, mis. 10.8.0.0/24")
	}
	if args.Port < 1 || args.Port > 65535 {
		return helperproto.WGServerInfo{}, errInvalid("port tidak valid")
	}
	endpoint := strings.TrimSpace(args.Endpoint)
	if !wgEndpointValid(endpoint) {
		return helperproto.WGServerInfo{}, errInvalid("endpoint tidak valid — isi IP publik atau hostname yang bisa dijangkau klien")
	}

	iface := wgInterfaceTerkonfigurasi()
	conf := wgConfPath(iface)
	// Config yang sudah ada TIDAK pernah ditimpa: isinya memuat private key
	// yang tidak bisa dibuat ulang, dan bisa saja dipasang di luar panel.
	if _, err := os.Stat(conf); err == nil {
		return helperproto.WGServerInfo{}, errKode(helperproto.ErrSudahAda,
			"config %s sudah ada — hapus dulu lewat tombol hapus config kalau memang mau diganti", conf)
	}

	priv, err := wgKunciBaru()
	if err != nil {
		return helperproto.WGServerInfo{}, err
	}
	wan := wgInterfaceWAN()
	if wan == "" {
		return helperproto.WGServerInfo{}, errInvalid("interface keluar tidak terbaca — tidak ada rute default di mesin ini")
	}
	alamatServer := wgAlamatKe(jaringan, 1)
	prefix, _ := jaringan.Mask.Size()

	naikkan, turunkan := wgAturanNAT(jaringan.String(), wan)

	isi := fmt.Sprintf(`# lindash-wg: subnet=%s endpoint=%s port=%d
[Interface]
Address = %s/%d
ListenPort = %d
PrivateKey = %s
PostUp = %s
PostDown = %s
`, jaringan.String(), endpoint, args.Port,
		alamatServer, prefix, args.Port, priv, naikkan, turunkan)

	if err := os.MkdirAll("/etc/wireguard", 0o700); err != nil {
		return helperproto.WGServerInfo{}, err
	}
	if err := os.WriteFile(conf, []byte(isi), 0o600); err != nil {
		return helperproto.WGServerInfo{}, err
	}

	// Tanpa forwarding, paket klien berhenti di server ini dan tunnel terlihat
	// "tersambung tapi tidak bisa apa-apa".
	if err := wgNyalakanForwarding(); err != nil {
		return helperproto.WGServerInfo{}, err
	}
	// Aturan ditulis selama ufw ADA, bukan hanya saat ufw sedang menyala:
	// `ufw allow` tersimpan di user.rules meskipun firewall nonaktif dan tidak
	// menyalakan apa pun, jadi menundanya hanya membuat port ini ikut mati saat
	// pemiliknya menyalakan firewall nanti.
	//
	// Sumber sengaja dibiarkan Anywhere, tidak dibatasi subnet lokal seperti
	// port komponen lain: peer WireGuard justru menyambung dari luar jaringan.
	if _, ada := lookBinary("ufw"); ada {
		_ = ufwAdd(helperproto.UfwRule{Action: "allow", Port: strconv.Itoa(args.Port), Proto: "udp"})
	}
	_, _ = run("systemctl", "enable", "wg-quick@"+iface)
	if _, err := run("wg-quick", "up", iface); err != nil {
		return helperproto.WGServerInfo{}, err
	}
	return wgServerInfo(), nil
}

func wgPeerTambah(args helperproto.WGPeerArgs) (helperproto.WGPeerBaru, error) {
	nama := strings.TrimSpace(args.Nama)
	if !wgNamaRe.MatchString(nama) {
		return helperproto.WGPeerBaru{}, errInvalid("nama klien tidak valid — huruf/angka/spasi/titik/strip, maksimal 32 karakter")
	}
	info := wgServerInfo()
	if !info.Server {
		return helperproto.WGPeerBaru{}, errInvalid("mode server belum disiapkan")
	}
	for _, p := range info.Peers {
		if strings.EqualFold(p.Nama, nama) {
			return helperproto.WGPeerBaru{}, errKode(helperproto.ErrSudahAda, "klien %q sudah ada", nama)
		}
	}
	_, jaringan, err := net.ParseCIDR(info.Subnet)
	if err != nil {
		return helperproto.WGPeerBaru{}, errInvalid("subnet di config tidak terbaca")
	}
	ipKlien := wgAlamatKosong(jaringan, info.Peers)
	if ipKlien == "" {
		return helperproto.WGPeerBaru{}, errInvalid("alamat di subnet %s sudah habis", info.Subnet)
	}

	privKlien, err := wgKunciBaru()
	if err != nil {
		return helperproto.WGPeerBaru{}, err
	}
	pubKlien, err := wgPublicKey(privKlien)
	if err != nil {
		return helperproto.WGPeerBaru{}, err
	}
	pubServer, err := wgPublicKeyServer(info.Iface)
	if err != nil {
		return helperproto.WGPeerBaru{}, err
	}

	conf := wgConfPath(info.Iface)
	b, err := os.ReadFile(conf)
	if err != nil {
		return helperproto.WGPeerBaru{}, err
	}
	blok := fmt.Sprintf("\n%s%s\n[Peer]\nPublicKey = %s\nAllowedIPs = %s/32\n", wgNamaTanda, nama, pubKlien, ipKlien)
	if err := os.WriteFile(conf, append(b, []byte(blok)...), 0o600); err != nil {
		return helperproto.WGPeerBaru{}, err
	}
	if err := wgTerapkan(info.Iface); err != nil {
		return helperproto.WGPeerBaru{}, err
	}

	prefix, _ := jaringan.Mask.Size()
	// AllowedIPs klien sengaja hanya subnet tunnel, bukan 0.0.0.0/0: rute
	// default yang berpindah diam-diam bisa memutus koneksi klien sendiri.
	cfg := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/%d

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = %s
PersistentKeepalive = 25
`, privKlien, ipKlien, prefix, pubServer, info.Endpoint, info.Port, jaringan.String())

	return helperproto.WGPeerBaru{
		Peer:   helperproto.WGPeer{Nama: nama, PublicKey: pubKlien, IP: ipKlien},
		Config: cfg,
	}, nil
}

func wgPeerHapus(args helperproto.WGPeerArgs) (helperproto.WGServerInfo, error) {
	if strings.TrimSpace(args.PublicKey) == "" {
		return helperproto.WGServerInfo{}, errInvalid("public key klien kosong")
	}
	iface := wgInterfaceTerkonfigurasi()
	conf := wgConfPath(iface)
	b, err := os.ReadFile(conf)
	if err != nil {
		return helperproto.WGServerInfo{}, err
	}
	baru, ketemu := wgBuangPeer(string(b), args.PublicKey)
	if !ketemu {
		return helperproto.WGServerInfo{}, errKode(helperproto.ErrNotFound, "klien tidak ada di config")
	}
	if err := os.WriteFile(conf, []byte(baru), 0o600); err != nil {
		return helperproto.WGServerInfo{}, err
	}
	// Peer yang dihapus dari config tetap hidup di kernel sampai config
	// diterapkan ulang — tanpa ini klien yang "dihapus" masih bisa masuk.
	_, _ = run("wg", "set", iface, "peer", args.PublicKey, "remove")
	if err := wgTerapkan(iface); err != nil {
		return helperproto.WGServerInfo{}, err
	}
	return wgServerInfo(), nil
}

// wgBuangPeer membuang satu blok [Peer] beserta baris penanda namanya.
// Dipisah dari berkasnya supaya bisa diuji tanpa menyentuh /etc/wireguard.
func wgBuangPeer(isi, pubkey string) (string, bool) {
	baris := strings.Split(isi, "\n")
	mulai, cocok := -1, false
	buangDari, buangSampai := -1, -1

	// Baris penanda nama berada TEPAT sebELUM [Peer] miliknya, jadi ia ikut
	// masuk blok yang dibuang dan tidak boleh ikut terbawa dari blok berikutnya.
	penanda := func(i int) bool {
		return i > 0 && strings.HasPrefix(strings.TrimSpace(baris[i-1]), wgNamaTanda)
	}
	tutup := func(i int) {
		if mulai >= 0 && cocok {
			if penanda(i) {
				i--
			}
			buangDari, buangSampai = mulai, i
		}
		mulai, cocok = -1, false
	}

	for i, b := range baris {
		t := strings.TrimSpace(b)
		if strings.HasPrefix(t, "[") {
			tutup(i)
			if strings.EqualFold(t, "[Peer]") {
				mulai = i
				if penanda(i) {
					mulai = i - 1
				}
			}
			continue
		}
		if mulai >= 0 && strings.HasPrefix(t, "PublicKey") {
			if _, nilai, ok := strings.Cut(t, "="); ok && strings.TrimSpace(nilai) == pubkey {
				cocok = true
			}
		}
	}
	tutup(len(baris))

	if buangDari < 0 {
		return isi, false
	}
	sisa := append(append([]string{}, baris[:buangDari]...), baris[buangSampai:]...)
	return strings.Join(sisa, "\n"), true
}

// wgTerapkan menyinkronkan config ke interface yang sedang hidup tanpa
// memutus peer lain — `wg-quick down/up` akan memutus semua klien yang sedang
// terhubung hanya karena satu klien ditambahkan.
func wgTerapkan(iface string) error {
	if _, err := run("wg", "show", iface); err != nil {
		return nil // interface sedang mati; config baru dipakai saat dinyalakan
	}
	res, err := run("wg-quick", "strip", iface)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "lindash-wg-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(res.Stdout); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	_, err = run("wg", "syncconf", iface, tmp.Name())
	return err
}

// wgAturanNAT menyusun PostUp/PostDown: trafik klien di-MASQUERADE ke
// interface keluar, dan forwarding lewat interface tunnel diizinkan. `%i`
// diganti wg-quick dengan nama interface saat dijalankan, jadi harus lolos
// utuh dari fmt.
func wgAturanNAT(subnet, wan string) (naik, turun string) {
	pola := "iptables -t nat -%[1]s POSTROUTING -s %[2]s -o %[3]s -j MASQUERADE; " +
		"iptables -%[1]s FORWARD -i %%i -j ACCEPT; " +
		"iptables -%[1]s FORWARD -o %%i -j ACCEPT"
	return fmt.Sprintf(pola, "A", subnet, wan), fmt.Sprintf(pola, "D", subnet, wan)
}

func wgKunciBaru() (string, error) {
	res, err := run("wg", "genkey")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func wgPublicKey(priv string) (string, error) {
	res, err := runStdin(priv+"\n", "wg", "pubkey")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// wgPublicKeyServer menurunkan public key server dari private key di config —
// tidak perlu menyimpannya terpisah, dan tidak akan pernah tidak sinkron.
func wgPublicKeyServer(iface string) (string, error) {
	b, err := os.ReadFile(wgConfPath(iface))
	if err != nil {
		return "", err
	}
	for _, baris := range strings.Split(string(b), "\n") {
		kunci, nilai, ok := strings.Cut(strings.TrimSpace(baris), "=")
		if ok && strings.EqualFold(strings.TrimSpace(kunci), "PrivateKey") {
			return wgPublicKey(strings.TrimSpace(nilai))
		}
	}
	return "", errInvalid("private key server tidak ada di config")
}

// wgInterfaceWAN mengembalikan interface rute default — tujuan MASQUERADE.
func wgInterfaceWAN() string {
	res, err := run("ip", "route", "show", "default")
	if err != nil {
		return ""
	}
	f := strings.Fields(firstLine(res.Stdout))
	for i, v := range f {
		if v == "dev" && i+1 < len(f) {
			return f[i+1]
		}
	}
	return ""
}

func wgNyalakanForwarding() error {
	const berkas = "/etc/sysctl.d/99-linux-dashboard-wg.conf"
	if err := os.WriteFile(berkas, []byte("net.ipv4.ip_forward = 1\n"), 0o644); err != nil {
		return err
	}
	_, err := run("sysctl", "-w", "net.ipv4.ip_forward=1")
	return err
}

func wgAlamatKe(jaringan *net.IPNet, ke int) string {
	ip := jaringan.IP.To4()
	if ip == nil {
		return ""
	}
	nilai := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	nilai += uint32(ke)
	return net.IPv4(byte(nilai>>24), byte(nilai>>16), byte(nilai>>8), byte(nilai)).String()
}

// wgAlamatKosong mencari alamat host pertama yang belum dipakai peer mana pun.
// Alamat .1 milik server, dan alamat broadcast dilewati.
func wgAlamatKosong(jaringan *net.IPNet, peers []helperproto.WGPeer) string {
	dipakai := map[string]bool{wgAlamatKe(jaringan, 1): true}
	for _, p := range peers {
		dipakai[p.IP] = true
	}
	prefix, bit := jaringan.Mask.Size()
	maks := 1<<(bit-prefix) - 2
	for i := 2; i <= maks; i++ {
		if kandidat := wgAlamatKe(jaringan, i); !dipakai[kandidat] {
			return kandidat
		}
	}
	return ""
}
