package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

const detailBytes = 2 << 20
const detailEvents = 200
const detailLines = 2000
const detailMessage = 2048

var detailLogPath = "/var/log/fail2ban.log"
var detailJournal = readDetailJournal
var detailEventRE = regexp.MustCompile(`\[([^\]]+)\]\s+(Found|Ban|Unban)\s+(\S+)(?:\s|$)`)
var detailTimeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)

func fail2banDetail(jail, ip string) (helperproto.Fail2banDetail, error) {
	out := helperproto.Fail2banDetail{Jail: jail, IP: ip, Events: make([]helperproto.Fail2banEvent, 0, detailEvents), Warnings: []string{}, Location: "Unavailable"}
	addr, err := netip.ParseAddr(ip)
	if !jailNamaRe.MatchString(jail) || err != nil || addr.Zone() != "" {
		return out, errInvalid("jail atau alamat IP tidak valid")
	}
	warn := func(s string) {
		for _, old := range out.Warnings {
			if old == s {
				return
			}
		}
		out.Warnings = append(out.Warnings, s)
	}
	warn("History limited to current and .1 Fail2ban logs, or the latest 2000 journal entries within 7 days; older events unavailable.")
	add := func(e helperproto.Fail2banEvent) {
		if len(e.Message) > detailMessage {
			e.Message = strings.ToValidUTF8(e.Message[:detailMessage], "")
			warn("Event messages truncated to 2048 bytes.")
		}
		stamp, _ := time.Parse(time.RFC3339Nano, e.Time)
		i := sort.Search(len(out.Events), func(i int) bool {
			t, _ := time.Parse(time.RFC3339Nano, out.Events[i].Time)
			return t.After(stamp)
		})
		// Retain only the newest 200 across sources; unknown timestamps sort first.
		if len(out.Events) == detailEvents {
			warn("Events truncated to 200 entries.")
			if i == 0 {
				return
			}
			copy(out.Events, out.Events[1:i])
			out.Events[i-1] = e
			return
		}
		out.Events = append(out.Events, helperproto.Fail2banEvent{})
		copy(out.Events[i+1:], out.Events[i:len(out.Events)-1])
		out.Events[i] = e
	}
	parse := func(text, source, stamp string, ssh bool) {
		m := detailEventRE.FindStringSubmatch(text)
		action := ""
		if ssh {
			// The peer is the final 'from IP port NUM', not text in a username.
			fields := strings.Fields(text)
			for i := len(fields) - 1; i >= 0; i-- {
				if fields[i] != "from" {
					continue
				}
				if i+3 < len(fields) && fields[i+2] == "port" {
					a, e := netip.ParseAddr(fields[i+1])
					_, portErr := strconv.ParseUint(fields[i+3], 10, 16)
					if e == nil && portErr == nil && a == addr {
						action = "SSH"
					}
				}
				break
			}
			if action == "" {
				return
			}
		} else {
			if m == nil || m[1] != jail {
				return
			}
			a, e := netip.ParseAddr(m[3])
			if e != nil || a != addr {
				return
			}
			action = m[2]
		}
		parseTime := func(raw string) (time.Time, error) {
			raw = strings.ReplaceAll(strings.ReplaceAll(raw, ",", "."), " ", "T")
			t, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				t, e = time.ParseInLocation("2006-01-02T15:04:05.999999999", raw, time.Local)
				if e == nil {
					warn("Fail2ban file timestamps lack timezone: server local timezone assumed; historical timezone changes/DST may be ambiguous.")
				}
			}
			return t, e
		}
		if action == "Found" {
			// Fail2ban filter.processLineAndAdd logs the failure ticket time after " - ", not processing time.
			tail := strings.TrimSpace(text[strings.Index(text, m[0])+len(m[0]):])
			if strings.HasPrefix(tail, "- ") {
				if t, e := parseTime(strings.TrimSpace(tail[2:])); e == nil {
					stamp = t.Format(time.RFC3339Nano)
				} else {
					warn("Found event time unavailable or unrecognized; processing timestamp used when available.")
				}
			}
		}
		if stamp == "" {
			if t, e := parseTime(detailTimeRE.FindString(text)); e == nil {
				stamp = t.Format(time.RFC3339Nano)
			} else {
				warn("Some event timestamps unavailable or unrecognized.")
			}
		}
		add(helperproto.Fail2banEvent{Time: stamp, Source: source, Action: action, Message: text})
	}
	available := false
	fallback := false
	for _, path := range []string{detailLogPath + ".1", detailLogPath} {
		text, truncated, e := readDetailLog(path)
		if e != nil {
			warn(fmt.Sprintf("Fail2ban log unavailable: %s.", path))
			fallback = true
			continue
		}
		available = true
		if truncated {
			warn(fmt.Sprintf("Fail2ban log truncated to latest 2 MiB: %s.", path))
		}
		scanDetail(text, func(line string) { parse(line, path, "", false) }, warn)
	}
	journal := func(ssh bool) {
		text, truncated, e := detailJournal(ssh)
		source := "journal:fail2ban"
		if ssh {
			source = "journal:ssh"
		}
		if e != nil {
			warn(source + " unavailable.")
			return
		}
		if truncated {
			warn(source + " output truncated to complete entries within latest 2 MiB; older entries discarded.")
		}
		count := 0
		scanDetail(text, func(line string) {
			var entry struct {
				Message   string `json:"MESSAGE"`
				Timestamp string `json:"__REALTIME_TIMESTAMP"`
			}
			if json.Unmarshal([]byte(line), &entry) != nil {
				warn(source + " entries unavailable or malformed.")
				return
			}
			count++
			stamp := ""
			us, e := strconv.ParseInt(entry.Timestamp, 10, 64)
			if e == nil {
				stamp = time.UnixMicro(us).UTC().Format(time.RFC3339Nano)
			}
			parse(entry.Message, source, stamp, ssh)
		}, warn)
		if count >= detailLines {
			warn(source + " truncated to latest 2000 entries.")
		}
	}
	if !available || fallback {
		journal(false)
	}
	if jail == "sshd" {
		warn("SSH journal attempts are IP-correlated context, not proof that each attempt caused this jail's ban.")
		journal(true)
	} else {
		warn("Service authentication details unavailable for this jail; Found events identify Fail2ban detections.")
	}
	if len(out.Events) == 0 {
		warn("No matching events available in the bounded history; this does not mean no attempts occurred.")
	}
	// Journal fallback can overlap file logs; preserve distinct sources for audit provenance.
	return out, nil
}

func readDetailLog(path string) (string, bool, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", false, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !st.Mode().IsRegular() {
		return "", false, fmt.Errorf("not a regular log")
	}
	truncated := st.Size() > detailBytes
	if truncated {
		if _, err = f.Seek(st.Size()-detailBytes, io.SeekStart); err != nil {
			return "", false, err
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, detailBytes))
	if truncated {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	return string(b), truncated, err
}

func scanDetail(text string, line func(string), warn func(string)) {
	s := bufio.NewScanner(strings.NewReader(text))
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		line(s.Text())
	}
	if s.Err() != nil {
		warn("Log scan truncated: oversized line or read failure.")
	}
}

type detailBuffer struct {
	b         []byte
	truncated bool
	partial   bool
}

func (b *detailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if cut := len(b.b) + n - detailBytes; cut > 0 {
		b.truncated = true
		if cut <= len(b.b) {
			b.partial = b.b[cut-1] != '\n'
			b.b = b.b[:copy(b.b, b.b[cut:])]
		} else {
			cut -= len(b.b)
			b.partial = p[cut-1] != '\n'
			b.b = b.b[:0]
			p = p[cut:]
		}
	}
	b.b = append(b.b, p...)
	return n, nil
}

func (b *detailBuffer) text() string {
	text := string(b.b)
	if b.partial {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			return ""
		}
	}
	return text
}

func readDetailJournal(ssh bool) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{"--no-pager", "--quiet", "--output=json", "--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "--since=-7 days", "-n", strconv.Itoa(detailLines)}
	if ssh {
		args = append(args, "_COMM=sshd", "+", "_COMM=sshd-session")
	} else {
		args = append(args, "-u", "fail2ban.service")
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	cmd.Env = []string{"PATH=" + pathExec, "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	var stdout, stderr detailBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.text(), stdout.truncated, err
}
