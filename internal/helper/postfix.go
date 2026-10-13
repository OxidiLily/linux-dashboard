package helper

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var postfixPath = "/etc/postfix/main.cf"
var postfixTransport = "/etc/postfix/transport"
var postfixRun = run
var postfixDefaults = map[string]string{
	"myhostname": "mail.oxidilily.com", "mydestination": "localhost", "inet_interfaces": "all", "inet_protocols": "ipv4", "relay_domains": "oxidilily.com", "transport_maps": "hash:/etc/postfix/transport", "mynetworks": "127.0.0.0/8, [::1]/128, 100.80.10.2/32", "smtpd_relay_restrictions": "permit_mynetworks, reject_unauth_destination",
}
var postfixKeys = []string{"myhostname", "mydestination", "inet_interfaces", "inet_protocols", "relay_domains", "transport_maps", "mynetworks", "smtpd_relay_restrictions"}
var postfixLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func postfixHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !postfixLabel.MatchString(label) {
			return false
		}
	}
	return true
}
func validatePostfix(v map[string]string) error {
	if len(v) != len(postfixKeys) {
		return errInvalid("Postfix: parameter tidak lengkap")
	}
	for _, k := range postfixKeys {
		s, ok := v[k]
		if !ok || len(s) > 2048 || strings.ContainsAny(s, "\r\n\x00#$\\") {
			return errInvalid("Postfix: nilai %s tidak valid", k)
		}
		switch k {
		case "myhostname":
			if !postfixHostname(s) || !strings.Contains(s, ".") {
				return errInvalid("Postfix: hostname tidak valid")
			}
		case "mydestination", "relay_domains":
			for _, d := range strings.Split(s, ",") {
				if !postfixHostname(strings.TrimSpace(d)) {
					return errInvalid("Postfix: domain tidak valid")
				}
			}
		case "inet_interfaces":
			if s != "all" && s != "loopback-only" {
				for _, a := range strings.Split(s, ",") {
					if net.ParseIP(strings.Trim(strings.TrimSpace(a), "[]")) == nil {
						return errInvalid("Postfix: interface tidak valid")
					}
				}
			}
		case "inet_protocols":
			if s != "ipv4" && s != "ipv6" && s != "all" {
				return errInvalid("Postfix: protocol tidak valid")
			}
		case "mynetworks":
			for _, a := range strings.Split(s, ",") {
				a = strings.TrimSpace(a)
				if strings.HasPrefix(a, "[") {
					end := strings.Index(a, "]/")
					if end < 0 || !strings.Contains(a[1:end], ":") {
						return errInvalid("Postfix: CIDR tidak valid")
					}
					a = a[1:end] + a[end+1:]
				}
				ip, n, e := net.ParseCIDR(a)
				if e != nil || !ip.Equal(n.IP) {
					return errInvalid("Postfix: CIDR tidak valid")
				}
				ones, _ := n.Mask.Size()
				if ones == 0 {
					return errInvalid("Postfix: jaringan publik tidak diizinkan")
				}
			}
		default:
			if s != postfixDefaults[k] {
				return errInvalid("Postfix: %s hanya baca", k)
			}
		}
	}
	return nil
}

type postfixFile struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotPostfixFiles() ([]postfixFile, error) {
	files := []postfixFile{}
	for _, path := range []string{postfixPath, postfixTransport, postfixTransport + ".db"} {
		// Transport source 0640 (root:root, Postfix baca via postmap),
		// database 0600 (hanya root perlu akses langsung).
		defaultMode := os.FileMode(0640)
		if strings.HasSuffix(path, ".db") {
			defaultMode = 0600
		}
		f := postfixFile{path: path, mode: defaultMode}
		st, e := os.Lstat(path)
		if os.IsNotExist(e) && path != postfixPath {
			files = append(files, f)
			continue
		}
		if e != nil {
			return nil, e
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !st.Mode().IsRegular() || !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) {
			return nil, errInvalid("Postfix: file atau pemilik tidak didukung: %s", path)
		}
		f.data, e = os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		f.exists, f.mode = true, st.Mode().Perm()
		files = append(files, f)
	}
	return files, nil
}
func postfixFilesRevision(files []postfixFile) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%t:%d:%d:", f.exists, f.mode, len(f.data))
		h.Write(f.data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
func renderPostfixRoutes(routes []helperproto.PostfixRoute) ([]byte, string, error) {
	if len(routes) == 0 || len(routes) > 100 {
		return nil, "", errInvalid("Postfix: isi 1–100 domain tujuan")
	}
	seen := map[string]bool{}
	var lines, domains []string
	for _, r := range routes {
		d := strings.ToLower(r.Domain)
		if !postfixHostname(d) || !strings.Contains(d, ".") || seen[d] || r.Port < 1 || r.Port > 65535 || !(net.ParseIP(r.Host) != nil || postfixHostname(r.Host)) {
			return nil, "", errInvalid("Postfix: domain, host, atau port tujuan tidak valid")
		}
		seen[d] = true
		domains = append(domains, d)
		host := r.Host
		// smtp(8) requires the ipv6: tag, including IPv4-mapped IPv6 literals.
		if strings.Contains(host, ":") {
			host = "ipv6:" + host
		}
		lines = append(lines, fmt.Sprintf("%s smtp:[%s]:%d", d, host, r.Port))
	}
	return []byte(strings.Join(lines, "\n") + "\n"), strings.Join(domains, ", "), nil
}
func parsePostfixRoutes(b []byte) ([]helperproto.PostfixRoute, error) {
	routes := []helperproto.PostfixRoute{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Fields(line)
		if len(p) != 2 || !strings.HasPrefix(p[1], "smtp:[") {
			return nil, errInvalid("Postfix: transport map tidak didukung")
		}
		i := strings.LastIndex(p[1], "]:")
		if i < 6 {
			return nil, errInvalid("Postfix: transport map tidak didukung")
		}
		port, e := strconv.Atoi(p[1][i+2:])
		if e != nil {
			return nil, e
		}
		host := p[1][6:i]
		if strings.HasPrefix(host, "ipv6:") {
			host = strings.TrimPrefix(host, "ipv6:")
			if !strings.Contains(host, ":") || net.ParseIP(host) == nil {
				return nil, errInvalid("Postfix: alamat IPv6 tidak valid")
			}
		}
		// Accept legacy untagged literals so existing panel configs can be repaired.
		routes = append(routes, helperproto.PostfixRoute{Domain: p[0], Host: host, Port: port})
	}
	if len(routes) > 0 {
		if _, _, e := renderPostfixRoutes(routes); e != nil {
			return nil, e
		}
	}
	return routes, nil
}
func postfixRead() (helperproto.PostfixConfig, error) {
	files, e := snapshotPostfixFiles()
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	v := map[string]string{}
	for _, k := range postfixKeys {
		r, e := postfixRun("postconf", "-h", k)
		if e != nil {
			return helperproto.PostfixConfig{}, e
		}
		v[k] = strings.TrimSpace(r.Stdout)
	}
	routes, e := parsePostfixRoutes(files[1].data)
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	return helperproto.PostfixConfig{Values: v, Routes: routes, Revision: postfixFilesRevision(files), TransportExists: files[1].exists && files[2].exists}, nil
}

// Preserve unrelated directives, comments, and continuation lines verbatim.
func renderPostfix(b []byte, v map[string]string) []byte {
	var out []string
	skip := false
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if !skip {
				out = append(out, line)
			}
			continue
		}
		skip = false
		if i := strings.IndexByte(line, '='); i >= 0 {
			_, skip = v[strings.TrimSpace(line[:i])]
		}
		if !skip {
			out = append(out, line)
		}
	}
	for _, k := range postfixKeys {
		out = append(out, k+" = "+v[k])
	}
	return []byte(strings.Join(out, "\n") + "\n")
}
func postfixSave(in helperproto.PostfixConfig) (helperproto.PostfixConfig, error) {
	transport, domains, e := renderPostfixRoutes(in.Routes)
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	v := map[string]string{}
	for k, s := range in.Values {
		v[k] = s
	}
	v["relay_domains"] = domains
	in.Values = v
	for _, r := range in.Routes {
		for _, d := range strings.Split(v["mydestination"], ",") {
			if strings.EqualFold(strings.TrimSpace(d), r.Domain) {
				return helperproto.PostfixConfig{}, errInvalid("Postfix: domain forwarding tidak boleh menjadi mydestination")
			}
		}
	}
	if e := validatePostfix(in.Values); e != nil {
		return helperproto.PostfixConfig{}, e
	}
	// ponytail: always confirm restart; avoids guessing effective interface defaults.
	if !in.Restart {
		return helperproto.PostfixConfig{}, errInvalid("Postfix: konfirmasi restart wajib")
	}
	lock, e := os.OpenFile(postfixPath+".linux-dashboard.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return helperproto.PostfixConfig{}, e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	files, e := snapshotPostfixFiles()
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	b := files[0].data
	if in.Revision != postfixFilesRevision(files) {
		return helperproto.PostfixConfig{}, errInvalid("Postfix: konfigurasi berubah; muat ulang")
	}
	// Private sibling directory keeps postmap away from the active hash DB.
	stage, e := os.MkdirTemp(filepath.Dir(postfixTransport), ".linux-dashboard-postfix-*")
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	defer os.RemoveAll(stage)
	stagedTransport := filepath.Join(stage, "transport")
	if e = os.WriteFile(stagedTransport, transport, 0600); e != nil {
		return helperproto.PostfixConfig{}, e
	}
	if _, e = postfixRun("postmap", "hash:"+stagedTransport); e != nil {
		return helperproto.PostfixConfig{}, e
	}
	db, e := os.OpenFile(stagedTransport+".db", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	defer db.Close()
	st, e := db.Stat()
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) {
		return helperproto.PostfixConfig{}, errInvalid("Postfix: database transport tidak valid")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(transport), "\n"), "\n") {
		parts := strings.Fields(line)
		result, err := postfixRun("postmap", "-q", parts[0], "hash:"+stagedTransport)
		if err != nil {
			return helperproto.PostfixConfig{}, err
		}
		if strings.TrimSpace(result.Stdout) != parts[1] {
			return helperproto.PostfixConfig{}, errInvalid("Postfix: database transport tidak cocok")
		}
	}
	// Read the validated descriptor, not a potentially replaced pathname.
	database, e := io.ReadAll(db)
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	for _, f := range files {
		if f.exists {
			if e = tulisAtomik(f.path+".linux-dashboard.bak", f.data, 0600); e != nil {
				return helperproto.PostfixConfig{}, e
			}
		}
	}
	rollback := func(cause error, restart bool) (helperproto.PostfixConfig, error) {
		var restore error
		for _, f := range files {
			var e error
			if f.exists {
				e = tulisAtomik(f.path, f.data, f.mode)
			} else {
				e = os.Remove(f.path)
				if os.IsNotExist(e) {
					e = nil
				}
			}
			restore = errors.Join(restore, e)
		}
		if restore == nil && restart {
			_, restore = postfixRun("systemctl", "restart", "postfix")
		}
		return helperproto.PostfixConfig{}, errors.Join(cause, restore)
	}
	current, e := snapshotPostfixFiles()
	if e != nil {
		return helperproto.PostfixConfig{}, e
	}
	if postfixFilesRevision(current) != in.Revision {
		return helperproto.PostfixConfig{}, errInvalid("Postfix: konfigurasi berubah; muat ulang")
	}
	if e = tulisAtomik(postfixTransport, transport, files[1].mode); e != nil {
		return rollback(e, false)
	}
	if e = tulisAtomik(postfixTransport+".db", database, files[2].mode); e != nil {
		return rollback(e, true)
	}
	if e = tulisAtomik(postfixPath, renderPostfix(b, in.Values), files[0].mode); e != nil {
		return rollback(e, true)
	}
	if _, e = postfixRun("postfix", "check"); e != nil {
		return rollback(e, true)
	}
	if _, e = postfixRun("systemctl", "restart", "postfix"); e != nil {
		return rollback(e, true)
	}
	out, e := postfixRead()
	if e != nil {
		return rollback(e, true)
	}
	return out, nil
}
