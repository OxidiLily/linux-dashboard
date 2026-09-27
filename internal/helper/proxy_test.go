package helper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestProxySaveHostIPMemperbaikiUpstreamPanelLama(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	legacy := helperproto.ProxyHost{ID: panelProxyID, Domain: "panel.example.com", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "https", Enabled: true, Managed: true}
	if err := tulisProxyState([]helperproto.ProxyHost{legacy}); err != nil {
		t.Fatal(err)
	}
	h, err := proxySave(helperproto.ProxyHost{Domain: "192.168.2.11", TargetHost: "192.168.2.11", TargetPort: 8085, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(proxyConfigPath(h.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), "server_name 192.168.2.11;") || !strings.Contains(string(conf), "proxy_pass http://192.168.2.11:8085;") {
		t.Fatalf("host IP tidak terpasang: %s", conf)
	}
	panelConf, err := os.ReadFile(proxyConfigPath(panelProxyID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(panelConf), "proxy_pass http://127.0.0.1:1122;") {
		t.Fatalf("upstream panel lama tidak diperbaiki: %s", panelConf)
	}
	list, err := proxyList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Scheme == "https" || list[1].Scheme == "https" {
		t.Fatalf("state panel lama tidak diperbaiki: %+v", list)
	}
}

func TestPanelRuleVirtualAdaPadaInstallBaruDanTidakDapatDihapus(t *testing.T) {
	dir := t.TempDir()
	old := proxyStatePath
	proxyStatePath = filepath.Join(dir, "state.json")
	t.Cleanup(func() { proxyStatePath = old })
	list, err := proxyPanelList()
	if err != nil || len(list) != 1 || list[0].ID != panelProxyID || list[0].Domain != "" || list[0].TargetHost != "127.0.0.1" || list[0].TargetPort != 1122 || !list[0].Managed {
		t.Fatalf("rule panel baru: %#v, %v", list, err)
	}
	if err := proxyDelete(panelProxyID); err == nil {
		t.Fatal("rule panel boleh dihapus")
	}
	if _, err := os.Stat(proxyStatePath); !os.IsNotExist(err) {
		t.Fatalf("list membuat state tanpa konfigurasi: %v", err)
	}
}

func TestPanelRuleBolehEditTargetNamunTidakBisaDihapus(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	h := helperproto.ProxyHost{ID: panelProxyID, Domain: "panel.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true}
	if _, err := proxySave(h); err != nil {
		t.Fatal(err)
	}
	h.TargetHost = "10.0.0.2"
	h.TargetPort = 8080
	h.Scheme = "http"
	if _, err := proxySave(h); err != nil {
		t.Fatalf("target panel boleh diedit: %v", err)
	}
	if err := proxyDelete(panelProxyID); err == nil {
		t.Fatal("rule panel dihapus")
	}
	list, err := proxyList()
	if err != nil || len(list) != 1 || list[0].TargetHost != "10.0.0.2" || list[0].TargetPort != 8080 || list[0].Scheme != "http" {
		t.Fatalf("edit target tidak tersimpan: %#v %v", list, err)
	}
}

func TestPanelRuleMengadopsiProxyPanelLama(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	h, err := proxySave(helperproto.ProxyHost{Domain: "panel.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	list, err := proxyPanelList()
	if err != nil || len(list) != 1 || !list[0].Managed || list[0].ID != h.ID {
		t.Fatalf("adopsi: %#v %v", list, err)
	}
	if err := proxyDelete(h.ID); err == nil {
		t.Fatal("rule panel lama bisa dihapus")
	}
	h.TargetPort = 1234
	if _, err := proxySave(h); err != nil {
		t.Fatalf("rule panel lama boleh diedit: %v", err)
	}
}

func TestPanelRuleTidakDapatDilewatiDenganHostBaru(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	first, err := proxySave(helperproto.ProxyHost{ID: panelProxyID, Domain: "panel.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = proxySave(helperproto.ProxyHost{Domain: "other.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err == nil {
		t.Fatal("duplikat rule panel diterima")
	}
	list, err := proxyPanelList()
	if err != nil || len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("list berubah: %#v, %v", list, err)
	}
}

func TestPanelRuleTidakBolehDitambahKeduaKalinyaDenganIDVirtual(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	if _, err := proxySave(helperproto.ProxyHost{Domain: "old.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, err := proxySave(helperproto.ProxyHost{ID: panelProxyID, Domain: "new.example.test", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err == nil {
		t.Fatal("ID virtual membuat rule kedua saat rule panel lama sudah ada")
	}
}

func TestCertbotMenolakDomainLokalSebelumMengubahNginx(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldBot := proxyStatePath, proxyConfigDir, proxyRun, certbotTerpasang
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	calls := 0
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { calls++; return helperproto.ExecResult{}, nil }
	certbotTerpasang = func() bool { return true }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun, certbotTerpasang = oldState, oldDir, oldRun, oldBot })
	h, err := proxySave(helperproto.ProxyHost{Domain: "core.local", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err = proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test"}); err == nil || !strings.Contains(err.Error(), "publik") {
		t.Fatalf(".local harus ditolak sebelum certbot: %v", err)
	}
	if calls != before {
		t.Fatalf("nginx/certbot berubah: %d→%d", before, calls)
	}
}

func TestValidasiProxyHostMenolakInputBerbahaya(t *testing.T) {
	kasus := []helperproto.ProxyHost{
		{Domain: "contoh.test; include /etc/passwd", TargetHost: "127.0.0.1", TargetPort: 8080, Scheme: "http"},
		{Domain: "contoh.test", TargetHost: "127.0.0.1$evil", TargetPort: 8080, Scheme: "http"},
		{Domain: "contoh.test", TargetHost: "127.0.0.1", TargetPort: 0, Scheme: "http"},
		{Domain: "contoh.test", TargetHost: "127.0.0.1", TargetPort: 8080, Scheme: "ftp"},
		{Domain: "*.contoh.test", TargetHost: "127.0.0.1", TargetPort: 8080, Scheme: "http"},
	}
	for _, h := range kasus {
		if err := validasiProxyHost(h); err == nil {
			t.Errorf("host berbahaya/tidak valid diterima: %#v", h)
		}
	}
}

func TestRenderProxyHostMenghasilkanReverseProxyAman(t *testing.T) {
	h := helperproto.ProxyHost{
		ID: "abc123abc123", Domain: "app.example.test", TargetHost: "10.20.30.40",
		TargetPort: 8080, Scheme: "http", Enabled: true,
	}
	isi, err := renderProxyHost(h)
	if err != nil {
		t.Fatal(err)
	}
	for _, baris := range []string{
		"listen 80;", "server_name app.example.test;",
		"proxy_pass http://10.20.30.40:8080;",
		"proxy_set_header Host $host;", "proxy_set_header Upgrade $http_upgrade;",
	} {
		if !strings.Contains(isi, baris) {
			t.Errorf("config tidak memuat %q:\n%s", baris, isi)
		}
	}
}

func TestProxySaveCRUDDanKonflikDomain(t *testing.T) {
	dir := t.TempDir()
	lamaState, lamaDir, lamaRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath = filepath.Join(dir, "proxy-hosts.json")
	proxyConfigDir = filepath.Join(dir, "conf.d")
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = lamaState, lamaDir, lamaRun })

	simpan, err := proxySave(helperproto.ProxyHost{Domain: "app.example.test", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if simpan.ID == "" {
		t.Fatal("ID host baru kosong")
	}
	if _, err := os.Stat(filepath.Join(proxyConfigDir, "linux-dashboard-"+simpan.ID+".conf")); err != nil {
		t.Fatalf("config host tidak dibuat: %v", err)
	}
	list, err := proxyList()
	if err != nil || len(list) != 1 || list[0].Domain != "app.example.test" {
		t.Fatalf("list sesudah save = %#v, err=%v", list, err)
	}

	_, err = proxySave(helperproto.ProxyHost{Domain: "app.example.test", TargetHost: "127.0.0.1", TargetPort: 4000, Scheme: "http", Enabled: true})
	if err == nil || kodeErr(err) != helperproto.ErrSudahAda {
		t.Fatalf("domain duplikat harus already_exists, dapat %v (%q)", err, kodeErr(err))
	}

	simpan.TargetPort = 4000
	if _, err := proxySave(simpan); err != nil {
		t.Fatalf("update gagal: %v", err)
	}
	list, _ = proxyList()
	if list[0].TargetPort != 4000 {
		t.Fatalf("port update = %d", list[0].TargetPort)
	}

	if err := proxyDelete(simpan.ID); err != nil {
		t.Fatalf("delete gagal: %v", err)
	}
	list, _ = proxyList()
	if len(list) != 0 {
		t.Fatalf("list sesudah delete = %#v", list)
	}
}

func TestRenderProxyHostTLSMemakaiSertifikatDomain(t *testing.T) {
	h := helperproto.ProxyHost{
		ID: "abc123abc123", Domain: "secure.example.test", TargetHost: "127.0.0.1",
		TargetPort: 8443, Scheme: "https", Enabled: true, TLSMode: "certbot",
	}
	isi, err := renderProxyHost(h)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragmen := range []string{
		"listen 443 ssl;",
		"ssl_certificate /etc/letsencrypt/live/secure.example.test/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/secure.example.test/privkey.pem;",
		"return 301 https://$host$request_uri;",
		"location ^~ /.well-known/acme-challenge/",
	} {
		if !strings.Contains(isi, fragmen) {
			t.Errorf("config TLS tidak memuat %q:\n%s", fragmen, isi)
		}
	}
}

func TestProxyCertIssueCloudflareDNS01TidakMemantulkanTokenPadaError(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled := proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang
	oldToken, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "hosts.json"), filepath.Join(dir, "nginx")
	letsEncryptLiveDir, certbotWebroot, certbotDeployHook = filepath.Join(dir, "live"), filepath.Join(dir, "webroot"), filepath.Join(dir, "hook")
	cloudflareTokenPath, cloudflareTokenOwnerUID = filepath.Join(dir, "cloudflare-dns-token"), os.Getuid()
	certbotTerpasang = func() bool { return true }
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		if name == "certbot" {
			return helperproto.ExecResult{}, fmt.Errorf("test-secret")
		}
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang = oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled
		cloudflareTokenPath, cloudflareTokenOwnerUID = oldToken, oldUID
	})
	if err := cloudflareTokenSave("test-secret"); err != nil {
		t.Fatal(err)
	}
	h, err := proxySave(helperproto.ProxyHost{Domain: "panel.oxidilily.com", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test", CloudflareDNS: true}); err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("unsafe certbot error: %v", err)
	}
}

func TestProxyCertIssueCloudflareDNS01MenolakTokenYangSudahDihapus(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled := proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang
	oldToken, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "hosts.json"), filepath.Join(dir, "nginx")
	letsEncryptLiveDir, certbotWebroot, certbotDeployHook = filepath.Join(dir, "live"), filepath.Join(dir, "webroot"), filepath.Join(dir, "hook")
	cloudflareTokenPath, cloudflareTokenOwnerUID = filepath.Join(dir, "cloudflare-dns-token"), os.Getuid()
	certbotTerpasang = func() bool { return true }
	called := false
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		if name == "certbot" {
			called = true
		}
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang = oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled
		cloudflareTokenPath, cloudflareTokenOwnerUID = oldToken, oldUID
	})
	h, err := proxySave(helperproto.ProxyHost{Domain: "panel.oxidilily.com", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test", CloudflareDNS: true}); err == nil || called {
		t.Fatalf("issuance without token: %v certbot=%v", err, called)
	}
}

func TestProxyCertIssueCloudflareDNS01MemakaiCredentialTanpaTokenDiArgumen(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled := proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang
	oldToken, oldUID := cloudflareTokenPath, cloudflareTokenOwnerUID
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "hosts.json"), filepath.Join(dir, "nginx")
	letsEncryptLiveDir, certbotWebroot, certbotDeployHook = filepath.Join(dir, "live"), filepath.Join(dir, "webroot"), filepath.Join(dir, "hook")
	cloudflareTokenPath, cloudflareTokenOwnerUID = filepath.Join(dir, "cloudflare-dns-token"), os.Getuid()
	certbotTerpasang = func() bool { return true }
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot, certbotDeployHook, certbotTerpasang = oldState, oldDir, oldRun, oldLive, oldRoot, oldHook, oldInstalled
		cloudflareTokenPath, cloudflareTokenOwnerUID = oldToken, oldUID
	})
	if err := cloudflareTokenSave("test-secret"); err != nil {
		t.Fatal(err)
	}
	var got []string
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		if name == "certbot" {
			got = append([]string{name}, args...)
			live := filepath.Join(letsEncryptLiveDir, "panel.oxidilily.com")
			if err := os.MkdirAll(live, 0700); err != nil {
				return helperproto.ExecResult{}, err
			}
			for _, f := range []string{"fullchain.pem", "privkey.pem"} {
				if err := os.WriteFile(filepath.Join(live, f), []byte("test"), 0600); err != nil {
					return helperproto.ExecResult{}, err
				}
			}
		}
		return helperproto.ExecResult{}, nil
	}
	h, err := proxySave(helperproto.ProxyHost{Domain: "panel.oxidilily.com", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test", CloudflareDNS: true}); err != nil {
		t.Fatal(err)
	}
	command := strings.Join(got, " ")
	if !strings.Contains(command, "--dns-cloudflare --dns-cloudflare-credentials "+cloudflareTokenPath) || strings.Contains(command, "--webroot") || strings.Contains(command, "test-secret") {
		t.Fatalf("DNS-01 args tidak aman: %s", command)
	}
}

func TestProxyCertIssueMenjalankanCertbotDanMengaktifkanTLS(t *testing.T) {
	dir := t.TempDir()
	lamaState, lamaDir, lamaRun, lamaLE, lamaRoot := proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot
	lamaHook, lamaTerpasang := certbotDeployHook, certbotTerpasang
	proxyStatePath = filepath.Join(dir, "proxy-hosts.json")
	proxyConfigDir = filepath.Join(dir, "conf.d")
	letsEncryptLiveDir = filepath.Join(dir, "letsencrypt", "live")
	certbotWebroot = filepath.Join(dir, "certbot")
	certbotDeployHook = filepath.Join(dir, "hooks", "reload")
	certbotTerpasang = func() bool { return true }
	var perintah [][]string
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		perintah = append(perintah, append([]string{name}, args...))
		if name == "certbot" {
			live := filepath.Join(letsEncryptLiveDir, "app.oxidilily.com")
			if err := os.MkdirAll(live, 0o755); err != nil {
				return helperproto.ExecResult{}, err
			}
			if err := os.WriteFile(filepath.Join(live, "fullchain.pem"), []byte("cert"), 0o644); err != nil {
				return helperproto.ExecResult{}, err
			}
			if err := os.WriteFile(filepath.Join(live, "privkey.pem"), []byte("key"), 0o600); err != nil {
				return helperproto.ExecResult{}, err
			}
		}
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot = lamaState, lamaDir, lamaRun, lamaLE, lamaRoot
		certbotDeployHook, certbotTerpasang = lamaHook, lamaTerpasang
	})

	h, err := proxySave(helperproto.ProxyHost{Domain: "app.oxidilily.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test", Staging: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.TLSMode != "certbot" {
		t.Fatalf("TLSMode = %q", got.TLSMode)
	}
	gabung := fmt.Sprint(perintah)
	for _, arg := range []string{"certbot", "certonly", "--webroot", "--staging", "admin@example.test", "app.oxidilily.com"} {
		if !strings.Contains(gabung, arg) {
			t.Errorf("perintah certbot tidak memuat %q: %s", arg, gabung)
		}
	}
}

func TestProxyCertIssueGagalMempertahankanTLSLama(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldRoot, oldHook, oldBot := proxyStatePath, proxyConfigDir, proxyRun, certbotWebroot, certbotDeployHook, certbotTerpasang
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "hosts.json"), filepath.Join(dir, "conf.d")
	certbotWebroot, certbotDeployHook = filepath.Join(dir, "webroot"), filepath.Join(dir, "hook")
	certbotTerpasang = func() bool { return true }
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		if name == "certbot" {
			return helperproto.ExecResult{}, fmt.Errorf("challenge gagal")
		}
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, certbotWebroot, certbotDeployHook, certbotTerpasang = oldState, oldDir, oldRun, oldRoot, oldHook, oldBot
	})
	h, err := proxySave(helperproto.ProxyHost{Domain: "app.example.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	h.TLSMode = "certbot"
	if err := tulisProxyState([]helperproto.ProxyHost{h}); err != nil {
		t.Fatal(err)
	}
	if _, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.com"}); err == nil {
		t.Fatal("certbot harus gagal")
	}
	list, err := proxyList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].TLSMode != "certbot" {
		t.Fatalf("TLS lama hilang setelah reissue gagal: %+v", list)
	}
}

func TestProxyIPPrivateHTTPDiizinkanTanpaCertbot(t *testing.T) {
	h := helperproto.ProxyHost{Domain: "192.168.2.11", TargetHost: "192.168.2.11", TargetPort: 8085, Scheme: "http", Enabled: true}
	conf, err := renderProxyHost(h)
	if err != nil || !strings.Contains(conf, "server_name 192.168.2.11;") || !strings.Contains(conf, "proxy_pass http://192.168.2.11:8085;") {
		t.Fatalf("proxy IP privat HTTP: %v %q", err, conf)
	}
}

func TestProxyIPPrivateTidakDapatMemintaCertbot(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldBot := proxyStatePath, proxyConfigDir, proxyRun, certbotTerpasang
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	calls := 0
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { calls++; return helperproto.ExecResult{}, nil }
	certbotTerpasang = func() bool { return true }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun, certbotTerpasang = oldState, oldDir, oldRun, oldBot })
	h, err := proxySave(helperproto.ProxyHost{Domain: "192.168.2.11", TargetHost: "192.168.2.11", TargetPort: 8085, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test"}); err == nil || !strings.Contains(err.Error(), "IP") {
		t.Fatalf("Certbot untuk IP privat harus ditolak: %v", err)
	}
	if calls != before {
		t.Fatalf("penerbitan IP mengubah nginx atau memanggil Certbot: %d -> %d", before, calls)
	}
}

func TestProxyPanelHTTPMenolakUpstreamHTTPS(t *testing.T) {
	if err := validasiProxyHost(helperproto.ProxyHost{Domain: "panel.example.com", TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "https"}); err == nil {
		t.Fatal("upstream HTTPS menuju panel HTTP harus ditolak")
	}
}

func TestProxyCertIssueMenolakTLSNativePanel(t *testing.T) {
	if err := validasiModeTLSProxy(helperproto.ProxyCertArgs{PanelTLS: true}); err == nil {
		t.Fatal("permintaan TLS native panel harus ditolak")
	}
}

func TestProxyListMembacaIssuerSertifikatDomain(t *testing.T) {
	dir := t.TempDir()
	lamaState, lamaLE := proxyStatePath, letsEncryptLiveDir
	proxyStatePath = filepath.Join(dir, "proxy-hosts.json")
	letsEncryptLiveDir = filepath.Join(dir, "live")
	t.Cleanup(func() { proxyStatePath, letsEncryptLiveDir = lamaState, lamaLE })

	live := filepath.Join(letsEncryptLiveDir, "app.oxidilily.com")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, key := buatPasanganTLSUji(t, live, "app.oxidilily.com"); key == "" {
		t.Fatal("pasangan TLS uji gagal")
	}
	// buatPasanganTLSUji menulis <nama>.crt/.key; certbot memakai nama tetap.
	if err := os.Rename(filepath.Join(live, "app.oxidilily.com.crt"), filepath.Join(live, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}
	st := `[{"id":"abc123abc123","domain":"app.oxidilily.com","target_host":"127.0.0.1","target_port":3000,"scheme":"http","enabled":true,"tls_mode":"certbot"}]`
	if err := os.WriteFile(proxyStatePath, []byte(st), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := proxyList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CertIssuer == "" || list[0].CertNotAfter == "" {
		t.Fatalf("info sertifikat tidak terisi: %#v", list)
	}
	if _, err := time.Parse(time.RFC3339, list[0].CertNotAfter); err != nil {
		t.Fatalf("CertNotAfter bukan RFC3339: %q", list[0].CertNotAfter)
	}
}

func TestCloudflareDNSUpsertMemakaiRecordAProxied(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-uji" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.test"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone123/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone123/dns_records":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["type"] != "A" || body["name"] != "app.oxidilily.com" || body["content"] != "203.0.113.10" || body["proxied"] != true {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"record123","type":"A","name":"app.oxidilily.com","content":"203.0.113.10","proxied":true}}`))
		default:
			t.Fatalf("request tak terduga: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	lamaURL, lamaClient := cloudflareAPIBase, cloudflareHTTPClient
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = lamaURL, lamaClient })

	got, err := cloudflareDNSUpsert(helperproto.CloudflareDNSArgs{Token: "token-uji", Domain: "app.oxidilily.com", Content: "203.0.113.10", Proxied: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "record123" || !got.Proxied {
		t.Fatalf("record = %#v", got)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestProxyCertIssueUlangPaksaRenewalDanServerProduksi(t *testing.T) {
	dir := t.TempDir()
	lamaState, lamaDir, lamaRun, lamaLE, lamaRoot := proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot
	lamaHook, lamaTerpasang := certbotDeployHook, certbotTerpasang
	proxyStatePath = filepath.Join(dir, "proxy-hosts.json")
	proxyConfigDir = filepath.Join(dir, "conf.d")
	letsEncryptLiveDir = filepath.Join(dir, "letsencrypt", "live")
	certbotWebroot = filepath.Join(dir, "certbot")
	certbotDeployHook = filepath.Join(dir, "hooks", "reload")
	certbotTerpasang = func() bool { return true }
	var perintah [][]string
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		perintah = append(perintah, append([]string{name}, args...))
		if name == "certbot" {
			live := filepath.Join(letsEncryptLiveDir, "app.oxidilily.com")
			if err := os.MkdirAll(live, 0o755); err != nil {
				return helperproto.ExecResult{}, err
			}
			if err := os.WriteFile(filepath.Join(live, "fullchain.pem"), []byte("cert"), 0o644); err != nil {
				return helperproto.ExecResult{}, err
			}
			if err := os.WriteFile(filepath.Join(live, "privkey.pem"), []byte("key"), 0o600); err != nil {
				return helperproto.ExecResult{}, err
			}
		}
		return helperproto.ExecResult{}, nil
	}
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, letsEncryptLiveDir, certbotWebroot = lamaState, lamaDir, lamaRun, lamaLE, lamaRoot
		certbotDeployHook, certbotTerpasang = lamaHook, lamaTerpasang
	})

	h, err := proxySave(helperproto.ProxyHost{Domain: "app.oxidilily.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Simulasikan host yang sudah punya sertifikat (mis. masih STAGING).
	st, err := proxyList()
	if err != nil {
		t.Fatal(err)
	}
	st[0].TLSMode = "certbot"
	if err := tulisProxyState(st); err != nil {
		t.Fatal(err)
	}
	perintah = nil

	if _, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.test", Staging: false}); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(perintah)
	for _, ingin := range []string{"--force-renewal", "--server https://acme-v02.api.letsencrypt.org/directory"} {
		if !strings.Contains(got, ingin) {
			t.Errorf("reissue tidak memuat %q: %s", ingin, got)
		}
	}
	if strings.Contains(got, "--staging") || strings.Contains(got, "--keep-until-expiring") {
		t.Errorf("reissue production masih memuat staging/keep-until-expiring: %s", got)
	}
}

func TestCloudflareDNSListMengembalikanRecordA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.test"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone123/dns_records":
			if got := r.URL.Query().Get("type"); got != "A" {
				t.Fatalf("type query = %q", got)
			}
			if got := r.URL.Query().Get("name"); got != "app.example.test" {
				t.Fatalf("name query = %q", got)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec1","type":"A","name":"app.example.test","content":"203.0.113.10","proxied":true}]}`))
		default:
			t.Fatalf("request tak terduga: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	lamaURL, lamaClient := cloudflareAPIBase, cloudflareHTTPClient
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = lamaURL, lamaClient })

	got, err := cloudflareDNSList(helperproto.CloudflareDNSArgs{Token: "token-uji", Domain: "app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "rec1" || got[0].Content != "203.0.113.10" || !got[0].Proxied {
		t.Fatalf("list = %#v", got)
	}
}

func TestCloudflareDNSDeleteMenghapusRecordTepat(t *testing.T) {
	var hapus []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.test"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone123/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec1","type":"A","name":"app.example.test","content":"203.0.113.10","proxied":true}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/zone123/dns_records/rec1":
			hapus = append(hapus, r.URL.Path)
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec1"}}`))
		default:
			t.Fatalf("request tak terduga: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	lamaURL, lamaClient := cloudflareAPIBase, cloudflareHTTPClient
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = lamaURL, lamaClient })

	n, err := cloudflareDNSDelete(helperproto.CloudflareDNSArgs{Token: "token-uji", Domain: "app.example.test", RecordID: "rec1"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(hapus) != 1 {
		t.Fatalf("deleted=%d hapus=%v", n, hapus)
	}
}

func TestProxySaveTLSModeTidakDapatDiaturKlien(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	h := helperproto.ProxyHost{Domain: "app.example.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true, TLSMode: "certbot"}
	if _, err := proxySave(h); err == nil {
		t.Fatal("new host accepted injected TLS")
	}
	h.TLSMode = "pending"
	if _, err := proxySave(h); err == nil {
		t.Fatal("new host accepted pending TLS")
	}
	h.TLSMode = ""
	saved, err := proxySave(h)
	if err != nil {
		t.Fatal(err)
	}
	saved.TLSMode = "certbot"
	if _, err := proxySave(saved); err == nil {
		t.Fatal("existing host accepted injected TLS")
	}
	stored, err := proxyList()
	if err != nil || len(stored) != 1 || stored[0].TLSMode != "" {
		t.Fatalf("state changed: %+v %v", stored, err)
	}
	saved.TLSMode = "pending"
	if _, err := proxySave(saved); err == nil {
		t.Fatal("existing host accepted pending TLS")
	}
}

func TestProxySavePreservesUnownedConfigAndFailsClosedOnUnreadableSnapshot(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { return helperproto.ExecResult{}, nil }
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = oldState, oldDir, oldRun })
	if err := os.MkdirAll(proxyConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(proxyConfigDir, "linux-dashboard-other.conf")
	if err := os.WriteFile(unrelated, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	host := helperproto.ProxyHost{Domain: "app.example.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true}
	saved, err := proxySave(host)
	if err != nil {
		t.Fatal(err)
	}
	if err := proxyDelete(saved.ID); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(unrelated)
	if err != nil || string(b) != "untouched" {
		t.Fatalf("unowned config lost: %q %v", b, err)
	}
	// A directory in place of state/config reliably fails ReadFile even as root.
	if err := os.Remove(proxyStatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(proxyStatePath, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := proxySave(host); err == nil {
		t.Fatal("unreadable state accepted")
	}
	if err := os.Remove(proxyStatePath); err != nil {
		t.Fatal(err)
	}
	if err := tulisProxyState([]helperproto.ProxyHost{saved}); err != nil {
		t.Fatal(err)
	}
	target := proxyConfigPath(saved.ID)
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := proxySave(helperproto.ProxyHost{Domain: "new.example.com", TargetHost: "127.0.0.1", TargetPort: 3001, Scheme: "http", Enabled: true}); err == nil {
		t.Fatal("unreadable config accepted")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("config snapshot changed: %v %v", info, err)
	}
}

func TestProxyCertIssueDisabledHostRejectedBeforeChanges(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldHook, oldBot := proxyStatePath, proxyConfigDir, proxyRun, certbotDeployHook, certbotTerpasang
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	certbotDeployHook = filepath.Join(dir, "hook")
	certbotTerpasang = func() bool { return true }
	calls := 0
	proxyRun = func(string, ...string) (helperproto.ExecResult, error) { calls++; return helperproto.ExecResult{}, nil }
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, certbotDeployHook, certbotTerpasang = oldState, oldDir, oldRun, oldHook, oldBot
	})
	h, err := proxySave(helperproto.ProxyHost{Domain: "app.example.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.com"}); err == nil {
		t.Fatal("disabled host issued")
	}
	if calls != before {
		t.Fatalf("commands executed: %d", calls-before)
	}
	if _, err := os.Stat(certbotDeployHook); !os.IsNotExist(err) {
		t.Fatalf("hook written: %v", err)
	}
}

func TestProxyCertReissueKeepsHTTPSDuringCertbot(t *testing.T) {
	dir := t.TempDir()
	oldState, oldDir, oldRun, oldHook, oldRoot, oldBot, oldLive := proxyStatePath, proxyConfigDir, proxyRun, certbotDeployHook, certbotWebroot, certbotTerpasang, letsEncryptLiveDir
	proxyStatePath, proxyConfigDir = filepath.Join(dir, "state.json"), filepath.Join(dir, "conf.d")
	certbotDeployHook, certbotWebroot, letsEncryptLiveDir = filepath.Join(dir, "hook"), filepath.Join(dir, "webroot"), filepath.Join(dir, "live")
	certbotTerpasang = func() bool { return true }
	t.Cleanup(func() {
		proxyStatePath, proxyConfigDir, proxyRun, certbotDeployHook, certbotWebroot, certbotTerpasang, letsEncryptLiveDir = oldState, oldDir, oldRun, oldHook, oldRoot, oldBot, oldLive
	})
	h := helperproto.ProxyHost{ID: "abcdef123456", Domain: "app.example.com", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true, TLSMode: "certbot"}
	if err := tulisProxyState([]helperproto.ProxyHost{h}); err != nil {
		t.Fatal(err)
	}
	conf, err := renderProxyHost(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := tulisAtomik(proxyConfigPath(h.ID), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		if name == "certbot" {
			b, err := os.ReadFile(proxyConfigPath(h.ID))
			if err != nil {
				t.Error(err)
			} else if !strings.Contains(string(b), "listen 443 ssl;") || !strings.Contains(string(b), "return 301 https://") {
				t.Errorf("HTTPS dropped during certbot: %s", b)
			}
			state, err := proxyList()
			if err != nil || len(state) != 1 || state[0].TLSMode != "certbot" {
				t.Errorf("TLS state changed during certbot: %+v %v", state, err)
			}
			return helperproto.ExecResult{}, fmt.Errorf("challenge failed")
		}
		return helperproto.ExecResult{}, nil
	}
	if _, err := proxyCertIssue(helperproto.ProxyCertArgs{ID: h.ID, Email: "admin@example.com"}); err == nil {
		t.Fatal("expected certbot failure")
	}
}

func TestCloudflareListRecordsAllPages(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Query().Get("page") != fmt.Sprint(pages) {
			t.Errorf("page %d query: %s", pages, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `{"success":true,"result":[{"id":"rec%d","type":"A"}],"result_info":{"page":%d,"total_pages":2}}`, pages, pages)
	}))
	defer srv.Close()
	oldURL, oldClient := cloudflareAPIBase, cloudflareHTTPClient
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = oldURL, oldClient })
	records, err := cloudflareListRecords("token", "zone", "app.example.com")
	if err != nil || len(records) != 2 || pages != 2 || records[1].ID != "rec2" {
		t.Fatalf("records=%+v pages=%d err=%v", records, pages, err)
	}
}

func TestProxySaveRollbackSaatNginxTestGagal(t *testing.T) {
	dir := t.TempDir()
	lamaState, lamaDir, lamaRun := proxyStatePath, proxyConfigDir, proxyRun
	proxyStatePath = filepath.Join(dir, "proxy-hosts.json")
	proxyConfigDir = filepath.Join(dir, "conf.d")
	proxyRun = func(name string, args ...string) (helperproto.ExecResult, error) {
		return helperproto.ExecResult{Stderr: "nginx: invalid config"}, errInvalid("nginx: invalid config")
	}
	t.Cleanup(func() { proxyStatePath, proxyConfigDir, proxyRun = lamaState, lamaDir, lamaRun })

	_, err := proxySave(helperproto.ProxyHost{Domain: "app.example.test", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true})
	if err == nil {
		t.Fatal("save harus gagal saat nginx -t gagal")
	}
	list, listErr := proxyList()
	if listErr != nil || len(list) != 0 {
		t.Fatalf("state tidak rollback: %#v, err=%v", list, listErr)
	}
	entries, _ := os.ReadDir(proxyConfigDir)
	if len(entries) != 0 {
		t.Fatalf("config tidak rollback: %v", entries)
	}
}
