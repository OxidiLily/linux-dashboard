package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Penyelarasan port container Docker dengan firewall.
//
// Container mempublikasikan port host (mis. 8090 dari `0.0.0.0:8090->8090/tcp`),
// tapi port itu tidak pernah masuk /etc/ufw/user.rules: `docker` membukanya
// lewat iptables-nya sendiri di chain DOCKER, dan ufw -- yang menaruh aturan
// masuknya di chain terpisah -- tidak tahu apa-apa soal itu. Selama firewall
// belum pernah dinyalakan, keduanya tidak bertabrakan dan semuanya tampak
// normal. Begitu ufw dinyalakan, port container tetap terbuka (DOCKER_USER
// dievaluasi lebih dulu untuk trafik yang datang lewat docker-proxy), tapi
// halaman Firewall tidak menampilkan satu pun dari port itu.
//
// Reconciler ini membuat keadaan docker terlihat dan ikut dijaga ufw:
//
//  1. Container yang SEDANG berjalan → setiap port host yang dipublikasikannya
//     didaftarkan sebagai `allow <port>/<proto>` (Anywhere, sama seperti port
//     komponen).
//  2. Container yang berhenti/keluar → rule yang dibuat langkah 1 dicabut lagi,
//     supaya port layanan yang sudah mati tidak tertinggal terbuka.
//
// Dua hal yang menjaga ini tidak merusak apa pun:
//
//   - Hanya rule yang BENAR-BENAR dibuat reconciler yang boleh dicabut
//     (entriStatePortDocker.Dibuat). Rule yang sudah ada SEBELUM reconciler
//     melihatnya tidak pernah diklaim, jadi tidak pernah dihapus — termasuk
//     rule komponen, akses admin, dan rule yang ditulis user sendiri lewat
//     Settings → Firewall.
//   - Kalau docker tidak bisa dijawab, reconciler berhenti tanpa mengubah apa
//     pun. `docker ps` yang gagal bukan bukti tidak ada container yang jalan;
//     memperlakukannya sebagai "tidak ada port" akan mencabut seluruh rule dan
//     menutup semua layanan container sampai daemonnya hidup lagi.

const (
	// versiStatePortDocker menandai bentuk berkas catatan. Berkas dari versi
	// yang tidak dikenal tetap dibaca sebagai kosong, bukan dianggap rusak.
	versiStatePortDocker = 1
	// jedaPortDocker adalah jarak penyelarasan berkala. Container juga
	// dinyalakan dari terminal dan oleh `restart: always` sesudah reboot —
	// jalur yang tidak pernah lewat panel, jadi tidak ada tombol yang bisa
	// memicu pemeriksaan.
	jedaPortDocker = 30 * time.Second
	// batchInspectDocker adalah jumlah container per satu panggilan
	// `docker inspect`. Satu container satu proses berarti ratusan proses per
	// putaran di mesin yang punya banyak container.
	batchInspectDocker = 20
)

// pathStatePortDocker menyimpan port mana yang dibuka reconciler. Variabel,
// bukan konstanta: test menunjuknya ke direktori sementara supaya tidak
// menyentuh catatan mesin sungguhan.
var pathStatePortDocker = "/var/lib/linux-dashboard/docker-ports.json"

// formatInspectDocker mengambil tepat field yang dibutuhkan reconciler.
//
// Sengaja bukan `docker inspect <id>` polos: keluaran itu memuat Config.Env,
// yaitu seluruh environment container — termasuk token dan password yang
// sengaja tidak pernah panel tampilkan. Yang tidak pernah dibaca tidak bisa
// bocor ke log.
//
// Setiap field dicetak sebagai JSON sehingga tidak ada pemisah yang bisa
// muncul di dalam nilai. Health dijaga `if` karena container tanpa healthcheck
// tidak punya key itu sama sekali, dan `json` atas key yang tidak ada membuat
// docker gagal mencetak baris apa pun untuk container tersebut.
const formatInspectDocker = `{{json .State.Status}}{{"\t"}}{{if .State.Health}}{{json .State.Health.Status}}{{else}}null{{end}}{{"\t"}}{{json .NetworkSettings.Ports}}{{"\t"}}{{json .HostConfig.NetworkMode}}{{"\t"}}{{json .Name}}`

// portTerbit adalah satu port host yang dipublikasikan container berjalan.
type portTerbit struct {
	Port      string
	Proto     string
	Container string
}

// bindingPortDocker adalah satu ikatan host→container dari NetworkSettings.
type bindingPortDocker struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// entriStatePortDocker adalah satu rule yang sedang dipantau reconciler.
type entriStatePortDocker struct {
	Port      string `json:"port"`
	Proto     string `json:"proto"`
	Container string `json:"container,omitempty"`
	// Dibuat menandai rule ini dibuat PANEL. Hanya rule dengan Dibuat=true
	// yang boleh dicabut kembali.
	Dibuat bool `json:"dibuat"`
	// Diabaikan menandai user menghapus rule ini sendiri lewat Settings →
	// Firewall. Selama container masih memakai portnya rule itu tidak dibuat
	// ulang: menghapusnya adalah keputusan user, bukan kesalahan yang perlu
	// diperbaiki otomatis.
	Diabaikan bool `json:"diabaikan,omitempty"`
}

type statePortDocker struct {
	Version int                   `json:"version"`
	Rules   []entriStatePortDocker `json:"rules"`
}

var (
	muPortDocker sync.Mutex
	// sinyalPortDocker membangunkan pengawas supaya penyelarasan berjalan
	// sekarang, bukan menunggu tick berikutnya.
	sinyalPortDocker = make(chan struct{}, 1)
)

// kunciPortDocker adalah identitas satu rule di catatan: port host + protokol.
// Sumber (From) tidak ikut jadi kunci karena rule reconciler selalu Anywhere.
func kunciPortDocker(port, proto string) string { return port + "/" + proto }

// aturanDocker menyusun rule allow Anywhere untuk satu port container, lewat
// jalur yang sama dengan port komponen supaya bentuk yang ditulis dan bentuk
// yang dicabut tidak pernah berbeda.
func aturanDocker(port, proto string) helperproto.UfwRule {
	return aturanPort(portKomponen{Port: port, Proto: proto}, "")
}

// picuSinkronPortDocker meminta penyelarasan secepatnya. Dipakai sesudah aksi
// yang mengubah keadaan container dari panel (start/stop/restart/rm, compose
// up/down) supaya rule-nya menyusul tanpa menunggu jeda berkala.
//
// Non-blocking: permintaan dari jalur request tidak boleh menunggu `docker
// inspect` selesai. Satu sinyal yang tertunda sudah cukup — pengawasnya akan
// menyelaraskan seluruh keadaan, bukan hanya container yang barusan disentuh.
func picuSinkronPortDocker() {
	select {
	case sinyalPortDocker <- struct{}{}:
	default:
	}
}

// pengawasPortDocker menyelaraskan port container dengan ufw secara berkala.
func pengawasPortDocker() {
	t := time.NewTicker(jedaPortDocker)
	defer t.Stop()
	var galatTerakhir string
	for {
		if err := sinkronkanPortDocker(); err != nil {
			// Kegagalan di sini berulang setiap jeda (docker mati, socket
			// belum ada), jadi hanya perubahan pesannya yang dicatat. Log yang
			// menuliskan kalimat yang sama ribuan kali sehari berhenti dibaca
			// orang, dan yang penting justru saat keadaannya berubah.
			if p := err.Error(); p != galatTerakhir {
				log.Printf("firewall: sinkronisasi port container dilewati: %v", err)
				galatTerakhir = p
			}
		} else if galatTerakhir != "" {
			log.Printf("firewall: sinkronisasi port container berjalan kembali")
			galatTerakhir = ""
		}
		select {
		case <-t.C:
		case <-sinyalPortDocker:
		}
	}
}

// errDockerTidakLengkap menandai pembacaan daftar container yang tidak bisa
// dipastikan lengkap: sebagian batch inspect gagal. Dibedakan dari kegagalan
// biasa karena akibatnya berbeda — lihat sinkronPortDockerTerkunci.
var errDockerTidakLengkap = errors.New("daftar container tidak lengkap")

// portDockerBerjalan mengembalikan seluruh publikasi port host dari container
// yang SEDANG berjalan. Container yang berhenti tidak pernah muncul di sini —
// itulah yang membuat rule-nya dicabut pada putaran berikutnya.
//
// Kegagalan yang dikembalikan bersama daftar yang tidak lengkap dibedakan dari
// kegagalan total: yang pertama masih bisa dipakai untuk menambah izin, yang
// kedua tidak bisa dipakai untuk apa pun.
func portDockerBerjalan() ([]portTerbit, error) {
	res, err := run("docker", "ps", "-q")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	ids := strings.Fields(res.Stdout)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []portTerbit
	var galatTerakhir error
	terbaca := 0
	for i := 0; i < len(ids); i += batchInspectDocker {
		j := min(i+batchInspectDocker, len(ids))
		args := append([]string{"inspect", "--format", formatInspectDocker}, ids[i:j]...)
		res, err := run("docker", args...)
		if err != nil {
			// Satu container yang hilang di tengah jalan (dihapus user, atau
			// container sementara yang sudah selesai) membuat docker keluar
			// dengan status bukan nol, tapi container lain tetap tercetak.
			// Keluarannya dipakai; errornya menentukan apakah hasilnya boleh
			// dipercaya sebagai daftar yang lengkap.
			galatTerakhir = err
		}
		for _, baris := range strings.Split(res.Stdout, "\n") {
			port, ok := portDariBarisInspect(baris)
			if !ok {
				continue
			}
			terbaca++
			out = append(out, port...)
		}
	}
	if galatTerakhir == nil {
		return out, nil
	}
	if terbaca == 0 {
		return nil, fmt.Errorf("docker inspect: %w", galatTerakhir)
	}
	return out, errDockerTidakLengkap
}

// portDariBarisInspect membaca satu baris keluaran formatInspectDocker.
// Kembalian ok=false berarti barisnya tidak bisa dibaca sama sekali — bukan
// sekadar "container ini tidak mempublikasikan port".
func portDariBarisInspect(baris string) ([]portTerbit, bool) {
	baris = strings.TrimRight(baris, "\r")
	if strings.TrimSpace(baris) == "" {
		return nil, false
	}
	f := strings.Split(baris, "	")
	if len(f) < 5 {
		return nil, false
	}
	var status, jaringan, nama string
	if json.Unmarshal([]byte(f[0]), &status) != nil {
		return nil, false
	}
	if json.Unmarshal([]byte(f[3]), &jaringan) != nil {
		return nil, false
	}
	if json.Unmarshal([]byte(f[4]), &nama) != nil {
		return nil, false
	}
	nama = strings.TrimPrefix(nama, "/")
	switch status {
	case "exited", "dead", "created", "removing":
		// Keadaan yang jelas tidak jalan: port host-nya sudah lepas.
		return nil, true
	}
	// Selain itu container dianggap hidup — termasuk `paused` dan `restarting`,
	// yang di `docker ps` masih tampil sebagai "Up". Default ke arah "hidup"
	// itu disengaja: keadaan baru yang belum dikenal docker lebih baik
	// menyisakan rule yang tidak berguna daripada mencabut izin layanan yang
	// sedang jalan.
	// Network host tidak mempublikasikan port lewat docker sama sekali:
	// prosesnya memakai port host apa adanya, jadi tidak ada binding yang bisa
	// dibaca dari sini (NetworkSettings.Ports juga kosong pada mode itu).
	if jaringan == "host" {
		return nil, true
	}
	var ports map[string][]bindingPortDocker
	if err := json.Unmarshal([]byte(f[2]), &ports); err != nil {
		return nil, true
	}
	var out []portTerbit
	for kunci, ikatan := range ports {
		portContainer, proto, ada := strings.Cut(kunci, "/")
		if !ada || !portRe.MatchString(portContainer) {
			continue
		}
		proto = strings.ToLower(proto)
		if proto != "tcp" && proto != "udp" {
			// sctp dan protokol lain tidak dikenal aturanDocker/ufwArgs.
			continue
		}
		for _, b := range ikatan {
			if !portRe.MatchString(b.HostPort) || !hostTerbukaDariLuar(b.HostIP) {
				continue
			}
			out = append(out, portTerbit{Port: b.HostPort, Proto: proto, Container: nama})
			// Satu ikatan sudah cukup: rule ufw-nya untuk port host, dan
			// container yang sama bisa mengikat port itu di 0.0.0.0 sekaligus
			// di :: (v4 + v6).
			break
		}
	}
	return out, true
}

// hostTerbukaDariLuar menandai binding yang benar-benar bisa dihubungi dari
// luar mesin.
//
// Port yang hanya terikat ke loopback tidak pernah butuh rule ufw: paketnya
// tidak masuk lewat interface jaringan. Menuliskan rule untuknya hanya
// menambah baris di halaman Firewall yang tidak menjaga apa pun.
func hostTerbukaDariLuar(ip string) bool {
	ip = strings.TrimSpace(ip)
	switch ip {
	case "", "0.0.0.0", "::", "[::]":
		// HostIp kosong diperlakukan docker sebagai 0.0.0.0.
		return true
	}
	if strings.HasPrefix(ip, "127.") || ip == "::1" || ip == "localhost" {
		return false
	}
	return true
}

// portDiklaimLain mengembalikan port yang sudah punya pemilik di luar
// reconciler: port komponen yang dideklarasikan dengan `denganPort`, dan port
// akses admin (SSH + panel).
//
// Tanpa daftar ini urutan pemasangan menentukan siapa yang berkuasa: container
// Supabase yang lebih dulu jalan akan mengklaim 8000/5432/6543 sebelum
// komponennya sempat mendaftar, dan begitu stack-nya berhenti reconciler
// mencabut izin port yang sebenarnya dideklarasikan komponen.
func portDiklaimLain() map[string]bool {
	out := map[string]bool{}
	for _, c := range components {
		for _, p := range c.ports {
			out[kunciPortDocker(p.Port, p.Proto)] = true
		}
	}
	for _, p := range portAksesAdmin() {
		out[kunciPortDocker(p.Port, p.Proto)] = true
	}
	return out
}

// sinkronkanPortDocker menyelaraskan rule ufw dengan container yang berjalan.
//
// Aman dipanggil dari beberapa tempat sekaligus: satu mutex menjaga agar dua
// penyelarasan tidak pernah berjalan bersamaan. Dua penyelarasan yang
// bersamaan bisa sama-sama memutuskan sebuah rule "belum ada", lalu keduanya
// menambahkannya dan catatannya jadi tidak konsisten dengan kenyataan.
func sinkronkanPortDocker() error {
	muPortDocker.Lock()
	defer muPortDocker.Unlock()
	return sinkronPortDockerTerkunci()
}

func sinkronPortDockerTerkunci() error {
	if _, ada := lookBinary("ufw"); !ada {
		// Belum ada tempat mendaftar. Saat ufw dipasang nanti, putaran
		// berikutnya menemukan container yang sudah jalan dan mendaftarkannya.
		return nil
	}
	if _, ada := lookBinary("docker"); !ada {
		return nil
	}

	ingin, err := portDockerBerjalan()
	tidakLengkap := errors.Is(err, errDockerTidakLengkap)
	if err != nil && !tidakLengkap {
		return err
	}

	st, err := ufwStatus()
	if err != nil {
		return err
	}
	adaRule := map[string]bool{}
	for _, r := range st.Rules {
		if r.Port == "" {
			continue
		}
		switch r.Proto {
		case "tcp", "udp":
			adaRule[kunciPortDocker(r.Port, r.Proto)] = true
		default:
			// `ufw allow 8080` tanpa proto berlaku untuk tcp DAN udp. Aturan
			// seperti itu sudah menutupi keduanya, jadi keduanya dianggap
			// sudah ada — dan tidak boleh ditimpa rule reconciler.
			adaRule[kunciPortDocker(r.Port, "tcp")] = true
			adaRule[kunciPortDocker(r.Port, "udp")] = true
		}
	}

	lama := bacaStatePortDocker()
	lamaPeta := map[string]entriStatePortDocker{}
	for _, e := range lama.Rules {
		if e.Port == "" || e.Proto == "" {
			continue
		}
		lamaPeta[kunciPortDocker(e.Port, e.Proto)] = e
	}

	inginPeta := map[string]portTerbit{}
	for _, p := range ingin {
		inginPeta[kunciPortDocker(p.Port, p.Proto)] = p
	}

	klaim := portDiklaimLain()
	simpan := make([]entriStatePortDocker, 0, len(lama.Rules)+len(inginPeta))
	ubah := false

	for k, p := range inginPeta {
		if e, sudah := lamaPeta[k]; sudah {
			if e.Diabaikan {
				// User pernah menghapus rule ini sendiri. Dihormati.
				simpan = append(simpan, e)
				continue
			}
			if e.Dibuat && !adaRule[k] {
				// Rule milik panel hilang di luar panel — `ufw reset`, mesin
				// dipulihkan dari cadangan, atau rule dihapus dari terminal.
				// Container-nya masih jalan, jadi izinnya dipasang lagi.
				if err := ufwAdd(aturanDocker(p.Port, p.Proto)); err != nil {
					log.Printf("firewall: gagal memasang ulang izin %s untuk container %s: %v",
						k, p.Container, err)
				} else {
					log.Printf("firewall: izin %s dipasang ulang untuk container %s", k, p.Container)
				}
			}
			simpan = append(simpan, e)
			continue
		}
		if klaim[k] {
			// Port ini sudah dideklarasikan komponen (atau dipakai akses
			// admin). Bukan urusan reconciler.
			continue
		}
		if adaRule[k] {
			// Sudah dibuka pihak lain — user sendiri, komponen lain, atau
			// panel versi sebelumnya. Dicatat tanpa klaim supaya tidak pernah
			// ikut dicabut.
			simpan = append(simpan, entriStatePortDocker{Port: p.Port, Proto: p.Proto, Container: p.Container})
			ubah = true
			continue
		}
		if err := ufwAdd(aturanDocker(p.Port, p.Proto)); err != nil {
			// Kegagalan mendaftar tidak membatalkan apa pun: container-nya
			// tetap jalan, dan putaran berikutnya mencoba lagi.
			log.Printf("firewall: gagal mengizinkan %s untuk container %s: %v", k, p.Container, err)
			continue
		}
		log.Printf("firewall: izin %s dibuka untuk container %s", k, p.Container)
		simpan = append(simpan, entriStatePortDocker{
			Port: p.Port, Proto: p.Proto, Container: p.Container, Dibuat: true,
		})
		ubah = true
	}

	for k, e := range lamaPeta {
		if _, masih := inginPeta[k]; masih {
			continue
		}
		if tidakLengkap {
			// Daftar container-nya tidak bisa dipastikan lengkap: container
			// yang gagal dibaca tidak terlihat di sini, dan mencabut izinnya
			// berarti menutup port layanan yang mungkin masih jalan.
			// Pencabutan ditunda ke putaran berikutnya, catatannya
			// dipertahankan.
			simpan = append(simpan, e)
			continue
		}
		ubah = true
		if !e.Dibuat || !adaRule[k] {
			// Bukan rule kita, atau sudah tidak ada. Catatannya cukup dibuang.
			continue
		}
		if err := ufwHapusRule(aturanDocker(e.Port, e.Proto)); err != nil {
			log.Printf("firewall: gagal mencabut izin %s (container %s berhenti): %v", k, e.Container, err)
			// Tetap dicatat supaya putaran berikutnya mencoba lagi.
			simpan = append(simpan, e)
			continue
		}
		log.Printf("firewall: izin %s dicabut — container %s tidak lagi memakainya", k, e.Container)
	}

	if !ubah && !tidakLengkap {
		return nil
	}
	if ubah {
		if err := tulisStatePortDocker(statePortDocker{Rules: simpan}); err != nil {
			return fmt.Errorf("catatan port docker: %w", err)
		}
	}
	if tidakLengkap {
		// Dilaporkan sesudah pekerjaan yang aman selesai: penyebabnya berulang
		// setiap jeda, dan pengawas hanya mencatat perubahan pesannya.
		return fmt.Errorf("%w — pencabutan izin dilewati pada putaran ini", errDockerTidakLengkap)
	}
	return nil
}

// tandaiPortDockerDihapus mencatat bahwa satu rule dihapus lewat
// Settings → Firewall.
//
// Tanpa catatan ini reconciler memasangnya lagi pada putaran berikutnya, dan
// panel yang mengembalikan rule yang baru saja user hapus adalah persis jenis
// kejutan yang membuat orang berhenti mempercayai otomatisasi. Penghapusan
// dianggap keputusan user sampai container itu berhenti memakai portnya.
func tandaiPortDockerDihapus(port, proto string) {
	if port == "" {
		return
	}
	muPortDocker.Lock()
	defer muPortDocker.Unlock()

	st := bacaStatePortDocker()
	ubah := false
	for i := range st.Rules {
		if st.Rules[i].Port != port || !protoCocokDengan(st.Rules[i].Proto, proto) {
			continue
		}
		if !st.Rules[i].Diabaikan || st.Rules[i].Dibuat {
			st.Rules[i].Diabaikan = true
			// Kepemilikan dilepas sekalian: rule-nya sudah tidak ada, dan
			// selama masih ditandai milik panel putaran berikutnya berhak
			// mencabutnya lagi — padahal yang terpasang setelah ini adalah
			// rule user.
			st.Rules[i].Dibuat = false
			ubah = true
		}
	}
	if ubah {
		if err := tulisStatePortDocker(st); err != nil {
			log.Printf("firewall: gagal mencatat penghapusan rule %s/%s: %v", port, proto, err)
		}
	}
}

// protoCocokDengan mencocokkan proto yang tercatat dengan proto rule yang
// dihapus. Rule ufw tanpa proto (`ufw allow 8080`) berlaku untuk tcp dan udp
// sekaligus, jadi keduanya dianggap cocok.
func protoCocokDengan(tercatat, rule string) bool {
	if rule == "" || rule == "any" {
		return tercatat == "tcp" || tercatat == "udp"
	}
	return tercatat == rule
}

// cabutSemuaPortDocker mencabut setiap rule yang dibuat reconciler lalu
// mengosongkan catatannya. Dipakai saat komponen docker dicopot: tanpa ini
// port container yang container-nya sudah tidak ada lagi tetap terbuka di
// firewall.
func cabutSemuaPortDocker() {
	muPortDocker.Lock()
	defer muPortDocker.Unlock()

	st := bacaStatePortDocker()
	if _, ada := lookBinary("ufw"); ada {
		for _, e := range st.Rules {
			if !e.Dibuat {
				continue
			}
			if err := ufwHapusRule(aturanDocker(e.Port, e.Proto)); err != nil {
				log.Printf("firewall: gagal mencabut %s/%s saat docker dicopot: %v", e.Port, e.Proto, err)
			}
		}
	}
	if err := tulisStatePortDocker(statePortDocker{}); err != nil {
		log.Printf("firewall: gagal mengosongkan catatan port docker: %v", err)
	}
}

// bacaStatePortDocker membaca catatan rule milik reconciler. Berkas yang hilang
// atau rusak diperlakukan sebagai catatan kosong — akibatnya paling buruk rule
// yang sudah ada dianggap milik pihak lain dan tidak pernah dicabut, bukan
// rule milik user yang terhapus.
func bacaStatePortDocker() statePortDocker {
	st := statePortDocker{Version: versiStatePortDocker}
	b, err := os.ReadFile(pathStatePortDocker)
	if err != nil {
		return st
	}
	var dibaca statePortDocker
	if err := json.Unmarshal(b, &dibaca); err != nil {
		log.Printf("firewall: catatan port docker %s tidak terbaca (%v) — dimulai dari kosong",
			pathStatePortDocker, err)
		return st
	}
	if dibaca.Version == 0 {
		dibaca.Version = versiStatePortDocker
	}
	return dibaca
}

// tulisStatePortDocker menulis catatan lewat berkas sementara + rename, supaya
// helper yang mati di tengah penulisan tidak meninggalkan JSON setengah jadi
// yang membuat seluruh rule kehilangan pemiliknya.
func tulisStatePortDocker(st statePortDocker) error {
	st.Version = versiStatePortDocker
	if st.Rules == nil {
		st.Rules = []entriStatePortDocker{}
	}
	sort.Slice(st.Rules, func(i, j int) bool {
		a, b := st.Rules[i], st.Rules[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		return a.Container < b.Container
	})
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(pathStatePortDocker), 0o750); err != nil {
		return err
	}
	sementara := pathStatePortDocker + ".baru"
	if err := os.WriteFile(sementara, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(sementara, pathStatePortDocker); err != nil {
		_ = os.Remove(sementara)
		return err
	}
	return nil
}
