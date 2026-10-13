package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSPNineRouterFrame(t *testing.T) {
	// Simulasi request dari host 192.168.2.11:1122 dengan NINE_ROUTER_URL diset.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "192.168.2.11:1122"
	w := httptest.NewRecorder()
	securityHeadersWithCSP("http://100.89.81.126:20128/")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, req)
	policy := w.Header().Get("Content-Security-Policy")
	var frame string
	for _, directive := range strings.Split(policy, ";") {
		if strings.HasPrefix(strings.TrimSpace(directive), "frame-src ") {
			frame = strings.TrimSpace(directive)
		}
	}
	// Harus mengandung URL eksplisit DAN URL otomatis dari request host.
	if !strings.Contains(frame, "http://100.89.81.126:20128") {
		t.Fatalf("missing explicit nine-router origin in frame policy: %q", frame)
	}
	if !strings.Contains(frame, "http://192.168.2.11:20128") {
		t.Fatalf("missing auto nine-router origin from request host: %q", frame)
	}
	for _, directive := range []string{"script-src 'self'", "object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("missing protection: %s", directive)
		}
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("panel must remain protected against embedding")
	}
}

func TestCSPNineRouterFrameEmpty(t *testing.T) {
	// Tanpa NINE_ROUTER_URL dan tanpa request host → hanya 'self'.
	csp := buildCSP("", "")
	if strings.Contains(csp, "http://") {
		t.Fatalf("empty URL should not add external origin: %q", csp)
	}
	if !strings.Contains(csp, "frame-src 'self'") {
		t.Fatalf("missing frame-src self: %q", csp)
	}
}

func TestCSPAutoFrameFromRequestHost(t *testing.T) {
	// Tanpa NINE_ROUTER_URL tapi request dari host tertentu → auto-add.
	csp := buildCSP("", "10.0.0.5:1122")
	if !strings.Contains(csp, "http://10.0.0.5:20128") {
		t.Fatalf("auto frame-src missing: %q", csp)
	}
}

func TestCSPNoDuplicateFrame(t *testing.T) {
	// Jika NINE_ROUTER_URL sama dengan yang akan di-auto-add, jangan duplikat.
	csp := buildCSP("http://10.0.0.5:20128", "10.0.0.5:1122")
	count := strings.Count(csp, "http://10.0.0.5:20128")
	if count != 1 {
		t.Fatalf("duplicate frame origin (%d times): %q", count, csp)
	}
}
