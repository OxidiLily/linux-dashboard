package helper

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var dashboardDefaultPath = "/etc/default/linux-dashboard"

// Restart dijadwalkan lewat unit transient agar response HTTP sempat kembali ke
// browser sebelum linux-dashboard-web mematikan proses yang sedang melayaninya.
var jadwalkanRestartWeb = func() error {
	unit := fmt.Sprintf("linux-dashboard-web-restart-%d", time.Now().UnixNano())
	_, err := run("systemd-run", "--unit="+unit, "--collect",
		"--on-active=1s", "/bin/systemctl", "restart", "linux-dashboard-web.service")
	return err
}

// Dibuat injectable agar test tidak mensyaratkan akun service terpasang.
var pastikanDapatDibacaService = func(certPath, keyPath string) error {
	u, err := lookupUser("linux-dashboard")
	if err != nil {
		return fmt.Errorf("user service linux-dashboard tidak tersedia: %w", err)
	}
	for nama, path := range map[string]string{"sertifikat": certPath, "private key": keyPath} {
		cmd := exec.Command("/usr/bin/test", "-r", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s tidak dapat dibaca user service linux-dashboard: %w", nama, err)
		}
	}
	return nil
}

func keyAssignment(baris string) (string, bool) {
	trim := strings.TrimSpace(baris)
	if trim == "" || strings.HasPrefix(trim, "#") {
		return "", false
	}
	k, _, ok := strings.Cut(trim, "=")
	return strings.TrimSpace(k), ok
}

func nilaiEnv(isi, key string) string {
	for _, baris := range strings.Split(isi, "\n") {
		baris = strings.TrimSpace(baris)
		k, ok := keyAssignment(baris)
		if !ok || k != key {
			continue
		}
		_, v, _ := strings.Cut(baris, "=")
		v = strings.TrimSpace(v)
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
		return strings.Trim(v, "'")
	}
	return ""
}

func ubahTLSDefault(isi, certPath, keyPath string) string {
	var out []string
	for _, baris := range strings.Split(strings.TrimSuffix(isi, "\n"), "\n") {
		key, aktif := keyAssignment(baris)
		if aktif && (key == "DASHBOARD_TLS_CERT" || key == "DASHBOARD_TLS_KEY") {
			continue
		}
		out = append(out, baris)
	}
	if certPath != "" && keyPath != "" {
		out = append(out,
			"DASHBOARD_TLS_CERT="+strconv.Quote(certPath),
			"DASHBOARD_TLS_KEY="+strconv.Quote(keyPath),
		)
	}
	return strings.Join(out, "\n") + "\n"
}

func statusCertificates(now time.Time) helperproto.CertificatesStatus {
	st := helperproto.CertificatesStatus{}
	b, err := os.ReadFile(dashboardDefaultPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		st.Error = fmt.Sprintf("gagal membaca %s: %v", dashboardDefaultPath, err)
		return st
	}
	st.CertPath = nilaiEnv(string(b), "DASHBOARD_TLS_CERT")
	st.KeyPath = nilaiEnv(string(b), "DASHBOARD_TLS_KEY")
	st.Active = st.CertPath != "" && st.KeyPath != ""
	if st.CertPath == "" && st.KeyPath == "" {
		return st
	}
	if st.CertPath == "" || st.KeyPath == "" {
		st.Error = "DASHBOARD_TLS_CERT dan DASHBOARD_TLS_KEY harus diisi bersama"
		return st
	}
	cert, err := validasiPasanganTLS(st.CertPath, st.KeyPath, now)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	isiMetadataCertificate(&st, cert)
	return st
}

func validasiPasanganTLS(certPath, keyPath string, now time.Time) (*x509.Certificate, error) {
	isi := make(map[string][]byte, 2)
	for nama, path := range map[string]string{"sertifikat": certPath, "private key": keyPath} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return nil, fmt.Errorf("path %s harus absolut dan valid", nama)
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, fmt.Errorf("%s tidak dapat dibaca: %w", nama, err)
		}
		f := os.NewFile(uintptr(fd), path)
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%s tidak dapat dibaca: %w", nama, err)
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return nil, fmt.Errorf("%s bukan berkas biasa", nama)
		}
		b, err := io.ReadAll(f)
		closeErr := f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s tidak dapat dibaca: %w", nama, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("%s gagal ditutup: %w", nama, closeErr)
		}
		isi[nama] = b
	}
	pasangan, err := tls.X509KeyPair(isi["sertifikat"], isi["private key"])
	if err != nil {
		return nil, fmt.Errorf("sertifikat dan private key tidak valid atau tidak cocok: %w", err)
	}
	if len(pasangan.Certificate) == 0 {
		return nil, errors.New("sertifikat tidak memuat certificate")
	}
	cert, err := x509.ParseCertificate(pasangan.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("sertifikat tidak dapat diparse: %w", err)
	}
	if now.Before(cert.NotBefore) {
		return nil, fmt.Errorf("sertifikat belum berlaku sampai %s", cert.NotBefore.Format(time.RFC3339))
	}
	if !now.Before(cert.NotAfter) {
		return nil, fmt.Errorf("sertifikat sudah kedaluwarsa sejak %s", cert.NotAfter.Format(time.RFC3339))
	}
	return cert, nil
}

func isiMetadataCertificate(st *helperproto.CertificatesStatus, cert *x509.Certificate) {
	st.Valid = true
	st.Subject = cert.Subject.CommonName
	if st.Subject == "" {
		st.Subject = cert.Subject.String()
	}
	st.Issuer = cert.Issuer.CommonName
	if st.Issuer == "" {
		st.Issuer = cert.Issuer.String()
	}
	st.NotBefore = cert.NotBefore.Format(time.RFC3339)
	st.NotAfter = cert.NotAfter.Format(time.RFC3339)
	st.DNSNames = append([]string(nil), cert.DNSNames...)
}

func simpanCertificates(args helperproto.CertificatesSetArgs, now time.Time) (helperproto.CertificatesStatus, error) {
	args.CertPath = strings.TrimSpace(args.CertPath)
	args.KeyPath = strings.TrimSpace(args.KeyPath)
	if (args.CertPath == "") != (args.KeyPath == "") {
		return helperproto.CertificatesStatus{}, errInvalid("DASHBOARD_TLS_CERT dan DASHBOARD_TLS_KEY harus diisi bersama")
	}
	var cert *x509.Certificate
	var err error
	if args.CertPath != "" {
		cert, err = validasiPasanganTLS(args.CertPath, args.KeyPath, now)
		if err != nil {
			return helperproto.CertificatesStatus{}, errInvalid("%v", err)
		}
		if err := pastikanDapatDibacaService(args.CertPath, args.KeyPath); err != nil {
			return helperproto.CertificatesStatus{}, errInvalid("%v", err)
		}
	}

	lama, err := os.ReadFile(dashboardDefaultPath)
	lamaAda := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return helperproto.CertificatesStatus{}, err
	}
	isi := ubahTLSDefault(string(lama), args.CertPath, args.KeyPath)
	if err := tulisDefaultAtomic(dashboardDefaultPath, []byte(isi)); err != nil {
		return helperproto.CertificatesStatus{}, err
	}
	if err := jadwalkanRestartWeb(); err != nil {
		var rollbackErr error
		if lamaAda {
			rollbackErr = tulisDefaultAtomic(dashboardDefaultPath, lama)
		} else {
			rollbackErr = os.Remove(dashboardDefaultPath)
		}
		if rollbackErr != nil {
			return helperproto.CertificatesStatus{}, fmt.Errorf("restart web gagal dijadwalkan: %v; rollback konfigurasi gagal: %w", err, rollbackErr)
		}
		return helperproto.CertificatesStatus{}, fmt.Errorf("restart web gagal dijadwalkan; konfigurasi lama dipulihkan: %w", err)
	}
	st := helperproto.CertificatesStatus{CertPath: args.CertPath, KeyPath: args.KeyPath, Active: args.CertPath != ""}
	if cert != nil {
		isiMetadataCertificate(&st, cert)
	}
	return st, nil
}

func tulisDefaultAtomic(path string, isi []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".linux-dashboard-default-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(isi); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
