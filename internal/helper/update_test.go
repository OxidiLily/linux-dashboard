package helper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// potongSampaiTerpasang memutuskan berapa commit yang benar-benar belum
// terpasang — angka yang dipakai modal Update untuk bilang "tertinggal N
// commit, dipasang sekaligus". Yang gampang salah di sini:
//
//   - `git log` menuliskan commit pendek (%h), sementara rev-parse HEAD
//     mengembalikan sha penuh; pencocokannya karena itu prefiks, bukan sama
//     dengan.
//   - commit yang sudah terpasang TIDAK ikut masuk daftar (dipotong tepat
//     sebelum barisnya), dan yang lebih baru daripadanya tetap ikut.
//   - checkout dangkal dari sumber lain tidak punya commit itu sama sekali:
//     daftarnya dikembalikan apa adanya dengan pasti=false, supaya modal tidak
//     mengaku daftar itu sebagai selisih yang persis.
//
// Git nyata, remote lokal, checkout depth=1; wrapper hanya mengalihkan konstanta
// produksi sehingga tes tidak menyentuh jaringan, source terpasang, atau systemd.
func updateGitFixture(t *testing.T) (string, string, func(...string) string) {
	t.Helper()
	updateTreeCache.Lock()
	updateTreeCache.local, updateTreeCache.remote = "", ""
	updateTreeCache.Unlock()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	remote, local := filepath.Join(dir, "remote"), filepath.Join(dir, "local")
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	command("init", "-b", "main", remote)
	for _, name := range []string{"README.md", "internal/helper/main.go", "internal/helper/policy-seed.md", "deploy/soul-default.md"} {
		p := filepath.Join(remote, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("initial\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command("-C", remote, "add", ".")
	command("-C", remote, "commit", "-m", "installed")
	command("clone", "--depth", "1", "file://"+remote, local)
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = ls-remote ]; then
  if [ -e %q ]; then printf 'bad refs/heads/main\n'; exit 0; fi
  shift 2
  exec %q ls-remote %q "$@"
fi
if [ "$1" = -C ]; then
  shift 2
  if [ "$1" = fetch ]; then
    printf 'fetch\n' >> %q
    [ ! -e %q ] || exit 1
    if [ -e %q ]; then
      %q -C %q reset --hard "$(cat %q)" >/dev/null || exit 1
    fi
    for arg in "$@"; do
      shift
      [ "$arg" != %q ] || arg=%q
      set -- "$@" "$arg"
    done
    exec %q -C %q "$@"
  fi
  [ "$1" != diff ] || [ ! -e %q ] || exit 1
  exec %q -C %q "$@"
fi
exit 1
`, filepath.Join(remote, "bad-remote"), git, remote, filepath.Join(dir, "fetches"), filepath.Join(remote, "fail-fetch"), filepath.Join(dir, "move-remote"), git, remote, filepath.Join(dir, "move-remote"), updateRepo, "file://"+remote, git, local, filepath.Join(remote, "fail-diff"), git, local)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	old := pathExec
	pathExec = bin + ":" + old
	t.Cleanup(func() { pathExec = old })
	return remote, local, command
}

func TestUpdateStatusConcurrentDocsOnlyCached(t *testing.T) {
	remote, _, git := updateGitFixture(t)
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("concurrent docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", t.Name())
	start := make(chan struct{})
	results := make(chan helperproto.UpdateStatus, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
		}()
	}
	close(start)
	for i := 0; i < cap(results); i++ {
		if st := <-results; st.Tertinggal || len(st.Perubahan) != 0 {
			t.Errorf("docs-only: %+v", st)
		}
	}
	if st := updateStatus(helperproto.UpdateArgs{Cek: true}); st.Tertinggal {
		t.Errorf("repeated docs-only: %+v", st)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(remote), "fetches"))
	if err != nil || string(b) != "fetch\n" {
		t.Fatalf("identical comparisons must fetch once: %q %v", b, err)
	}
}

func TestUpdateStatusRemoteMovesBeforeFetch(t *testing.T) {
	remote, local, git := updateGitFixture(t)
	installed := git("-C", local, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("moving docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", t.Name())
	observed := git("-C", remote, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(filepath.Dir(remote), "move-remote"), []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
	st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
	if st.Tertinggal || st.Remote != observed[:7] {
		t.Fatalf("must compare observed SHA despite branch movement: %+v", st)
	}
	if got := git("-C", local, "rev-parse", "FETCH_HEAD"); got != observed {
		t.Fatalf("fetched %s want %s", got, observed)
	}
}

func TestUpdateGitFixturePreservesFetchOptions(t *testing.T) {
	remote, local, git := updateGitFixture(t)
	git("-C", remote, "commit", "--allow-empty", "-m", t.Name())
	sha := git("-C", remote, "rev-parse", "HEAD")
	if _, err := run("git", "-C", updateSrc, "fetch", "--quiet", "--no-tags", "--depth", "1", updateRepo, sha); err != nil {
		t.Fatal(err)
	}
	if count := git("-C", local, "rev-list", "--count", "FETCH_HEAD"); count != "1" {
		t.Fatalf("fetch --depth 1 changed: commit count %s", count)
	}
}

func TestUpdateStatusREADMEOnlyShallow(t *testing.T) {
	remote, local, git := updateGitFixture(t)
	if got := git("-C", local, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("new docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", "docs only")
	before := git("-C", local, "rev-parse", "HEAD")
	st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
	if st.Tertinggal || len(st.Perubahan) != 0 {
		t.Fatalf("documentation-only update: %+v", st)
	}
	if got := git("-C", local, "rev-parse", "HEAD"); got != before {
		t.Fatal("installed HEAD changed")
	}
}

func TestUpdateStatusMalformedRemoteNotFetched(t *testing.T) {
	remote, _, _ := updateGitFixture(t)
	if err := os.WriteFile(filepath.Join(remote, "bad-remote"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
	if !st.Tertinggal {
		t.Fatal("invalid remote must stay conservative")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(remote), "fetches")); !os.IsNotExist(err) {
		t.Fatalf("invalid object ID fetched: %v", err)
	}
}

func TestUpdateStatusTreeChanges(t *testing.T) {
	for _, name := range []string{"README.en.md", "CHANGELOG.md", "docs/guide.md", "docs/image.svg", "internal/helper/main.go", "internal/helper/policy-seed.md", "deploy/soul-default.md", "deploy/install.sh", "web/ui/package-lock.json", "web/ui/public/icon.svg", "Makefile"} {
		t.Run(name, func(t *testing.T) {
			remote, _, git := updateGitFixture(t)
			p := filepath.Join(remote, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git("-C", remote, "add", ".")
			git("-C", remote, "commit", "-m", "change")
			want := strings.Contains(name, "/") && !strings.HasPrefix(name, "docs/") || name == "Makefile"
			st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
			if st.Tertinggal != want {
				t.Fatalf("Tertinggal=%v want=%v: %+v", st.Tertinggal, want, st)
			}
			if want && (!st.PerubahanPasti || len(st.Perubahan) != 1 || !strings.HasSuffix(st.Perubahan[0], " change")) {
				t.Fatalf("incorrect detailed changes: %+v", st)
			}
			b, err := os.ReadFile(filepath.Join(filepath.Dir(remote), "fetches"))
			if err != nil || string(b) != "fetch\n" {
				t.Fatalf("fetch once: %q %v", b, err)
			}
		})
	}
}

func TestUpdateStatusRevertedCodeHasNoNetUpdate(t *testing.T) {
	remote, _, git := updateGitFixture(t)
	if err := os.WriteFile(filepath.Join(remote, "internal/helper/main.go"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", "code")
	git("-C", remote, "revert", "--no-edit", "HEAD")
	st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
	if st.Tertinggal || len(st.Perubahan) != 0 {
		t.Fatalf("net identical tree: %+v", st)
	}
}

func TestUpdateStatusDocsPlusCode(t *testing.T) {
	remote, _, git := updateGitFixture(t)
	for _, name := range []string{"README.md", "internal/helper/main.go"} {
		if err := os.WriteFile(filepath.Join(remote, name), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("-C", remote, "commit", "-am", name)
	}
	st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
	if !st.Tertinggal || !st.PerubahanPasti || len(st.Perubahan) != 2 {
		t.Fatalf("docs plus code: %+v", st)
	}
}

func TestUpdateStatusNoFetchWithoutDifference(t *testing.T) {
	remote, _, _ := updateGitFixture(t)
	for _, args := range []helperproto.UpdateArgs{{}, {Cek: true, Rinci: true}} {
		st := updateStatus(args)
		if st.Tertinggal || len(st.Perubahan) != 0 {
			t.Fatalf("unchanged: %+v", st)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(remote), "fetches")); !os.IsNotExist(err) {
		t.Fatalf("unexpected fetch: %v", err)
	}
}

func TestUpdateStatusCacheKeyChanges(t *testing.T) {
	remote, local, git := updateGitFixture(t)
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("key docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", t.Name())
	if st := updateStatus(helperproto.UpdateArgs{Cek: true}); st.Tertinggal {
		t.Fatalf("docs-only: %+v", st)
	}
	if err := os.WriteFile(filepath.Join(remote, "internal/helper/main.go"), []byte("key code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", remote, "commit", "-am", "remote key changed")
	if st := updateStatus(helperproto.UpdateArgs{Cek: true}); !st.Tertinggal {
		t.Fatal("remote SHA change reused docs-only result")
	}
	git("-C", local, "checkout", "--detach", "FETCH_HEAD")
	git("-C", local, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "local key changed")
	if st := updateStatus(helperproto.UpdateArgs{Cek: true}); st.Tertinggal {
		t.Fatal("local SHA change reused code-change result")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(remote), "fetches"))
	if err != nil || string(b) != strings.Repeat("fetch\n", 3) {
		t.Fatalf("each changed key must recompute: %q %v", b, err)
	}
}

func TestUpdateStatusFailuresStayConservative(t *testing.T) {
	for _, failure := range []string{"fetch", "diff"} {
		t.Run(failure, func(t *testing.T) {
			remote, _, git := updateGitFixture(t)
			if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("docs\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git("-C", remote, "commit", "-am", "docs")
			if err := os.WriteFile(filepath.Join(remote, "fail-"+failure), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			st := updateStatus(helperproto.UpdateArgs{Cek: true, Rinci: true})
			if !st.Tertinggal {
				t.Fatalf("failed comparison claimed docs-only: %+v", st)
			}
			if failure == "fetch" && (st.PerubahanPasti || len(st.Perubahan) != 0) {
				t.Fatalf("stale detailed changes: %+v", st)
			}
			if err := os.Remove(filepath.Join(remote, "fail-"+failure)); err != nil {
				t.Fatal(err)
			}
			if st := updateStatus(helperproto.UpdateArgs{Cek: true}); st.Tertinggal {
				t.Fatalf("failed comparison cached instead of retried: %+v", st)
			}
			if st := updateStatus(helperproto.UpdateArgs{Cek: true}); st.Tertinggal {
				t.Fatalf("successful retry not cached: %+v", st)
			}
			b, err := os.ReadFile(filepath.Join(filepath.Dir(remote), "fetches"))
			if err != nil || string(b) != "fetch\nfetch\n" {
				t.Fatalf("retry once, cache success: %q %v", b, err)
			}
		})
	}
}

func TestPotongSampaiTerpasang(t *testing.T) {
	penuhTerpasang := "c3412cb1658312caf8e486b31262bbd70a933932"

	logGit := strings.Join([]string{
		"9b82d9a commit 12",
		"7f1c4aa commit 11",
		"5d0e6bb commit 10",
		"c3412cb commit 3",
		"aaaaaaa commit 2",
		"bbbbbbb commit 1",
		"",
	}, "\n")

	t.Run("dipotong tepat sebelum commit terpasang", func(t *testing.T) {
		dapat, pasti := potongSampaiTerpasang(logGit, penuhTerpasang)
		if !pasti {
			t.Fatal("pasti harus true kalau commit terpasang ada di daftar")
		}
		if len(dapat) != 3 {
			t.Fatalf("harus 3 commit belum terpasang, dapat %d: %v", len(dapat), dapat)
		}
		if dapat[0] != "9b82d9a commit 12" {
			t.Fatalf("urutan harus terbaru dulu, dapat %q", dapat[0])
		}
		if strings.Contains(strings.Join(dapat, "\n"), "commit 2") {
			t.Fatalf("commit yang lebih lama dari versi terpasang tidak boleh ikut: %v", dapat)
		}
	})

	t.Run("sha penuh dicocokkan sebagai prefiks", func(t *testing.T) {
		// Baris pertama sudah sama dengan yang terpasang: panel tidak
		// tertinggal, dan daftarnya kosong (bukan seluruh riwayat).
		dapat, pasti := potongSampaiTerpasang(logGit, "9b82d9a684f6b497cb87948ef4dc50bff7d982b5")
		if !pasti || len(dapat) != 0 {
			t.Fatalf("harus kosong dan pasti, dapat %v pasti=%v", dapat, pasti)
		}
	})

	t.Run("commit terpasang tidak ada di daftar", func(t *testing.T) {
		dapat, pasti := potongSampaiTerpasang(logGit, "ffffffffffffffffffffffffffffffffffffffff")
		if pasti {
			t.Fatal("pasti harus false kalau commit terpasang tidak ketemu")
		}
		if len(dapat) != 6 {
			t.Fatalf("daftar dikembalikan apa adanya (6 baris), dapat %d", len(dapat))
		}
	})

	t.Run("sha pendek dari sumber lain tidak bikin panic", func(t *testing.T) {
		// rev-parse bisa mengembalikan nilai pendek/asing di checkout yang
		// rusak; fungsi ini tidak boleh mengasumsikan 40 karakter.
		dapat, pasti := potongSampaiTerpasang(logGit, "abc")
		if pasti || len(dapat) != 6 {
			t.Fatalf("diharapkan daftar apa adanya, dapat %d pasti=%v", len(dapat), pasti)
		}
		if _, pasti := potongSampaiTerpasang(logGit, ""); pasti {
			t.Fatal("tanpa versi lokal, daftar tidak boleh dianggap pasti")
		}
	})

	t.Run("log kosong", func(t *testing.T) {
		for _, isi := range []string{"", "\n\n", "   \n"} {
			dapat, pasti := potongSampaiTerpasang(isi, penuhTerpasang)
			if len(dapat) != 0 || pasti {
				t.Fatalf("log kosong (%q) harus menghasilkan daftar kosong, dapat %v", isi, dapat)
			}
		}
	})
}
