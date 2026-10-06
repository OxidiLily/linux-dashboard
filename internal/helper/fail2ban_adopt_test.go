package helper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestFail2banSaveExplicitAdoption(t *testing.T) {
	dir := t.TempDir()
	old := jailLocalPath
	jailLocalPath = filepath.Join(dir, "jail.local")
	t.Cleanup(func() { jailLocalPath = old })
	reload := filepath.Join(dir, "reload")
	binary := "#!/bin/sh\n[ \"$*\" = reload ] || exit 1\nprintf reload >> '" + reload + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "fail2ban-client"), []byte(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	custom := "# custom protection\nfilter = sshd[mode=aggressive]\nlogpath = /var/log/auth-custom.log\n          /var/log/auth-extra.log\nbackend = auto\nbanaction = nftables-multiport\nignoreip = 127.0.0.1/8 ::1\naction = custom[name=sshd,\n         destination=security]\n         second[name=sshd]\n"
	original := "[DEFAULT]\nbackend = systemd\n\n[sshd]\n" + custom + "enabled = true\nmaxretry = 3\n\n" + f2bTanda + "\n[samba]\nenabled = true\nmaxretry = 7\n\n[admin]\nenabled = false\nport = 1234\n"
	for _, intent := range []string{"", `,"adopt":false`, `,"adopt":true`} {
		t.Run(intent, func(t *testing.T) {
			if err := os.WriteFile(jailLocalPath, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			var jail helperproto.Fail2banJail
			if err := json.Unmarshal([]byte(`{"name":"sshd","enabled":true,"maxretry":9,"external":false`+intent+`}`), &jail); err != nil {
				t.Fatal(err)
			}
			err := fail2banSave(jail)
			data, readErr := os.ReadFile(jailLocalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if intent != `,"adopt":true` {
				if err == nil {
					t.Fatal("external edit accepted without explicit adoption")
				}
				if string(data) != original {
					t.Fatal("rejected edit changed configuration")
				}
				if _, err := os.Stat(reload); !os.IsNotExist(err) {
					t.Fatal("rejected edit reloaded fail2ban")
				}
				return
			}
			if err != nil {
				t.Fatalf("explicit adoption rejected: %v", err)
			}
			for _, sibling := range []string{"[DEFAULT]\nbackend = systemd", f2bTanda + "\n[samba]\nenabled = true\nmaxretry = 7", "[admin]\nenabled = false\nport = 1234"} {
				if !strings.Contains(string(data), sibling) {
					t.Fatalf("sibling changed: %s", sibling)
				}
			}
			if !strings.Contains(string(data), custom) {
				t.Fatalf("adoption discarded custom directives: %s", data)
			}
			if strings.Count(string(data), "[sshd]") != 1 {
				t.Fatal("adoption duplicated jail")
			}
			for _, j := range bacaJailLocal() {
				if j.Name == "sshd" && (j.External || j.MaxRetry != 9) {
					t.Fatalf("adoption not persisted: %+v", j)
				}
			}
			if b, err := os.ReadFile(reload); err != nil || string(b) != "reload" {
				t.Fatalf("reload: %q %v", b, err)
			}
			// Adoption is request intent, not sticky state; later ordinary edits work.
			jail = helperproto.Fail2banJail{}
			if err := json.Unmarshal([]byte(`{"name":"sshd","enabled":true,"maxretry":8}`), &jail); err != nil {
				t.Fatal(err)
			}
			if err := fail2banSave(jail); err != nil {
				t.Fatalf("managed edit rejected: %v", err)
			}
			data, err = os.ReadFile(jailLocalPath)
			if err != nil || !strings.Contains(string(data), custom) {
				t.Fatalf("later edit discarded custom directives: %s err=%v", data, err)
			}
			for _, j := range bacaJailLocal() {
				if j.Name == "sshd" && (j.External || j.MaxRetry != 8) {
					t.Fatalf("later edit not persisted: %+v", j)
				}
			}
		})
	}
}
