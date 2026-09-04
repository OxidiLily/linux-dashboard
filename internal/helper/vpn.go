package helper

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Tailscale, Cloudflare Tunnel, dan WireGuard dikelompokkan jadi satu kelompok
// VPN/Tunnel — konfigurasinya ada di Settings → Network, bukan halaman sendiri.

const wgInterface = "wg0"

// Panjang dicek terpisah dari pola: repeat count di regexp Go dibatasi 1000,
// sedangkan token Cloudflare Tunnel bisa jauh lebih panjang dari itu.
var (
	authKeyRe = regexp.MustCompile(`^[A-Za-z0-9\-_.]+$`)
	tokenRe   = regexp.MustCompile(`^[A-Za-z0-9\-_.=]+$`)
)

func validSecret(re *regexp.Regexp, s string, max int) bool {
	return len(s) >= 10 && len(s) <= max && re.MatchString(s)
}

func vpnStatusAll() []helperproto.VPNStatus {
	return []helperproto.VPNStatus{
		tailscaleStatus(), cloudflaredStatus(), wireguardStatus(),
	}
}

func installed(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// mulaiDaemonLepas menjalankan proses yang memang tidak pernah selesai,
// terlepas dari daemon helper: sesi sendiri (Setsid) supaya tidak ikut mati
// saat helper di-restart, dan tanpa Wait supaya pemanggilnya bisa lanjut.
//
// Proses yang ditinggalkan begini menjadi zombie sampai init memungutnya —
// itu wajar untuk daemon, dan satu-satunya jalan di mesin tanpa systemd yang
// tidak punya siapa pun untuk mengawasi prosesnya.
func mulaiDaemonLepas(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Keluaran dibuang: tidak ada yang membacanya, dan pipe yang penuh akan
	// membekukan daemon-nya sendiri setelah beberapa KB log.
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// tungguSocket menunggu socket unix muncul sampai batas waktu.
func tungguSocket(path string, batas time.Duration) {
	tenggat := time.Now().Add(batas)
	for time.Now().Before(tenggat) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func tailscaleStatus() helperproto.VPNStatus {
	st := helperproto.VPNStatus{Name: "tailscale", Installed: installed("tailscale")}
	if !st.Installed {
		st.State = "belum terpasang"
		return st
	}
	// tailscaled belum jalan adalah penyebab paling sering "failed to connect
	// to local tailscaled" — daemon default tidak auto-start setelah install.
	// Coba hidupkan; kalau systemd menolak (WSL/lxc tanpa init), tampilkan
	// status apa adanya + instruksi manual di field Detail.
	if _, err := run("systemctl", "is-active", "--quiet", "tailscaled"); err != nil {
		if _, e := run("systemctl", "start", "tailscaled"); e == nil {
			// Beri waktu singkat agar socket unix tailscaled siap.
			time.Sleep(500 * time.Millisecond)
		}
	}
	// Mask kunci terakhir tampil di semua keadaan, sama seperti token
	// cloudflared: user perlu tahu kunci mana yang terpasang saat memutuskan
	// mau memakainya lagi atau menimpanya dengan kunci lain.
	st.Token = bacaMaskTailscale()
	res, err := run("tailscale", "status", "--peers=false")
	bacaStatusTS(&st, res.Stdout, res.Stderr, err)
	return st
}

// bacaStatusTS menerjemahkan keluaran `tailscale status` jadi keadaan yang
// dipahami panel. Dipisah dari tailscaleStatus supaya bisa diuji tanpa
// tailscaled — tepat di sini letak satu-satunya bagian yang rapuh, yaitu
// pencocokan kalimat CLI.
func bacaStatusTS(st *helperproto.VPNStatus, stdout, stderr string, err error) {
	out := strings.TrimSpace(stdout)
	pesan := out + "\n" + stderr
	switch {
	// Tailnet dengan Device approval aktif: node sudah terdaftar, kunci sudah
	// ditukar, dan tidak ada satu pun langkah tersisa di mesin ini — yang
	// kurang hanya klik admin di konsol Tailscale. Tanpa keadaan tersendiri
	// ini tampil sebagai "tidak aktif" berbadge merah dan terbaca seperti bug
	// panel.
	//
	// ponytail: dicocokkan dari teks `tailscale status`; kalau kalimatnya
	// berubah di versi Tailscale mendatang, pindah ke
	// `tailscale status --json` lalu baca BackendState == "NeedsMachineAuth".
	case err != nil && strings.Contains(pesan, "not yet approved"):
		st.NeedsApproval = true
		st.State = "menunggu persetujuan admin tailnet"
		st.Detail = tsPesanApproval
	case err != nil && strings.Contains(pesan, "Logged out"):
		st.State = "logged out"
		// `tailscale status` menaruh URL login di baris KEDUA; firstLine akan
		// membuangnya dan menyisakan kalimat tanpa langkah lanjutan.
		st.Detail = ringkasDuaBaris(firstNonEmpty(out, stderr))
	case err != nil:
		st.State = "tidak aktif"
		st.Detail = firstLine(firstNonEmpty(stderr, err.Error()))
	default:
		st.Connected = true
		st.State = "terhubung"
		st.Detail = ringkasStatusTS(out)
	}
}

// tsPesanApproval adalah satu-satunya langkah yang tersisa untuk node yang
// menunggu persetujuan; alamatnya ditulis lengkap karena user membuka konsol
// Tailscale dari mesin lain, bukan dari server ini.
const tsPesanApproval = "Node ini sudah terdaftar di tailnet tapi belum disetujui admin. " +
	"Buka https://login.tailscale.com/admin/machines lalu setujui mesin ini — " +
	"tidak ada yang perlu diubah di server."

// ringkasDuaBaris menyatukan dua baris pertama jadi satu kalimat. `tailscale
// status` memisahkan pesan dan URL tindak lanjutnya ke baris berbeda, dan
// kartu VPN hanya menampilkan satu baris.
func ringkasDuaBaris(s string) string {
	baris := strings.SplitN(strings.TrimSpace(s), "\n", 3)
	if len(baris) > 2 {
		baris = baris[:2]
	}
	for i := range baris {
		baris[i] = strings.TrimSpace(baris[i])
	}
	return strings.Join(baris, " ")
}

// ringkasStatusTS memangkas baris pertama `tailscale status` jadi empat kolom
// yang berarti: IP tailnet, hostname, pemilik, dan OS. Kolom sesudahnya berisi
// penanda status ("-", "offline", "idle") yang sudah diwakili label Terkoneksi
// di kartu, jadi cuma bikin baris detail penuh.
func ringkasStatusTS(out string) string {
	f := strings.Fields(firstLine(out))
	if len(f) > 4 {
		f = f[:4]
	}
	return strings.Join(f, " ")
}

func cloudflaredStatus() helperproto.VPNStatus {
	st := helperproto.VPNStatus{Name: "cloudflared", Installed: installed("cloudflared")}
	if !st.Installed {
		st.State = "belum terpasang"
		return st
	}
	// Yang dikirim ke browser hanya bentuk tersamar: cukup untuk mengenali
	// tunnel mana yang terpasang, tanpa membocorkan token utuh ke halaman web.
	full := cloudflaredToken()
	st.Token = maskToken(full)
	_, aktifErr := run("systemctl", "is-active", "--quiet", "cloudflared")
	switch {
	case aktifErr == nil:
		st.Connected = true
		st.State = "tunnel aktif"
		st.Detail = cloudflaredDetail()
	case full != "":
		st.State = "tunnel berhenti"
	case cloudflaredServiceInstalled():
		// Unit sudah ada di sistem tapi tokennya tidak terbaca panel — bisa
		// dipasang manual di luar panel, atau file tokennya hilang. Jangan
		// diperlakukan seperti "belum ada".
		st.State = "service terpasang di sistem, token tidak terbaca"
		st.Detail = "Isi token lalu Sambung — panel memasang ulang service dengan token itu."
	default:
		st.State = "belum dikonfigurasi"
	}
	return st
}

// cloudflaredDetail mengambil hostname yang dilayani tunnel, sepadan dengan
// baris detail Tailscale yang berisi alamat & identitas node.
//
// cloudflared tidak punya perintah status seperti `tailscale status`, dan
// endpoint metrics-nya memakai port acak kecuali di-set di unit — jadi journal
// proses yang sedang jalan adalah satu-satunya sumber yang selalu ada.
//
// Nama tunnel TIDAK bisa ditampilkan: tunnel yang dijalankan dengan token
// dikelola dari sisi Cloudflare, dan yang sampai ke mesin ini hanya tunnelID
// (UUID) plus konfigurasi ingress-nya — namanya tidak pernah dikirim.
func cloudflaredDetail() string {
	res, err := run("systemctl", "show", "-p", "MainPID", "--value", "cloudflared")
	pid := strings.TrimSpace(res.Stdout)
	if err != nil || pid == "" || pid == "0" {
		return ""
	}
	// Dibatasi ke PID proses yang sedang jalan supaya konfigurasi dari sesi
	// sebelum restart tidak ikut terbaca.
	res, err = run("journalctl", "_PID="+pid, "-n", "300", "-o", "cat", "--no-pager")
	if err != nil {
		return ""
	}
	return identitasTunnelCF(res.Stdout)
}

var (
	cloudflaredIngressRe = regexp.MustCompile(`"hostname":"([^"]+)"`)
	cloudflaredIDRe      = regexp.MustCompile(`Starting tunnel tunnelID=(\S+)`)
)

// identitasTunnelCF mengambil penanda tunnel dari log: hostname ingress kalau
// konfigurasinya sudah diterima dari Cloudflare, kalau tidak potongan tunnelID.
// Dibatasi tiga hostname supaya baris detail tetap terbaca di kartu; sisanya
// diringkas jadi penanda jumlah.
func identitasTunnelCF(log string) string {
	// Baris config di journal ber-escape (\"hostname\":\"..."), jadi
	// backslash-nya dibuang dulu supaya polanya cocok.
	bersih := strings.ReplaceAll(log, `\`, "")
	host := []string{}
	for _, m := range cloudflaredIngressRe.FindAllStringSubmatch(bersih, -1) {
		if !slices.Contains(host, m[1]) {
			host = append(host, m[1])
		}
	}
	if len(host) > 0 {
		slices.Sort(host)
		if sisa := len(host) - 3; sisa > 0 {
			return strings.Join(host[:3], ", ") + fmt.Sprintf(" +%d", sisa)
		}
		return strings.Join(host, ", ")
	}
	if m := cloudflaredIDRe.FindStringSubmatch(log); m != nil && len(m[1]) >= 8 {
		return m[1][:8]
	}
	return ""
}

// `cloudflared service install <token>` menaruh token di ExecStart unit
// systemd, bukan di file config tersendiri — itu satu-satunya tempat token
// tunnel yang sedang terpasang bisa dibaca balik.
var (
	cloudflaredTokenRe     = regexp.MustCompile(`--token[= ]([A-Za-z0-9\-_.=]+)`)
	cloudflaredTokenFileRe = regexp.MustCompile(`--token-file[= ](\S+)`)
)

const cloudflaredUnit = "/etc/systemd/system/cloudflared.service"

// cloudflaredServiceInstalled memeriksa keberadaan unit systemd-nya langsung.
//
// Ini TIDAK boleh disimpulkan dari "token terbaca": versi cloudflared berbeda
// menulis ExecStart berbeda (`--token <nilai>` vs `--token-file <path>`), dan
// unit juga bisa dipasang orang di luar panel. Menyimpulkan "belum terpasang"
// dari token yang gagal dibaca membuat panel menjalankan `service install` di
// atas unit yang sudah ada, dan cloudflared menolak dengan
// "service is already installed at /etc/systemd/system/cloudflared.service".
func cloudflaredServiceInstalled() bool {
	if _, err := os.Stat(cloudflaredUnit); err == nil {
		return true
	}
	_, err := run("systemctl", "cat", "cloudflared")
	return err == nil
}

// Input dari UI boleh berupa perintah lengkap yang disalin dari dashboard
// Cloudflare — `sudo cloudflared service install <token>` atau
// `cloudflared tunnel run --token <token>` — maupun token telanjang. Yang
// diambil selalu tokennya saja.
var cloudflaredInputRe = regexp.MustCompile(`(?:--token[= ]|service\s+install\s+)([A-Za-z0-9\-_.=]+)`)

func extractCloudflaredToken(input string) string {
	input = strings.TrimSpace(input)
	if m := cloudflaredInputRe.FindStringSubmatch(input); m != nil {
		return m[1]
	}
	// Token telanjang: satu kata tanpa spasi.
	if input != "" && !strings.ContainsAny(input, " \t\n\r") {
		return input
	}
	return ""
}

// maskToken memampatkan token jadi bentuk yang aman ditampilkan: cukup untuk
// membedakan satu tunnel dari tunnel lain, tidak cukup untuk dipakai.
func maskToken(t string) string { return maskPotong(t, 9, 16) }

// Auth key Tailscale disamarkan dengan kepala lebih panjang: sebelas karakter
// pertama hanya jenis kunci ("tskey-auth-"), jadi memotong di 9 karakter
// membuat semua kunci terlihat sama persis.
func maskAuthKey(t string) string { return maskPotong(t, 18, 15) }

func maskPotong(t string, head, tail int) string {
	if t == "" {
		return ""
	}
	if len(t) <= head+tail {
		return strings.Repeat("*", len(t))
	}
	return t[:head] + "..." + t[len(t)-tail:]
}

// Input dari UI boleh berupa perintah yang disalin utuh dari dashboard
// Tailscale — termasuk yang digabung dengan installer lewat `&&` — maupun
// auth key telanjang.
var tailscaleKeyRe = regexp.MustCompile(`--auth-?key[= ]([A-Za-z0-9\-_.]+)`)

func extractTailscaleKey(input string) string {
	input = strings.TrimSpace(input)
	if m := tailscaleKeyRe.FindStringSubmatch(input); m != nil {
		return m[1]
	}
	if input != "" && !strings.ContainsAny(input, " \t\n\r") {
		return input
	}
	return ""
}

// Tailscale menukar auth key dengan node key saat login dan TIDAK pernah
// mengembalikan kuncinya lagi, jadi tidak ada tempat di sistem untuk
// membacanya balik. Yang disimpan panel hanya bentuk tersamarnya — cukup untuk
// menunjukkan kunci mana yang dipakai, dan tidak bisa dipakai siapa pun.
const tailscaleMaskPath = "/var/lib/linux-dashboard/tailscale-authkey.mask"

// tsTimeoutUp membatasi tunggu `tailscale up`. Cukup panjang untuk pertukaran
// kunci dan pembentukan rute di jaringan lambat, cukup pendek supaya tombol
// Sambung selalu menjawab sebelum user menyerah dan memuat ulang halaman.
const tsTimeoutUp = 30 * time.Second

func simpanMaskTailscale(key string) {
	if key == "" {
		return
	}
	_ = os.WriteFile(tailscaleMaskPath, []byte(maskAuthKey(key)), 0o600)
}

func bacaMaskTailscale() string {
	b, err := os.ReadFile(tailscaleMaskPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func cloudflaredToken() string {
	res, err := run("systemctl", "show", "-p", "ExecStart", "--value", "cloudflared")
	if err != nil {
		return ""
	}
	if m := cloudflaredTokenRe.FindStringSubmatch(res.Stdout); m != nil {
		return m[1]
	}
	// Varian `--token-file`: tokennya ada di file, bukan di baris perintah.
	if m := cloudflaredTokenFileRe.FindStringSubmatch(res.Stdout); m != nil {
		if b, err := os.ReadFile(strings.TrimSpace(m[1])); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// wgInterfaceAktif mengembalikan nama interface WireGuard yang benar-benar
// hidup di kernel, apa pun namanya. Menganggap hanya "wg0" yang ada membuat
// tunnel yang sudah jalan dengan nama lain terlihat mati di panel.
func wgInterfaceAktif() string {
	res, err := run("wg", "show", "interfaces")
	if err != nil {
		return ""
	}
	if f := strings.Fields(res.Stdout); len(f) > 0 {
		return f[0]
	}
	return ""
}

// wgInterfaceTerkonfigurasi mencari config yang sudah ada di /etc/wireguard,
// supaya panel memakai config milik sistem alih-alih menuntut nama wg0.
func wgInterfaceTerkonfigurasi() string {
	if aktif := wgInterfaceAktif(); aktif != "" {
		return aktif
	}
	entries, err := os.ReadDir("/etc/wireguard")
	if err != nil {
		return wgInterface
	}
	for _, e := range entries {
		if name := strings.TrimSuffix(e.Name(), ".conf"); name != e.Name() {
			return name
		}
	}
	return wgInterface
}

// wireguardDiagnose mengambil 30 baris terakhir journal untuk wg-quick agar
// pesan yang sampai ke user berisi alasan kegagalan (DNS resolve, route
// conflict, dsb), bukan cuma "Job for wg-quick@wg0.service failed because
// the control process exited with error code".
func wireguardDiagnose(iface string, orig error) string {
	unit := "wg-quick@" + iface
	if j, e := run("journalctl", "-u", unit, "-n", "30", "-o", "cat", "--no-pager"); e == nil {
		js := strings.TrimSpace(j.Stdout)
		if js != "" {
			return strings.TrimSpace(orig.Error()) + "\n" + js
		}
	}
	return strings.TrimSpace(orig.Error())
}

func wireguardStatus() helperproto.VPNStatus {
	st := helperproto.VPNStatus{Name: "wireguard", Installed: installed("wg")}
	if !st.Installed {
		st.State = "belum terpasang"
		return st
	}
	iface := wgInterfaceTerkonfigurasi()
	res, err := run("wg", "show", iface)
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		st.State = "interface " + iface + " tidak aktif"
		return st
	}
	st.Connected = true
	st.State = "terhubung (" + iface + ")"
	st.Detail = strings.TrimSpace(res.Stdout)
	return st
}

func vpnConfigure(args helperproto.VPNArgs) (helperproto.VPNStatus, error) {
	switch args.Name {
	case "tailscale":
		return tailscaleConfigure(args)
	case "cloudflared":
		return cloudflaredConfigure(args)
	case "wireguard":
		return wireguardConfigure(args)
	}
	return helperproto.VPNStatus{}, errInvalid("VPN %q tidak dikenal", args.Name)
}

func tailscaleConfigure(args helperproto.VPNArgs) (helperproto.VPNStatus, error) {
	if !installed("tailscale") {
		return helperproto.VPNStatus{}, errInvalid("Tailscale belum terpasang — install dulu lewat Components")
	}
	switch args.Action {
	case "down":
		if _, err := run("tailscale", "down"); err != nil {
			return helperproto.VPNStatus{}, err
		}
		// Mask kunci sengaja dipertahankan setelah Putus: `tailscale down` cuma
		// memutus sesi, node key tetap ada, jadi Sambung berikutnya bisa jalan
		// tanpa kunci baru. User yang memutuskan sendiri kapan menimpanya.
	case "up":
		// tailscaled harus hidup sebelum `tailscale up` — kalau daemon
		// belum jalan, `tailscale up` gagal dengan "failed to connect
		// to local tailscaled; it doesn't appear to be running" yang
		// menyesatkan (user mengira Tailscale rusak, padahal service
		// belum distart). tailscaleStatus() sudah auto-start, tapi di
		// sini dipanggil lagi supaya konfigurasi via API murni (tanpa
		// buka halaman status dulu) tetap andal.
		if _, err := run("systemctl", "is-active", "--quiet", "tailscaled"); err != nil {
			// enable --now, bukan start saja: tanpa enable, VPN yang baru
			// disambung akan putus diam-diam pada reboot berikutnya.
			if _, e := run("systemctl", "enable", "--now", "tailscaled"); e == nil {
				time.Sleep(500 * time.Millisecond)
			} else if hasNoSystemd() {
				// WSL/LXC tanpa systemd: nyalakan daemon langsung.
				//
				// Harus lepas (setsid, tanpa Wait): tailscaled adalah proses
				// yang memang tidak pernah selesai. Menjalankannya lewat run()
				// akan menggantung permintaan ini sampai timeout, dan dari
				// sisi user tombol Sambung terlihat macet total.
				if err := mulaiDaemonLepas("tailscaled",
					"--state=/var/lib/tailscale/tailscaled.state",
					"--socket=/var/run/tailscale/tailscaled.sock"); err != nil {
					return helperproto.VPNStatus{}, errInvalid(
						"tailscaled tidak bisa dijalankan di mesin tanpa systemd: %v", err)
				}
				// Socket unix-nya baru ada beberapa ratus milidetik setelah
				// proses hidup; `tailscale up` sebelum itu gagal dengan pesan
				// "it doesn't appear to be running" yang justru salah.
				tungguSocket("/var/run/tailscale/tailscaled.sock", 5*time.Second)
			}
		}
		// --timeout WAJIB ada. Tanpa itu `tailscale up` menunggu tanpa batas
		// pada dua keadaan yang sama-sama lazim — tailnet yang mewajibkan
		// Device approval, dan login interaktif tanpa auth key — dan karena
		// helper maupun klien HTTP panel tidak memasang batas waktu baca,
		// permintaan Sambung menggantung selamanya: tidak ada toast berhasil,
		// tidak ada toast gagal, dan user menyimpulkan panelnya rusak.
		cmd := []string{"up", "--timeout=" + tsTimeoutUp.String()}
		key := ""
		if strings.TrimSpace(args.AuthKey) != "" {
			key = extractTailscaleKey(args.AuthKey)
			if key == "" || !validSecret(authKeyRe, key, 256) {
				return helperproto.VPNStatus{}, errInvalid(
					"auth key tidak terbaca — tempel kuncinya saja, atau perintah lengkap " +
						"`sudo tailscale up --auth-key=<kunci>`")
			}
			// Ganti kunci di atas sesi yang masih hidup akan mendaftarkan ulang
			// mesin ini diam-diam; putuskan dulu supaya user sadar.
			if tailscaleStatus().Connected && bacaMaskTailscale() != maskAuthKey(key) {
				return helperproto.VPNStatus{}, errKode(helperproto.ErrMasihTersambung,
					"Tailscale masih tersambung — tekan Putus dulu sebelum memakai auth key lain")
			}
			cmd = append(cmd, "--authkey="+key)
		}
		if args.Host != "" {
			if !hostnameRe.MatchString(args.Host) {
				return helperproto.VPNStatus{}, errInvalid("hostname tidak valid")
			}
			cmd = append(cmd, "--hostname="+args.Host)
		}
		if _, err := run("tailscale", cmd...); err != nil {
			// Auth key sudah ditukar dan node sudah masuk daftar tailnet —
			// yang menghabiskan --timeout hanyalah menunggu admin menyetujui.
			// Itu keadaan sah, bukan kegagalan: kunci disimpan dan status
			// dikembalikan normal supaya panel bisa memberi tahu user langkah
			// berikutnya alih-alih menampilkan error tanpa jalan keluar.
			if st := tailscaleStatus(); st.NeedsApproval {
				simpanMaskTailscale(key)
				return st, nil
			}
			// "failed to connect to local tailscaled" tidak menyebut apa yang
			// harus dilakukan user, dan yang dilihat di panel cuma potongan
			// itu. Sambungkan dengan langkah nyatanya.
			if strings.Contains(err.Error(), "failed to connect to local tailscaled") {
				return helperproto.VPNStatus{}, errInvalid(
					"daemon tailscaled tidak berjalan dan panel gagal menyalakannya. " +
						"Jalankan `sudo systemctl enable --now tailscaled` di mesin ini, " +
						"lalu tekan Sambung lagi")
			}
			return helperproto.VPNStatus{}, err
		}
		simpanMaskTailscale(key)
	default:
		return helperproto.VPNStatus{}, errInvalid("aksi tidak dikenal")
	}
	return tailscaleStatus(), nil
}

func cloudflaredConfigure(args helperproto.VPNArgs) (helperproto.VPNStatus, error) {
	if !installed("cloudflared") {
		return helperproto.VPNStatus{}, errInvalid("cloudflared belum terpasang — install dulu lewat Components")
	}
	switch args.Action {
	case "down":
		if _, err := run("systemctl", "stop", "cloudflared"); err != nil {
			return helperproto.VPNStatus{}, err
		}
	case "up":
		if strings.TrimSpace(args.Token) != "" {
			token := extractCloudflaredToken(args.Token)
			if token == "" || !validSecret(tokenRe, token, 4096) {
				return helperproto.VPNStatus{}, errInvalid(
					"token tidak terbaca — tempel tokennya saja, atau perintah lengkap " +
						"`cloudflared service install <token>` / `cloudflared tunnel run --token <token>`")
			}
			cur := cloudflaredToken()
			if cur != token {
				// Token pengganti tidak boleh dipasang di atas tunnel yang masih
				// jalan: `service uninstall` akan memutus koneksi yang sedang
				// dipakai tanpa user sadar menekan apa pun soal itu.
				if _, err := run("systemctl", "is-active", "--quiet", "cloudflared"); err == nil {
					return helperproto.VPNStatus{}, errKode(helperproto.ErrMasihTersambung,
						"tunnel masih jalan — tekan Putus dulu sebelum memasang token lain")
				}
				// Bersihkan berdasarkan KEBERADAAN unit, bukan keberadaan token:
				// unit bawaan varian --token-file atau yang dipasang di luar
				// panel tetap membuat `service install` gagal kalau tidak dicopot.
				if cloudflaredServiceInstalled() {
					if _, err := run("cloudflared", "service", "uninstall"); err != nil {
						// cloudflared lawas tidak punya subcommand itu — buang
						// unitnya langsung supaya install berikutnya jalan.
						_, _ = run("systemctl", "disable", "--now", "cloudflared")
						_ = os.Remove(cloudflaredUnit)
						_, _ = run("systemctl", "daemon-reload")
					}
				}
				if _, err := run("cloudflared", "service", "install", token); err != nil {
					return helperproto.VPNStatus{}, err
				}
			}
		} else if cloudflaredToken() == "" {
			if cloudflaredServiceInstalled() {
				return helperproto.VPNStatus{}, errInvalid(
					"service cloudflared sudah terpasang di sistem tapi tokennya tidak terbaca — " +
						"isi token tunnel di kolom ini supaya panel memasang ulang service-nya")
			}
			return helperproto.VPNStatus{}, errInvalid(
				"token tunnel belum diisi — tempel token dari Cloudflare Zero Trust → Networks → Tunnels")
		}
		if _, err := run("systemctl", "enable", "--now", "cloudflared"); err != nil {
			return helperproto.VPNStatus{}, err
		}
	default:
		return helperproto.VPNStatus{}, errInvalid("aksi tidak dikenal")
	}
	return cloudflaredStatus(), nil
}

func wireguardConfigure(args helperproto.VPNArgs) (helperproto.VPNStatus, error) {
	if !installed("wg-quick") {
		return helperproto.VPNStatus{}, errInvalid("WireGuard belum terpasang — install dulu lewat Components")
	}
	iface := wgInterfaceTerkonfigurasi()
	confPath := "/etc/wireguard/" + iface + ".conf"
	switch args.Action {
	case "down":
		if _, err := run("wg-quick", "down", iface); err != nil {
			return helperproto.VPNStatus{}, err
		}
	case "up":
		if args.Config != "" {
			if !strings.Contains(args.Config, "[Interface]") {
				return helperproto.VPNStatus{}, errInvalid("config WireGuard harus punya section [Interface]")
			}
			if err := os.MkdirAll("/etc/wireguard", 0o700); err != nil {
				return helperproto.VPNStatus{}, err
			}
			// 0600: config memuat private key.
			if err := os.WriteFile(confPath, []byte(args.Config), 0o600); err != nil {
				return helperproto.VPNStatus{}, err
			}
		}
		if _, err := os.Stat(confPath); err != nil {
			return helperproto.VPNStatus{}, errInvalid("config %s belum ada", confPath)
		}
		// Kalau tidak ada `wg-quick@iface` di systemctl, tampilkan diagnosa
		// dari journal — baris "Job for wg-quick@wg0.service failed because..."
		// tanpa konteks tidak membantu. Bind interface config dulu ke iface.
		if _, err := run("wg-quick", "up", iface); err != nil {
			detail := wireguardDiagnose(iface, err)
			if detail != "" {
				return helperproto.VPNStatus{}, errInvalid("%s", detail)
			}
			return helperproto.VPNStatus{}, err
		}
	case "remove":
		// Config WireGuard memuat private key: menghapusnya berarti kehilangan
		// identitas peer ini, jadi file lama disalin ke .bak lebih dulu.
		_, _ = run("wg-quick", "down", iface)
		if b, err := os.ReadFile(confPath); err == nil {
			_ = os.WriteFile(confPath+".bak", b, 0o600)
		}
		if err := os.Remove(confPath); err != nil && !os.IsNotExist(err) {
			return helperproto.VPNStatus{}, err
		}
		// Unit boot dimatikan bersama confignya: wg-quick@<iface> yang tetap
		// enabled tanpa config akan gagal tiap boot dan mengotori status
		// systemd dengan unit failed yang tidak bisa dijelaskan user.
		_, _ = run("systemctl", "disable", "wg-quick@"+iface)
	default:
		return helperproto.VPNStatus{}, errInvalid("aksi tidak dikenal")
	}
	return wireguardStatus(), nil
}
