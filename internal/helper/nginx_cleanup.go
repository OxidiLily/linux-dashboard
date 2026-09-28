package helper

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// prepareNginxUninstall validates owned state/config before removing the package.
func prepareNginxUninstall() error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	_, _, _, err := snapshotNginxProxy()
	return err
}

// cleanupNginxProxy runs after nginx package removal; Certbot lineages are separate.
func cleanupNginxProxy() error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	oldState, stateErr, oldConfigs, err := snapshotNginxProxy()
	if err != nil {
		return err
	}
	// Uninstall resets the panel to its virtual, domain-free default. Certbot
	// lineages remain independent; removing nginx must not delete certificates.
	rollback := func(cause error) error {
		var restore []error
		for path, b := range oldConfigs {
			if err := tulisAtomik(path, b, 0o644); err != nil {
				restore = append(restore, fmt.Errorf("restore %s: %w", path, err))
			}
		}
		if stateErr == nil {
			if err := tulisAtomik(proxyStatePath, oldState, 0o600); err != nil {
				restore = append(restore, fmt.Errorf("restore proxy state: %w", err))
			}
		}
		return errors.Join(append([]error{cause}, restore...)...)
	}
	for path := range oldConfigs {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return rollback(fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if stateErr == nil {
		if err := os.Remove(proxyStatePath); err != nil && !os.IsNotExist(err) {
			return rollback(fmt.Errorf("remove proxy state: %w", err))
		}
	}
	return nil
}

func snapshotNginxProxy() ([]byte, error, map[string][]byte, error) {
	hosts, err := proxyList()
	if err != nil {
		return nil, nil, nil, err
	}
	for _, h := range hosts {
		if !proxyIDRe.MatchString(h.ID) {
			return nil, nil, nil, fmt.Errorf("invalid proxy state ID %q", h.ID)
		}
	}
	oldState, stateErr := os.ReadFile(proxyStatePath)
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return nil, nil, nil, stateErr
	}
	paths := []string{filepath.Join(proxyConfigDir, "linux-dashboard-00-map.conf")}
	for _, h := range hosts {
		paths = append(paths, proxyConfigPath(h.ID))
	}
	oldConfigs := make(map[string][]byte)
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		if !bytes.HasPrefix(b, []byte("# Managed by linux-dashboard. Do not edit.\n")) {
			return nil, nil, nil, fmt.Errorf("refusing to remove unmanaged config %s", path)
		}
		oldConfigs[path] = b
	}
	return oldState, stateErr, oldConfigs, nil
}
