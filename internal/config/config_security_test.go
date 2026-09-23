package config

import (
	"path/filepath"
	"testing"
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
