package helper

import (
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"strings"

	"github.com/msteinert/pam/v2"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// pamService adalah nama file di /etc/pam.d/ yang dipakai untuk autentikasi.
// Installer menulis file ini (lihat deploy/pam.d/linux-dashboard).
const pamService = "linux-dashboard"

// Autentikasi dijalankan di helper daemon (root), bukan di web app.
// Bukan sekadar demi kerapian: membaca /etc/shadow memang butuh root, jadi
// proses web non-root tidak akan pernah bisa memvalidasi password sendiri.

func authenticate(username, password string) error {
	t, err := pam.StartFunc(pamService, username, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff, pam.PromptEchoOn:
			return password, nil
		case pam.ErrorMsg, pam.TextInfo:
			return "", nil
		}
		return "", errors.New("style PAM tidak didukung")
	})
	if err != nil {
		return err
	}
	defer t.End()

	if err := t.Authenticate(0); err != nil {
		return err
	}
	// AcctMgmt menegakkan akun tidak expired/locked.
	return t.AcctMgmt(0)
}

// autentikasi adalah SEAM untuk test, bukan titik konfigurasi: verifikasi
// kredensial lewat PAM hanya bisa dijalankan sebagai root dengan akun nyata di
// mesin, jadi tanpa seam ini jalur login (termasuk aturan umur token yang
// ditentukan web app) tidak bisa diuji sama sekali. Nilainya selalu
// authenticate di produksi.
var autentikasi = authenticate

func passwordMustChange(username string) bool {
	out, err := exec.Command("chage", "-l", username).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "password must be changed") {
			return true
		}
		if strings.HasPrefix(l, "password expires") && !strings.Contains(l, "never") {
			return true
		}
	}
	return false
}

// isServiceAccount menolak akun sistem yang shell-nya nologin/false.
func isServiceAccount(shell string) bool {
	switch {
	case shell == "":
		return true
	case strings.HasSuffix(shell, "/nologin"):
		return true
	case strings.HasSuffix(shell, "/false"):
		return true
	case strings.HasSuffix(shell, "/sync"):
		return true
	}
	return false
}

func (s *Server) handleLogin(conn net.Conn, req helperproto.Request) {
	args, err := decodeArgs[helperproto.LoginArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	if args.Username == "" || args.Password == "" {
		fail(conn, errInvalid("username dan password wajib diisi"))
		return
	}
	// Identitas dibaca lewat seam yang sama dengan pemeriksaan sudo ulang
	// (lihat identitasSekarang di token.go): di produksi keduanya membaca
	// keadaan akun yang sama, dan di test keduanya bisa dipasang tanpa akun
	// nyata di mesin.
	u, err := identitasSekarang(args.Username)
	if err != nil {
		// Pesan sengaja sama dengan password salah supaya tidak membocorkan
		// user mana yang ada di sistem.
		fail(conn, errDenied("username atau password salah"))
		return
	}
	if isServiceAccount(u.Shell) {
		fail(conn, errDenied("akun service tidak boleh login"))
		return
	}
	if err := autentikasi(args.Username, args.Password); err != nil {
		fail(conn, errDenied("username atau password salah"))
		return
	}
	// Autentikasi berhasil → terbitkan token capability. Ini satu-satunya
	// tempat token dibuat, dan satu-satunya bukti identitas yang dipakai
	// helper pada permintaan-permintaan berikutnya.
	// Umur token ditentukan setelan operator yang dikirim web app
	// (DASHBOARD_SESSION_TTL_HOURS), bukan angka tetap di helper.
	token, _ := s.terbitkanTokenLogin(u, args)
	if token == "" {
		// crypto/rand gagal: ini kegagalan infrastruktur, bukan kredensial
		// salah. Kode `internal` membuat web app melaporkannya sebagai
		// masalah layanan (bukan menyuruh user mengetik ulang password yang
		// sebenarnya benar).
		fail(conn, &helperErr{code: helperproto.ErrInternal, msg: "gagal menerbitkan token sesi"})
		return
	}
	res := helperproto.LoginResult{
		UID: u.UID, GID: u.GID, Home: u.Home, Shell: u.Shell,
		Sudo: u.Sudo, Groups: u.Names, Token: token,
		MustChangePassword: passwordMustChange(args.Username),
	}
	data, _ := json.Marshal(res)
	writeResp(conn, helperproto.Response{OK: true, Data: data})
}

func (s *Server) changePassword(u *userInfo, args helperproto.PasswdArgs) error {
	if args.NewPassword == "" {
		return errInvalid("password baru wajib diisi")
	}
	target := args.Target
	if target == "" || target == u.Name {
		// Ganti password sendiri — wajib konfirmasi password lama.
		if args.OldPassword == "" {
			return errInvalid("password lama wajib diisi")
		}
		if err := autentikasi(u.Name, args.OldPassword); err != nil {
			return errDenied("password lama salah")
		}
		return chauthtok(u.Name, args.NewPassword)
	}
	// Reset password user lain — hanya root/sudo.
	if !u.Sudo {
		return errRequiresSudo()
	}
	if _, err := lookupUser(target); err != nil {
		return errInvalid("user %q tidak ditemukan", target)
	}
	return chauthtok(target, args.NewPassword)
}

// chauthtok mengganti password lewat PAM, bukan menulis /etc/shadow langsung,
// supaya kebijakan password & algoritma hash sistem tetap dipatuhi.
func chauthtok(username, newPassword string) error {
	asked := 0
	t, err := pam.StartFunc(pamService, username, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff:
			// pam_unix menanyakan password baru dua kali (isi + konfirmasi).
			asked++
			return newPassword, nil
		case pam.PromptEchoOn, pam.ErrorMsg, pam.TextInfo:
			return "", nil
		}
		return "", errors.New("style PAM tidak didukung")
	})
	if err != nil {
		return err
	}
	defer t.End()
	// Flag 0 = tanpa PAM_CHANGE_EXPIRED_AUTHTOK; sebagai root, PAM tidak
	// meminta password lama.
	if err := t.ChangeAuthTok(0); err != nil {
		return &helperErr{code: helperproto.ErrDenied, msg: "ganti password ditolak: " + err.Error()}
	}
	return nil
}
