package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func detailFixture(t *testing.T) string {
	t.Helper()
	oldPath, oldJournal := detailLogPath, detailJournal
	detailLogPath = filepath.Join(t.TempDir(), "fail2ban.log")
	detailJournal = func(bool) (string, bool, error) { return "", false, fmt.Errorf("unavailable") }
	t.Cleanup(func() { detailLogPath = oldPath; detailJournal = oldJournal })
	return detailLogPath
}

func TestFail2banDetailDoesNotClaimExternalPrivacy(t *testing.T) {
	detailFixture(t)
	out, err := fail2banDetail("sshd", "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range out.Warnings {
		if strings.Contains(warning, "IP was not shared externally") || strings.Contains(warning, "no local Geo-IP facility") {
			t.Fatal(warning)
		}
	}
}

func TestFail2banDetailValidationAndAuthz(t *testing.T) {
	if !sudoRequired[helperproto.CmdFail2banDetail] {
		t.Fatal("detail must require sudo")
	}
	detailFixture(t)
	for _, tc := range []struct{ jail, ip string }{{"../sshd", "1.2.3.4"}, {"sshd", "999.1.2.3"}, {"sshd", "1.2.3.4/24"}, {"sshd", "fe80::1%eth0"}, {"", "::1"}, {"sshd", "-n"}, {"sshd", " 1.2.3.4"}} {
		if _, err := fail2banDetail(tc.jail, tc.ip); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
	for _, ip := range []string{"1.2.3.4", "2001:db8::1", "::1"} {
		if _, err := fail2banDetail("sshd", ip); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFail2banDetailExactMatchAndTimes(t *testing.T) {
	path := detailFixture(t)
	text := "2026-10-05 12:30:00,123 fail2ban.filter [123]: INFO [sshd] Found 1.2.3.4 - 2026-10-05 12:29:58\n" +
		"2026-10-05T12:31:00+07:00 fail2ban.actions [123]: NOTICE [sshd] Ban 1.2.3.4\n" +
		"2026-10-05T12:31:00Z fail2ban.actions [123]: NOTICE [sshd] Ban 1.2.3.40\n" +
		"2026-10-05T12:31:00Z fail2ban.actions [123]: NOTICE [sshd-other] Ban 1.2.3.4\n" +
		"2026-10-05T12:31:00Z fail2ban.actions [123]: NOTICE [sshd] Ban 1.2.3.4evil\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("2026-10-04T12:31:00Z fail2ban.actions [123]: NOTICE [sshd] Unban 1.2.3.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := fail2banDetail("sshd", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 3 {
		t.Fatalf("events=%+v", out.Events)
	}
	actions := map[string]string{}
	for _, e := range out.Events {
		if _, err := time.Parse(time.RFC3339Nano, e.Time); err != nil {
			t.Fatal(err)
		}
		actions[e.Action] = e.Time
	}
	if actions["Ban"] != "2026-10-05T12:31:00+07:00" {
		t.Fatal(actions)
	}
	expected, _ := time.ParseInLocation("2006-01-02 15:04:05.999", "2026-10-05 12:29:58", time.Local)
	if actions["Found"] != expected.Format(time.RFC3339Nano) {
		t.Fatal(actions)
	}
	if !strings.Contains(strings.Join(out.Warnings, " "), "timezone assumed") {
		t.Fatal(out.Warnings)
	}
}

func TestFail2banDetailFoundJournalDetectionTime(t *testing.T) {
	detailFixture(t)
	for _, tc := range []struct{ suffix, want string }{
		{"2026-10-05 12:29:58", ""},
		{"2026-10-05T12:29:58Z", "2026-10-05T12:29:58Z"},
		{"invalid", time.UnixMicro(1791201600123456).UTC().Format(time.RFC3339Nano)},
		{"2026-99-05 12:29:58", time.UnixMicro(1791201600123456).UTC().Format(time.RFC3339Nano)},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			detailJournal = func(bool) (string, bool, error) {
				data, err := json.Marshal(map[string]string{"MESSAGE": "[samba] Found 1.2.3.4 - " + tc.suffix, "__REALTIME_TIMESTAMP": "1791201600123456"})
				return string(data) + "\n", false, err
			}
			want := tc.want
			if want == "" {
				stamp, err := time.ParseInLocation("2006-01-02 15:04:05", tc.suffix, time.Local)
				if err != nil {
					t.Fatal(err)
				}
				want = stamp.Format(time.RFC3339Nano)
			}
			out, err := fail2banDetail("samba", "1.2.3.4")
			if err != nil || len(out.Events) != 1 || out.Events[0].Time != want {
				t.Fatalf("events=%+v err=%v want=%s", out.Events, err, want)
			}
		})
	}
}

func TestFail2banDetailJournalFallbackSSH(t *testing.T) {
	detailFixture(t)
	calls := 0
	detailJournal = func(ssh bool) (string, bool, error) {
		calls++
		msgs := []string{"[sshd] Found 2001:db8::1 - recent", "[sshd-other] Ban 2001:db8::1", "[sshd] Ban 2001:db8::10"}
		if ssh {
			msgs = []string{"Failed password for invalid user admin from 2001:db8::1 port 123 ssh2", "Failed password for 2001:db8::1 from 2001:db8::10 port 123 ssh2"}
		}
		var b strings.Builder
		for _, m := range msgs {
			data, _ := json.Marshal(map[string]string{"MESSAGE": m, "__REALTIME_TIMESTAMP": "1791201600123456"})
			b.Write(data)
			b.WriteByte('\n')
		}
		return b.String(), true, nil
	}
	out, err := fail2banDetail("sshd", "2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(out.Events) != 2 {
		t.Fatalf("calls=%d events=%+v", calls, out.Events)
	}
	for _, e := range out.Events {
		if e.Time != time.UnixMicro(1791201600123456).UTC().Format(time.RFC3339Nano) {
			t.Fatal(e)
		}
	}
	if !strings.Contains(strings.Join(out.Warnings, " "), "complete entries within latest 2 MiB; older entries discarded") {
		t.Fatal(out.Warnings)
	}
}

func TestFail2banDetailSSHActualPeer(t *testing.T) {
	detailFixture(t)
	for _, tc := range []struct {
		message, ip string
		want        int
	}{
		{"Failed password for invalid user from 8.8.8.8 from 1.1.1.1 port 54321 ssh2", "8.8.8.8", 0},
		{"Failed password for invalid user from 8.8.8.8 from 1.1.1.1 port 54321 ssh2", "1.1.1.1", 1},
		{"Failed password for invalid user from 8.8.8.8 port 22 from 1.1.1.1 port 54321 ssh2", "8.8.8.8", 0},
		{"Failed password for root from 8.8.8.8 port 54321 ssh2", "8.8.8.8", 1},
		{"Failed publickey for root from 2001:db8::1 port 54321 ssh2: ED25519 SHA256:abc", "2001:db8::1", 1},
		{"Failed password for invalid user from 2001:db8::1 from 2001:db8::2 port 54321 ssh2", "2001:db8::1", 0},
		{"Failed password for root from 8.8.8.8 port invalid ssh2", "8.8.8.8", 0},
	} {
		t.Run(tc.message+"/"+tc.ip, func(t *testing.T) {
			detailJournal = func(ssh bool) (string, bool, error) {
				if !ssh {
					return "", false, nil
				}
				data, err := json.Marshal(map[string]string{"MESSAGE": tc.message, "__REALTIME_TIMESTAMP": "1791201600123456"})
				return string(data) + "\n", false, err
			}
			out, err := fail2banDetail("sshd", tc.ip)
			if err != nil || len(out.Events) != tc.want {
				t.Fatalf("events=%+v err=%v want=%d", out.Events, err, tc.want)
			}
		})
	}
}

func TestDetailBufferNewestCompleteLines(t *testing.T) {
	line := "{\"MESSAGE\":\"newest\"}\n"
	for _, tc := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"single write", []string{strings.Repeat("x", detailBytes) + "\n" + line}, line},
		{"split writes", []string{strings.Repeat("x", detailBytes), "\n", line[:8], line[8:]}, line},
		{"exact boundary", []string{"old\n" + strings.Repeat(" ", detailBytes-len(line)) + line}, strings.Repeat(" ", detailBytes-len(line)) + line},
		{"exact boundary across writes", []string{"old\n" + strings.Repeat(" ", detailBytes-len(line)), line}, strings.Repeat(" ", detailBytes-len(line)) + line},
		{"oversized line", []string{strings.Repeat("x", detailBytes+99)}, ""},
		{"oversized line then recovery", []string{strings.Repeat("x", detailBytes+99), "\n" + line}, line},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b detailBuffer
			for _, chunk := range tc.chunks {
				n, err := b.Write([]byte(chunk))
				if n != len(chunk) || err != nil || len(b.b) > detailBytes {
					t.Fatalf("write=%d err=%v retained=%d", n, err, len(b.b))
				}
			}
			// Obtain exactly what readDetailJournal returns, without executing journalctl.
			got := b.text()
			if !b.truncated || got != tc.want {
				t.Fatalf("truncated=%v got length=%d want length=%d", b.truncated, len(got), len(tc.want))
			}
		})
	}
}

func TestFail2banDetailRetainsNewestAcrossSources(t *testing.T) {
	path := detailFixture(t)
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var logs strings.Builder
	for i := 0; i < detailEvents; i++ {
		fmt.Fprintf(&logs, "%s [sshd] Found 1.2.3.4\n", base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano))
	}
	banTime := base.Add(time.Hour).Format(time.RFC3339Nano)
	fmt.Fprintf(&logs, "%s [sshd] Ban 1.2.3.4\n", banTime)
	if err := os.WriteFile(path, []byte(logs.String()), 0600); err != nil {
		t.Fatal(err)
	}
	detailJournal = func(ssh bool) (string, bool, error) {
		var journal strings.Builder
		for i := 0; i < detailEvents; i++ {
			stamp := base.Add(time.Duration(i) * time.Second)
			message := "[sshd] Found 1.2.3.4"
			if ssh {
				message = "Failed password for root from 1.2.3.4 port 123 ssh2"
			}
			data, err := json.Marshal(map[string]string{"MESSAGE": message, "__REALTIME_TIMESTAMP": fmt.Sprint(stamp.UnixMicro())})
			if err != nil {
				t.Fatal(err)
			}
			journal.Write(data)
			journal.WriteByte('\n')
		}
		return journal.String(), false, nil
	}
	out, err := fail2banDetail("sshd", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != detailEvents {
		t.Fatalf("events=%d", len(out.Events))
	}
	last := out.Events[len(out.Events)-1]
	if last.Action != "Ban" || last.Time != banTime {
		t.Fatalf("latest Ban evicted: last=%+v", last)
	}
	for i, event := range out.Events {
		stamp, err := time.Parse(time.RFC3339Nano, event.Time)
		if err != nil || stamp.Before(base.Add(133*time.Second)) {
			t.Fatalf("retained older event: %+v, err=%v", event, err)
		}
		if i > 0 {
			previous, _ := time.Parse(time.RFC3339Nano, out.Events[i-1].Time)
			if stamp.Before(previous) {
				t.Fatal("events not chronological")
			}
		}
	}
	if !strings.Contains(strings.Join(out.Warnings, " "), "Events truncated to 200 entries.") {
		t.Fatal(out.Warnings)
	}
}

func TestFail2banDetailBoundsAndUnavailable(t *testing.T) {
	path := detailFixture(t)
	line := "2026-10-05T12:00:00Z fail2ban.actions [1]: NOTICE [samba] Ban 1.2.3.4 " + strings.Repeat("a", 3000) + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(line, 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	text, truncated, err := readDetailLog(path)
	if err != nil || !truncated || len(text) > detailBytes {
		t.Fatalf("size=%d truncated=%v err=%v", len(text), truncated, err)
	}
	out, err := fail2banDetail("samba", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != detailEvents {
		t.Fatal(len(out.Events))
	}
	for _, e := range out.Events {
		if len(e.Message) > detailMessage {
			t.Fatal("unbounded message")
		}
	}
	warnings := strings.Join(out.Warnings, " ")
	for _, s := range []string{"latest 2 MiB", "200 entries", "2048 bytes", "unavailable"} {
		if !strings.Contains(warnings, s) {
			t.Fatal(warnings)
		}
	}
	var buffer detailBuffer
	n, err := buffer.Write([]byte(strings.Repeat("x", detailBytes+99)))
	if n != detailBytes+99 || err != nil || len(buffer.b) != detailBytes || !buffer.truncated {
		t.Fatal("journal buffer unbounded")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readDetailLog(path); err == nil {
		t.Fatal("symlink accepted")
	}
	out, err = fail2banDetail("samba", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 0 || out.Location != "Unavailable" || !strings.Contains(strings.Join(out.Warnings, " "), "No matching events available") {
		t.Fatal(out)
	}
	warned := false
	scanDetail(strings.Repeat("x", 100000), func(string) { t.Fatal("oversize line accepted") }, func(string) { warned = true })
	if !warned {
		t.Fatal("scan truncation not reported")
	}
}
