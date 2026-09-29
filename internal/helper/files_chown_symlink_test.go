package helper

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRecursiveChownDoesNotFollowSymlink(t *testing.T) {
	for _, rootLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "nested", true: "root"}[rootLink], func(t *testing.T) {
			base := t.TempDir()
			outside := filepath.Join(base, "outside")
			root := filepath.Join(base, "root")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(outside, "file")
			if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			link := root
			if !rootLink {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(root, "link")
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			// UID/GID sendiri; ctime mendeteksi Lchown tanpa mengganti ownership.
			if err := filepathWalkChown(root, os.Getuid(), os.Getgid()); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if before.Sys().(*syscall.Stat_t).Ctim != after.Sys().(*syscall.Stat_t).Ctim {
				t.Fatal("recursive chown touched file outside tree via symlink")
			}
		})
	}
}
