package helper

import (
	"linux-dashboard/OxidiLily/internal/helperproto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudflareTokenLifecycle(t *testing.T) {
	oldPath, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	tmpDir := t.TempDir()
	cloudflareTokenPath = filepath.Join(tmpDir, "cloudflare-dns-token")
	cloudflareTokenOwnerUID = os.Getuid()
	// Ensure parent dir has correct permissions (0700) for the security check
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	t.Cleanup(func() { cloudflareTokenPath, cloudflareTokenOwnerUID = oldPath, oldUID })
	if saved, err := cloudflareTokenStatus(); err != nil || saved {
		t.Fatalf("initial status: %v %v", saved, err)
	}
	if err := cloudflareTokenSave("test-secret"); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(cloudflareTokenPath)
	if err != nil || string(encoded) != "dns_cloudflare_api_token = test-secret\n" {
		t.Fatal("berkas token harus kompatibel dengan plugin Certbot DNS Cloudflare")
	}
	info, err := os.Lstat(cloudflareTokenPath)
	if err != nil || info.Mode().Perm() != 0o600 || info.Sys() == nil {
		t.Fatalf("permissions: %v %v", info, err)
	}
	if saved, err := cloudflareTokenStatus(); err != nil || !saved {
		t.Fatalf("saved status: %v %v", saved, err)
	}
	if token, err := cloudflareTokenRead(); err != nil || token != "test-secret" {
		t.Fatalf("read failed: %v", err)
	}
	if err := cloudflareTokenDelete(); err != nil {
		t.Fatal(err)
	}
	if saved, err := cloudflareTokenStatus(); err != nil || saved {
		t.Fatalf("deleted status: %v %v", saved, err)
	}
	if _, err := os.Stat(cloudflareTokenPath); !os.IsNotExist(err) {
		t.Fatalf("file persists: %v", err)
	}
}

func TestCloudflareDNSStoredTokenAndSafeErrors(t *testing.T) {
	oldPath, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	tmpDir := t.TempDir()
	cloudflareTokenPath = filepath.Join(tmpDir, "token")
	cloudflareTokenOwnerUID = os.Getuid()
	// Ensure parent dir has correct permissions (0700) for the security check
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	t.Cleanup(func() { cloudflareTokenPath, cloudflareTokenOwnerUID = oldPath, oldUID })
	if err := cloudflareTokenSave("test-secret"); err != nil {
		t.Fatal(err)
	}
	oldBase, oldClient := cloudflareAPIBase, cloudflareHTTPClient
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"message":"test-secret"}]}`))
	}))
	defer srv.Close()
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = oldBase, oldClient })
	for _, args := range []helperproto.CloudflareDNSArgs{
		{Domain: "app.example.test"},
		{Domain: "app.example.test", Content: "203.0.113.10"},
		{Domain: "app.example.test", RecordID: "abc"},
	} {
		var err error
		switch {
		case args.Content != "":
			_, err = cloudflareDNSUpsertSafe(args)
		case args.RecordID != "":
			_, err = cloudflareDNSDeleteSafe(args)
		default:
			_, err = cloudflareDNSListSafe(args)
		}
		if err == nil || strings.Contains(err.Error(), "test-secret") || gotAuth != "Bearer test-secret" {
			t.Fatalf("unsafe result: %v auth=%v", err, gotAuth == "Bearer test-secret")
		}
	}
	_, err := cloudflareDNSListSafe(helperproto.CloudflareDNSArgs{Token: "explicit-secret", Domain: "app.example.test"})
	if err == nil || strings.Contains(err.Error(), "explicit-secret") || gotAuth != "Bearer explicit-secret" {
		t.Fatal("explicit token failure")
	}
}

func TestCloudflareDNSResponseCannotEchoToken(t *testing.T) {
	oldPath, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	tmpDir := t.TempDir()
	cloudflareTokenPath, cloudflareTokenOwnerUID = filepath.Join(tmpDir, "token"), os.Getuid()
	// Ensure parent dir has correct permissions (0700) for the security check
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	t.Cleanup(func() { cloudflareTokenPath, cloudflareTokenOwnerUID = oldPath, oldUID })
	if err := cloudflareTokenSave("test-secret"); err != nil {
		t.Fatal(err)
	}
	oldBase, oldClient := cloudflareAPIBase, cloudflareHTTPClient
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123"}]}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"test-secret","type":"A","name":"app.example.test","content":"203.0.113.1"}]}`))
		}
	}))
	defer srv.Close()
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = oldBase, oldClient })
	records, err := cloudflareDNSListSafe(helperproto.CloudflareDNSArgs{Domain: "app.example.test"})
	if err == nil || strings.Contains(err.Error(), "test-secret") || len(records) != 0 {
		t.Fatalf("response leaked secret: %v %#v", err, records)
	}
}

func TestCloudflareTokenCommandsRequireSudo(t *testing.T) {
	for _, cmd := range []string{helperproto.CmdProxyCloudflareTokenStatus, helperproto.CmdProxyCloudflareTokenSave, helperproto.CmdProxyCloudflareTokenDelete} {
		if !sudoRequired[cmd] {
			t.Fatalf("command %s not sudo-only", cmd)
		}
	}
}

func TestCloudflareTokenRejectsBadInputAndSymlink(t *testing.T) {
	oldPath, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	cloudflareTokenPath = filepath.Join(t.TempDir(), "cloudflare-dns-token")
	cloudflareTokenOwnerUID = os.Getuid()
	t.Cleanup(func() { cloudflareTokenPath, cloudflareTokenOwnerUID = oldPath, oldUID })
	for _, token := range []string{"", " ", "unsafe\nheader", strings.Repeat("a", 4097)} {
		if err := cloudflareTokenSave(token); err == nil || len(token) > 8 && strings.Contains(err.Error(), token) {
			t.Fatal("bad token accepted or echoed")
		}
	}
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cloudflareTokenPath); err != nil {
		t.Fatal(err)
	}
	if err := cloudflareTokenSave("test-secret"); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := cloudflareTokenStatus(); err == nil {
		t.Fatal("symlink status accepted")
	}
	if err := cloudflareTokenDelete(); err == nil {
		t.Fatal("symlink delete accepted")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "keep" {
		t.Fatal("unrelated file changed")
	}
}
