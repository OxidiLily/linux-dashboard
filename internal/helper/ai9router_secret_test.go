package helper

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Opt-in integration: synthetic key, isolated HOME, no gateway/provider requests.
func TestOpenClawActualFileRef(t *testing.T) {
	cli := os.Getenv("OPENCLAW_TEST_BINARY")
	if cli == "" {
		t.Skip("set OPENCLAW_TEST_BINARY for installed CLI")
	}
	u := &userInfo{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	key := "synthetic-integration-only"
	err := konfigurasiOpenClaw9Router(u, key, func(_ *userInfo, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), key) {
			t.Fatal("secret in argv")
		}
		cmd := exec.Command(cli, args...)
		cmd.Env = []string{"HOME=" + u.Home, "PATH=" + os.Getenv("PATH"), "OPENCLAW_STATE_DIR=" + filepath.Join(u.Home, ".openclaw"), "OPENCLAW_CONFIG_PATH=" + filepath.Join(u.Home, ".openclaw", "openclaw.json")}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.New(string(out))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(u.Home, ".openclaw", "openclaw.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), key) {
		t.Fatal("plaintext config")
	}
	if !sudahPunyaProviderOpenClaw(u, p, key) {
		t.Fatal("configured reference not recognized")
	}
}

func TestOpenClawSecretNeverInArgv(t *testing.T) {
	u := &userInfo{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	key := "synthetic-regression-key"
	var calls [][]string
	err := konfigurasiOpenClaw9Router(u, key, func(_ *userInfo, _ string, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		if strings.Contains(strings.Join(args, " "), key) {
			t.Fatal("secret in argv")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || !strings.Contains(strings.Join(calls[0], " "), "--secret-input-mode ref") {
		t.Fatalf("unexpected calls: %v", calls)
	}
	path := filepath.Join(u.Home, ".openclaw", "linux-dashboard-9router", ".env")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != key {
		t.Fatal("missing secret file", err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe mode", err)
	}
	var ref map[string]string
	if err := json.Unmarshal([]byte(calls[2][3]), &ref); err != nil || ref["source"] != "file" || ref["id"] != "value" {
		t.Fatal("not a file SecretRef")
	}
}

func TestOpenClawUnsafeSecretFileRejected(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "public"} {
		t.Run(kind, func(t *testing.T) {
			u := &userInfo{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
			dir := filepath.Join(u.Home, ".openclaw", "linux-dashboard-9router")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(u.Home, "unrelated")
			if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".env")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "public":
				err = os.WriteFile(path, []byte("preserve"), 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = konfigurasiOpenClaw9Router(u, "synthetic-key", func(_ *userInfo, _ string, _ ...string) error { calls++; return nil })
			if err == nil || calls != 2 {
				t.Fatal("unsafe file accepted")
			}
			b, err := os.ReadFile(target)
			if err != nil || string(b) != "preserve" {
				t.Fatal("unrelated file changed")
			}
		})
	}
}

func TestOpenClawManagedRefRotation(t *testing.T) {
	u := &userInfo{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	dir := filepath.Join(u.Home, ".openclaw")
	os.MkdirAll(filepath.Join(dir, "linux-dashboard-9router"), 0700)
	path := filepath.Join(dir, "openclaw.json")
	os.WriteFile(path, []byte(`{"models":{"providers":{"9router":{"baseUrl":"`+base9Router+`","apiKey":{"source":"file","provider":"panel9router","id":"value"}}}}}`), 0600)
	secret := filepath.Join(dir, "linux-dashboard-9router", ".env")
	os.WriteFile(secret, []byte("old-synthetic"), 0600)
	if sudahPunyaProviderOpenClaw(u, path, "new-synthetic") {
		t.Fatal("stale managed ref treated as current")
	}
	os.WriteFile(secret, []byte("new-synthetic"), 0600)
	if !sudahPunyaProviderOpenClaw(u, path, "new-synthetic") {
		t.Fatal("current managed ref not recognized")
	}
}

func TestOpenClawUnsupportedDoesNotWriteSecret(t *testing.T) {
	u := &userInfo{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	calls := 0
	err := konfigurasiOpenClaw9Router(u, "synthetic-key", func(_ *userInfo, _ string, _ ...string) error { calls++; return errors.New("unsupported") })
	if err == nil || calls != 1 {
		t.Fatal("failure not propagated")
	}
	if _, err := os.Stat(filepath.Join(u.Home, ".openclaw", "linux-dashboard-9router", ".env")); !os.IsNotExist(err) {
		t.Fatal("secret written before capability accepted")
	}
}
