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

// modeUninstall adalah mode yang diterima helper, berurutan dari yang paling
// ringan. Dipisah jadi daftar (bukan switch inline) supaya UI bisa diuji
// terhadapnya: mode yang ditawarkan modal tapi ditolak di sini = permintaan
// yang gagal dengan pesan "mode uninstall tidak dikenal" setelah user mengetik
// password.
var modeUninstall = []string{"panel", "panel-data", "total", "total-data"}

func uninstallJalankan(u *userInfo, args helperproto.UninstallArgs) error {
	dikenal := false
	for _, m := range modeUninstall {
		if args.Mode == m {
			dikenal = true
			break
		}
	}
	if !dikenal {
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

// Reboot mesin dari panel. Jadwalkan lewat timer transient satu detik, bukan
// `systemctl reboot` langsung: begitu job reboot masuk antrean, systemd mulai
// menghentikan unit — termasuk linux-dashboard-web — dan jawaban HTTP untuk
// klik ini bisa mati sebelum sampai ke browser. Satu detik cukup untuk
// membalas "ok" sehingga browser tahu reboot memang dimulai, bukan gagal.
func rebootJalankan() error {
	_, _ = run("systemctl", "reset-failed", "linux-dashboard-reboot")
	_, err := run("systemd-run",
		"--unit=linux-dashboard-reboot",
		"--description=linux-dashboard reboot",
		"--on-active=1",
		"systemctl", "reboot",
	)
	return err
}
