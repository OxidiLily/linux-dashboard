package helper

import (
	_ "embed"
	"os"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Uninstall panel dari panel itu sendiri.
//
// Sama seperti pembaruan, skripnya dijalankan sebagai unit transient lewat
// systemd-run: langkah pertamanya menghentikan linux-dashboard-helper, dan
// proses yang jadi anak helper akan ikut mati di situ — meninggalkan sistem
// setengah ter-uninstall.
//
// Berkas kerjanya ditaruh di /run, bukan /var/lib/linux-dashboard-update:
// mode panel-data menghapus folder itu, sementara bash masih membaca skripnya
// baris demi baris dari disk.

//go:embed uninstall.sh
var uninstallScript string

const (
	uninstallUnit  = "linux-dashboard-uninstall"
	uninstallSkrip = "/run/lindash-uninstall.sh"
	uninstallLog   = "/var/log/linux-dashboard-uninstall.log"
)

func uninstallJalankan(u *userInfo, args helperproto.UninstallArgs) error {
	switch args.Mode {
	case "panel", "panel-data", "total":
	default:
		return errInvalid("mode uninstall tidak dikenal: %s", args.Mode)
	}
	if !u.Sudo {
		return errRequiresSudo()
	}
	// Password diverifikasi lewat PAM sebagai akun yang sedang login — akun
	// itu sudah pasti sudoer. Memakai "root" mati-matian tidak bisa: di
	// Ubuntu akun root umumnya terkunci sehingga tidak ada password yang
	// pernah cocok, dan uninstall jadi mustahil dijalankan.
	if strings.TrimSpace(args.Password) == "" {
		return errKode(helperproto.ErrDenied, "password wajib diisi")
	}
	if err := authenticate(u.Name, args.Password); err != nil {
		return errKode(helperproto.ErrDenied, "password salah")
	}

	if err := os.WriteFile(uninstallSkrip, []byte(uninstallScript), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(uninstallLog, []byte("[i] Memulai uninstall (mode "+args.Mode+")…\n"), 0o640); err != nil {
		return err
	}
	_, _ = run("systemctl", "reset-failed", uninstallUnit)
	_, err := run("systemd-run",
		"--unit="+uninstallUnit,
		"--description=linux-dashboard uninstall",
		"--property=StandardOutput=append:"+uninstallLog,
		"--property=StandardError=append:"+uninstallLog,
		"/bin/bash", uninstallSkrip, args.Mode,
	)
	return err
}
