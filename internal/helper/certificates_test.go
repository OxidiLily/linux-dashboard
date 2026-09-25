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
	for _, cmd := range []string{helperproto.CmdCertificatesGet, helperproto.CmdCertificatesSet} {
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
