package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Panel bicara HTTP polos kalau tidak diberi sertifikat. Bind ke 0.0.0.0
// sebagai bawaan berarti password dan cookie sesi (termasuk sesi sudo) lewat
// apa adanya di jaringan yang sama, jadi bawaan yang aman adalah loopback dan
// instalasi yang ingin dijangkau dari perangkat lain menyalakannya sendiri.
func TestDefaultListenIsLoopback(t *testing.T) {
	t.Setenv("DASHBOARD_LISTEN", "")
	if got := Load().Listen; got != "127.0.0.1:8080" {
		t.Fatalf("default DASHBOARD_LISTEN = %q, ingin 127.0.0.1:8080", got)
	}
}

// Secret HMAC helper tidak boleh satu direktori dengan state dir web: user
// service web adalah pemilik direktori itu, dan pemilik direktori bisa
// mengganti nama/isi secret.key. Sekali helper restart, secret penyerang yang
// dipakai — dan dari situ identitas RPC bisa dipalsukan menjadi root.
func TestSecretPathIsOutsideWebStateDir(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DASHBOARD_STATE_DIR", stateDir)
	t.Setenv("DASHBOARD_SECRET", "")
	t.Setenv("DASHBOARD_SECRET_DIR", "")

	got := Load()
	if got.SecretPath != filepath.Join("/var/lib/linux-dashboard-helper", "secret.key") {
		t.Fatalf("default SecretPath = %q", got.SecretPath)
	}
	if filepath.Dir(got.SecretPath) == stateDir {
		t.Fatalf("secret helper berada di state dir web (%q)", stateDir)
	}
	if got.LegacySecretPath != filepath.Join(stateDir, "secret.key") {
		t.Fatalf("LegacySecretPath = %q", got.LegacySecretPath)
	}
}

func buatPasanganTLSUji(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestTLSParsialDitolak(t *testing.T) {
	t.Setenv("DASHBOARD_TLS_CERT", "/tidak/ada.crt")
	t.Setenv("DASHBOARD_TLS_KEY", "")
	if _, err := LoadValidated(); err == nil {
		t.Fatal("konfigurasi TLS parsial diterima")
	}
}

func TestTLSYangDikonfigurasiTetapiHilangDitolak(t *testing.T) {
	t.Setenv("DASHBOARD_TLS_CERT", "/tidak/ada.crt")
	t.Setenv("DASHBOARD_TLS_KEY", "/tidak/ada.key")
	if _, err := LoadValidated(); err == nil {
		t.Fatal("TLS hilang diterima dan berpotensi downgrade ke HTTP")
	}
}

func TestTLSNativeMemaksaSecureCookie(t *testing.T) {
	cert, key := buatPasanganTLSUji(t)
	t.Setenv("DASHBOARD_TLS_CERT", cert)
	t.Setenv("DASHBOARD_TLS_KEY", key)
	t.Setenv("DASHBOARD_SECURE_COOKIE", "false")
	got, err := LoadValidated()
	if err != nil {
		t.Fatal(err)
	}
	if !got.SecureCookie {
		t.Fatal("TLS native mengizinkan cookie tanpa Secure")
	}
}

func TestPublicPlaintextDitolakTanpaOptIn(t *testing.T) {
	t.Setenv("DASHBOARD_LISTEN", "0.0.0.0:1122")
	t.Setenv("DASHBOARD_TLS_CERT", "")
	t.Setenv("DASHBOARD_TLS_KEY", "")
	t.Setenv("DASHBOARD_ALLOW_PLAINTEXT", "")
	if _, err := LoadValidated(); err == nil {
		t.Fatal("public bind tanpa TLS diterima tanpa opt-in")
	}
}

func TestPublicPlaintextBolehDenganOptInEksplisit(t *testing.T) {
	t.Setenv("DASHBOARD_LISTEN", "0.0.0.0:1122")
	t.Setenv("DASHBOARD_TLS_CERT", "")
	t.Setenv("DASHBOARD_TLS_KEY", "")
	t.Setenv("DASHBOARD_ALLOW_PLAINTEXT", "true")
	if _, err := LoadValidated(); err != nil {
		t.Fatalf("opt-in plaintext eksplisit ditolak: %v", err)
	}
}
