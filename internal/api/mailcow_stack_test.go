package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestMailcowStackRegistered(t *testing.T) {
	for _, stack := range stackKomponen {
		if stack.nama == "mailcow" && stack.compose == "/opt/mailcow-dockerized/docker-compose.yml" {
			return
		}
	}
	t.Fatal("mailcow installation must be registered for System Docker lifecycle controls")
}

func TestMailcowStackRegistrationState(t *testing.T) {
	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := stackKomponen
	stackKomponen = []struct{ nama, compose, ket string }{{"mailcow", compose, ""}}
	t.Cleanup(func() { stackKomponen = old })
	h := &helperTiruan{periksaToken: true, tokenSah: map[string]bool{"tok-ani": true}}
	router, db := buatServerTTL(t, h, 12)
	cookie := buatSesi(t, db, "ani", true)
	marker := filepath.Join(dir, ".linux-dashboard-uninstalled")
	for _, state := range []struct {
		marker    string
		installed bool
	}{
		{"installation pending\n", false},
		{"corrupt!!!!\n", true}, // Same byte length as uninstalled; metadata is not authority.
		{"corrupt marker\n", false},
		{"", true},
		{"uninstalled\n", false},
	} {
		if err := os.WriteFile(marker, []byte(state.marker), 0600); err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		h.balas = helperproto.ComponentStatus{Name: "mailcow", Installed: state.installed}
		h.cmds = nil
		h.mu.Unlock()
		req := httptest.NewRequest(http.MethodGet, "/api/docker/stacks", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		stacks, err := db.Stacks()
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if state.installed {
			want = 1
		}
		if w.Code != 200 || len(stacks) != want {
			t.Fatalf("state %q: status %d registrations %d, want %d", state.marker, w.Code, len(stacks), want)
		}
		calls := h.riwayatOperasi()
		if len(calls) == 0 || calls[0] != "mailcow.status" {
			t.Fatalf("missing fast trusted state RPC: %v", calls)
		}
	}
}

func TestMailcowStackRegistrationUntrusted(t *testing.T) {
	for _, reply := range []any{helperproto.ComponentStatus{Name: "other", Installed: true}, helperproto.ExecResult{Stdout: "[]"}} {
		h := &helperTiruan{balas: reply}
		router, db := buatServerTTL(t, h, 12)
		st, err := db.AddStack("mailcow", "/opt/mailcow-dockerized/docker-compose.yml", "")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/docker/stacks", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: buatSesi(t, db, "ani", true)})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if _, err := db.Stack(st.ID); err == nil {
			t.Fatalf("untrusted state retained registration: %+v", reply)
		}
	}
}

func TestMailcowConfigEditorDisabled(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "mailcow")
	if err := os.Symlink("/opt/mailcow-dockerized", alias); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.Symlink("/opt/mailcow-dockerized/mailcow.conf", filepath.Join(external, ".env")); err != nil {
		t.Fatal(err)
	}
	for _, compose := range []string{filepath.Join(external, "compose.yml"), "/opt/mailcow-dockerized/docker-compose.yml", "/opt/mailcow-dockerized/docker-compose.override.yml", "/opt/mailcow-dockerized/sub/compose.yaml", "/opt/mailcow-dockerized/sub/../compose.yml", alias + "/docker-compose.override.yml"} {
		for _, endpoint := range []string{"env", "compose"} {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				t.Run(compose+endpoint+method, func(t *testing.T) {
					h := &helperTiruan{}
					router, db := buatServerTTL(t, h, 12)
					st, err := db.AddStack("renamed", compose, "")
					if err != nil {
						t.Fatal(err)
					}
					req := httptest.NewRequest(method, fmt.Sprintf("/api/docker/stacks/%d/%s", st.ID, endpoint), strings.NewReader(`{"content":"services: {}"}`))
					req.AddCookie(&http.Cookie{Name: sessionCookie, Value: buatSesi(t, db, "ani", true)})
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "sudoedit") || !strings.Contains(w.Body.String(), "0600") {
						t.Fatalf("editor should explain safe operator action: %d %s", w.Code, w.Body.String())
					}
					if calls := h.riwayatOperasi(); len(calls) != 0 {
						t.Fatalf("editor touched root checkout: %v", calls)
					}
				})
			}
		}
	}
}

func TestMailcowProjectSurvivesDown(t *testing.T) {
	h := &helperTiruan{balas: helperproto.ExecResult{Stdout: "[]"}}
	router, db := buatServerTTL(t, h, 12)
	st, err := db.AddStack("renamed mail", "/opt/mailcow-dockerized/docker-compose.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	cookie := buatSesi(t, db, "ani", true)
	for _, action := range []string{"down", "up", "restart", "stop", "start", "pull"} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/docker/stacks/%d/%s", st.ID, action), nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var args helperproto.DockerExecArgs
		h.mu.Lock()
		err := json.Unmarshal(h.args, &args)
		h.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(args.Args) < 3 || args.Args[2] != "mailcowdockerized" {
			t.Fatalf("%s after holder absent: status %d, args %v", action, w.Code, args.Args)
		}
	}
	st.Name = "mailcow"
	got := argsCompose(st, map[string]string{st.ComposePath: "wrong-project"})
	if got[2] != "mailcowdockerized" {
		t.Fatalf("holder overrides vendor project: %v", got)
	}
}
