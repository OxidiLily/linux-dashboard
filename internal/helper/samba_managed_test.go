package helper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestNamaAkunShareDeterministikValidDanBerbeda(t *testing.T) {
	a := namaAkunShare("Dokumen", "/srv/dokumen", 0)
	b := namaAkunShare("Dokumen", "/srv/dokumen", 0)
	c := namaAkunShare("Dokumen", "/srv/lain", 0)
	if a != b {
		t.Fatalf("nama tidak deterministik: %q != %q", a, b)
	}
	if a == c {
		t.Fatalf("path berbeda bertabrakan: %q", a)
	}
	if !usernameRe.MatchString(a) || len(a) > 32 {
		t.Fatalf("username tidak valid: %q", a)
	}
}

func TestPasswordShareAcakTidakKosong(t *testing.T) {
	a, err := passwordShare()
	if err != nil {
		t.Fatal(err)
	}
	b, err := passwordShare()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 24 || a == b || strings.ContainsAny(a, "\r\n") {
		t.Fatalf("password tidak aman: len=%d sama=%v", len(a), a == b)
	}
}

func TestManifestAtomicModeRootOnlyDanTanpaPassword(t *testing.T) {
	d := t.TempDir()
	old := sambaManifestPath
	sambaManifestPath = filepath.Join(d, "managed.json")
	t.Cleanup(func() { sambaManifestPath = old })
	m := sambaManifest{Shares: map[string]sambaManagedShare{"Data": {Username: "ld-test", UID: 123, Path: "/srv/data", GECOS: "linux-dashboard samba share:Data"}}}
	if err := tulisManifestSamba(m); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(sambaManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode manifest %o, ingin 600", st.Mode().Perm())
	}
	b, _ := os.ReadFile(sambaManifestPath)
	if strings.Contains(strings.ToLower(string(b)), "password") {
		t.Fatalf("manifest menyimpan secret: %s", b)
	}
	var got sambaManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Shares["Data"].Username != "ld-test" {
		t.Fatalf("manifest salah: %+v", got)
	}
}

func TestRenderShareTerkelolaMembatasiSatuAkun(t *testing.T) {
	b := renderSambaShares([]helperproto.SambaShare{{Name: "Data", Path: "/srv/data", Writable: true, ValidUsers: []string{"ld-data"}}})
	s := string(b)
	if !strings.Contains(s, "valid users = ld-data") || !strings.Contains(s, "guest ok = no") {
		t.Fatalf("config tidak fail-closed:\n%s", s)
	}
}

func TestPathMakroUTetapLegacyManual(t *testing.T) {
	if perluAkunOtomatis(helperproto.SambaShare{Name: "Homes", Path: "/home/%U/Data"}, sambaManifest{}) {
		t.Fatal("path bermakro user tidak boleh membuat akun otomatis")
	}
}

func TestSambaRotateWajibSudoDiHelper(t *testing.T) {
	if !sudoRequired[helperproto.CmdSambaRotate] {
		t.Fatal("samba.rotate tidak dilindungi sudo di boundary helper")
	}
}
