package helper

import (
	"bufio"
	"encoding/json"
	"errors"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exercise signed frames, capability auth and the real handle/dispatch path.
func mailcowStatusRequest(t *testing.T, s *Server, token string) helperproto.Response {
	t.Helper()
	req := helperproto.Request{Cmd: helperproto.CmdMailcowStatus, Token: token, Username: "root", TS: time.Now().Unix(), Nonce: strconv.FormatInt(time.Now().UnixNano(), 10)}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); s.handle(server) }()
	defer func() { client.Close(); <-done }()
	client.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write(append([]byte(helperproto.Sign(s.secret, payload)+" "), append(payload, '\n')...)); err != nil {
		t.Fatal(err)
	}
	var response helperproto.Response
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestMailcowStatusRealDispatch(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	t.Cleanup(func() { mailcowDir, mailcowExec, mailcowOwnerUID = oldDir, oldExec, oldUID })
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	mailcowDir, mailcowOwnerUID = filepath.Join(parent, "mailcow"), uint32(os.Getuid())
	if err := os.Mkdir(mailcowDir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(mailcowMarker, mailcowRepo+"\n")
	write("docker-compose.yml", "services: {}\n")
	write("mailcow.conf", "MAILCOW_HOSTNAME=mail.example.com\n")
	mailcowExec = func(string, []string, string, ...string) (helperproto.ExecResult, error) {
		t.Error("status must not execute Docker, Git or network probes")
		return helperproto.ExecResult{}, errors.New("no probes")
	}
	s := &Server{secret: []byte("test-only-mailcow-hmac"), seenNonce: map[string]time.Time{}}
	// Read-only install metadata is available to authenticated non-sudo sessions.
	token, _ := s.terbitkanToken(&userInfo{Name: "reader"}, time.Hour)
	for _, invalid := range []string{"", "forged"} {
		if r := mailcowStatusRequest(t, s, invalid); r.OK || r.Code != helperproto.ErrSesiTidakValid {
			t.Fatalf("auth bypass: %+v", r)
		}
	}
	for _, tc := range []struct {
		name, marker, pending string
		installed             bool
	}{
		{"complete", "", "", true},
		{"uninstalled", "uninstalled\n", "", false},
		{"bootstrap", "installation pending\n", "", false},
		{"corrupt", "corrupt!!!!\n", "", false},
		{"generation", "", "pending\n", false},
		{"corrupt-generation", "", "broken", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			os.Remove(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"))
			os.Remove(filepath.Join(mailcowDir, ".linux-dashboard-config-pending"))
			if tc.marker != "" {
				write(".linux-dashboard-uninstalled", tc.marker)
			}
			if tc.pending != "" {
				write(".linux-dashboard-config-pending", tc.pending)
			}
			r := mailcowStatusRequest(t, s, token)
			var status helperproto.ComponentStatus
			if !r.OK || json.Unmarshal(r.Data, &status) != nil || status.Name != "mailcow" || status.Installed != tc.installed {
				t.Fatalf("status contract: %+v data=%s", r, r.Data)
			}
		})
	}
	os.Remove(filepath.Join(mailcowDir, ".linux-dashboard-config-pending"))
	os.Remove(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"))
	write(mailcowMarker, "corrupt\n")
	r := mailcowStatusRequest(t, s, token)
	var status helperproto.ComponentStatus
	if !r.OK || json.Unmarshal(r.Data, &status) != nil || status.Installed {
		t.Fatalf("corrupt ownership accepted: %+v", r)
	}
}

func TestMailcowRunnerPreservesFailureDiagnostics(t *testing.T) {
	r, err := mailcowRunBounded(t.TempDir(), nil, "bash", "-c", "printf output; printf 'registry unavailable' >&2; exit 7")
	if err == nil || r.Stdout != "output" || r.Stderr != "registry unavailable" || r.ExitCode != 7 {
		t.Fatalf("missing runner diagnostics: %+v err=%v", r, err)
	}
}

func TestMailcowGeneratorDeclinesDockerChanges(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "generate_config.sh"), []byte("#!/bin/bash\nread -r answer\nprintf '%s' \"$answer\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := mailcowRunBounded(dir, nil, "bash", "./generate_config.sh", "--dev")
	if err != nil || r.Stdout != "n" {
		t.Fatalf("generator must decline Docker mutation: stdout=%q err=%v", r.Stdout, err)
	}
}

func TestMailcowGeneratorSecretsPrivateAtCreation(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/bash\nprintf 'DBPASS=fixture\\n' > mailcow.conf\nstat -c %a mailcow.conf\n"
	if err := os.WriteFile(filepath.Join(dir, "generate_config.sh"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := mailcowRunBounded(dir, nil, "bash", "./generate_config.sh", "--dev")
	if err != nil || strings.TrimSpace(r.Stdout) != "600" {
		t.Fatalf("secret initially public: mode=%q err=%v", r.Stdout, err)
	}
}

func TestMailcowTimeoutKillsSubtree(t *testing.T) {
	old := mailcowRunTimeout
	mailcowRunTimeout = 150 * time.Millisecond
	defer func() { mailcowRunTimeout = old }()
	dir := t.TempDir()
	start := time.Now()
	r, err := mailcowRunBounded(dir, nil, "bash", "-c", "sleep 3 & echo $!; wait")
	pid, _ := strconv.Atoi(strings.TrimSpace(r.Stdout))
	if pid > 0 {
		defer syscall.Kill(pid, syscall.SIGKILL)
	}
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout unbounded: %v %s", err, time.Since(start))
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil && !strings.Contains(string(b), ") Z ") {
		t.Fatal("descendant still alive")
	}
}

func TestMailcowGeneratedMountPermissions(t *testing.T) {
	oldDir, oldUID := mailcowDir, mailcowOwnerUID
	defer func() { mailcowDir, mailcowOwnerUID = oldDir, oldUID }()
	mailcowDir, mailcowOwnerUID = t.TempDir(), uint32(os.Getuid())
	os.Chmod(mailcowDir, 0700)
	for _, name := range []string{"data/assets/ssl/cert.pem", "data/assets/ssl/key.pem", "data/assets/ssl-example/cert.pem", "data/assets/ssl-example/key.pem", "data/web/inc/app_info.inc.php"} {
		os.MkdirAll(filepath.Dir(filepath.Join(mailcowDir, name)), 0755)
		os.WriteFile(filepath.Join(mailcowDir, name), []byte("fixture"), 0600)
	}
	if err := mailcowGeneratedPermissions(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"data/assets/ssl/cert.pem": 0644, "data/assets/ssl/key.pem": 0600, "data/web/inc/app_info.inc.php": 0644} {
		fi, _ := os.Stat(filepath.Join(mailcowDir, name))
		if fi.Mode().Perm() != want {
			t.Fatalf("%s mode=%o", name, fi.Mode().Perm())
		}
	}
}

func TestMailcowLocalConfig(t *testing.T) {
	vendor := "HTTP_PORT=80\nHTTP_BIND=\nHTTPS_PORT=443\nHTTPS_BIND=\nDBPASS=keep\n"
	conf, err := mailcowLocalConfig(vendor)
	if err != nil {
		t.Fatal(err)
	}
	if conf != "HTTP_PORT=8080\nHTTP_BIND=0.0.0.0\nHTTPS_PORT=8443\nHTTPS_BIND=0.0.0.0\nDBPASS=keep\n" {
		t.Fatalf("unexpected generated config: %q", conf)
	}
	if again, err := mailcowLocalConfig(conf); err != nil || again != conf {
		t.Fatal("binding rewrite not idempotent")
	}
	for _, invalid := range []string{strings.Replace(vendor, "HTTPS_BIND=\n", "HTTPS_BIND=192.168.1.1\n", 1), vendor + "HTTPS_BIND=\n"} {
		if _, err := mailcowLocalConfig(invalid); err == nil {
			t.Fatal("changed vendor format accepted")
		}
	}
}

func TestMailcowGenerationRetry(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	defer func() { mailcowDir = oldDir; mailcowExec = oldExec; mailcowOwnerUID = oldUID }()
	mailcowOwnerUID = uint32(os.Getuid())
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	mailcowDir = filepath.Join(parent, "mailcow")
	os.Mkdir(mailcowDir, 0700)
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "docker-compose.yml": "services: {}\n"} {
		os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600)
	}
	generations := 0
	mailcowExec = func(_ string, _ []string, name string, args ...string) (helperproto.ExecResult, error) {
		call := strings.Join(args, " ")
		if name == "git" {
			t.Fatal("retry cloned owned checkout")
		}
		if name == "bash" {
			if mailcowTerpasang() {
				t.Fatal("generation registered before bootstrap completion")
			}
			if _, err := os.Stat(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled")); err != nil {
				t.Fatal("missing installation pending marker during generation", err)
			}
			generations++
			if call != "./generate_config.sh --dev" {
				t.Fatalf("generator invocation: %q", call)
			}
			if generations == 1 {
				return helperproto.ExecResult{}, errors.New("generation failed")
			}
			os.WriteFile(filepath.Join(mailcowDir, "mailcow.conf"), []byte("MAILCOW_HOSTNAME=mail.example.com\nHTTP_PORT=80\nHTTP_BIND=\nHTTPS_PORT=443\nHTTPS_BIND=\nDBPASS=keep\n"), 0600)
		}
		if call == "version --format {{.Server.Version}}" {
			return helperproto.ExecResult{Stdout: "24.0.0"}, nil
		}
		if call == "compose version --short" {
			return helperproto.ExecResult{Stdout: "2.18.0"}, nil
		}
		if strings.HasSuffix(call, "ps --all --format json") {
			return helperproto.ExecResult{Stdout: `[{"Service":"nginx-mailcow","State":"running","Health":"healthy"},{"Service":"postfix-mailcow","State":"running","Health":""},{"Service":"dovecot-mailcow","State":"running","Health":"healthy"},{"Service":"mysql-mailcow","State":"running","Health":"healthy"}]`}, nil
		}
		if strings.HasSuffix(call, "config --services") {
			return helperproto.ExecResult{Stdout: "nginx-mailcow\npostfix-mailcow\ndovecot-mailcow\nmysql-mailcow\n"}, nil
		}
		if strings.Contains(call, " up ") && !strings.HasSuffix(call, "up -d --wait --wait-timeout 300") {
			t.Fatal("startup did not wait for health")
		}
		return helperproto.ExecResult{}, nil
	}
	if installMailcow("mail.example.com") == nil {
		t.Fatal("generation failure accepted")
	}
	if err := installMailcow("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	if generations != 2 {
		t.Fatalf("retry generation count=%d", generations)
	}
	conf, _ := os.ReadFile(filepath.Join(mailcowDir, "mailcow.conf"))
	if !strings.Contains(string(conf), "HTTPS_BIND=0.0.0.0\n") {
		t.Fatal("retry did not apply requested installation binding")
	}
	if err := installMailcow("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	if generations != 2 {
		t.Fatal("reinstall overwrote configuration")
	}
	// Reinstallation must not expose an existing loopback-only configuration.
	private := strings.ReplaceAll(string(conf), "BIND=0.0.0.0", "BIND=127.0.0.1")
	if err := os.WriteFile(filepath.Join(mailcowDir, "mailcow.conf"), []byte(private), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installMailcow("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	preserved, err := os.ReadFile(filepath.Join(mailcowDir, "mailcow.conf"))
	if err != nil || string(preserved) != private {
		t.Fatal("reinstall changed existing bindings")
	}
	mailcowExec = func(_ string, _ []string, _ string, args ...string) (helperproto.ExecResult, error) {
		if strings.HasSuffix(strings.Join(args, " "), "config --services") {
			return helperproto.ExecResult{Stdout: "nginx-mailcow\npostfix-mailcow\ndovecot-mailcow\nmysql-mailcow\nunbound-mailcow\n"}, nil
		}
		return helperproto.ExecResult{Stdout: "nginx-mailcow\npostfix-mailcow\ndovecot-mailcow\nmysql-mailcow\n"}, nil
	}
	if mailcowRunning() {
		t.Fatal("missing fifth service considered healthy")
	}
	mailcowExec = func(_ string, _ []string, _ string, args ...string) (helperproto.ExecResult, error) {
		if strings.HasSuffix(strings.Join(args, " "), "config --services") {
			return helperproto.ExecResult{Stdout: "nginx-mailcow\n"}, nil
		}
		return helperproto.ExecResult{Stdout: `{"Service":"nginx-mailcow","State":"running","Health":"unhealthy"}`}, nil
	}
	if mailcowRunning() {
		t.Fatal("unhealthy container considered ready")
	}
	os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), []byte("installation pending\n"), 0600)
	if mailcowTerpasang() {
		t.Fatal("partial bootstrap considered installed")
	}
	os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), []byte("uninstalled\n"), 0600)
	if mailcowTerpasang() {
		t.Fatal("explicit uninstall remains visible")
	}
}

func TestMailcowConfigRewriteAtomic(t *testing.T) {
	oldDir, oldUID := mailcowDir, mailcowOwnerUID
	defer func() { mailcowDir, mailcowOwnerUID = oldDir, oldUID }()
	mailcowDir, mailcowOwnerUID = t.TempDir(), uint32(os.Getuid())
	os.Chmod(mailcowDir, 0700)
	path := filepath.Join(mailcowDir, "mailcow.conf")
	os.WriteFile(path, []byte("original"), 0644)
	before, _ := os.Stat(path)
	if err := mailcowWriteConfig([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if os.SameFile(before, after) || after.Mode().Perm() != 0600 {
		t.Fatal("rewrite not atomic/private")
	}
	os.Remove(path)
	os.Symlink("missing", path)
	if mailcowWriteConfig([]byte("unsafe")) == nil {
		t.Fatal("symlink accepted")
	}
	if target, _ := os.Readlink(path); target != "missing" {
		t.Fatal("failure replaced original")
	}
}

func TestMailcowDockerRejectsPendingBeforeExecution(t *testing.T) {
	oldDir := mailcowDir
	defer func() { mailcowDir = oldDir }()
	mailcowDir = t.TempDir()
	os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-config-pending"), []byte("pending\n"), 0600)
	_, err := dockerExec(helperproto.DockerExecArgs{Dir: mailcowDir, Args: []string{"compose", "up", "-d"}})
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("pending bootstrap not refused: %v", err)
	}
}

func mailcowDockerFake(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf 'safe-fake\\n'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	old := pathExec
	pathExec = bin
	t.Cleanup(func() { pathExec = old })
}

func TestMailcowDockerAlias(t *testing.T) {
	mailcowDockerFake(t)
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	t.Cleanup(func() { mailcowDir, mailcowExec, mailcowOwnerUID = oldDir, oldExec, oldUID })
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	mailcowDir, mailcowOwnerUID = filepath.Join(parent, "mailcow"), uint32(os.Getuid())
	if err := os.Mkdir(mailcowDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "docker-compose.yml": "services: {}", "mailcow.conf": "fixture", "docker-compose.override.yml": "services: {}"} {
		if err := os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	external := t.TempDir()
	alias := filepath.Join(external, "alias.yml")
	if err := os.Symlink(filepath.Join(mailcowDir, "docker-compose.yml"), alias); err != nil {
		t.Fatal(err)
	}
	calls := 0
	mailcowExec = func(dir string, env []string, name string, args ...string) (helperproto.ExecResult, error) {
		calls++
		want := "compose --project-name " + mailcowProject + " --file " + filepath.Join(mailcowDir, "docker-compose.yml") + " --file " + filepath.Join(mailcowDir, "docker-compose.override.yml") + " ps"
		if dir != mailcowDir || len(env) != 0 || name != "docker" || strings.Join(args, " ") != want {
			t.Fatalf("alias not normalized: %s %v", dir, args)
		}
		return helperproto.ExecResult{Stdout: "bounded-mailcow"}, nil
	}
	for _, flag := range []string{"-f", "--file", "--env-file"} {
		t.Run(flag, func(t *testing.T) {
			pending := filepath.Join(mailcowDir, ".linux-dashboard-config-pending")
			if err := os.WriteFile(pending, []byte("pending\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := helperproto.DockerExecArgs{Dir: external, Args: []string{"compose", flag, "alias.yml", "-p", "renamed", "up", "-d"}}
			if _, err := dockerExec(args); err == nil || !strings.Contains(err.Error(), "pending") {
				t.Errorf("alias bypassed pending gate: %v", err)
			}
			if err := os.Remove(pending); err != nil {
				t.Fatal(err)
			}
			args.Args = []string{"compose", flag, alias, "--project-name", "renamed", "ps"}
			before := calls
			if res, err := dockerExec(args); err != nil || res.Stdout != "bounded-mailcow" || calls != before+1 {
				t.Errorf("trusted alias bypassed bounded runner: %+v %v", res, err)
			}
			os.Chmod(filepath.Join(mailcowDir, "docker-compose.override.yml"), 0666)
			before = calls
			if _, err := dockerExec(args); err == nil || calls != before {
				t.Error("untrusted override executed")
			}
			os.Chmod(filepath.Join(mailcowDir, "docker-compose.override.yml"), 0600)
		})
	}
}

func TestMailcowDockerRejectsNoncanonicalGlobals(t *testing.T) {
	mailcowDockerFake(t)
	oldDir := mailcowDir
	t.Cleanup(func() { mailcowDir = oldDir })
	mailcowDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-config-pending"), []byte("pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(mailcowDir, "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services: {}"), 0600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	alias := filepath.Join(external, "alias.yml")
	if err := os.Symlink(compose, alias); err != nil {
		t.Fatal(err)
	}
	for _, globals := range [][]string{
		{"--file=" + alias}, {"-f" + alias},
		{"--env-file=" + alias}, {"-prenamed"}, {"--project-name=renamed"},
		{"--project-directory", "up"}, {"--project-directory=" + mailcowDir},
		{"--unknown"}, {"-d"},
	} {
		t.Run(strings.Join(globals, " "), func(t *testing.T) {
			args := append([]string{"compose"}, globals...)
			args = append(args, "-p", "renamed", "up", "-d")
			res, err := dockerExec(helperproto.DockerExecArgs{Dir: external, Args: args})
			if err == nil || !strings.Contains(err.Error(), "tidak diizinkan") {
				t.Errorf("unsupported global not rejected by validator: %v", err)
			}
			if res.Stdout != "" {
				t.Errorf("runIn executed before rejection: %q", res.Stdout)
			}
		})
	}
}

func TestMailcowDockerLifecycleLock(t *testing.T) {
	mailcowDockerFake(t)
	for _, args := range [][]string{{"compose", "ls"}, {"compose", "-p", "unrelated", "ps"}} {
		mailcowLifecycle.Lock()
		done := make(chan helperproto.ExecResult, 1)
		go func() { res, _ := dockerExec(helperproto.DockerExecArgs{Args: args}); done <- res }()
		select {
		case res := <-done:
			mailcowLifecycle.Unlock()
			if strings.TrimSpace(res.Stdout) != "safe-fake" {
				t.Fatal("safe runner not exercised", res)
			}
		case <-time.After(time.Second):
			mailcowLifecycle.Unlock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("Docker lock not released")
			}
			t.Errorf("unrelated Compose read blocked: %v", args)
		}
	}
}

func TestMailcowDockerMutationLifecycleLock(t *testing.T) {
	mailcowDockerFake(t)
	for _, args := range [][]string{{"compose", "-p", mailcowProject, "up"}, {"restart", "container-id"}} {
		mailcowLifecycle.Lock()
		done := make(chan struct{})
		go func() { _, _ = dockerExec(helperproto.DockerExecArgs{Args: args}); close(done) }()
		select {
		case <-done:
			mailcowLifecycle.Unlock()
			t.Fatal("mutation bypassed lifecycle lock", args)
		case <-time.After(30 * time.Millisecond):
		}
		mailcowLifecycle.Unlock()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Docker lock not released")
		}
	}
}

func TestMailcowManagedRejectsPublicSecret(t *testing.T) {
	oldDir, oldUID := mailcowDir, mailcowOwnerUID
	defer func() { mailcowDir, mailcowOwnerUID = oldDir, oldUID }()
	mailcowDir, mailcowOwnerUID = t.TempDir(), uint32(os.Getuid())
	os.Chmod(mailcowDir, 0700)
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "docker-compose.yml": "services: {}", "mailcow.conf": "fixture"} {
		os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0644)
	}
	if mailcowManaged() == nil {
		t.Fatal("public secret accepted before startup")
	}
}

func TestMailcowInputs(t *testing.T) {
	for _, host := range []string{"", "example.com", "127.0.0.1", "mail.example.com\nX=y", "mail.example.com.", "mail.-example.com", "mail.example.local", "mail.example.com/admin"} {
		if validMailcowHostname(host) == nil {
			t.Errorf("accepted unsafe hostname %q", host)
		}
	}
	if err := validMailcowHostname("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	old := mailcowExec
	defer func() { mailcowExec = old }()
	mailcowExec = func(string, []string, string, ...string) (helperproto.ExecResult, error) {
		t.Fatal("invalid input executed command")
		return helperproto.ExecResult{}, nil
	}
	if _, err := installComponent("mailcow", nil); err == nil {
		t.Fatal("missing hostname accepted")
	}
}

func TestMailcowDockerVersions(t *testing.T) {
	old := mailcowExec
	defer func() { mailcowExec = old }()
	for _, tc := range []struct {
		docker, compose string
		ok              bool
	}{{"23.0.0", "v2.1.0", false}, {"24.0.0", "1.29.0", false}, {"bad", "2.0.0", false}, {"24.0.0", "v2.0.0", false}, {"24.0.0", "v2.17.9", false}, {"24.0.0", "v2.18.0", true}, {"29.1.0", "v5.0.0", true}} {
		mailcowExec = func(_ string, _ []string, _ string, args ...string) (helperproto.ExecResult, error) {
			v := tc.docker
			if args[0] == "compose" {
				v = tc.compose
			}
			return helperproto.ExecResult{Stdout: v}, nil
		}
		if (mailcowDockerReady() == nil) != tc.ok {
			t.Errorf("versions %v", tc)
		}
	}
	mailcowExec = func(string, []string, string, ...string) (helperproto.ExecResult, error) {
		return helperproto.ExecResult{}, errors.New("offline")
	}
	if mailcowDockerReady() == nil {
		t.Fatal("offline daemon accepted")
	}
}

func TestMailcowComposeOverrideTrust(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	defer func() { mailcowDir, mailcowExec, mailcowOwnerUID = oldDir, oldExec, oldUID }()
	mailcowDir, mailcowOwnerUID = t.TempDir(), uint32(os.Getuid())
	os.Chmod(mailcowDir, 0700)
	override := filepath.Join(mailcowDir, "docker-compose.override.yml")
	os.WriteFile(override, []byte("services: {}"), 0644)
	calls := 0
	mailcowExec = func(_ string, _ []string, _ string, args ...string) (helperproto.ExecResult, error) {
		calls++
		if !strings.Contains(strings.Join(args, " "), "--file "+override+" up") {
			t.Fatal("override omitted", args)
		}
		return helperproto.ExecResult{}, nil
	}
	if _, err := mailcowCompose("up"); err != nil {
		t.Fatal(err)
	}
	os.Chmod(override, 0666)
	if _, err := mailcowCompose("up"); err == nil || calls != 1 {
		t.Fatal("untrusted override executed")
	}
}

func TestMailcowComposeScope(t *testing.T) {
	old := mailcowExec
	defer func() { mailcowExec = old }()
	mailcowExec = func(dir string, env []string, name string, args ...string) (helperproto.ExecResult, error) {
		if dir != mailcowDir || len(env) != 0 || name != "docker" || strings.Join(args, " ") != "compose --project-name mailcowdockerized --file /opt/mailcow-dockerized/docker-compose.yml down --volumes" {
			t.Fatalf("unsafe invocation %s %v", dir, args)
		}
		return helperproto.ExecResult{}, nil
	}
	if _, err := mailcowCompose("down", "--volumes"); err != nil {
		t.Fatal(err)
	}
}

func TestMailcowUnsafePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if mailcowSafePath(link, false) == nil {
		t.Fatal("symlink accepted")
	}
	if mailcowSafePath(dir, false) == nil {
		t.Fatal("directory accepted as file")
	}
}

func TestMailcowManagedLifecycle(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	defer func() { mailcowDir = oldDir; mailcowExec = oldExec; mailcowOwnerUID = oldUID }()
	mailcowOwnerUID = uint32(os.Getuid())
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	mailcowDir = filepath.Join(parent, "mailcow")
	os.Mkdir(mailcowDir, 0700)
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "mailcow.conf": "MAILCOW_HOSTNAME=mail.example.com\n", "docker-compose.yml": "services: {}\n"} {
		if err := os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := mailcowManaged(); err != nil {
		t.Fatal(err)
	}
	if mailcowWebURL() != "" {
		t.Fatal("unconfigured binding exposes misleading public URL")
	}
	for _, tc := range []struct{ bind, port, want string }{
		{"127.0.0.1", "8443", ""}, {"::1", "8443", ""}, {"0.0.0.0", "9443", "https://mail.example.com:9443/admin"}, {"", "443", "https://mail.example.com/admin"},
	} {
		os.WriteFile(filepath.Join(mailcowDir, "mailcow.conf"), []byte("MAILCOW_HOSTNAME=mail.example.com\nHTTPS_BIND="+tc.bind+"\nHTTPS_PORT="+tc.port+"\n"), 0600)
		if got := mailcowWebURL(); got != tc.want {
			t.Fatalf("bind=%s: URL=%q want=%q", tc.bind, got, tc.want)
		}
	}
	mailcowExec = func(string, []string, string, ...string) (helperproto.ExecResult, error) {
		return helperproto.ExecResult{}, errors.New("down failed")
	}
	if purgeMailcow() == nil {
		t.Fatal("failed down accepted")
	}
	if _, err := os.Stat(mailcowDir); err != nil {
		t.Fatal("failed purge lost data")
	}
	mailcowExec = func(string, []string, string, ...string) (helperproto.ExecResult, error) {
		return helperproto.ExecResult{}, nil
	}
	if err := uninstallMailcow(); err != nil {
		t.Fatal(err)
	}
	if mailcowTerpasang() {
		t.Fatal("uninstalled remains installed")
	}
	if _, err := os.Stat(filepath.Join(mailcowDir, "mailcow.conf")); err != nil {
		t.Fatal("uninstall lost config")
	}
	if err := purgeMailcow(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mailcowDir); !os.IsNotExist(err) {
		t.Fatal("purge retained checkout")
	}
}

func TestMailcowInstallRetry(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	defer func() { mailcowDir = oldDir; mailcowExec = oldExec; mailcowOwnerUID = oldUID }()
	mailcowOwnerUID = uint32(os.Getuid())
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	mailcowDir = filepath.Join(parent, "mailcow")
	os.Mkdir(mailcowDir, 0700)
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "mailcow.conf": "MAILCOW_HOSTNAME=mail.example.com\n", "docker-compose.yml": "services: {}\n", ".linux-dashboard-uninstalled": "pending\n"} {
		os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600)
	}
	calls := []string{}
	mailcowExec = func(_ string, _ []string, name string, args ...string) (helperproto.ExecResult, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		if args[0] == "version" {
			return helperproto.ExecResult{Stdout: "24.0.0"}, nil
		}
		if len(args) > 1 && args[1] == "version" {
			return helperproto.ExecResult{Stdout: "v2.18.0"}, nil
		}
		if strings.HasSuffix(call, "ps --all --format json") {
			return helperproto.ExecResult{Stdout: `[{"Service":"nginx-mailcow","State":"running","Health":"healthy"},{"Service":"postfix-mailcow","State":"running","Health":""},{"Service":"dovecot-mailcow","State":"running","Health":"healthy"},{"Service":"mysql-mailcow","State":"running","Health":"healthy"}]`}, nil
		}
		if strings.HasSuffix(call, "config --services") {
			return helperproto.ExecResult{Stdout: "nginx-mailcow\npostfix-mailcow\ndovecot-mailcow\nmysql-mailcow\n"}, nil
		}
		return helperproto.ExecResult{}, nil
	}
	if err := installMailcow("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	if !mailcowTerpasang() {
		t.Fatal("successful install not installed")
	}
	for _, suffix := range []string{" pull", " up -d --wait --wait-timeout 300"} {
		found := false
		for _, call := range calls {
			if strings.HasSuffix(call, suffix) {
				found = true
			}
		}
		if !found {
			t.Fatal("missing compose operation", suffix)
		}
	}
	before := len(calls)
	if installMailcow("other.example.com") == nil {
		t.Fatal("changed hostname accepted")
	}
	if len(calls) != before {
		t.Fatal("changed hostname executed command")
	}
}

func TestMailcowFailedPullNotInstalled(t *testing.T) {
	oldDir, oldExec, oldUID := mailcowDir, mailcowExec, mailcowOwnerUID
	defer func() { mailcowDir = oldDir; mailcowExec = oldExec; mailcowOwnerUID = oldUID }()
	mailcowOwnerUID = uint32(os.Getuid())
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	mailcowDir = filepath.Join(parent, "mailcow")
	os.Mkdir(mailcowDir, 0700)
	for name, value := range map[string]string{mailcowMarker: mailcowRepo + "\n", "mailcow.conf": "MAILCOW_HOSTNAME=mail.example.com\n", "docker-compose.yml": "services: {}\n"} {
		os.WriteFile(filepath.Join(mailcowDir, name), []byte(value), 0600)
	}
	mailcowExec = func(_ string, _ []string, _ string, args ...string) (helperproto.ExecResult, error) {
		if args[0] == "version" {
			return helperproto.ExecResult{Stdout: "24.0.0"}, nil
		}
		if args[1] == "version" {
			return helperproto.ExecResult{Stdout: "2.18.0"}, nil
		}
		return helperproto.ExecResult{}, errors.New("pull failed")
	}
	runner := mailcowExec
	for _, tc := range []struct{ diagnostic, want string }{
		{"lookup ghcr.io: no such host password=fixture-secret", "DNS"},
		{"Get https://registry: i/o timeout password=fixture-secret", "timeout"},
		{"unauthorized: password=fixture-secret", "autentikasi"},
		{"toomanyrequests: password=fixture-secret", "rate limit"},
		{"no space left on device password=fixture-secret", "disk"},
		{"invalid interpolation format password=fixture-secret", "konfigurasi"},
		{"unexpected error password=fixture-secret", "exit code 1"},
	} {
		mailcowExec = func(dir string, env []string, name string, args ...string) (helperproto.ExecResult, error) {
			if strings.HasSuffix(strings.Join(args, " "), " pull") {
				return helperproto.ExecResult{Stderr: tc.diagnostic, ExitCode: 1}, errors.New("fixture-secret")
			}
			return runner(dir, env, name, args...)
		}
		err := installMailcow("mail.example.com")
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "fixture-secret") || !mailcowTerpasang() {
			t.Fatalf("unsafe or missing pull diagnosis: %v", err)
		}
	}
	mailcowExec = runner
	if err := os.WriteFile(filepath.Join(mailcowDir, ".linux-dashboard-uninstalled"), []byte("installation pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if installMailcow("mail.example.com") == nil {
		t.Fatal("failure accepted")
	}
	if mailcowTerpasang() {
		t.Fatal("failed install considered installed; retry skips installer")
	}
}

func TestMailcowNumericHostname(t *testing.T) {
	if validMailcowHostname("mail.example.1com") == nil {
		t.Fatal("numeric TLD accepted")
	}
}

func TestMailcowCatalog(t *testing.T) {
	if components["stalwart"] != nil {
		t.Fatal("old mail server remains")
	}
	c := components["mailcow"]
	if c == nil {
		t.Fatal("mailcow missing")
	}
	if c.Service != "" || c.Binary != "" || c.terpasang == nil || c.uninstall == nil || c.purge == nil {
		t.Fatal("mailcow must manage compose stack, not binary/systemd")
	}
	found := false
	for _, n := range ComponentNames() {
		if n == "mailcow" {
			found = true
		}
	}
	if !found {
		t.Fatal("mailcow absent from visible catalog")
	}
}
