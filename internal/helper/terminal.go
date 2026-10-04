package helper

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// handleTerminal men-spawn shell login user di PTY asli lalu menjembatani
// I/O-nya ke koneksi socket.
//
// Ini titik paling sensitif di seluruh sistem: daemon berjalan sebagai root
// saat mem-fork, dan menurunkan privilege lewat SysProcAttr.Credential sebelum
// exec. Semua pembatasan setelah itu murni permission Unix milik akun tersebut.
func (s *Server) handleTerminal(conn net.Conn, br *bufio.Reader, u *userInfo, req helperproto.Request) {
	defer conn.Close()

	args, err := decodeArgs[helperproto.TerminalArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	if args.Cols == 0 {
		args.Cols = 80
	}
	if args.Rows == 0 {
		args.Rows = 24
	}
	shell := u.Shell
	if shell == "" || isServiceAccount(shell) {
		fail(conn, errDenied("akun ini tidak punya shell login"))
		return
	}

	// Arahan alat & skill wajib ditulis SEBELUM shell hidup, supaya agent
	// yang langsung dieksekusi di bawah sudah membacanya di sesi ini juga —
	// bukan baru berlaku di sesi berikutnya.
	if args.Command != "" {
		// Urutannya mengikat: siapkanArahanAI membuat direktori config agent
		// (~/.claude, ~/.codex, …), dan `rtk init -g` menolak menulis kalau
		// direktori itu belum ada.
		siapkanArahanAI(u, args.Command)
		if err := siapkanPolicyAI(u, args.Command); err != nil {
			fail(conn, errInvalid("bootstrap AI Agent gagal: %s", err))
			return
		}
		siapkanToolingAgent(u, args.Command)
		// Hermes dan OpenClaw menolak mulai bekerja sebelum provider dipilih.
		// Jawabannya di panel ini selalu 9router di mesin yang sama, jadi
		// ditulis di muka — hanya kalau user belum pernah memilih sendiri.
		siapkanProvider9Router(u, args.Command)
	}

	cmd := exec.Command(shell, "-l")
	cmd.Dir = u.Home
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/.local/bin:/root/.hermes/bin",
		"HOME=" + u.Home,
		"USER=" + u.Name,
		"LOGNAME=" + u.Name,
		"SHELL=" + shell,
		"TERM=xterm-256color",
		"LANG=" + envOr("LANG", "C.UTF-8"),
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Setctty:    true,
		Credential: u.credential(),
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: args.Cols, Rows: args.Rows})
	if err != nil {
		fail(conn, &helperErr{code: helperproto.ErrInternal, msg: "gagal membuka PTY: " + err.Error()})
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_, _ = cmd.Process.Wait()
	}()

	writeResp(conn, helperproto.Response{OK: true})

	if args.Command != "" {
		// Agent dijalankan di bawah supervisor, bukan dipanggil langsung.
		//
		// Ctrl+C yang membuat agent keluar sebelumnya meninggalkan user di
		// prompt shell kosong: halaman AI Agent terlihat mati padahal sesinya
		// masih hidup, dan satu-satunya jalan kembali adalah menekan Refresh.
		// Supervisor memuat agent yang sama begitu ia keluar, dengan batas
		// crash supaya agent yang memang gagal jalan tidak diputar terus.
		//
		// Kalau skripnya gagal ditulis, agent tetap dijalankan langsung —
		// kehilangan pemuatan ulang jauh lebih ringan daripada sesi yang
		// tidak bisa dibuka sama sekali.
		// Agent yang terpasang tapi binary-nya cacat diperbaiki di sini,
		// SETELAH PTY hidup — bukan sebelum. Pemasangan ulang paket npm bisa
		// memakan puluhan detik, dan kalau dikerjakan sebelum PTY dibuka,
		// browser hanya melihat WebSocket menggantung tanpa satu pun tanda
		// kehidupan. Dengan PTY sudah jalan, user melihat pesannya lebih dulu
		// lalu menunggu di prompt yang jelas sedang mengerjakan sesuatu.
		//
		// Nama yang ditulis ke shell adalah kunci map internal (lihat
		// komponenAgenPerBinary), bukan string mentah dari API — perluPerbaikanAgent
		// menolak apa pun yang tidak ada di map itu.
		if perluPerbaikanAgent(args.Command) {
			_, _ = ptmx.Write([]byte("echo '[panel] " + args.Command +
				" terpasang tapi tidak bisa dijalankan — memperbaiki instalasinya, mohon tunggu…'\n"))
			perbaikiAgent(args.Command, u)
		}
		if jalur, err := pastikanAgentLoop(); err == nil {
			_, _ = ptmx.Write([]byte(jalur + " " + args.Command + "\n"))
		} else {
			log.Printf("terminal: supervisor agent tidak tersedia (%v), jalankan langsung", err)
			_, _ = ptmx.Write([]byte(args.Command + "\n"))
		}
	} else {
		// Banner: fastfetch adalah default di Ubuntu 24.10+ (neofetch sudah
		// dihapus dari repo), neofetch dipertahankan sebagai fallback untuk
		// user lama yang masih punya binari dari instalasi sebelumnya.
		//
		// Kalau tidak ada dua-duanya, terminal dibuka polos tanpa pesan apa
		// pun. Banner adalah pemanis, dan "belum terpasang — pasang lewat
		// Settings → Components" muncul di setiap sesi baru sampai user
		// memasangnya: itu gangguan berulang, bukan informasi.
		if _, err := exec.LookPath("fastfetch"); err == nil {
			_, _ = ptmx.Write([]byte("fastfetch\n"))
		} else if _, err := exec.LookPath("neofetch"); err == nil {
			_, _ = ptmx.Write([]byte("neofetch\n"))
		}
	}

	jembataniPTY(conn, br, ptmx)
}

// jembataniPTY menjembatani PTY dengan koneksi socket: keluaran PTY mengalir
// mentah ke klien, input klien datang berframe (TermFrameData/TermFrameResize)
// supaya resize bisa lewat kanal yang sama. Blokir sampai salah satu sisi mati.
//
// Dipakai terminal host DAN terminal container (docker exec). Dua salinan loop
// ini berarti perbaikan perilaku — batas ukuran frame, kode tutup — harus
// dibuat dua kali dan bisa berbeda pendapat.
func jembataniPTY(conn net.Conn, br *bufio.Reader, ptmx *os.File) {
	done := make(chan struct{})
	// PTY → client (raw).
	go func() {
		defer close(done)
		_, _ = io.Copy(conn, ptmx)
	}()

	// client → PTY (berframe, supaya resize bisa lewat kanal yang sama).
	go func() {
		defer conn.Close()
		header := make([]byte, 5)
		for {
			if _, err := io.ReadFull(br, header); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(header[1:])
			if n > 1<<20 {
				log.Printf("terminal: frame terlalu besar (%d byte), sesi ditutup", n)
				return
			}
			payload := make([]byte, n)
			if _, err := io.ReadFull(br, payload); err != nil {
				return
			}
			switch header[0] {
			case helperproto.TermFrameData:
				if _, err := ptmx.Write(payload); err != nil {
					return
				}
			case helperproto.TermFrameResize:
				if len(payload) == 4 {
					// Dijepit seperti ukuran awal (queryDim): ukuran datang
					// dari klien, dan 0 kolom / 65535 baris akan membuat PTY
					// (di host maupun di container) berukuran konyol.
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Cols: jepitU16(binary.BigEndian.Uint16(payload[0:2]), 20, 1000),
						Rows: jepitU16(binary.BigEndian.Uint16(payload[2:4]), 5, 500),
					})
				}
			}
		}
	}()

	<-done
}

// handleDockerTerm membuka sesi interaktif DI DALAM container: PTY di helper,
// isinya proses `docker exec -i -t`. Berbeda dari handleTerminal, akun user
// tidak dipakai sama sekali — docker berjalan sebagai root dan seluruh batas
// yang berlaku adalah batas container itu sendiri. Karena itu command ini
// wajib sudo (lihat sudoRequired).
func (s *Server) handleDockerTerm(conn net.Conn, br *bufio.Reader, req helperproto.Request) {
	defer conn.Close()

	args, err := decodeArgs[helperproto.DockerTermArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	if args.Cols == 0 {
		args.Cols = 80
	}
	if args.Rows == 0 {
		args.Rows = 24
	}
	id := strings.TrimSpace(args.ID)
	// ID diawali "-" dibaca docker sebagai flag, bukan nama container.
	if id == "" || strings.HasPrefix(id, "-") {
		fail(conn, errInvalid("id container tidak valid"))
		return
	}
	if _, ada := lookBinary("docker"); !ada {
		fail(conn, errKode(helperproto.ErrBelumTerpasang,
			"Docker belum terpasang — pasang dulu lewat Settings → Components"))
		return
	}
	// Container harus HIDUP. `docker exec` pada container berhenti memang
	// gagal, tapi menolaknya SEBELUM PTY dibuka membuat pesannya jelas —
	// bukan layar terminal kosong berisi error docker yang hilang cepat.
	if res, iErr := runIn("", nil, "docker", "inspect", "-f", "{{.State.Running}}", id); iErr != nil {
		fail(conn, errInvalid("container %q tidak bisa diperiksa: %s", id,
			strings.TrimSpace(firstNonEmpty(res.Stderr, iErr.Error()))))
		return
	} else if strings.TrimSpace(res.Stdout) != "true" {
		fail(conn, errInvalid("container %q tidak berjalan — jalankan dulu", id))
		return
	}

	// Tab-completion, riwayat, dan prompt warna hanya dimiliki shell yang
	// lengkap. `sh` di banyak image Debian adalah dash — interaktif tapi
	// tanpa completion sama sekali, jadi shell container dicari dulu:
	// bash → zsh → ash (busybox) → sh. Probe memakai satu `docker exec`
	// saja, dan hasilnya divalidasi bentuknya karena datang dari dalam
	// container.
	shell := pilihShellContainer(id)
	// ponytail: kalau ada container tanpa sh sama sekali (distroless),
	// probe gagal dan sesi menampilkan error docker apa adanya. Jalan
	// naiknya: baca `Config.Shell` image lewat `docker inspect` sebagai
	// cadangan.
	argv := []string{"exec", "-i", "-t", id, shell}
	cmd := exec.Command("docker", argv...)
	cmd.Env = []string{"PATH=" + pathExec, "LC_ALL=C", "TERM=xterm-256color"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	ptmx, pErr := pty.StartWithSize(cmd, &pty.Winsize{Cols: args.Cols, Rows: args.Rows})
	if pErr != nil {
		fail(conn, &helperErr{code: helperproto.ErrInternal, msg: "gagal membuka PTY: " + pErr.Error()})
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_, _ = cmd.Process.Wait()
	}()

	writeResp(conn, helperproto.Response{OK: true})
	jembataniPTY(conn, br, ptmx)
}

// bentukShell membatasi shell hasil probe container: hanya nama/path polos
// tanpa spasi, kutipan, atau karakter kendali — hasil probe datang dari dalam
// container dan langsung dipakai sebagai argumen exec.
// Tanda "-" di depan ditolak: hasil probe datang dari dalam container dan
// tidak boleh berbentuk seperti flag (konsisten dengan cek id container).
var bentukShell = regexp.MustCompile(`^[A-Za-z0-9_./+][A-Za-z0-9_./+-]{0,63}$`)

// pilihShellContainer mengembalikan shell paling lengkap yang tersedia DI
// DALAM container, atau "sh" kalau probe gagal. Satu `docker exec` saja per
// kali sesi dibuka.
func pilihShellContainer(id string) string {
	res, err := runIn("", nil, "docker", "exec", id, "sh", "-c",
		"command -v bash || command -v zsh || command -v ash || echo sh")
	if err != nil {
		return "sh"
	}
	hasil := strings.TrimSpace(strings.SplitN(res.Stdout, "\n", 2)[0])
	if !bentukShell.MatchString(hasil) {
		return "sh"
	}
	return hasil
}

// jepitU16 menahan nilai dalam rentang wajar; pemakaian: ukuran PTY dari
// frame resize klien (lihat jembataniPTY).
func jepitU16(v, min, max uint16) uint16 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
