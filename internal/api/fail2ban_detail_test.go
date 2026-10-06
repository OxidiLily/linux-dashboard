package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestFail2banDetailAPIAuthAndContract(t *testing.T) {
	fake := &helperTiruan{balas: helperproto.Fail2banDetail{Jail: "sshd", IP: "2001:db8::1", Events: []helperproto.Fail2banEvent{{Time: "2026-10-05T12:00:00+07:00", Source: "journal:ssh", Action: "SSH", Message: "Failed password"}}, Warnings: []string{"Location unavailable"}, Location: "Unavailable"}}
	router, st := buatServerCron(t, fake)
	path := "/api/security/fail2ban/sshd/detail?ip=2001:db8::1"
	for _, sudo := range []bool{false, true} {
		sess := buatSesi(t, st, map[bool]string{false: "budi", true: "ani"}[sudo], sudo)
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if !sudo {
			if w.Code != http.StatusForbidden || len(fake.riwayatOperasi()) != 0 {
				t.Fatal("non-sudo accepted")
			}
			continue
		}
		if w.Code != http.StatusOK || fake.cmd != helperproto.CmdFail2banDetail {
			t.Fatalf("status=%d cmd=%s body=%s", w.Code, fake.cmd, w.Body.String())
		}
		var args helperproto.Fail2banUnbanArgs
		if err := json.Unmarshal(fake.args, &args); err != nil || args.Jail != "sshd" || args.IP != "2001:db8::1" {
			t.Fatalf("args=%s err=%v", fake.args, err)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"jail", "ip", "events", "warnings", "location"} {
			if _, ok := body[key]; !ok {
				t.Fatal(key)
			}
		}
		if !strings.Contains(w.Body.String(), "2026-10-05T12:00:00+07:00") {
			t.Fatal(w.Body.String())
		}
		before := len(fake.riwayatOperasi())
		for _, invalid := range []string{"999.1.1.1", "1.2.3.4/24", "fe80::1%25eth0", ""} {
			req := httptest.NewRequest("GET", "/api/security/fail2ban/sshd/detail?ip="+invalid, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest || len(fake.riwayatOperasi()) != before {
				t.Fatalf("invalid IP %q reached helper: %d", invalid, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
}
