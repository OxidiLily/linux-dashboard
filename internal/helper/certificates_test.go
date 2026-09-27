package helper

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func buatPasanganTLSUji(t *testing.T, dir, nama string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: nama},
		DNSNames:     []string{nama},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, nama+".crt")
	keyPath := filepath.Join(dir, nama+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestUbahTLSDefaultMempertahankanSetelanLain(t *testing.T) {
	lama := "# komentar\nDASHBOARD_LISTEN=0.0.0.0:1122\nDASHBOARD_TLS_CERT=/lama.crt\n#DASHBOARD_TLS_CERT=/contoh.crt\nDASHBOARD_TLS_KEY='/lama.key'\n"
	got := ubahTLSDefault(lama, "/baru.crt", "/baru.key")
	for _, ingin := range []string{
		"# komentar",
		"DASHBOARD_LISTEN=0.0.0.0:1122",
		"#DASHBOARD_TLS_CERT=/contoh.crt",
		"DASHBOARD_TLS_CERT=\"/baru.crt\"",
		"DASHBOARD_TLS_KEY=\"/baru.key\"",
	} {
		if !strings.Contains(got, ingin) {
			t.Errorf("hasil tidak memuat %q:\n%s", ingin, got)
		}
	}
	if strings.Contains(got, "/lama") {
		t.Fatalf("nilai lama masih tertinggal:\n%s", got)
	}
}

func TestUbahTLSDefaultKosongMenonaktifkanTLS(t *testing.T) {
	got := ubahTLSDefault("A=1\nDASHBOARD_TLS_CERT=/x\nDASHBOARD_TLS_KEY=/y\n", "", "")
	if strings.Contains(got, "DASHBOARD_TLS_") {
		t.Fatalf("baris TLS aktif masih ada: %q", got)
	}
	if !strings.Contains(got, "A=1") {
		t.Fatalf("setelan lain hilang: %q", got)
	}
}

func TestSimpanCertificatesMenolakTLSKosongPadaBindPublik(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "linux-dashboard")
	lama := []byte("DASHBOARD_LISTEN=0.0.0.0:1122\nDASHBOARD_TLS_CERT=/lama.crt\nDASHBOARD_TLS_KEY=/lama.key\n")
	if err := os.WriteFile(defaultPath, lama, 0o644); err != nil {
		t.Fatal(err)
	}

	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	dashboardDefaultPath = defaultPath
	jadwalkanRestartWeb = func() error { return nil }
	t.Cleanup(func() {
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
	})

	if _, err := simpanCertificates(helperproto.CertificatesSetArgs{}, time.Now()); err == nil {
		t.Fatal("TLS boleh dinonaktifkan pada bind publik tanpa opt-in plaintext")
	}
	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lama) {
		t.Fatalf("konfigurasi berubah setelah penolakan:\n%s", got)
	}
}

func TestSimpanCertificatesBolehMenonaktifkanTLSUntukTerminasiEksternal(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "linux-dashboard")
	if err := os.WriteFile(defaultPath, []byte("DASHBOARD_LISTEN=0.0.0.0:1122\nDASHBOARD_ALLOW_PLAINTEXT=true\nDASHBOARD_SECURE_COOKIE=true\nDASHBOARD_TLS_CERT=/lama.crt\nDASHBOARD_TLS_KEY=/lama.key\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	dashboardDefaultPath = defaultPath
	jadwalkanRestartWeb = func() error { return nil }
	t.Cleanup(func() {
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
	})

	st, err := simpanCertificates(helperproto.CertificatesSetArgs{}, time.Now())
	if err != nil {
		t.Fatalf("terminasi TLS eksternal ditolak: %v", err)
	}
	if st.Active {
		t.Fatalf("status masih aktif: %+v", st)
	}
	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if nilaiEnv(string(got), "DASHBOARD_SECURE_COOKIE") != "true" {
		t.Fatalf("cookie Secure proxy tidak dipertahankan: %s", got)
	}
}

func TestUbahTLSDefaultMenonaktifkanSecureCookieUntukHTTP(t *testing.T) {
	got := ubahTLSDefault("DASHBOARD_LISTEN=0.0.0.0:1122\nDASHBOARD_ALLOW_PLAINTEXT=true\nDASHBOARD_TLS_CERT=/x\nDASHBOARD_TLS_KEY=/y\n", "", "")
	if nilaiEnv(got, "DASHBOARD_SECURE_COOKIE") != "false" {
		t.Fatalf("HTTP langsung membutuhkan cookie non-Secure: %q", got)
	}
}

func TestUbahTLSDefaultMempertahankanSecureCookieProxy(t *testing.T) {
	got := ubahTLSDefault("DASHBOARD_LISTEN=127.0.0.1:1122\nDASHBOARD_SECURE_COOKIE=true\nDASHBOARD_TLS_CERT=/x\nDASHBOARD_TLS_KEY=/y\n", "", "")
	if nilaiEnv(got, "DASHBOARD_SECURE_COOKIE") != "true" {
		t.Fatalf("cookie Secure untuk terminasi HTTPS eksternal diturunkan: %q", got)
	}
}

func TestUbahTLSDefaultMengenaliWhitespaceDiSekitarAssignment(t *testing.T) {
	lama := " DASHBOARD_TLS_CERT = /lama.crt\n	DASHBOARD_TLS_KEY	=	/lama.key\nLAIN = tetap\n"
	got := ubahTLSDefault(lama, "/baru.crt", "/baru.key")
	if strings.Contains(got, "/lama") {
		t.Fatalf("assignment TLS lama dengan whitespace masih tertinggal:\n%s", got)
	}
	if nilaiEnv(lama, "DASHBOARD_TLS_CERT") != "/lama.crt" || nilaiEnv(lama, "DASHBOARD_TLS_KEY") != "/lama.key" {
		t.Fatal("parser gagal membaca assignment dengan whitespace")
	}
	if !strings.Contains(got, "LAIN = tetap") {
		t.Fatalf("assignment lain berubah:\n%s", got)
	}
}

func TestPasangCertificatesUploadMenyimpanPasanganTerkelola(t *testing.T) {
	dir := t.TempDir()
	cert, key := buatPasanganTLSUji(t, dir, "upload.local")
	certPEM, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}

	lamaDir := certificatesManagedDir
	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	lamaAkses := pastikanDapatDibacaService
	lamaPemilik := pemilikCertificatesManaged
	certificatesManagedDir = filepath.Join(dir, "managed")
	dashboardDefaultPath = filepath.Join(dir, "linux-dashboard")
	jadwalkanRestartWeb = func() error { return nil }
	pastikanDapatDibacaService = func(_, _ string) error { return nil }
	pemilikCertificatesManaged = func() (int, int, error) { return os.Getuid(), os.Getgid(), nil }
	t.Cleanup(func() {
		certificatesManagedDir = lamaDir
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
		pastikanDapatDibacaService = lamaAkses
		pemilikCertificatesManaged = lamaPemilik
	})
	if err := os.WriteFile(dashboardDefaultPath, []byte("DASHBOARD_LISTEN=0.0.0.0:1122\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := pasangCertificatesUpload(helperproto.CertificatesUploadArgs{
		CertificatePEM: string(certPEM), PrivateKeyPEM: string(keyPEM),
	}, time.Now())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !st.Active || !st.Valid || st.Subject != "upload.local" {
		t.Fatalf("status salah: %+v", st)
	}
	if filepath.Dir(st.CertPath) != certificatesManagedDir || filepath.Dir(st.KeyPath) != certificatesManagedDir ||
		!strings.HasSuffix(st.CertPath, ".crt") || !strings.HasSuffix(st.KeyPath, ".key") {
		t.Fatalf("path managed salah: %+v", st)
	}
	if strings.TrimSuffix(filepath.Base(st.CertPath), ".crt") != strings.TrimSuffix(filepath.Base(st.KeyPath), ".key") {
		t.Fatalf("cert/key tidak memakai ID pasangan yang sama: %+v", st)
	}
	for path, mode := range map[string]os.FileMode{st.CertPath: 0o644, st.KeyPath: 0o640} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("mode %s = %o, ingin %o", path, info.Mode().Perm(), mode)
		}
	}
}

func TestBuatCertificatesSelfSignedMemuatSAN(t *testing.T) {
	dir := t.TempDir()
	lamaDir := certificatesManagedDir
	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	lamaAkses := pastikanDapatDibacaService
	lamaPemilik := pemilikCertificatesManaged
	certificatesManagedDir = filepath.Join(dir, "managed")
	dashboardDefaultPath = filepath.Join(dir, "linux-dashboard")
	jadwalkanRestartWeb = func() error { return nil }
	pastikanDapatDibacaService = func(_, _ string) error { return nil }
	pemilikCertificatesManaged = func() (int, int, error) { return os.Getuid(), os.Getgid(), nil }
	t.Cleanup(func() {
		certificatesManagedDir = lamaDir
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
		pastikanDapatDibacaService = lamaAkses
		pemilikCertificatesManaged = lamaPemilik
	})
	if err := os.WriteFile(dashboardDefaultPath, []byte("DASHBOARD_LISTEN=0.0.0.0:1122\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Second)
	st, err := buatCertificatesSelfSigned(helperproto.CertificatesSelfSignedArgs{
		CommonName: "panel.local", DNSNames: []string{"panel.local", "localhost"},
		IPAddresses: []string{"127.0.0.1", "192.0.2.10"}, Days: 30,
	}, now)
	if err != nil {
		t.Fatalf("self-signed: %v", err)
	}
	if !st.Valid || st.Subject != "panel.local" {
		t.Fatalf("status salah: %+v", st)
	}
	cert, err := validasiPasanganTLS(st.CertPath, st.KeyPath, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cert.DNSNames, []string{"panel.local", "localhost"}) {
		t.Fatalf("DNS SAN = %v", cert.DNSNames)
	}
	if got := []string{cert.IPAddresses[0].String(), cert.IPAddresses[1].String()}; !slices.Equal(got, []string{"127.0.0.1", "192.0.2.10"}) {
		t.Fatalf("IP SAN = %v", got)
	}
	if cert.NotAfter.Sub(cert.NotBefore) < 30*24*time.Hour {
		t.Fatalf("masa berlaku terlalu pendek: %s", cert.NotAfter.Sub(cert.NotBefore))
	}
}

func TestBuatCertificatesSelfSignedCommonNameIPDiverifikasi(t *testing.T) {
	dir := t.TempDir()
	lamaDir, lamaPath := certificatesManagedDir, dashboardDefaultPath
	lamaRestart, lamaAkses, lamaPemilik := jadwalkanRestartWeb, pastikanDapatDibacaService, pemilikCertificatesManaged
	certificatesManagedDir = filepath.Join(dir, "managed")
	dashboardDefaultPath = filepath.Join(dir, "linux-dashboard")
	jadwalkanRestartWeb = func() error { return nil }
	pastikanDapatDibacaService = func(_, _ string) error { return nil }
	pemilikCertificatesManaged = func() (int, int, error) { return os.Getuid(), os.Getgid(), nil }
	t.Cleanup(func() {
		certificatesManagedDir, dashboardDefaultPath = lamaDir, lamaPath
		jadwalkanRestartWeb, pastikanDapatDibacaService, pemilikCertificatesManaged = lamaRestart, lamaAkses, lamaPemilik
	})
	if err := os.WriteFile(dashboardDefaultPath, []byte("DASHBOARD_LISTEN=0.0.0.0:1122\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.0.2.10", "2001:db8::10"} {
		t.Run(ip, func(t *testing.T) {
			st, err := buatCertificatesSelfSigned(helperproto.CertificatesSelfSignedArgs{CommonName: ip, Days: 1}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			cert, err := validasiPasanganTLS(st.CertPath, st.KeyPath, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := cert.VerifyHostname(ip); err != nil {
				t.Fatalf("sertifikat tidak valid untuk IP common name %q: %v", ip, err)
			}
			if len(cert.DNSNames) != 0 || len(cert.IPAddresses) != 1 {
				t.Fatalf("SAN tidak sesuai untuk IP: DNS=%v IP=%v", cert.DNSNames, cert.IPAddresses)
			}
		})
	}
}

func TestBuatCertificatesSelfSignedMenolakInputBerbahaya(t *testing.T) {
	for _, args := range []helperproto.CertificatesSelfSignedArgs{
		{CommonName: "", Days: 30},
		{CommonName: "panel\n.local", Days: 30},
		{CommonName: "panel.local", IPAddresses: []string{"bukan-ip"}, Days: 30},
		{CommonName: "panel.local", Days: 0},
		{CommonName: "panel.local", Days: 5000},
	} {
		if _, err := buatCertificatesSelfSigned(args, time.Now()); err == nil {
			t.Fatalf("input tidak valid diterima: %+v", args)
		}
	}
}

func TestPasangCertificatesUploadMenolakPasanganTidakCocokTanpaMengubahConfig(t *testing.T) {
	dir := t.TempDir()
	certA, _ := buatPasanganTLSUji(t, dir, "a.local")
	_, keyB := buatPasanganTLSUji(t, dir, "b.local")
	certPEM, _ := os.ReadFile(certA)
	keyPEM, _ := os.ReadFile(keyB)

	lamaDir := certificatesManagedDir
	lamaPath := dashboardDefaultPath
	certificatesManagedDir = filepath.Join(dir, "managed")
	dashboardDefaultPath = filepath.Join(dir, "linux-dashboard")
	awal := []byte("DASHBOARD_TLS_CERT=/tetap.crt\nDASHBOARD_TLS_KEY=/tetap.key\n")
	if err := os.WriteFile(dashboardDefaultPath, awal, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { certificatesManagedDir, dashboardDefaultPath = lamaDir, lamaPath })

	if _, err := pasangCertificatesUpload(helperproto.CertificatesUploadArgs{CertificatePEM: string(certPEM), PrivateKeyPEM: string(keyPEM)}, time.Now()); err == nil {
		t.Fatal("pasangan upload yang tidak cocok diterima")
	}
	got, _ := os.ReadFile(dashboardDefaultPath)
	if string(got) != string(awal) {
		t.Fatalf("config berubah setelah upload ditolak: %s", got)
	}
}

func TestValidasiPasanganTLSMenolakKeyYangTidakCocok(t *testing.T) {
	dir := t.TempDir()
	certA, _ := buatPasanganTLSUji(t, dir, "a.local")
	_, keyB := buatPasanganTLSUji(t, dir, "b.local")
	if _, err := validasiPasanganTLS(certA, keyB, time.Now()); err == nil {
		t.Fatal("pasangan sertifikat dan key yang berbeda diterima")
	}
}

func TestValidasiPasanganTLSMenolakSymlink(t *testing.T) {
	dir := t.TempDir()
	cert, key := buatPasanganTLSUji(t, dir, "asli.local")
	certLink := filepath.Join(dir, "cert-link.pem")
	if err := os.Symlink(cert, certLink); err != nil {
		t.Fatal(err)
	}
	if _, err := validasiPasanganTLS(certLink, key, time.Now()); err == nil {
		t.Fatal("symlink sertifikat diterima")
	}
}

func TestSimpanCertificatesMemeriksaAksesUserServiceSebelumMenulis(t *testing.T) {
	dir := t.TempDir()
	cert, key := buatPasanganTLSUji(t, dir, "panel.local")
	defaultPath := filepath.Join(dir, "linux-dashboard")
	lama := []byte("DASHBOARD_LISTEN=127.0.0.1:1122\n")
	if err := os.WriteFile(defaultPath, lama, 0o644); err != nil {
		t.Fatal(err)
	}

	lamaPath := dashboardDefaultPath
	lamaAkses := pastikanDapatDibacaService
	dashboardDefaultPath = defaultPath
	dipanggil := 0
	pastikanDapatDibacaService = func(gotCert, gotKey string) error {
		dipanggil++
		if gotCert != cert || gotKey != key {
			t.Fatalf("path akses = %q, %q", gotCert, gotKey)
		}
		return errors.New("user service tidak dapat membaca")
	}
	t.Cleanup(func() {
		dashboardDefaultPath = lamaPath
		pastikanDapatDibacaService = lamaAkses
	})

	if _, err := simpanCertificates(helperproto.CertificatesSetArgs{CertPath: cert, KeyPath: key}, time.Now()); err == nil {
		t.Fatal("konfigurasi diterapkan walau user service tidak dapat membaca")
	}
	if dipanggil != 1 {
		t.Fatalf("pemeriksaan akses dipanggil %d kali, ingin 1", dipanggil)
	}
	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lama) {
		t.Fatalf("konfigurasi berubah setelah pemeriksaan akses gagal:\n%s", got)
	}
}

func TestSimpanCertificatesMenulisAtomicDanMenjadwalkanRestart(t *testing.T) {
	dir := t.TempDir()
	cert, key := buatPasanganTLSUji(t, dir, "panel.local")
	defaultPath := filepath.Join(dir, "linux-dashboard")
	if err := os.WriteFile(defaultPath, []byte("DASHBOARD_LISTEN=0.0.0.0:1122\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	lamaAkses := pastikanDapatDibacaService
	dashboardDefaultPath = defaultPath
	dipanggil := 0
	jadwalkanRestartWeb = func() error { dipanggil++; return nil }
	pastikanDapatDibacaService = func(_, _ string) error { return nil }
	t.Cleanup(func() {
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
		pastikanDapatDibacaService = lamaAkses
	})

	st, err := simpanCertificates(helperproto.CertificatesSetArgs{CertPath: cert, KeyPath: key}, time.Now())
	if err != nil {
		t.Fatalf("simpan: %v", err)
	}
	if !st.Active || !st.Valid || st.Subject != "panel.local" {
		t.Fatalf("status tidak benar: %+v", st)
	}
	if dipanggil != 1 {
		t.Fatalf("restart dipanggil %d kali, ingin 1", dipanggil)
	}
	b, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DASHBOARD_TLS_CERT="+strconv.Quote(cert)) || !strings.Contains(string(b), "DASHBOARD_TLS_KEY="+strconv.Quote(key)) {
		t.Fatalf("default tidak diperbarui:\n%s", b)
	}
}

func TestSimpanCertificatesRollbackJikaPenjadwalanRestartGagal(t *testing.T) {
	dir := t.TempDir()
	cert, key := buatPasanganTLSUji(t, dir, "panel.local")
	defaultPath := filepath.Join(dir, "linux-dashboard")
	lama := []byte("# tetap persis\nDASHBOARD_LISTEN=0.0.0.0:1122\n")
	if err := os.WriteFile(defaultPath, lama, 0o640); err != nil {
		t.Fatal(err)
	}

	lamaPath := dashboardDefaultPath
	lamaRestart := jadwalkanRestartWeb
	lamaAkses := pastikanDapatDibacaService
	dashboardDefaultPath = defaultPath
	jadwalkanRestartWeb = func() error { return errors.New("systemd-run gagal") }
	pastikanDapatDibacaService = func(_, _ string) error { return nil }
	t.Cleanup(func() {
		dashboardDefaultPath = lamaPath
		jadwalkanRestartWeb = lamaRestart
		pastikanDapatDibacaService = lamaAkses
	})

	if _, err := simpanCertificates(helperproto.CertificatesSetArgs{CertPath: cert, KeyPath: key}, time.Now()); err == nil {
		t.Fatal("penjadwalan restart gagal tetapi simpan dilaporkan berhasil")
	}
	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lama) {
		t.Fatalf("konfigurasi lama tidak di-rollback persis:\n%s", got)
	}
	info, err := os.Stat(defaultPath)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode konfigurasi rollback salah: info=%v err=%v", info, err)
	}
}

func TestCertificatesHelperMenolakTokenNonSudo(t *testing.T) {
	for _, cmd := range []string{
		helperproto.CmdCertificatesGet,
		helperproto.CmdCertificatesSet,
		helperproto.CmdCertificatesUpload,
		helperproto.CmdCertificatesSelfSigned,
	} {
		t.Run(cmd, func(t *testing.T) {
			h := jalankanHelperSocket(t)
			token, _ := h.srv.terbitkanToken(userUji("ani", false), sesiTTL)
			resp := h.kirim(t, map[string]any{
				"cmd":   cmd,
				"token": token,
			})
			if resp.OK || resp.Code != helperproto.ErrRequiresSudo {
				t.Fatalf("%s non-sudo = %+v, ingin %q", cmd, resp, helperproto.ErrRequiresSudo)
			}
		})
	}
}
