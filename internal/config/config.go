// Package config memuat konfigurasi dari environment variable.
// Semua nilai punya default yang masuk akal untuk instalasi single-node.
package config

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

func berkasAda(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

type Config struct {
	// Listen adalah alamat bind web app, mis. "127.0.0.1:8080".
	Listen string
	// TLSCert/TLSKey opsional — kosongkan kalau pakai reverse proxy.
	TLSCert string
	TLSKey  string

	// SocketPath: Unix socket helper daemon.
	SocketPath string
	// SecretPath: file berisi HMAC secret (permission 0600, owner root,
	// group web app supaya bisa dibaca).
	SecretPath string
	// LegacySecretPath: lokasi secret versi lama (di dalam state dir web).
	// Dibaca hanya kalau SecretPath belum ada, supaya pembaruan panel tidak
	// memutus hubungan web dengan helper di mesin yang belum dipindahkan.
	LegacySecretPath string
	// SocketGroup: grup yang boleh mengakses socket.
	SocketGroup string

	// DBPath: file SQLite.
	DBPath string

	// SessionTTLHours: umur session cookie.
	SessionTTLHours int
	// Secure menandai cookie Secure (aktifkan kalau di belakang HTTPS).
	SecureCookie bool
	// AllowPlaintext mengizinkan bind non-loopback tanpa TLS hanya jika operator
	// memilihnya secara eksplisit (misalnya terminasi TLS ada di luar proses).
	AllowPlaintext bool
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() Config {
	runDir := env("DASHBOARD_RUN_DIR", "/run/linux-dashboard")
	stateDir := env("DASHBOARD_STATE_DIR", "/var/lib/linux-dashboard")
	// Direktori secret helper SENGAJA dipisah dari state dir web. Kalau
	// keduanya satu direktori, user service web (pemilik state dir) bisa
	// mengganti nama berkas secret lalu mengisi miliknya sendiri, dan helper
	// memakai secret penyerang begitu ia restart — jalur itu berujung pada
	// pemalsuan identitas RPC menjadi root. Direktori terpisah ini milik root
	// dan grup web hanya boleh membacanya.
	secretDir := env("DASHBOARD_SECRET_DIR", "/var/lib/linux-dashboard-helper")
	c := Config{
		// Bind ke loopback secara bawaan. Panel ini bicara HTTP polos kalau
		// tidak diberi sertifikat, dan HTTP polos di 0.0.0.0 berarti password
		// serta cookie sesi (termasuk sesi sudo) lewat begitu saja di jaringan
		// yang sama. Instalasi yang memang ingin dijangkau dari perangkat lain
		// menyalakannya sendiri di /etc/default/linux-dashboard (unit systemd
		// bawaan sudah memakai DASHBOARD_LISTEN=0.0.0.0:1122).
		Listen:      env("DASHBOARD_LISTEN", "127.0.0.1:8080"),
		TLSCert:     os.Getenv("DASHBOARD_TLS_CERT"),
		TLSKey:      os.Getenv("DASHBOARD_TLS_KEY"),
		SocketPath:  env("DASHBOARD_SOCKET", filepath.Join(runDir, "helper.sock")),
		SecretPath:  env("DASHBOARD_SECRET", filepath.Join(secretDir, "secret.key")),
		SocketGroup: env("DASHBOARD_SOCKET_GROUP", "linux-dashboard"),
		DBPath:      env("DASHBOARD_DB", filepath.Join(stateDir, "lindash.db")),
		// Jalur lama (satu direktori dengan state web) tetap dibaca supaya
		// instalasi yang belum dipindahkan tidak mati setelah pembaruan.
		// Helper memperingatkan saat ini terjadi.
		LegacySecretPath: filepath.Join(stateDir, "secret.key"),

		// Nilai yang tidak bisa diurai (mis. "dua belas") jatuh ke default,
		// bukan nol: session ber-TTL 0 jam akan membuat semua login langsung
		// kedaluwarsa.
		SessionTTLHours: 12,
	}
	// Load tetap murni dan kompatibel untuk pemanggil/test lama; validasi yang
	// dapat menggagalkan startup dilakukan LoadValidated.
	c.SecureCookie = c.TLSCert != "" && c.TLSKey != ""
	if n, err := strconv.Atoi(os.Getenv("DASHBOARD_SESSION_TTL_HOURS")); err == nil {
		c.SessionTTLHours = n
	}
	if b, err := strconv.ParseBool(os.Getenv("DASHBOARD_SECURE_COOKIE")); err == nil {
		c.SecureCookie = b
	}
	if b, err := strconv.ParseBool(os.Getenv("DASHBOARD_ALLOW_PLAINTEXT")); err == nil {
		c.AllowPlaintext = b
	}
	// Pada TLS native, cookie tanpa Secure tidak pernah benar. Env false tidak
	// boleh menurunkan perlindungan transport yang sudah aktif.
	if c.TLSCert != "" && c.TLSKey != "" {
		c.SecureCookie = true
	}
	return c
}

// LoadValidated memuat konfigurasi startup dan menolak downgrade diam-diam
// dari HTTPS ke HTTP. Kesalahan sertifikat harus mematikan service agar password
// Linux, OTP, dan cookie sesi tidak pernah terkirim plaintext tanpa disadari.
func LoadValidated() (Config, error) {
	c := Load()
	punyaCert, punyaKey := c.TLSCert != "", c.TLSKey != ""
	if punyaCert != punyaKey {
		return Config{}, fmt.Errorf("DASHBOARD_TLS_CERT dan DASHBOARD_TLS_KEY harus diisi bersama")
	}
	if punyaCert {
		if !berkasAda(c.TLSCert) || !berkasAda(c.TLSKey) {
			return Config{}, fmt.Errorf("sertifikat atau private key TLS tidak ada/bukan berkas regular")
		}
		if _, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey); err != nil {
			return Config{}, fmt.Errorf("pasangan sertifikat TLS tidak valid: %w", err)
		}
		c.SecureCookie = true
		return c, nil
	}
	if !alamatLoopback(c.Listen) && !c.AllowPlaintext {
		return Config{}, fmt.Errorf("bind publik %s wajib memakai TLS; set DASHBOARD_ALLOW_PLAINTEXT=true hanya jika terminasi TLS ditangani di luar proses", c.Listen)
	}
	return c, nil
}

func alamatLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
