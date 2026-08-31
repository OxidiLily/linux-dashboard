// Package config memuat konfigurasi dari environment variable.
// Semua nilai punya default yang masuk akal untuk instalasi single-node.
package config

import (
	"log"
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
	// SocketGroup: grup yang boleh mengakses socket.
	SocketGroup string

	// DBPath: file SQLite.
	DBPath string

	// SessionTTLHours: umur session cookie.
	SessionTTLHours int
	// Secure menandai cookie Secure (aktifkan kalau di belakang HTTPS).
	SecureCookie bool
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
	c := Config{
		Listen:      env("DASHBOARD_LISTEN", "0.0.0.0:8080"),
		TLSCert:     os.Getenv("DASHBOARD_TLS_CERT"),
		TLSKey:      os.Getenv("DASHBOARD_TLS_KEY"),
		SocketPath:  env("DASHBOARD_SOCKET", filepath.Join(runDir, "helper.sock")),
		SecretPath:  env("DASHBOARD_SECRET", filepath.Join(stateDir, "secret.key")),
		SocketGroup: env("DASHBOARD_SOCKET_GROUP", "linux-dashboard"),
		DBPath:      env("DASHBOARD_DB", filepath.Join(stateDir, "lindash.db")),
		// Nilai yang tidak bisa diurai (mis. "dua belas") jatuh ke default,
		// bukan nol: session ber-TTL 0 jam akan membuat semua login langsung
		// kedaluwarsa.
		SessionTTLHours: 12,
	}
	// Sertifikat yang ditunjuk tapi tidak ada di disk diperlakukan seperti tidak
	// diset sama sekali. Unit systemd menunjuk sertifikat bawaan yang dibuat
	// installer; kalau berkasnya hilang (dihapus manual, partisi /etc dipulihkan
	// dari cadangan lama), ListenAndServeTLS akan gagal dan panel mati total
	// tiap boot — HTTP polos jauh lebih baik daripada tidak bisa login sama
	// sekali untuk memperbaikinya.
	if !berkasAda(c.TLSCert) || !berkasAda(c.TLSKey) {
		if c.TLSCert != "" || c.TLSKey != "" {
			log.Printf("sertifikat TLS tidak lengkap (cert=%q key=%q) — panel jalan tanpa TLS", c.TLSCert, c.TLSKey)
		}
		c.TLSCert, c.TLSKey = "", ""
	}
	// TLS langsung di panel berarti setiap koneksi sudah HTTPS, jadi cookie
	// tanpa flag Secure hanya melemahkan diri sendiri. Di belakang reverse
	// proxy kedua variabel ini kosong dan panel tidak bisa tahu apakah sisi
	// luar HTTPS — di situ DASHBOARD_SECURE_COOKIE harus diset manual.
	c.SecureCookie = c.TLSCert != "" && c.TLSKey != ""
	if n, err := strconv.Atoi(os.Getenv("DASHBOARD_SESSION_TTL_HOURS")); err == nil {
		c.SessionTTLHours = n
	}
	if b, err := strconv.ParseBool(os.Getenv("DASHBOARD_SECURE_COOKIE")); err == nil {
		c.SecureCookie = b
	}
	return c
}
