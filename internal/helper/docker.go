package helper

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Subcommand docker yang boleh dipanggil dari panel. Whitelist, bukan
// blacklist — `docker run` sengaja tidak ada karena setara akses root penuh
// lewat bind-mount ke filesystem host.
var allowedDockerSub = map[string]bool{
	"ps": true, "images": true, "inspect": true, "logs": true, "stats": true,
	"start": true, "stop": true, "restart": true, "version": true, "info": true,
	// `rm` dipakai tombol Hapus container di UI; `-f` ditambahkan API, bukan user.
	"rm": true,
	// volume/network/image/system/builder dipakai halaman Docker untuk
	// mengelola sumber daya selain container. Semuanya punya sub-subcommand
	// sendiri yang dicek terpisah di checkDayaArgs — `docker volume` saja
	// tidak berarti apa-apa.
	"volume":  true,
	"network": true,
	"image":   true,
	"system":  true,
	"builder": true,
	"compose": true,
}

// Sub-subcommand yang boleh per sumber daya. Whitelist dengan alasan yang sama
// seperti daftar di atas: `docker image save` dan `docker image load` misalnya
// bisa membaca dan menulis berkas sembarang di host lewat argumennya.
//
// Dipisah per sumber daya, bukan satu daftar bersama, karena dua penghuni
// terakhir memang tidak boleh menerima apa pun selain satu perintah:
//
//   - `system` HANYA df, yaitu laporan pemakaian disk. `system prune` sengaja
//     tidak ada: ia membuang container berhenti, network, cache build, dan
//     — dengan --volumes — seluruh volume yang tidak terpakai dalam SATU
//     perintah. Cakupan sebesar itu tidak bisa dijelaskan dengan jujur di satu
//     dialog konfirmasi, dan tombol per sumber daya sudah menutupi semuanya
//     dengan kalimat yang benar untuk masing-masing.
//
//   - `builder` HANYA prune. Cache build adalah satu-satunya sumber daya
//     docker yang isinya murni hasil turunan: menghapusnya tidak pernah
//     menghilangkan data, paling mahal membuat build berikutnya mulai dari nol.
var allowedDayaSub = map[string]map[string]bool{
	"volume":  {"ls": true, "inspect": true, "rm": true, "prune": true},
	"network": {"ls": true, "inspect": true, "rm": true, "prune": true},
	"image":   {"ls": true, "inspect": true, "rm": true, "prune": true},
	"system":  {"df": true},
	"builder": {"prune": true},
}

// Flag yang boleh menyertai sub-subcommand di atas. Semuanya disusun API
// sendiri, tidak satu pun berasal dari input user — daftar ini adalah jaring
// pengaman kalau suatu saat ada yang lupa.
var flagDayaAman = map[string]bool{
	"-f": true, "--force": true, "--format": true, "--filter": true,
	"-a": true, "--all": true, "-q": true, "--quiet": true,
}

var allowedComposeSub = map[string]bool{
	"up": true, "down": true, "restart": true, "ps": true, "logs": true,
	"pull": true, "stop": true, "start": true, "config": true,
	// `ls` dipakai untuk menemukan stack yang sudah jalan tapi belum terdaftar.
	// Tanpa ini helper menolaknya dan daftar stack luar diam-diam kosong.
	"ls": true,
}

// Kuota `docker logs -f` di helper: garis pertahanan di tempat proses itu
// benar-benar berjalan (root), untuk kalau lapisan web kehilangan hitungannya
// sendiri. Angka sengaja lebih longgar dari batas web (kuotaLogMaks = 4).
// var, bukan konstanta: test memakai angka kecil supaya tidak perlu
// menahan delapan stream sekaligus.
var logHelperMaks = 8

var (
	muLogHelper    sync.Mutex
	logHelperAktif int
)

func ambilSlotLogHelper() bool {
	muLogHelper.Lock()
	defer muLogHelper.Unlock()
	if logHelperAktif >= logHelperMaks {
		return false
	}
	logHelperAktif++
	return true
}

func lepasSlotLogHelper() {
	muLogHelper.Lock()
	if logHelperAktif > 0 {
		logHelperAktif--
	}
	muLogHelper.Unlock()
}

// maxTailLog membatasi baris log yang boleh diminta sekali stream. Sama
// dengan batas di layer API, supaya klien yang bandel tidak menarik seluruh
// riwayat log container besar ke RAM helper.
const maxTailLog = 2000

// handleDockerLogs menstream `docker logs -f --tail N <id>` ke klien:
// response OK dulu (tanda stream siap), lalu byte mentah mengalir sampai
// proses docker berhenti atau klien menutup koneksi.
//
// Berbeda dari dockerExec yang menunggu proses selesai, log harus mengalir
// selama container hidup — karenanya koneksi diambil alih anak proses docker,
// dan penutupan klien dibaca dari arah baca koneksi untuk mematikannya lagi.
func (s *Server) handleDockerLogs(conn net.Conn, req helperproto.Request) {
	defer conn.Close()
	args, err := decodeArgs[helperproto.DockerLogsArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	id := strings.TrimSpace(args.ID)
	// ID diawali "-" akan dibaca docker sebagai flag, bukan nama container —
	// pemeriksaan yang sama dengan checkDayaArgs untuk sumber daya lain.
	if id == "" || strings.HasPrefix(id, "-") {
		fail(conn, errInvalid("id container tidak valid"))
		return
	}
	tail := args.Tail
	if tail < 1 {
		tail = 1
	}
	if tail > maxTailLog {
		tail = maxTailLog
	}
	if _, ada := lookBinary("docker"); !ada {
		fail(conn, errKode(helperproto.ErrBelumTerpasang,
			"Docker belum terpasang — pasang dulu lewat Settings → Components"))
		return
	}
	// Container yang tidak dikenal ditolak SEBELUM response OK: sesudah OK,
	// kegagalan hanya bisa ditandai dengan koneksi yang ditutup, sehingga
	// pesan docker ("No such container") hilang tanpa jejak di layar.
	if res, iErr := runIn("", nil, "docker", "inspect", "-f", "{{.State.Status}}", id); iErr != nil {
		fail(conn, errInvalid("container %q tidak dikenal: %s", id,
			strings.TrimSpace(firstNonEmpty(res.Stderr, iErr.Error()))))
		return
	}
	// Slot diambil SEBELUM proses docker dijalankan dan dilepas saat handler
	// selesai — stream `docker logs -f` hidup sampai klien menutup.
	if !ambilSlotLogHelper() {
		fail(conn, errKode(helperproto.ErrAksiBerjalan,
			"Terlalu banyak stream log container — tutup salah satu dulu"))
		return
	}
	defer lepasSlotLogHelper()

	writeResp(conn, helperproto.Response{OK: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", strconv.Itoa(tail), id)
	cmd.Env = []string{"PATH=" + pathExec, "LC_ALL=C"}
	// Log aplikasi ditulis docker ke stdout DAN stderr — keduanya bagian dari
	// log container, jadi keduanya diteruskan apa adanya.
	cmd.Stdout = conn
	cmd.Stderr = conn
	// Stream ini satu arah; satu-satunya cara melihat klien pergi adalah
	// membaca dari koneksi. EOF → batalkan konteks → docker logs ikut mati,
	// supaya tidak ada proses docker tertinggal tiap kali modal ditutup.
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		cancel()
	}()
	// Kegagalan setelah OK (container dihapus di tengah jalan, dsb.) hanya
	// menutup koneksi; klien menampilkan isi terakhir yang sudah terkirim.
	_ = cmd.Run()
}

func dockerExec(args helperproto.DockerExecArgs) (helperproto.ExecResult, error) {
	if len(args.Args) == 0 {
		return helperproto.ExecResult{}, errInvalid("argumen docker kosong")
	}
	sub := args.Args[0]
	if !allowedDockerSub[sub] {
		return helperproto.ExecResult{}, errInvalid("subcommand docker %q tidak diizinkan", sub)
	}
	switch sub {
	case "compose":
		if err := checkComposeArgs(args.Args[1:]); err != nil {
			return helperproto.ExecResult{}, err
		}
	case "volume", "network", "image", "system", "builder":
		if err := checkDayaArgs(sub, args.Args[1:]); err != nil {
			return helperproto.ExecResult{}, err
		}
	}
	if args.Dir != "" && !strings.HasPrefix(args.Dir, "/") {
		return helperproto.ExecResult{}, errInvalid("dir harus absolut")
	}
	// Docker belum terpasang dijawab dengan kode yang bisa dikenali UI,
	// bukan dibiarkan jatuh ke exec dan mengembalikan pesan Go mentah
	// `exec: "docker": executable file not found in $PATH` — kalimat yang
	// menyebut $PATH kepada user yang cuma membuka sebuah halaman panel.
	if _, ada := lookBinary("docker"); !ada {
		return helperproto.ExecResult{}, errKode(helperproto.ErrBelumTerpasang,
			"Docker belum terpasang — pasang dulu lewat Settings → Components")
	}
	mailcowTarget := filepath.Clean(args.Dir) == filepath.Clean(mailcowDir)
	if canonical, err := filepath.EvalSymlinks(args.Dir); err == nil && canonical == filepath.Clean(mailcowDir) {
		mailcowTarget = true
	}
	for i, arg := range args.Args[1:] {
		if arg == mailcowProject || strings.HasPrefix(filepath.Clean(arg), filepath.Clean(mailcowDir)+"/") {
			mailcowTarget = true
		}
		if sub == "compose" && (arg == "-f" || arg == "--file" || arg == "--env-file") && i+2 < len(args.Args) {
			path := args.Args[i+2]
			if !filepath.IsAbs(path) {
				path = filepath.Join(args.Dir, path)
			}
			if canonical, err := filepath.EvalSymlinks(path); err == nil && strings.HasPrefix(canonical, filepath.Clean(mailcowDir)+"/") {
				mailcowTarget = true
			}
		}
	}
	// Tombol panel start/restart mengirim ID hex, bukan nama — kepemilikan
	// dicek lewat label compose project docker, bukan tebakan nama/ID, supaya
	// instalasi mailcow manual (unmanaged) tidak memblokir start/restart
	// container lain (DoS) dan mailcow tetap tidak bisa dinyalakan saat
	// konfigurasi pending.
	if !mailcowTarget && (sub == "start" || sub == "restart") && containerMilikMailcow(args.Args[1:]) {
		mailcowTarget = true
	}
	// volume/network/image rm|prune bisa menyentuh sumber daya mailcow
	// (nama volume mailcowdockerized_*, image bersama) tanpa argumen yang
	// menyebut mailcow — ikut kunci lifecycle yang sama dengan install/purge.
	dayaRusak := (sub == "volume" || sub == "network" || sub == "image") &&
		len(args.Args) > 1 && (args.Args[1] == "rm" || args.Args[1] == "prune")
	if mailcowTarget || dayaRusak || dockerUbahKeadaan(sub, args.Args[1:]) {
		// Unrelated Compose reads need no Mailcow lock. Keep mutation safety:
		// Docker IDs and Compose project names can target existing Mailcow services.
		mailcowLifecycle.Lock()
		defer mailcowLifecycle.Unlock()
		if mailcowTarget {
			if _, e := os.Lstat(filepath.Join(mailcowDir, ".linux-dashboard-config-pending")); e == nil {
				return helperproto.ExecResult{}, errInvalid("konfigurasi mailcow pending; lanjutkan installer Components")
			} else if !os.IsNotExist(e) {
				return helperproto.ExecResult{}, e
			}
		}
		if mailcowTarget && (sub == "start" || sub == "restart") {
			if _, err := os.Lstat(mailcowDir); err == nil {
				if err := mailcowManaged(); err != nil {
					return helperproto.ExecResult{}, err
				}
			} else if !os.IsNotExist(err) {
				return helperproto.ExecResult{}, err
			}
		}
		if mailcowTarget && sub == "compose" {
			if err := mailcowManaged(); err != nil {
				return helperproto.ExecResult{}, err
			}
			operation, err := composeSub(args.Args[1:])
			if err != nil {
				return helperproto.ExecResult{}, err
			}
			for i, arg := range args.Args[1:] {
				if arg == operation {
					res, err := mailcowCompose(args.Args[i+1:]...)
					if err == nil && dockerUbahKeadaan(sub, args.Args[1:]) {
						picuSinkronPortDocker()
					}
					return res, err
				}
			}
		}
	}
	res, err := runIn(args.Dir, nil, "docker", args.Args...)
	if err != nil {
		return res, err
	}
	// Aksi yang mengubah keadaan container mengubah pula daftar port host yang
	// perlu diizinkan firewall. Reconciler dibangunkan supaya izinnya menyusul
	// segera, bukan menunggu putaran berkala. Asinkron: balasan ke panel tidak
	// boleh menunggu `docker inspect` selesai.
	if dockerUbahKeadaan(sub, args.Args[1:]) {
		picuSinkronPortDocker()
	}
	return res, nil
}

// containerMilikMailcow mengecek kepemilikan container lewat label compose
// project `docker inspect`: tombol panel memakai ID hex, jadi pencocokan
// nama/ID tidak bisa membedakan milik siapa. Inspect yang gagal dianggap
// bukan mailcow — docker sendiri akan menolak target yang tidak dikenal,
// jadi tidak ada state yang berubah.
func containerMilikMailcow(rest []string) bool {
	for _, a := range rest {
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		res, err := runIn("", nil, "docker", "inspect",
			"--format", `{{index .Config.Labels "com.docker.compose.project"}}`, a)
		if err == nil && strings.TrimSpace(res.Stdout) == mailcowProject {
			return true
		}
	}
	return false
}

// dockerUbahKeadaan menandai subcommand docker yang bisa membuat daftar port
// host berubah — dinyalakan, dimatikan, di-restart, atau dihapus.
//
// Daftarnya sengaja pendek: `ps`, `logs`, `inspect`, `stats`, dan `compose ps`
// dibaca berkali-kali saat halaman Docker terbuka, dan membangunkan reconciler
// untuk setiap bacaan berarti `docker inspect` yang tidak ada gunanya.
func dockerUbahKeadaan(sub string, rest []string) bool {
	switch sub {
	case "start", "stop", "restart", "rm":
		return true
	case "compose":
		s, err := composeSub(rest)
		if err != nil {
			return false
		}
		switch s {
		case "up", "down", "start", "stop", "restart":
			return true
		}
	}
	return false
}

// checkDayaArgs memvalidasi `docker <volume|network|image|system|builder> <sub> ...`.
//
// Pemeriksaan kedua — nama/ID tidak boleh diawali "-" — bukan formalitas:
// nama volume bernama `--help` akan dibaca docker sebagai flag, sehingga
// `docker volume rm --help` keluar dengan status 0 tanpa menghapus apa pun
// dan panel melaporkan penghapusan yang tidak pernah terjadi. Yang lebih
// buruk, `--filter` atau `-a` yang menyelinap ke `prune` mengubah cakupan
// penghapusan jauh melampaui yang dikonfirmasi user.
func checkDayaArgs(daya string, rest []string) error {
	if len(rest) == 0 {
		return errInvalid("subcommand %s tidak ditemukan", daya)
	}
	if !allowedDayaSub[daya][rest[0]] {
		return errInvalid("subcommand %s %q tidak diizinkan", daya, rest[0])
	}
	for _, a := range rest[1:] {
		if strings.HasPrefix(a, "-") && !flagDayaAman[a] {
			return errInvalid("opsi %q tidak diizinkan untuk docker %s", a, daya)
		}
	}
	return nil
}

// composeSub mengambil subcommand dari daftar argumen compose, melewati opsi
// yang membawa nilai. Kembalian "" berarti tidak ada subcommand sama sekali.
//
// Dipakai dua tempat: validasi izin (`checkComposeArgs`) dan penentuan apakah
// aksinya mengubah keadaan container (`dockerUbahKeadaan`). Dipisah supaya
// keduanya tidak pernah berbeda pendapat soal di mana subcommand-nya berada.
func composeSub(rest []string) (string, error) {
	// Bentuk yang dipakai panel: compose -f <path> [--env-file <path>] <sub> ...
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "-f" || a == "--file" || a == "--env-file" || a == "-p" || a == "--project-name":
			if i+1 >= len(rest) {
				return "", errInvalid("opsi %s butuh nilai", a)
			}
			i++
		case strings.HasPrefix(a, "-"):
			// Only separated panel globals are supported; unknown/combined options
			// can redirect paths or hide the real operation from the Mailcow guard.
			return "", errInvalid("opsi global compose %q tidak diizinkan", a)
		default:
			return a, nil
		}
	}
	return "", nil
}

func checkComposeArgs(rest []string) error {
	sub, err := composeSub(rest)
	if err != nil {
		return err
	}
	if sub == "" {
		return errInvalid("subcommand compose tidak ditemukan")
	}
	if !allowedComposeSub[sub] {
		return errInvalid("subcommand compose %q tidak diizinkan", sub)
	}
	return nil
}
