package helper

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"github.com/creack/pty"
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
			perbaikiAgent(args.Command)
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
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Cols: binary.BigEndian.Uint16(payload[0:2]),
						Rows: binary.BigEndian.Uint16(payload[2:4]),
					})
				}
			}
		}
	}()

	<-done
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
