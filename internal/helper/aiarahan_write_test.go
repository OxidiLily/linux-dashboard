package helper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestNativeWriterRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	path := home + "/../escaped-config"
	if err := tulisBerkasUser(path, "panel", u, 0o600); err == nil {
		t.Fatal("dot-dot accepted")
	}
	if err := tulisBlokTerpilih(path, home, u.UID, u.GID, "START", "END", "block"); err == nil {
		t.Fatal("dot-dot accepted")
	}
}

func TestNativeWriterConcurrentCreation(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config")
	script := skripTulisAgen + "\nwith open(path, 'w') as editor: editor.write('external')\nwrite_file(b'panel')\n"
	cmd := exec.Command("/usr/bin/python3", "-c", script, path, home, "600")
	if err := cmd.Run(); err == nil {
		t.Fatal("creation conflict silently accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "external" {
		t.Fatal("concurrent creation overwritten")
	}
}

func TestNativeWriterNoopDetectsReplacement(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := skripTulisAgen + "\nos.rename(path, path + '.old')\nwith open(path, 'w') as editor: editor.write('external')\nwrite_file(original)\n"
	cmd := exec.Command("/usr/bin/python3", "-c", script, path, home, "600")
	if err := cmd.Run(); err == nil {
		t.Fatal("replacement silently accepted by no-op")
	}
}

// Inject a competing filesystem operation at the syscall boundary, not a fake
// filesystem. This makes the final-check/commit race deterministic.
func TestNativeWriterCommitRaces(t *testing.T) {
	for _, scenario := range []string{"edit", "replace", "parent"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "agent")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			injection := ""
			switch scenario {
			case "edit":
				injection = "with open(path, 'w') as editor: editor.write('external edit')"
			case "replace":
				injection = "os.rename(path, path + '.old')\nwith open(path, 'w') as editor: editor.write('external edit')"
			case "parent":
				outside := t.TempDir()
				injection = fmt.Sprintf("os.rename(os.path.dirname(path), os.path.dirname(path) + '.old')\nos.symlink(%q, os.path.dirname(path))", outside)
			}
			injection = "            " + strings.ReplaceAll(injection, "\n", "\n            ") + "\n"
			script := strings.Replace(skripTulisAgen, "            if exchange(folder,", injection+"            if exchange(folder,", 1) + "\nwrite_file(b'panel')\n"
			cmd := exec.Command("/usr/bin/python3", "-c", script, path, home, "600")
			cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
			if err := cmd.Run(); err == nil {
				t.Fatal("concurrent mutation not reported")
			}
			if scenario == "parent" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("symlink destination modified")
				}
				dir += ".old"
			}
			files, err := filepath.Glob(filepath.Join(dir, ".linux-dashboard-backup-*"))
			if err != nil || len(files) != 1 {
				t.Fatalf("missing recovery backup: %v", err)
			}
			got, err := os.ReadFile(files[0])
			want := "external edit"
			if scenario == "parent" {
				want = "original"
			}
			if err != nil || string(got) != want {
				t.Fatalf("lost displaced data: %v", err)
			}
		})
	}
}

func TestNativeWriterSerializesPanelWriters(t *testing.T) {
	home := t.TempDir()
	lock, err := os.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(home, "config")
	var wg sync.WaitGroup
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start, end := fmt.Sprintf("START%d", i), fmt.Sprintf("END%d", i)
			done <- tulisBlokTerpilih(path, home, os.Getuid(), os.Getgid(), start, end, start+"\nmanaged\n"+end+"\n")
		}(i)
	}
	select {
	case err := <-done:
		t.Fatalf("writer bypassed lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(done)
	for err := range done {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if strings.Count(string(got), fmt.Sprintf("START%d", i)) != 1 {
			t.Fatal("concurrent block lost")
		}
	}
}

func TestNativeWriterPreservesBackup(t *testing.T) {
	for _, block := range []bool{false, true} {
		home := t.TempDir()
		path := filepath.Join(home, "config")
		if err := os.WriteFile(path, []byte("external original\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
		var err error
		if block {
			err = tulisBlokTerpilih(path, home, u.UID, u.GID, "START", "END", "START\nmanaged\nEND\n")
		} else {
			err = tulisBerkasUser(path, "new", u, 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		files, err := filepath.Glob(filepath.Join(home, ".linux-dashboard-backup-*"))
		if err != nil || len(files) != 1 {
			t.Fatalf("backup missing: %v %v", files, err)
		}
		got, err := os.ReadFile(files[0])
		if err != nil || string(got) != "external original\n" {
			t.Fatalf("original not preserved: %v", err)
		}
	}
}

func TestWorkspaceOpenClawCustom(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".openclaw"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".openclaw/openclaw.json")
	if err := os.WriteFile(cfg, []byte(`{"agents":{"defaults":{"workspace":"~/my-agents"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := workspaceOpenClaw(&userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()})
	if err != nil || got != filepath.Join(home, "my-agents") {
		t.Fatalf("workspace: %q %v", got, err)
	}
	if err := os.WriteFile(cfg, []byte(`{"agents":{"defaults":{"workspace":"/etc"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceOpenClaw(&userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}); err == nil {
		t.Fatal("workspace luar home diterima")
	}
}

func TestWorkspaceOpenClawJSON5(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".openclaw"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".npm-global/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(home, ".npm-global/bin/openclaw")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '\"~/json5-workspace\"\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".openclaw/openclaw.json")
	if err := os.WriteFile(cfg, []byte("{ agents: { defaults: { workspace: '~/json5-workspace', }, }, }"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := workspaceOpenClaw(&userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()})
	if err != nil || got != filepath.Join(home, "json5-workspace") {
		t.Fatalf("JSON5 workspace: %q %v", got, err)
	}
}

func TestSiapkanStateAIBaruTanpaVault(t *testing.T) {
	home := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Clean(filepath.Join(cwd, "../.."))
	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	if err := jalankanSeedAI(u, repo); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "DATA/AppData/linux-dashboard")
	for _, name := range []string{"SOUL.md", "knowledge-base.md", "grounded-search.py", "PROMPT-DEPLOY-SHARED.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTulisBerkasUserMenolakSymlinkDanHardlink(t *testing.T) {
	home := t.TempDir()
	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	target := filepath.Join(home, ".hermes", ".env")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other")
	if err := os.WriteFile(other, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatal(err)
	}
	if err := tulisBerkasUser(target, "new", u, 0o600); err == nil {
		t.Fatal("symlink diterima")
	}
	got, err := os.ReadFile(other)
	if err != nil || string(got) != "original" {
		t.Fatalf("symlink target berubah: %q %v", got, err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(other, target); err != nil {
		t.Fatal(err)
	}
	if err := tulisBerkasUser(target, "new", u, 0o600); err == nil {
		t.Fatal("hardlink diterima")
	}
	got, err = os.ReadFile(other)
	if err != nil || string(got) != "original" {
		t.Fatalf("hardlink target berubah: %q %v", got, err)
	}
}

func TestTulisBerkasUserMenolakEnvTerbuka(t *testing.T) {
	home := t.TempDir()
	u := &userInfo{Home: home, UID: os.Getuid(), GID: os.Getgid()}
	path := filepath.Join(home, ".hermes", ".env")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tulisBerkasUser(path, "new\n", u, 0o600); err == nil {
		t.Fatal("env terbuka diterima")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original\n" {
		t.Fatalf("env berubah: %q %v", got, err)
	}
}

func TestTulisBlokTerpilihMenolakSymlinkParent(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	block := penandaPolicyMulai + "\npolicy\n" + penandaPolicySelesai + "\n"
	if err := tulisBlokTerpilih(path, home, os.Getuid(), os.Getgid(), penandaPolicyMulai, penandaPolicySelesai, block); err == nil {
		t.Fatal("symlink parent diterima")
	}
	if _, err := os.Stat(filepath.Join(outside, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("target luar tertulis: %v", err)
	}
}

func TestTulisBlokTerpilihMenolakHardlinkDanMenjagaIsiUser(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "other.md")
	if err := os.WriteFile(outside, []byte("jangan ditimpa\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, path); err != nil {
		t.Fatal(err)
	}
	block := penandaPolicyMulai + "\npolicy\n" + penandaPolicySelesai + "\n"
	if err := tulisBlokTerpilih(path, home, os.Getuid(), os.Getgid(), penandaPolicyMulai, penandaPolicySelesai, block); err == nil {
		t.Fatal("hardlink diterima")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "jangan ditimpa\n" {
		t.Fatalf("target lain berubah: %q %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("milik user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tulisBlokTerpilih(path, home, os.Getuid(), os.Getgid(), penandaPolicyMulai, penandaPolicySelesai, block); err != nil {
		t.Fatal(err)
	}
	if err := tulisBlokTerpilih(path, home, os.Getuid(), os.Getgid(), penandaPolicyMulai, penandaPolicySelesai, block); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), "milik user\n") || strings.Count(string(got), penandaPolicyMulai) != 1 {
		t.Fatalf("blok rusak: %q %v", got, err)
	}
}
