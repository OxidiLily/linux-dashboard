package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestCleanupNginxProxyPreservesUnrelatedFilesAndCertbotMetadata(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir := proxyStatePath, proxyConfigDir
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	t.Cleanup(func() { proxyStatePath, proxyConfigDir = oldState, oldDir })
	if err := os.Mkdir(proxyConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := []helperproto.ProxyHost{{ID: "abcdef123456", Domain: "example.com", TargetHost: "localhost", TargetPort: 1122, Scheme: "http", Enabled: true, TLSMode: "certbot"}, {ID: "abcdef123457", Domain: "other.com", TargetHost: "localhost", TargetPort: 8080, Scheme: "http", Enabled: false}}
	if err := tulisProxyState(hosts); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"linux-dashboard-abcdef123456.conf": "# Managed by linux-dashboard. Do not edit.\nhost", "linux-dashboard-00-map.conf": "# Managed by linux-dashboard. Do not edit.\nmap", "unrelated.conf": "keep", "linux-dashboard-deadbeef1234.conf": "keep"} {
		if err := os.WriteFile(filepath.Join(proxyConfigDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupNginxProxy(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(proxyStatePath); !os.IsNotExist(err) {
		t.Fatalf("proxy state survived uninstall: %v", err)
	}
	got, err := proxyPanelList()
	if err != nil || len(got) != 1 || got[0].ID != panelProxyID || got[0].Domain != "" {
		t.Fatalf("panel did not return to default: %+v %v", got, err)
	}
	for _, name := range []string{"linux-dashboard-abcdef123456.conf", "linux-dashboard-00-map.conf"} {
		if _, err := os.Stat(filepath.Join(proxyConfigDir, name)); !os.IsNotExist(err) {
			t.Errorf("owned config %s still exists: %v", name, err)
		}
	}
	for _, name := range []string{"unrelated.conf", "linux-dashboard-deadbeef1234.conf"} {
		if b, err := os.ReadFile(filepath.Join(proxyConfigDir, name)); err != nil || string(b) != "keep" {
			t.Errorf("unrelated config %s: %q, %v", name, b, err)
		}
	}
}

func TestCleanupNginxProxyRefusesUnmanagedMatchingConfig(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir := proxyStatePath, proxyConfigDir
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	t.Cleanup(func() { proxyStatePath, proxyConfigDir = oldState, oldDir })
	if err := os.Mkdir(proxyConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := []helperproto.ProxyHost{{ID: "abcdef123456", Domain: "example.com", Enabled: true}}
	if err := tulisProxyState(hosts); err != nil {
		t.Fatal(err)
	}
	config := proxyConfigPath(hosts[0].ID)
	if err := os.WriteFile(config, []byte("third-party config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareNginxUninstall(); err == nil {
		t.Fatal("unmanaged config passed preflight before aptRemove")
	}
	if err := cleanupNginxProxy(); err == nil {
		t.Fatal("accepted unmanaged config")
	}
	if b, err := os.ReadFile(config); err != nil || string(b) != "third-party config" {
		t.Fatalf("config changed: %v", err)
	}
	got, err := proxyList()
	if err != nil || len(got) != 1 || !got[0].Enabled {
		t.Fatalf("state changed: %+v %v", got, err)
	}
}

func TestCleanupNginxProxyAbsentNginx(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir := proxyStatePath, proxyConfigDir
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "missing-nginx")
	t.Cleanup(func() { proxyStatePath, proxyConfigDir = oldState, oldDir })
	if err := tulisProxyState([]helperproto.ProxyHost{{ID: "abcdef123456", Domain: "example.com", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := cleanupNginxProxy(); err != nil {
		t.Fatal(err)
	}
	got, err := proxyPanelList()
	if err != nil || len(got) != 1 || got[0].ID != panelProxyID || got[0].Domain != "" {
		t.Fatalf("panel did not return to default: %+v %v", got, err)
	}
	if _, err := os.Stat(proxyStatePath); !os.IsNotExist(err) {
		t.Fatalf("proxy state survived uninstall: %v", err)
	}
	if _, err := os.Stat(proxyConfigDir); !os.IsNotExist(err) {
		t.Fatalf("created nginx directory: %v", err)
	}
}

func TestCleanupNginxProxyMalformedStateFailsClosed(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir := proxyStatePath, proxyConfigDir
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	t.Cleanup(func() { proxyStatePath, proxyConfigDir = oldState, oldDir })
	if err := os.Mkdir(proxyConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"{broken", `[{"id":"../evil","enabled":true}]`} {
		if err := os.WriteFile(proxyStatePath, []byte(state), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(proxyConfigDir, "linux-dashboard-00-map.conf")
		if err := os.WriteFile(path, []byte("# Managed by linux-dashboard. Do not edit.\nmap"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanupNginxProxy(); err == nil {
			t.Fatalf("accepted state %s", state)
		}
		if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), "map") {
			t.Fatalf("mutated config on invalid state: %v", err)
		}
		if b, err := os.ReadFile(proxyStatePath); err != nil || string(b) != state {
			t.Fatalf("mutated invalid state: %v", err)
		}
	}
}
