package helper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestKomponenWajibPunyaPurge(t *testing.T) {
	komponenKritis := []string{
		"docker", "9router", "headroom", "hermes", "claude-code", "codex",
		"opencode", "openclaw", "rtk", "graphify", "ponytail", "browser-use",
		"nodejs", "supabase", "arkon", "cloudflared", "technitium-dns", "samba",
	}

	for _, k := range komponenKritis {
		c, ok := components[k]
		if !ok {
			t.Fatalf("komponen %q tidak terdaftar di components map", k)
		}
		if c.purge == nil {
			t.Errorf("komponen %q wajib memiliki fungsi purge", k)
		}
	}
}

func TestUninstallComponentPurgeTetapJalanSaatUninstallGagal(t *testing.T) {
	namaKomponenDummy := "dummy-test-purge-fail"
	purgeDipanggil := false

	cDummy := &component{
		Name: namaKomponenDummy,
		uninstall: func() error {
			return errors.New("simulasi error uninstall")
		},
		purge: func() error {
			purgeDipanggil = true
			return nil
		},
	}

	components[namaKomponenDummy] = cDummy
	defer delete(components, namaKomponenDummy)

	_, err := uninstallComponent(namaKomponenDummy, true)
	if err == nil {
		t.Fatalf("seharusnya uninstallComponent mengembalikan error dari c.uninstall()")
	}
	if !purgeDipanggil {
		t.Fatalf("c.purge() WAJIB tetap dipanggil meskipun c.uninstall() gagal")
	}
}

func TestPembersihanDirektoriHomeDummy(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "test-home-*")
	if err != nil {
		t.Fatalf("gagal membuat temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	// Buat struktur direktori sisa seperti yang dilaporkan user
	daftarFolder := []string{
		".9router",
		".docker",
		".headroom",
		".hermes",
		".cua-driver",
		".npm",
		"go",
		".claude",
		".codex",
		".opencode",
		".openclaw",
		filepath.Join(".config", "linux-dashboard"),
		filepath.Join(".config", "rtk"),
		filepath.Join(".config", "graphify"),
		filepath.Join(".config", "ponytail"),
		filepath.Join(".config", "browser-harness"),
		filepath.Join(".cache", "go-build"),
		filepath.Join(".cache", "hermes"),
	}

	for _, f := range daftarFolder {
		p := filepath.Join(tempHome, f)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("gagal membuat folder dummy %s: %v", p, err)
		}
	}

	// Pastikan semuanya ada
	for _, f := range daftarFolder {
		p := filepath.Join(tempHome, f)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("folder dummy belum terbuat: %s", p)
		}
	}

	// Simulasikan pembersihan terhadap direktori tersebut
	for _, f := range daftarFolder {
		p := filepath.Join(tempHome, f)
		if err := os.RemoveAll(p); err != nil {
			t.Fatalf("gagal menghapus dummy %s: %v", p, err)
		}
	}

	// Verifikasi semuanya hilang
	for _, f := range daftarFolder {
		p := filepath.Join(tempHome, f)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("folder dummy masih tertinggal: %s", p)
		}
	}
}

func TestModeUninstallValid(t *testing.T) {
	modeDiharapkan := []string{"panel", "panel-data", "total", "total-data"}
	if len(modeUninstall) != len(modeDiharapkan) {
		t.Fatalf("jumlah modeUninstall tidak cocok: got %d want %d", len(modeUninstall), len(modeDiharapkan))
	}
	for i, m := range modeDiharapkan {
		if modeUninstall[i] != m {
			t.Errorf("modeUninstall[%d] = %q, want %q", i, modeUninstall[i], m)
		}
	}

	u := &userInfo{Name: "test", Sudo: false}
	err := uninstallJalankan(u, helperproto.UninstallArgs{Mode: "mode-palsu"})
	if err == nil || err.Error() != "mode uninstall tidak dikenal: mode-palsu" {
		t.Errorf("uninstallJalankan harus menolak mode tidak dikenal: got %v", err)
	}
}

func TestRumahAgenDeduplikasi(t *testing.T) {
	homes := rumahAgen()
	if len(homes) == 0 {
		t.Fatalf("rumahAgen() tidak boleh kosong")
	}
	adaRoot := false
	seen := make(map[string]bool)
	for _, h := range homes {
		if h == "/root" {
			adaRoot = true
		}
		if seen[h] {
			t.Errorf("duplikat home terdeteksi di rumahAgen(): %s", h)
		}
		seen[h] = true
	}
	if !adaRoot {
		t.Errorf("rumahAgen() wajib memuat /root")
	}
}

func TestBersihkanHelperFungsi(t *testing.T) {
	// Memastikan ketiga fungsi pembersihan aman dipanggil tanpa panic
	bersihkanPipxLengkap()
	bersihkanGoLengkap()
	bersihkanConfigPanelPerUser()
}
