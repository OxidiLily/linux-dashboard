package helper

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

const panelProxyID = "000000000000"

var (
	proxyStatePath       = "/etc/linux-dashboard/proxy-hosts.json"
	proxyConfigDir       = "/etc/nginx/conf.d"
	proxyRun             = run
	letsEncryptLiveDir   = "/etc/letsencrypt/live"
	certbotWebroot       = "/var/www/certbot"
	certbotDeployHook    = "/etc/letsencrypt/renewal-hooks/deploy/linux-dashboard-nginx-reload"
	certbotTerpasang     = func() bool { _, ok := lookBinary("certbot"); return ok }
	cloudflareAPIBase    = "https://api.cloudflare.com/client/v4"
	cloudflareHTTPClient = &http.Client{Timeout: 15 * time.Second}
	proxyMu              sync.Mutex
)

var (
	proxyDomainRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	proxyHostRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)
	proxyIDRe     = regexp.MustCompile(`^[a-f0-9]{12}$`)
	// ID record Cloudflare: hex/UUID-ish; cukup longgar untuk semua bentuk
	// yang dipakai API, tetapi menutup injeksi path (`/`, `%`, `..`).
	cloudflareRecordIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func validasiProxyHost(h helperproto.ProxyHost) error {
	h.Domain = strings.ToLower(strings.TrimSpace(h.Domain))
	h.TargetHost = strings.TrimSpace(h.TargetHost)
	if !proxyDomainRe.MatchString(h.Domain) || strings.Contains(h.Domain, "..") {
		return errKode(helperproto.ErrNilaiTidakValid, "nama domain atau IPv4 tidak valid: %s", h.Domain)
	}
	if ip := net.ParseIP(h.Domain); ip != nil && (ip.To4() == nil || h.TLSMode != "") {
		return errKode(helperproto.ErrNilaiTidakValid, "alamat IP hanya mendukung IPv4 tanpa TLS Certbot")
	}
	if strings.HasPrefix(h.Domain, "*.") {
		return errKode(helperproto.ErrNilaiTidakValid, "wildcard belum didukung: %s", h.Domain)
	}
	if ip := net.ParseIP(strings.Trim(h.TargetHost, "[]")); ip == nil && (!proxyHostRe.MatchString(h.TargetHost) || strings.Contains(h.TargetHost, "..")) {
		return errKode(helperproto.ErrNilaiTidakValid, "alamat target tidak valid: %s", h.TargetHost)
	}
	if h.TargetPort < 1 || h.TargetPort > 65535 {
		return errKode(helperproto.ErrNilaiTidakValid, "port target tidak valid: %d", h.TargetPort)
	}
	if h.Scheme != "http" && h.Scheme != "https" {
		return errKode(helperproto.ErrNilaiTidakValid, "scheme target harus http atau https")
	}
	// Panel bawaan di 1122 dilayani HTTP; TLS publik diterminasi nginx.
	if (h.ID == panelProxyID || h.Managed || (h.TargetHost == "127.0.0.1" && h.TargetPort == 1122)) && h.Scheme != "http" {
		return errKode(helperproto.ErrNilaiTidakValid, "upstream panel port 1122 harus HTTP; aktifkan HTTPS pada host nginx")
	}
	if h.ID != "" && !proxyIDRe.MatchString(h.ID) {
		return errKode(helperproto.ErrNilaiTidakValid, "ID proxy host tidak valid")
	}
	return nil
}

func renderProxyHost(h helperproto.ProxyHost) (string, error) {
	if err := validasiProxyHost(h); err != nil {
		return "", err
	}
	target := h.TargetHost
	if strings.Contains(target, ":") && !strings.HasPrefix(target, "[") {
		target = "[" + target + "]"
	}
	proxy := fmt.Sprintf(`    location / {
        proxy_pass %s://%s:%d;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
`, h.Scheme, target, h.TargetPort)
	challenge := fmt.Sprintf(`    location ^~ /.well-known/acme-challenge/ {
        root %s;
    }
`, certbotWebroot)
	if h.TLSMode == "certbot" {
		live := filepath.Join(letsEncryptLiveDir, h.Domain)
		return fmt.Sprintf(`# Managed by linux-dashboard. Do not edit.
server {
    listen 80;
    listen [::]:80;
    server_name %s;
%s    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name %s;
    ssl_certificate %s/fullchain.pem;
    ssl_certificate_key %s/privkey.pem;
%s}
`, h.Domain, challenge, h.Domain, live, live, proxy), nil
	}
	return fmt.Sprintf(`# Managed by linux-dashboard. Do not edit.
server {
    listen 80;
    listen [::]:80;
    server_name %s;
%s%s}
`, h.Domain, challenge, proxy), nil
}

// Rule panel tanpa domain adalah entri virtual: install pertama tidak menulis
// nginx maupun state, dan sertifikat native tetap dikelola terpisah.
func proxyPanelList() ([]helperproto.ProxyHost, error) {
	list, err := proxyList()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == panelProxyID || list[i].Managed || (list[i].TargetHost == "127.0.0.1" && list[i].TargetPort == 1122 && list[i].Enabled && (list[i].Scheme == "http" || list[i].Scheme == "https")) {
			list[i].Managed = true
			return list, nil
		}
	}
	panel := helperproto.ProxyHost{ID: panelProxyID, TargetHost: "127.0.0.1", TargetPort: 1122, Scheme: "http", Enabled: true, Managed: true}
	return append([]helperproto.ProxyHost{panel}, list...), nil
}

func proxyList() ([]helperproto.ProxyHost, error) {
	b, err := os.ReadFile(proxyStatePath)
	if os.IsNotExist(err) {
		return []helperproto.ProxyHost{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []helperproto.ProxyHost
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, errInvalid("state Proxy manager rusak: %v", err)
	}
	if out == nil {
		out = []helperproto.ProxyHost{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	for i := range out {
		isiInfoSertifikat(&out[i])
	}
	return out, nil
}

// isiInfoSertifikat membaca issuer + masa berlaku dari fullchain.pem milik
// satu host. Gagal membaca bukan error: state tetap valid, hanya info status
// yang kosong (mis. file certbot hilang di luar panel).
func isiInfoSertifikat(h *helperproto.ProxyHost) {
	if h.TLSMode != "certbot" || h.Domain == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(letsEncryptLiveDir, h.Domain, "fullchain.pem"))
	if err != nil {
		return
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}
	h.CertIssuer = cert.Issuer.CommonName
	if h.CertIssuer == "" {
		h.CertIssuer = cert.Issuer.String()
	}
	h.CertNotAfter = cert.NotAfter.UTC().Format(time.RFC3339)
}

func proxyID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func proxyConfigPath(id string) string {
	return filepath.Join(proxyConfigDir, "linux-dashboard-"+id+".conf")
}

func tulisAtomik(path string, isi []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".linux-dashboard-*")
	if err != nil {
		return err
	}
	nama := tmp.Name()
	defer os.Remove(nama)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(isi)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if tutup := tmp.Close(); err == nil {
		err = tutup
	}
	if err != nil {
		return err
	}
	return os.Rename(nama, path)
}

func tulisProxyState(list []helperproto.ProxyHost) error {
	// Info sertifikat adalah data runtime dari fullchain.pem, bukan state.
	// Jangan tertulis ke file supaya tidak basi antar restart.
	simpan := make([]helperproto.ProxyHost, len(list))
	copy(simpan, list)
	for i := range simpan {
		simpan[i].CertIssuer, simpan[i].CertNotAfter = "", ""
	}
	b, err := json.MarshalIndent(simpan, "", "  ")
	if err != nil {
		return err
	}
	return tulisAtomik(proxyStatePath, append(b, '\n'), 0o600)
}

func proxySave(h helperproto.ProxyHost) (helperproto.ProxyHost, error) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	h.Domain = strings.ToLower(strings.TrimSpace(h.Domain))
	h.TargetHost = strings.TrimSpace(h.TargetHost)
	list, err := proxyList()
	if err != nil {
		return helperproto.ProxyHost{}, err
	}
	if h.ID == "" && h.TargetHost == "127.0.0.1" && h.TargetPort == 1122 {
		for _, existing := range list {
			if existing.ID == panelProxyID || existing.Managed || (existing.TargetHost == "127.0.0.1" && existing.TargetPort == 1122 && existing.Enabled) {
				return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "rule panel sudah ada; edit rule bawaan")
			}
		}
	}
	if h.ID == panelProxyID {
		for _, existing := range list {
			if existing.ID != panelProxyID && (existing.Managed || (existing.TargetHost == "127.0.0.1" && existing.TargetPort == 1122 && existing.Enabled)) {
				return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "rule panel sudah ada; edit rule bawaan")
			}
		}
	}
	managed := h.ID == panelProxyID
	for _, existing := range list {
		if existing.ID == h.ID && (existing.Managed || (existing.TargetHost == "127.0.0.1" && existing.TargetPort == 1122 && existing.Enabled)) {
			managed = true
		}
	}
	if managed {
		// Domain belum diatur: entri virtual tidak menulis nginx/state.
		// Setelah dibuat, target/scheme dapat diedit; hanya delete yang dilarang.
		h.Managed = true
		if h.Domain == "" {
			return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "isi domain panel sebelum menyimpan")
		}
	} else {
		h.Managed = false
	}
	if err := validasiProxyHost(h); err != nil {
		return helperproto.ProxyHost{}, err
	}
	idx := -1
	for i, ada := range list {
		if ada.Domain == h.Domain && ada.ID != h.ID {
			return helperproto.ProxyHost{}, &helperErr{code: helperproto.ErrSudahAda, kodeUI: helperproto.ErrSudahAda, params: []string{h.Domain}, msg: fmt.Sprintf("domain %s sudah ada", h.Domain)}
		}
		if ada.ID == h.ID && h.ID != "" {
			idx = i
		}
	}
	// TLS state is owned by certificate issuance, never by proxySave callers.
	if idx >= 0 {
		if h.TLSMode != list[idx].TLSMode {
			return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "mode TLS hanya dapat diubah melalui penerbitan sertifikat")
		}
	} else if h.TLSMode != "" {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "mode TLS hanya dapat diubah melalui penerbitan sertifikat")
	}
	if h.ID == "" {
		h.ID, err = proxyID()
		if err != nil {
			return helperproto.ProxyHost{}, err
		}
		list = append(list, h)
	} else if idx >= 0 {
		// Pertahankan mode TLS saat mengubah target/domain; penggantian domain
		// dengan sertifikat lama tidak boleh menghasilkan listener 443 invalid.
		if list[idx].TLSMode == "certbot" && list[idx].Domain != h.Domain {
			return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "matikan TLS sebelum mengubah domain")
		}
		list[idx] = h
	} else if h.ID == panelProxyID {
		list = append(list, h)
	} else {
		return helperproto.ProxyHost{}, &helperErr{code: helperproto.ErrNotFound, msg: "proxy host tidak ditemukan"}
	}
	if err := terapkanProxy(list); err != nil {
		return helperproto.ProxyHost{}, err
	}
	return h, nil
}

// proxyDisableTLS is the only path that can turn a Certbot host back to HTTP.
// Certificates remain on disk for renewal/history; nginx no longer serves them.
func proxyDisableTLS(id string) (helperproto.ProxyHost, error) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if !proxyIDRe.MatchString(id) {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "ID proxy host tidak valid")
	}
	list, err := proxyList()
	if err != nil {
		return helperproto.ProxyHost{}, err
	}
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if list[i].TLSMode != "certbot" {
			return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "TLS Certbot belum aktif pada host ini")
		}
		list[i].TLSMode = ""
		if err := terapkanProxy(list); err != nil {
			return helperproto.ProxyHost{}, err
		}
		return list[i], nil
	}
	return helperproto.ProxyHost{}, &helperErr{code: helperproto.ErrNotFound, msg: "proxy host tidak ditemukan"}
}

func proxyDelete(id string) error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if !proxyIDRe.MatchString(id) {
		return errKode(helperproto.ErrNilaiTidakValid, "ID proxy host tidak valid")
	}
	list, err := proxyList()
	if err != nil {
		return err
	}
	for _, host := range list {
		if host.ID == id && (host.Managed || (host.TargetHost == "127.0.0.1" && host.TargetPort == 1122 && host.Enabled)) {
			return errKode(helperproto.ErrNilaiTidakValid, "rule panel tidak dapat dihapus")
		}
	}
	if id == panelProxyID {
		return errKode(helperproto.ErrNilaiTidakValid, "rule panel tidak dapat dihapus")
	}
	out := make([]helperproto.ProxyHost, 0, len(list))
	ditemukan := false
	for _, h := range list {
		if h.ID == id {
			ditemukan = true
			continue
		}
		out = append(out, h)
	}
	if !ditemukan {
		return &helperErr{code: helperproto.ErrNotFound, msg: "proxy host tidak ditemukan"}
	}
	return terapkanProxy(out)
}

func terapkanProxy(list []helperproto.ProxyHost) error {
	// State lama dapat menunjuk panel HTTP di port 1122 dengan scheme HTTPS.
	// Pulihkan upstream itu saat menulis ulang semua rule, bukan menolak host baru.
	for i := range list {
		if list[i].TargetHost == "127.0.0.1" && list[i].TargetPort == 1122 && list[i].Scheme == "https" {
			list[i].Scheme = "http"
		}
	}
	if err := os.MkdirAll(proxyConfigDir, 0o755); err != nil {
		return err
	}
	lamaState, stateErr := os.ReadFile(proxyStatePath)
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return stateErr
	}
	var oldHosts []helperproto.ProxyHost
	if stateErr == nil {
		if err := json.Unmarshal(lamaState, &oldHosts); err != nil {
			return err
		}
	}
	// Only the map and IDs in state belong to this manager. Snapshot every
	// touched path before writing; unreadable files must never be discarded.
	paths := map[string]bool{"linux-dashboard-00-map.conf": true}
	for _, h := range oldHosts {
		if !proxyIDRe.MatchString(h.ID) {
			return errInvalid("ID proxy host dalam state tidak valid")
		}
		paths[filepath.Base(proxyConfigPath(h.ID))] = true
	}
	for _, h := range list {
		if !proxyIDRe.MatchString(h.ID) {
			return errInvalid("ID proxy host tidak valid")
		}
		paths[filepath.Base(proxyConfigPath(h.ID))] = true
	}
	lamaConfig := map[string][]byte{}
	for n := range paths {
		path := filepath.Join(proxyConfigDir, n)
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("snapshot %s: %w", n, err)
		}
		if err == nil {
			lamaConfig[n] = b
		}
	}
	rollback := func(cause error) error {
		var restore []error
		for n := range paths {
			path := filepath.Join(proxyConfigDir, n)
			if b, ok := lamaConfig[n]; ok {
				restore = append(restore, tulisAtomik(path, b, 0o644))
			} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				restore = append(restore, err)
			}
		}
		if stateErr == nil {
			restore = append(restore, tulisAtomik(proxyStatePath, lamaState, 0o600))
		} else if err := os.Remove(proxyStatePath); err != nil && !os.IsNotExist(err) {
			restore = append(restore, err)
		}
		return errors.Join(append([]error{cause}, restore...)...)
	}
	mapConf := "# Managed by linux-dashboard. Do not edit.\nmap $http_upgrade $connection_upgrade {\n    default upgrade;\n    '' close;\n}\n"
	if err := tulisAtomik(filepath.Join(proxyConfigDir, "linux-dashboard-00-map.conf"), []byte(mapConf), 0o644); err != nil {
		return rollback(err)
	}
	aktif := map[string]bool{"linux-dashboard-00-map.conf": true}
	for _, h := range list {
		if !h.Enabled {
			continue
		}
		isi, err := renderProxyHost(h)
		if err != nil {
			return rollback(err)
		}
		nama := filepath.Base(proxyConfigPath(h.ID))
		if err := tulisAtomik(filepath.Join(proxyConfigDir, nama), []byte(isi), 0o644); err != nil {
			return rollback(err)
		}
		aktif[nama] = true
	}
	for n := range lamaConfig {
		if !aktif[n] {
			if err := os.Remove(filepath.Join(proxyConfigDir, n)); err != nil && !os.IsNotExist(err) {
				return rollback(err)
			}
		}
	}
	if err := tulisProxyState(list); err != nil {
		return rollback(err)
	}
	if _, err := proxyRun("nginx", "-t"); err != nil {
		return rollback(errInvalid("konfigurasi nginx ditolak: %v", err))
	}
	if _, err := proxyRun("systemctl", "reload", "nginx"); err != nil {
		cause := rollback(errInvalid("reload nginx gagal: %v", err))
		// Reload may have applied the new (HTTP) config despite returning an
		// error. Restore the old runtime too, not only files and state.
		if _, reloadErr := proxyRun("systemctl", "reload", "nginx"); reloadErr != nil {
			return errors.Join(cause, fmt.Errorf("reload pemulihan nginx gagal: %w", reloadErr))
		}
		return cause
	}
	return nil
}

func validasiModeTLSProxy(args helperproto.ProxyCertArgs) error {
	if args.PanelTLS {
		return errKode(helperproto.ErrNilaiTidakValid, "TLS native panel tidak didukung; HTTPS host dilayani nginx")
	}
	return nil
}

func proxyCertIssue(args helperproto.ProxyCertArgs) (helperproto.ProxyHost, error) {
	if err := validasiModeTLSProxy(args); err != nil {
		return helperproto.ProxyHost{}, err
	}
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if !proxyIDRe.MatchString(args.ID) {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "ID proxy host tidak valid")
	}
	if !strings.Contains(args.Email, "@") || strings.ContainsAny(args.Email, "\r\n\x00 ;") {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "email ACME tidak valid")
	}
	list, err := proxyList()
	if err != nil {
		return helperproto.ProxyHost{}, err
	}
	idx := -1
	for i := range list {
		if list[i].ID == args.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return helperproto.ProxyHost{}, &helperErr{code: helperproto.ErrNotFound, msg: "proxy host tidak ditemukan"}
	}
	if !list[idx].Enabled {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "aktifkan proxy host sebelum menerbitkan sertifikat")
	}
	// ACME publik tidak pernah menerbitkan sertifikat untuk .local atau nama
	// satu label. Tolak sebelum mengubah konfigurasi nginx/menjalankan certbot.
	domain := list[idx].Domain
	if net.ParseIP(domain) != nil {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "Let's Encrypt tidak mendukung alamat IP privat: %s", domain)
	}
	if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".local") || strings.HasSuffix(domain, ".localhost") || strings.HasSuffix(domain, ".test") || strings.HasSuffix(domain, ".invalid") || strings.HasSuffix(domain, ".example") {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrNilaiTidakValid, "Let's Encrypt memerlukan domain publik; gunakan sertifikat lokal untuk %s", domain)
	}
	if !certbotTerpasang() {
		return helperproto.ProxyHost{}, errKode(helperproto.ErrBelumTerpasang, "certbot")
	}
	if args.CloudflareDNS {
		if _, err := cloudflareTokenRead(); err != nil {
			return helperproto.ProxyHost{}, err
		}
	} else if err := os.MkdirAll(certbotWebroot, 0o755); err != nil {
		return helperproto.ProxyHost{}, err
	}
	if err := tulisAtomik(certbotDeployHook, []byte("#!/bin/sh\nsystemctl reload nginx\n"), 0o755); err != nil {
		return helperproto.ProxyHost{}, fmt.Errorf("gagal memasang deploy hook certbot: %w", err)
	}
	// HTTP config wajib aktif sebelum certbot melakukan challenge.
	// ulang = host sudah punya sertifikat (mis. masih STAGING): certbot dengan
	// --keep-until-expiring akan menolak ganti karena sertifikat lama belum
	// kedaluwarsa, dan server ACME ikut keturunan lineage — jadi dipaksa
	// renew + server eksplisit supaya pergantian staging → production nyata.
	modeLama := list[idx].TLSMode
	ulang := modeLama == "certbot"
	if !ulang {
		list[idx].TLSMode = "pending"
		if err := terapkanProxy(list); err != nil {
			return helperproto.ProxyHost{}, err
		}
	}
	pulihkan := func() error {
		if ulang {
			return nil // Existing HTTPS config and state were never changed.
		}
		list[idx].TLSMode = modeLama
		return terapkanProxy(list)
	}
	cmdArgs := []string{"certonly"}
	if args.CloudflareDNS {
		cmdArgs = append(cmdArgs, "--dns-cloudflare", "--dns-cloudflare-credentials", cloudflareTokenPath)
	} else {
		cmdArgs = append(cmdArgs, "--webroot", "-w", certbotWebroot)
	}
	cmdArgs = append(cmdArgs, "-d", list[idx].Domain,
		"--email", args.Email, "--agree-tos", "--non-interactive")
	if ulang {
		cmdArgs = append(cmdArgs, "--force-renewal")
	} else {
		cmdArgs = append(cmdArgs, "--keep-until-expiring")
	}
	if args.Staging {
		cmdArgs = append(cmdArgs, "--staging")
	} else {
		cmdArgs = append(cmdArgs, "--server", "https://acme-v02.api.letsencrypt.org/directory")
	}
	if _, err := proxyRun("certbot", cmdArgs...); err != nil {
		restoreErr := pulihkan()
		if restoreErr != nil {
			return helperproto.ProxyHost{}, fmt.Errorf("gagal memulihkan proxy setelah certbot: %w", restoreErr)
		}
		if args.CloudflareDNS {
			// Output plugin/Cloudflare bisa memantulkan credential; jangan keluar dari helper.
			return helperproto.ProxyHost{}, errInvalid("penerbitan DNS-01 gagal; periksa log Certbot di server")
		}
		return helperproto.ProxyHost{}, errInvalid("penerbitan sertifikat gagal: %v", err)
	}
	certPath := filepath.Join(letsEncryptLiveDir, list[idx].Domain, "fullchain.pem")
	keyPath := filepath.Join(letsEncryptLiveDir, list[idx].Domain, "privkey.pem")
	if _, err := os.Stat(certPath); err != nil {
		return helperproto.ProxyHost{}, errors.Join(errInvalid("sertifikat certbot tidak ditemukan: %v", err), pulihkan())
	}
	if _, err := os.Stat(keyPath); err != nil {
		return helperproto.ProxyHost{}, errors.Join(errInvalid("private key certbot tidak ditemukan: %v", err), pulihkan())
	}
	list[idx].TLSMode = "certbot"
	if err := terapkanProxy(list); err != nil {
		return helperproto.ProxyHost{}, errors.Join(err, pulihkan())
	}
	if args.PanelTLS {
		if err := aktifkanTLSNativeDariCertbot(list[idx].Domain, certPath, keyPath); err != nil {
			return helperproto.ProxyHost{}, err
		}
	}
	return list[idx], nil
}

func aktifkanTLSNativeDariCertbot(domain, certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("gagal membaca sertifikat certbot: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("gagal membaca private key certbot: %w", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("pasangan TLS certbot tidak valid: %w", err)
	}
	_, gid, err := pemilikCertificatesManaged()
	if err != nil {
		return fmt.Errorf("user service linux-dashboard tidak tersedia: %w", err)
	}
	if err := os.MkdirAll(certificatesManagedDir, 0o750); err != nil {
		return err
	}
	managedCert := filepath.Join(certificatesManagedDir, "certbot-panel.crt")
	managedKey := filepath.Join(certificatesManagedDir, "certbot-panel.key")
	if err := tulisBerkasTLSAtomic(managedCert, certPEM, 0o644, 0, gid); err != nil {
		return err
	}
	if err := tulisBerkasTLSAtomic(managedKey, keyPEM, 0o640, 0, gid); err != nil {
		return err
	}
	if _, err := simpanCertificates(helperproto.CertificatesSetArgs{CertPath: managedCert, KeyPath: managedKey}, time.Now()); err != nil {
		return fmt.Errorf("gagal mengaktifkan sertifikat untuk TLS native panel: %w", err)
	}
	// Renewal Certbot berjalan sebagai root. Salin hasil baru ke path yang dapat
	// dibaca service, lalu restart panel agar tls.Listen memuat pasangan terbaru.
	hook := fmt.Sprintf("#!/bin/sh\nset -eu\nsystemctl reload nginx\nif [ \"${RENEWED_LINEAGE:-}\" = %q ]; then\n  install -o root -g linux-dashboard -m 0644 \"$RENEWED_LINEAGE/fullchain.pem\" %q\n  install -o root -g linux-dashboard -m 0640 \"$RENEWED_LINEAGE/privkey.pem\" %q\n  systemctl restart linux-dashboard-web.service\nfi\n", filepath.Join(letsEncryptLiveDir, domain), managedCert, managedKey)
	if err := tulisAtomik(certbotDeployHook, []byte(hook), 0o755); err != nil {
		return fmt.Errorf("gagal memasang deploy hook TLS panel: %w", err)
	}
	return nil
}

type cloudflareEnvelope[T any] struct {
	Success bool `json:"success"`
	Result  T    `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func cloudflareCall(token, method, path string, body any, out any) error {
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return errInvalid("API token Cloudflare wajib diisi")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(cloudflareAPIBase, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cloudflareHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare API gagal: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errInvalid("Cloudflare API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("respons Cloudflare API tidak valid: %w", err)
	}
	return nil
}

func validasiDomainCloudflare(domain string) error {
	if !proxyDomainRe.MatchString(domain) || strings.Contains(domain, "..") || net.ParseIP(domain) != nil {
		return errInvalid("nama domain Cloudflare tidak valid")
	}
	if len(strings.Split(domain, ".")) < 2 {
		return errInvalid("nama domain Cloudflare harus memiliki zone")
	}
	return nil
}

// cloudflareZoneID mencari zone aktif yang menaungi domain, dari suffix
// terpanjang ke terpendek (app.example.test → app.example.test, lalu example.test).
func cloudflareZoneID(token, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1; i++ {
		zone := strings.Join(labels[i:], ".")
		var env cloudflareEnvelope[[]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}]
		q := url.Values{"name": {zone}, "status": {"active"}}
		if err := cloudflareCall(token, http.MethodGet, "/zones?"+q.Encode(), nil, &env); err != nil {
			return "", err
		}
		if !env.Success {
			return "", errInvalid("Cloudflare menolak pencarian zone: %v", env.Errors)
		}
		if len(env.Result) == 1 {
			return env.Result[0].ID, nil
		}
	}
	return "", errInvalid("zone Cloudflare untuk %s tidak ditemukan", domain)
}

func cloudflareListRecords(token, zoneID, domain string) ([]helperproto.CloudflareDNSRecord, error) {
	var all []helperproto.CloudflareDNSRecord
	q := url.Values{"type": {"A"}, "name": {domain}}
	for page := 1; ; page++ {
		q.Set("page", fmt.Sprint(page))
		var listed struct {
			cloudflareEnvelope[[]helperproto.CloudflareDNSRecord]
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if err := cloudflareCall(token, http.MethodGet, "/zones/"+zoneID+"/dns_records?"+q.Encode(), nil, &listed); err != nil {
			return nil, err
		}
		if !listed.Success {
			return nil, errInvalid("Cloudflare menolak pembacaan DNS: %v", listed.Errors)
		}
		all = append(all, listed.Result...)
		if listed.ResultInfo.TotalPages <= page {
			return all, nil
		}
	}
}

func cloudflareDNSList(args helperproto.CloudflareDNSArgs) ([]helperproto.CloudflareDNSRecord, error) {
	args.Domain = strings.ToLower(strings.TrimSpace(args.Domain))
	if err := validasiDomainCloudflare(args.Domain); err != nil {
		return nil, err
	}
	zoneID, err := cloudflareZoneID(args.Token, args.Domain)
	if err != nil {
		return nil, err
	}
	recs, err := cloudflareListRecords(args.Token, zoneID, args.Domain)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []helperproto.CloudflareDNSRecord{}
	}
	return recs, nil
}

// cloudflareDNSDelete menghapus record A milik domain. RecordID kosong =
// hapus semua record A pada nama itu (dipakai tombol hapus di panel).
func cloudflareDNSDelete(args helperproto.CloudflareDNSArgs) (int, error) {
	args.Domain = strings.ToLower(strings.TrimSpace(args.Domain))
	if err := validasiDomainCloudflare(args.Domain); err != nil {
		return 0, err
	}
	if args.RecordID != "" && !cloudflareRecordIDRe.MatchString(args.RecordID) {
		return 0, errInvalid("ID record Cloudflare tidak valid")
	}
	zoneID, err := cloudflareZoneID(args.Token, args.Domain)
	if err != nil {
		return 0, err
	}
	recs, err := cloudflareListRecords(args.Token, zoneID, args.Domain)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range recs {
		if args.RecordID != "" && rec.ID != args.RecordID {
			continue
		}
		var del cloudflareEnvelope[struct {
			ID string `json:"id"`
		}]
		if err := cloudflareCall(args.Token, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+rec.ID, nil, &del); err != nil {
			return n, err
		}
		if !del.Success {
			return n, errInvalid("Cloudflare menolak penghapusan DNS: %v", del.Errors)
		}
		n++
	}
	return n, nil
}

func cloudflareDNSUpsert(args helperproto.CloudflareDNSArgs) (helperproto.CloudflareDNSRecord, error) {
	args.Domain = strings.ToLower(strings.TrimSpace(args.Domain))
	args.Content = strings.TrimSpace(args.Content)
	if err := validasiDomainCloudflare(args.Domain); err != nil {
		return helperproto.CloudflareDNSRecord{}, err
	}
	if ip := net.ParseIP(args.Content); ip == nil || ip.To4() == nil {
		return helperproto.CloudflareDNSRecord{}, errInvalid("alamat IPv4 Cloudflare tidak valid")
	}
	zoneID, err := cloudflareZoneID(args.Token, args.Domain)
	if err != nil {
		return helperproto.CloudflareDNSRecord{}, err
	}
	listed, err := cloudflareListRecords(args.Token, zoneID, args.Domain)
	if err != nil {
		return helperproto.CloudflareDNSRecord{}, err
	}
	payload := map[string]any{"type": "A", "name": args.Domain, "content": args.Content, "proxied": args.Proxied, "ttl": 1}
	method, path := http.MethodPost, "/zones/"+zoneID+"/dns_records"
	if len(listed) > 0 {
		method, path = http.MethodPut, path+"/"+listed[0].ID
	}
	var saved cloudflareEnvelope[helperproto.CloudflareDNSRecord]
	if err := cloudflareCall(args.Token, method, path, payload, &saved); err != nil {
		return helperproto.CloudflareDNSRecord{}, err
	}
	if !saved.Success {
		return helperproto.CloudflareDNSRecord{}, errInvalid("Cloudflare menolak perubahan DNS: %v", saved.Errors)
	}
	return saved.Result, nil
}

func proxyTest() error {
	_, err := proxyRun("nginx", "-t")
	return err
}

func proxyStatus() helperproto.ProxyStatus {
	_, installed := lookBinary("nginx")
	st := helperproto.ProxyStatus{Installed: installed}
	if !installed {
		st.Message = "nginx belum terpasang"
		return st
	}
	if _, err := run("systemctl", "is-active", "--quiet", "nginx"); err == nil {
		st.Running = true
	}
	if err := proxyTest(); err == nil {
		st.ConfigOK = true
	} else {
		st.Message = err.Error()
	}
	return st
}
