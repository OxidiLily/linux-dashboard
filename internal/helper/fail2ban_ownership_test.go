package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestFail2banPreserveCustomBody(t *testing.T) {
	old := jailLocalPath
	jailLocalPath = filepath.Join(t.TempDir(), "jail.local")
	t.Cleanup(func() { jailLocalPath = old })
	custom := "; keep comment\nfilter = custom\naction = custom[\n    maxretry = 99]\n    next[name=custom]\n"
	initial := f2bTanda + "\n[custom]\n" + custom + "enabled = true\nmaxretry = 3\nbantime = 2h\nfindtime = 3m\nport = 22\n    23\n" + f2bTanda + "\n[other]\nenabled = true\n"
	if err := os.WriteFile(jailLocalPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	jail := helperproto.Fail2banJail{Name: "custom", MaxRetry: 8, BanTime: "4h", FindTime: "5m"}
	for i := 0; i < 2; i++ {
		if err := tulisJailLocal(jail.Name, susunJail(jail)); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(jailLocalPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), custom) {
			t.Fatalf("custom body changed: %s", data)
		}
		for _, removed := range []string{"maxretry = 3", "bantime = 2h", "findtime = 3m", "port = 22", "    23"} {
			if strings.Contains(string(data), removed) {
				t.Fatalf("old managed value retained: %s", removed)
			}
		}
		if strings.Count(string(data), f2bTanda) != 2 {
			t.Fatalf("ownership duplicated: %s", data)
		}
	}
	if err := tulisJailLocal(jail.Name, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(jailLocalPath)
	if err != nil || strings.Contains(string(data), "[custom]") || strings.Contains(string(data), custom) {
		t.Fatalf("delete retained custom section: %s err=%v", data, err)
	}
	jails := bacaJailLocal()
	if len(jails) != 1 || jails[0].Name != "other" || jails[0].External {
		t.Fatalf("sibling ownership changed: %+v", jails)
	}
}

func TestFail2banPreserveFollowingOwnership(t *testing.T) {
	old := jailLocalPath
	jailLocalPath = filepath.Join(t.TempDir(), "jail.local")
	t.Cleanup(func() { jailLocalPath = old })
	for _, replacement := range []string{susunJail(helperproto.Fail2banJail{Name: "samba", Enabled: true, MaxRetry: 5}), ""} {
		initial := susunJail(helperproto.Fail2banJail{Name: "samba", Enabled: true, MaxRetry: 5}) + "\n" + susunJail(helperproto.Fail2banJail{Name: "sshd", Enabled: true, MaxRetry: 5})
		if err := os.WriteFile(jailLocalPath, []byte(initial), 0600); err != nil {
			t.Fatal(err)
		}
		if err := tulisJailLocal("samba", replacement); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, j := range bacaJailLocal() {
			if j.Name == "sshd" {
				found = true
				if j.External {
					t.Fatal("editing/deleting samba made sshd external")
				}
			}
		}
		if !found {
			t.Fatal("sshd disappeared")
		}
		data, err := os.ReadFile(jailLocalPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), f2bTanda+"\n[sshd]") {
			t.Fatal("sshd ownership marker lost")
		}
	}
}
