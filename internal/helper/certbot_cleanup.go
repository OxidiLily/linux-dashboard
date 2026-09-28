package helper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// prepareCertbotUninstall runs before aptRemove. Keep proxy state intact on
// failure so the operator can retry deletion with Certbot still installed.
func deactivateCertbotProxy(list []helperproto.ProxyHost) error {
	changed := false
	for i := range list {
		if list[i].TLSMode == "certbot" || list[i].TLSMode == "pending" {
			// Simpan penanda lineage untuk retry bila certbot/apt gagal.
			if list[i].TLSMode != "pending" || list[i].Enabled {
				changed = true
			}
			list[i].TLSMode = "pending"
			list[i].Enabled = false
		}
	}
	if !changed {
		return nil
	}
	if _, installed := lookBinary("nginx"); installed {
		return terapkanProxy(list)
	}
	return tulisProxyState(list)
}

func prepareCertbotUninstall() error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	list, err := proxyList()
	if err != nil {
		return err
	}
	// Validate the entire state before any destructive command. Never derive a
	// cert-name from directory listings or untrusted state fields.
	for _, h := range list {
		if !proxyIDRe.MatchString(h.ID) || !proxyDomainRe.MatchString(h.Domain) || strings.Contains(h.Domain, "..") {
			return fmt.Errorf("invalid proxy state for Certbot uninstall")
		}
	}
	seen := make(map[string]bool)
	var lineages []string
	for _, h := range list {
		if h.TLSMode != "certbot" && h.TLSMode != "pending" {
			continue
		}
		if seen[h.Domain] {
			continue
		}
		seen[h.Domain] = true
		// Missing live directory means this lineage was already removed on a
		// previous partial attempt; never infer unrelated lineages from disk.
		_, err := os.Lstat(filepath.Join(letsEncryptLiveDir, h.Domain))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		lineages = append(lineages, h.Domain)
	}
	// Hentikan penyajian TLS lebih dahulu. Kegagalan reload tidak menghapus cert.
	if err := deactivateCertbotProxy(list); err != nil {
		return err
	}
	for _, domain := range lineages {
		if _, err := proxyRun("certbot", "delete", "--non-interactive", "--cert-name", domain); err != nil {
			return fmt.Errorf("certbot delete %s: %w", domain, err)
		}
	}
	return nil
}

// cleanupCertbotProxy runs only after prepareCertbotUninstall and aptRemove
// succeed. It does not remove anything outside the panel-owned paths.
func cleanupCertbotProxy() error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	list, err := proxyList()
	if err != nil {
		return err
	}
	// Reject unsafe webroots before changing state or removing the hook.
	for _, path := range []string{certbotWebroot, filepath.Join(certbotWebroot, ".well-known"), filepath.Join(certbotWebroot, ".well-known", "acme-challenge")} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("unsafe Certbot webroot: %s", path)
		}
	}
	changed := false
	for i := range list {
		if list[i].TLSMode == "certbot" || list[i].TLSMode == "pending" {
			list[i].TLSMode = ""
			list[i].Enabled = false
			changed = true
		}
	}
	if changed {
		if _, installed := lookBinary("nginx"); installed {
			if err := terapkanProxy(list); err != nil {
				return err
			}
		} else if err := tulisProxyState(list); err != nil {
			return err
		}
	}
	if err := os.Remove(certbotDeployHook); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Challenge files may belong to other users of this webroot. Remove only
	// empty panel directories, never recurse into unproven files.
	for _, path := range []string{filepath.Join(certbotWebroot, ".well-known", "acme-challenge"), filepath.Join(certbotWebroot, ".well-known"), certbotWebroot} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTEMPTY) && !os.IsExist(err) {
			return err
		}
	}
	return cloudflareTokenDelete()
}
