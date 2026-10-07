package helper

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

const mailcowRepo = "https://github.com/mailcow/mailcow-dockerized"
const mailcowMarker = ".linux-dashboard-mailcow"
const mailcowProject = "mailcowdockerized"
const catatanMailcow = "Instalasi baru: HTTPS 0.0.0.0:8443 dan HTTP 0.0.0.0:8080 pada semua interface; akses memakai IP/domain server, bukan 0.0.0.0. HTTP tidak terenkripsi. Batasi jaringan ke perangkat tepercaya; login vendor admin / moohoo, segera ganti password dan aktifkan 2FA sebelum membuka akses internet. Instalasi existing mempertahankan binding. HTTPS memakai sertifikat self-signed sampai TLS dikonfigurasi. Installer menolak perubahan daemon.json/restart Docker; siapkan Docker IPv6 secara manual bila generator meminta. DNS A/MX/PTR, port SMTP 25, TLS mail dan firewall Docker forwarding wajib disiapkan; UFW INPUT tidak membatasi port Docker. Port 80 challenge ACME perlu reverse proxy ke 127.0.0.1:8080 atau pasang sertifikat sendiri. Uninstall mempertahankan checkout, konfigurasi dan volume; purge menghapus semuanya."

var mailcowDir = "/opt/mailcow-dockerized"
var mailcowLifecycle sync.Mutex
var mailcowOwnerUID uint32 = 0
var mailcowExec = mailcowRunBounded
var mailcowRunTimeout = 45 * time.Minute
var mailcowHostRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$`)

func validMailcowHostname(host string) error {
	if len(host) > 253 || !mailcowHostRE.MatchString(host) || strings.Count(host, ".") < 2 {
		return errInvalid("mailcow_hostname wajib FQDN subdomain, misalnya mail.example.com")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errInvalid("mailcow_hostname tidak valid")
		}
	}
	// Reject IPs, numeric TLDs and private names, not silently repair input.
	tld := host[strings.LastIndex(host, ".")+1:]
	for _, c := range tld {
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				return errInvalid("mailcow_hostname harus domain DNS publik")
			}
		}
	}
	switch strings.ToLower(tld) {
	case "local", "localhost", "internal", "lan", "test", "invalid":
		return errInvalid("mailcow_hostname harus domain DNS publik")
	}
	return nil
}

func mailcowSafePath(path string, directory bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != mailcowOwnerUID || fi.Mode()&0o022 != 0 || fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() != directory || (!directory && (!fi.Mode().IsRegular() || st.Nlink != 1)) {
		return errInvalid("mailcow path tidak aman: %s (root-owned, tanpa symlink atau group/world write)", path)
	}
	return nil
}

func mailcowOwned() error {
	for p := filepath.Dir(mailcowDir); mailcowOwnerUID == 0; p = filepath.Dir(p) {
		if err := mailcowSafePath(p, true); err != nil {
			return err
		}
		if p == "/" {
			break
		}
	}
	if _, err := os.Lstat(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled")); err == nil {
		if err := mailcowSafePath(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), false); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, p := range []string{filepath.Dir(mailcowDir), mailcowDir} {
		if err := mailcowSafePath(p, true); err != nil {
			return err
		}
	}
	for _, p := range []string{mailcowMarker, "docker-compose.yml"} {
		if err := mailcowSafePath(filepath.Join(mailcowDir, p), false); err != nil {
			return err
		}
	}
	marker, err := os.ReadFile(filepath.Join(mailcowDir, mailcowMarker))
	if err != nil || string(marker) != mailcowRepo+"\n" {
		return errInvalid("checkout mailcow bukan milik panel")
	}
	return nil
}

func mailcowManaged() error {
	if err := mailcowOwned(); err != nil {
		return err
	}
	path := filepath.Join(mailcowDir, "mailcow.conf")
	if err := mailcowSafePath(path, false); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0077 != 0 {
		return errInvalid("mailcow.conf wajib private 0600 sebelum startup")
	}
	return nil
}

func mailcowCompose(args ...string) (helperproto.ExecResult, error) {
	// Explicit file/project: never execute an arbitrary COMPOSE_FILE inherited from a session.
	argv := []string{"compose", "--project-name", mailcowProject, "--file", filepath.Join(mailcowDir, "docker-compose.yml")}
	override := filepath.Join(mailcowDir, "docker-compose.override.yml")
	if _, err := os.Lstat(override); err == nil {
		if err := mailcowSafePath(override, false); err != nil {
			return helperproto.ExecResult{}, err
		}
		argv = append(argv, "--file", override)
	} else if !os.IsNotExist(err) {
		return helperproto.ExecResult{}, err
	}
	argv = append(argv, args...)
	return mailcowExec(mailcowDir, nil, "docker", argv...)
}
func mailcowTerpasang() bool {
	if mailcowManaged() != nil {
		return false
	}
	// Any pending/corrupt sentinel fails closed, without runtime probes.
	for _, marker := range []string{".linux-dashboard-uninstalled", ".linux-dashboard-config-pending"} {
		if _, err := os.Lstat(filepath.Join(mailcowDir, marker)); !os.IsNotExist(err) {
			return false
		}
	}
	return true
}
func versiMailcow() string {
	if mailcowManaged() != nil {
		return ""
	}
	r, e := mailcowExec(mailcowDir, nil, "git", "describe", "--tags", "--always")
	if e != nil {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}
func mailcowRunning() bool {
	if mailcowManaged() != nil {
		return false
	}
	expected, e := mailcowCompose("config", "--services")
	if e != nil || len(strings.Fields(expected.Stdout)) == 0 {
		return false
	}
	r, e := mailcowCompose("ps", "--all", "--format", "json")
	if e != nil {
		return false
	}
	var containers []struct{ Service, State, Health string }
	// Compose 2.18 emits an array; newer releases emit one JSON object per line.
	if strings.HasPrefix(strings.TrimSpace(r.Stdout), "[") {
		if json.Unmarshal([]byte(r.Stdout), &containers) != nil {
			return false
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(r.Stdout))
		for {
			var container struct{ Service, State, Health string }
			if err := decoder.Decode(&container); err == io.EOF {
				break
			} else if err != nil {
				return false
			}
			containers = append(containers, container)
		}
	}
	services := map[string]bool{}
	for _, c := range containers {
		if c.State != "running" || (c.Health != "" && c.Health != "healthy") {
			return false
		}
		services[c.Service] = true
	}
	for _, s := range strings.Fields(expected.Stdout) {
		if !services[s] {
			return false
		}
	}
	return true
}
func mailcowWebURL() string {
	if mailcowManaged() != nil {
		return ""
	}
	b, e := os.ReadFile(filepath.Join(mailcowDir, "mailcow.conf"))
	if e != nil {
		return ""
	}
	values := mailcowConfigValues(string(b))
	host, bind := values["MAILCOW_HOSTNAME"], values["HTTPS_BIND"]
	port, err := strconv.Atoi(values["HTTPS_PORT"])
	if _, ok := values["HTTPS_BIND"]; !ok || validMailcowHostname(host) != nil || err != nil || port < 1 || port > 65535 || bind == "127.0.0.1" || bind == "::1" {
		return ""
	}
	if port == 443 {
		return "https://" + host + "/admin"
	}
	return "https://" + host + ":" + strconv.Itoa(port) + "/admin"
}

func mailcowConfigValues(conf string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(conf, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	return values
}

func mailcowPreflight() error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return errInvalid("mailcow hanya mendukung x86_64 dan ARM64")
	}
	virt, e := mailcowExec("", nil, "systemd-detect-virt")
	if e != nil && strings.TrimSpace(virt.Stdout) != "none" {
		return errInvalid("tidak dapat memeriksa virtualisasi mailcow")
	}
	switch strings.TrimSpace(virt.Stdout) {
	case "lxc", "lxc-libvirt", "openvz":
		return errInvalid("mailcow tidak mendukung LXC/OpenVZ")
	}
	var info syscall.Sysinfo_t
	if e := syscall.Sysinfo(&info); e != nil {
		return e
	}
	if uint64(info.Totalram)*uint64(info.Unit) < 6<<30 || uint64(info.Totalswap)*uint64(info.Unit) < 1<<30 {
		return errInvalid("mailcow memerlukan minimal 6 GiB RAM dan 1 GiB swap")
	}
	var disk syscall.Statfs_t
	if e := syscall.Statfs(filepath.Dir(mailcowDir), &disk); e != nil {
		return e
	}
	if disk.Bavail*uint64(disk.Bsize) < 20<<30 {
		return errInvalid("mailcow memerlukan minimal 20 GiB disk bebas")
	}
	// Include HTTP and vendor auxiliary bindings in conflict detection,
	// but do not advertise them as public firewall ports.
	c := *components["mailcow"]
	c.ports = append(append([]portKomponen{}, c.ports...), portKomponen{"8080", "tcp", ""}, portKomponen{"19991", "tcp", ""}, portKomponen{"13306", "tcp", ""}, portKomponen{"7654", "tcp", ""})
	return preflightKonflikPort(&c)
}

func mailcowDockerReady() error {
	r, e := mailcowExec("", nil, "docker", "version", "--format", "{{.Server.Version}}")
	major, _ := strconv.Atoi(strings.Split(strings.TrimSpace(r.Stdout), ".")[0])
	if e != nil || major < 24 {
		return errInvalid("mailcow memerlukan Docker Engine >=24; pasang/perbarui Docker resmi melalui Components")
	}
	r, e = mailcowExec("", nil, "docker", "compose", "version", "--short")
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(r.Stdout), "v"), ".")
	major, _ = strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if e != nil || major < 2 || (major == 2 && minor < 18) {
		return errInvalid("mailcow memerlukan Docker Compose plugin >=2.18 (--wait-timeout)")
	}
	return nil
}

func mailcowLocalConfig(conf string) (string, error) {
	for _, pair := range [][2]string{{"HTTP_PORT=80", "HTTP_PORT=8080"}, {"HTTP_BIND=", "HTTP_BIND=0.0.0.0"}, {"HTTPS_PORT=443", "HTTPS_PORT=8443"}, {"HTTPS_BIND=", "HTTPS_BIND=0.0.0.0"}} {
		key, _, _ := strings.Cut(pair[0], "=")
		lines := strings.Split(conf, "\n")
		count := 0
		for i, line := range lines {
			if strings.HasPrefix(line, key+"=") {
				if line != pair[0] && line != pair[1] {
					return "", errInvalid("format konfigurasi vendor mailcow berubah; hentikan sebelum startup")
				}
				count++
				lines[i] = pair[1]
			}
		}
		if count != 1 {
			return "", errInvalid("format konfigurasi vendor mailcow berubah; hentikan sebelum startup")
		}
		conf = strings.Join(lines, "\n")
	}
	return conf, nil
}

// Only these vendor-generated public assets need non-root container reads.
// TLS private keys remain 0600; vendor TLS services read them as root.
func mailcowGeneratedPermissions() error {
	for name, mode := range map[string]os.FileMode{
		"data/assets/ssl": 0755, "data/assets/ssl-example": 0755,
		"data/assets/ssl/cert.pem": 0644, "data/assets/ssl-example/cert.pem": 0644,
		"data/web/inc/app_info.inc.php": 0644,
	} {
		path := filepath.Join(mailcowDir, name)
		fi, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for p := filepath.Dir(path); p != mailcowDir; p = filepath.Dir(p) {
			if err := mailcowSafePath(p, true); err != nil {
				return err
			}
		}
		if err := mailcowSafePath(path, fi.IsDir()); err != nil {
			return err
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
	}
	return nil
}

func mailcowWriteConfig(data []byte) error {
	if err := mailcowSafePath(mailcowDir, true); err != nil {
		return err
	}
	dfd, err := unix.Open(mailcowDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dfd)
	var st unix.Stat_t
	if err := unix.Fstat(dfd, &st); err != nil {
		return err
	}
	if st.Uid != mailcowOwnerUID || st.Mode&0022 != 0 {
		return errInvalid("direktori mailcow tidak aman")
	}
	if err := unix.Fstatat(dfd, "mailcow.conf", &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Uid != mailcowOwnerUID || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || st.Nlink != 1 {
		return errInvalid("konfigurasi mailcow tidak aman")
	}
	tmp := ".mailcow-conf-" + rand.Text()
	fd, err := unix.Openat(dfd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(dfd, tmp, 0)
	f := os.NewFile(uintptr(fd), tmp)
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return unix.Renameat(dfd, tmp, dfd, "mailcow.conf")
}

// Never echo Compose output: interpolation errors can contain configuration secrets.
func mailcowComposeError(operation string, result helperproto.ExecResult, err error) error {
	diagnostic := strings.ToLower(result.Stderr + "\n" + result.Stdout + "\n" + err.Error())
	reason := "periksa koneksi registry dan konfigurasi Docker"
	switch {
	case strings.Contains(diagnostic, "no such host"), strings.Contains(diagnostic, "temporary failure in name resolution"):
		reason = "DNS registry gagal; periksa DNS pada host dan daemon Docker"
	case strings.Contains(diagnostic, "timeout"), strings.Contains(diagnostic, "deadline exceeded"):
		reason = "timeout registry; periksa jaringan dan proxy daemon Docker"
	case strings.Contains(diagnostic, "unauthorized"), strings.Contains(diagnostic, "denied"):
		reason = "akses registry ditolak; periksa autentikasi dan izin Docker"
	case strings.Contains(diagnostic, "toomanyrequests"), strings.Contains(diagnostic, "too many requests"), strings.Contains(diagnostic, "429"):
		reason = "rate limit registry; tunggu sebelum mencoba kembali"
	case strings.Contains(diagnostic, "no space left on device"):
		reason = "disk Docker penuh; periksa ruang dan inode"
	case strings.Contains(diagnostic, "interpolation"), strings.Contains(diagnostic, "invalid compose"), strings.Contains(diagnostic, "required variable"):
		reason = "konfigurasi Compose tidak valid; periksa mailcow.conf tanpa membagikan secret"
	case strings.Contains(diagnostic, "x509"), strings.Contains(diagnostic, "tls handshake"):
		reason = "TLS registry gagal; periksa waktu sistem dan CA Docker"
	}
	return &helperErr{code: helperproto.ErrInternal, msg: "docker compose " + operation + " mailcow gagal (exit code " + strconv.Itoa(result.ExitCode) + "): " + reason}
}

func installMailcow(host string) error {
	mailcowLifecycle.Lock()
	defer mailcowLifecycle.Unlock()
	if err := validMailcowHostname(host); err != nil {
		return err
	}
	if err := mailcowSafePath(filepath.Dir(mailcowDir), true); err != nil {
		return err
	}
	if _, e := os.Lstat(mailcowDir); e == nil {
		if err := mailcowOwned(); err != nil {
			return err
		}
		if b, err := os.ReadFile(filepath.Join(mailcowDir, "mailcow.conf")); err == nil {
			if err := mailcowSafePath(filepath.Join(mailcowDir, "mailcow.conf"), false); err != nil {
				return err
			}
			if mailcowConfigValues(string(b))["MAILCOW_HOSTNAME"] != host {
				return errInvalid("hostname berbeda dari konfigurasi mailcow yang dipertahankan")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := mailcowDockerReady(); err != nil {
			return err
		}
	} else {
		if !os.IsNotExist(e) {
			return e
		}
		if err := mailcowPreflight(); err != nil {
			return err
		}
		tahapBaru("memasang dependensi mailcow")
		if err := aptInstall("git", "openssl", "curl", "gawk", "coreutils", "grep", "jq"); err != nil {
			return err
		}
		if _, ok := lookBinary("docker"); !ok {
			if err := installDocker(); err != nil {
				return err
			}
		}
		if err := mailcowDockerReady(); err != nil {
			return err
		}
		tahapBaru("mengunduh checkout resmi mailcow")
		if _, err := mailcowExec(filepath.Dir(mailcowDir), []string{"GIT_TERMINAL_PROMPT=0"}, "git", "clone", "--branch", "master", mailcowRepo, mailcowDir); err != nil {
			return err
		}
		if err := mailcowSafePath(mailcowDir, true); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(mailcowDir, mailcowMarker), []byte(mailcowRepo+"\n"), 0o600); err != nil {
			return err
		}
	}
	// Keep a completed installation registered during an interrupted re-up.
	// Incomplete/reinstalled checkouts stay hidden until health verification commits.
	if !mailcowTerpasang() {
		if err := os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), []byte("installation pending\n"), 0o600); err != nil {
			return err
		}
	}
	path := filepath.Join(mailcowDir, "mailcow.conf")
	pending := filepath.Join(mailcowDir, ".linux-dashboard-config-pending")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if err := os.WriteFile(pending, []byte("pending\n"), 0o600); err != nil {
			return err
		}
		// --dev keeps the verified checkout; stdin explicitly declines Docker changes.
		tahapBaru("generate_config.sh resmi mailcow")
		if _, err := mailcowExec(mailcowDir, []string{"MAILCOW_HOSTNAME=" + host, "MAILCOW_TZ=Etc/UTC", "SKIP_CLAMD=n", "GIT_TERMINAL_PROMPT=0"}, "bash", "./generate_config.sh", "--dev"); err != nil {
			return errInvalid("generate_config.sh gagal; perubahan daemon Docker ditolak. Siapkan Docker IPv6 secara manual bila diperlukan, lalu coba lagi")
		}
	} else if err != nil {
		return err
	}
	if _, err := os.Lstat(pending); err == nil {
		if err := mailcowSafePath(pending, false); err != nil {
			return err
		}
		if err := mailcowSafePath(path, false); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		conf, err := mailcowLocalConfig(string(b))
		if err != nil {
			return err
		}
		if err := mailcowWriteConfig([]byte(conf)); err != nil {
			return err
		}
		if err := mailcowGeneratedPermissions(); err != nil {
			return err
		}
		if err := os.Remove(pending); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := mailcowManaged(); err != nil {
		return err
	}
	tahapBaru("docker compose pull mailcow")
	if result, err := mailcowCompose("pull"); err != nil {
		return mailcowComposeError("pull", result, err)
	}
	tahapBaru("docker compose up mailcow; segera ganti admin/moohoo")
	if result, err := mailcowCompose("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return mailcowComposeError("up", result, err)
	}
	if !mailcowRunning() {
		return errInvalid("container inti mailcow belum berjalan; periksa System → Docker")
	}
	if err := os.Remove(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func uninstallMailcow() error {
	mailcowLifecycle.Lock()
	defer mailcowLifecycle.Unlock()
	if _, err := os.Lstat(mailcowDir); os.IsNotExist(err) {
		return nil
	}
	if err := mailcowManaged(); err != nil {
		return err
	}
	if _, err := mailcowCompose("down"); err != nil {
		return errInvalid("compose down mailcow gagal; data dipertahankan")
	}
	return os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), []byte("uninstalled\n"), 0o600)
}
func purgeMailcow() error {
	mailcowLifecycle.Lock()
	defer mailcowLifecycle.Unlock()
	if _, err := os.Lstat(mailcowDir); os.IsNotExist(err) {
		return nil
	}
	if err := mailcowManaged(); err != nil {
		return err
	}
	if _, err := mailcowCompose("down", "--volumes"); err != nil {
		return errInvalid("purge volume mailcow gagal; checkout dipertahankan")
	}
	return os.RemoveAll(mailcowDir)
}

// ponytail: vendor operations remain in the existing async Components job;
// cap hung network/compose processes without exposing generated secrets in errors.
func mailcowRunBounded(dir string, env []string, name string, args ...string) (helperproto.ExecResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mailcowRunTimeout)
	defer cancel()
	umask := "0022"
	if name == "bash" && len(args) > 0 && args[0] == "./generate_config.sh" {
		umask = "0077" // Secrets must be private from their first write, including failures.
	}
	argv := append([]string{"-c", `umask ` + umask + `; exec "$@"`, "mailcow", name}, args...)
	cmd := exec.CommandContext(ctx, "/bin/bash", argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + pathExec, "HOME=/root", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive"}, env...)
	if name == "bash" && len(args) > 0 && args[0] == "./generate_config.sh" {
		// Upstream defaults EOF to Yes for daemon.json edits and Docker restart.
		// All other prompts are bypassed; explicitly decline this one.
		cmd.Stdin = strings.NewReader("n\n")
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := helperproto.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
	}
	return result, err
}
