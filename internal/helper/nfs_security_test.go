package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestNFSSaveOpsiKosongDefaultReadOnly(t *testing.T) {
	dir := t.TempDir()
	lamaExports := exportsPath
	exportsPath = filepath.Join(dir, "exports")
	t.Cleanup(func() { exportsPath = lamaExports })

	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exportfs := filepath.Join(bin, "exportfs")
	if err := os.WriteFile(exportfs, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	share := filepath.Join(dir, "data")
	if err := os.Mkdir(share, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := nfsSave(helperproto.NFSExport{
		Path:    share,
		Clients: []helperproto.NFSClient{{Host: "192.0.2.0/24"}},
	}); err != nil {
		t.Fatalf("nfsSave: %v", err)
	}

	b, err := os.ReadFile(exportsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "192.0.2.0/24(ro,sync,no_subtree_check)") {
		t.Fatalf("export tanpa opsi tidak default read-only: %q", got)
	}
	if strings.Contains(got, "192.0.2.0/24(rw,") {
		t.Fatalf("export tanpa opsi masih writable: %q", got)
	}
}
