package helper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAmbilVersiURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/npm":
			_, _ = w.Write([]byte(`{"name":"openclaw","version":"2026.9.6"}`))
		case "/text":
			_, _ = w.Write([]byte("2.1.283"))
		case "/error":
			http.Error(w, "2.1.999", http.StatusBadGateway)
		}
	}))
	defer server.Close()
	for _, tc := range []struct{ path, want string }{
		{"/npm", "2026.9.6"}, {"/text", "2.1.283"}, {"/error", ""},
	} {
		if got := ambilVersiURL(server.URL + tc.path); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestBandingVersiAgen(t *testing.T) {
	for _, tc := range []struct {
		old, next string
		want      bool
	}{
		{"1.9.0", "1.10.0", true}, {"2.1.10", "2.1.9", false},
		{"2.1.9", "2.1.9", false}, {"2.1.9-beta.1", "2.1.10", false},
		{"2.1.9", "2.1.10-beta.1", false}, {"n/a", "2.0.0", false},
	} {
		if got := bandingVersi(tc.old, tc.next); got != tc.want {
			t.Errorf("%s -> %s: %v, want %v", tc.old, tc.next, got, tc.want)
		}
	}
}

func TestAgenMilikUserMenolakSymlinkKeluarHome(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local/bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(home, ".hermes/hermes-agent/.hermes/bin/hermes")
	if err := os.MkdirAll(filepath.Dir(owned), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owned, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf 'Hermes Agent v0.21.5\\nInstall method: git\\n';;\nupdate) printf 'Update available: 5 commits behind\\n';;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, "shim")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec "+owned+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "hermes")
	if err := os.Symlink(shim, link); err != nil {
		t.Fatal(err)
	}
	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	if _, ok := agenMilikUser("hermes", u); !ok {
		t.Fatal("binary milik user tidak dikenali")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", link); err != nil {
		t.Fatal(err)
	}
	if _, ok := agenMilikUser("hermes", u); ok {
		t.Fatal("symlink keluar HOME diterima")
	}
	if _, err := updateAgen("nginx", u); err == nil {
		t.Fatal("komponen lain boleh update sebagai agent")
	}
}

func TestUpdateAgenSudahTerbaru(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local/bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(home, ".hermes/hermes-agent/.hermes/bin/hermes")
	if err := os.MkdirAll(filepath.Dir(owned), 0700); err != nil {
		t.Fatal(err)
	}
	// Tanpa baris 'Update available:' atau commit behind — kondisi up to date
	if err := os.WriteFile(owned, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf 'Hermes Agent v0.21.5\\nInstall method: git\\n';;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(bin, "hermes")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec "+owned+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}

	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	res, err := updateAgen("hermes", u)
	if err != nil {
		t.Fatalf("updateAgen gagal: %v", err)
	}
	if res.Updated {
		t.Fatalf("seharusnya tidak ada update, tapi updated=true")
	}
	if res.Message != "Sudah di versi yang terbaru" {
		t.Fatalf("pesan tidak sesuai: %q", res.Message)
	}
}

func TestUpdateAgenAdaUpdate(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local/bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(home, ".hermes/hermes-agent/.hermes/bin/hermes")
	if err := os.MkdirAll(filepath.Dir(owned), 0700); err != nil {
		t.Fatal(err)
	}
	// Ada notice 'Update available: 5 commits behind'
	script := `#!/bin/sh
case "$1" in
--version)
  if [ -f "` + home + `/updated" ]; then
    printf 'Hermes Agent v0.21.6\nInstall method: git\n'
  else
    printf 'Hermes Agent v0.21.5\nInstall method: git\nUpdate available: 5 commits behind\n'
  fi
  ;;
update)
  touch "` + home + `/updated"
  printf 'Successfully updated\n'
  ;;
esac
`
	if err := os.WriteFile(owned, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(bin, "hermes")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec "+owned+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}

	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	res, err := updateAgen("hermes", u)
	if err != nil {
		t.Fatalf("updateAgen gagal: %v", err)
	}
	t.Logf("res: %+v", res)
	if !res.Updated {
		t.Fatalf("seharusnya updated=true, tapi updated=false")
	}
}
