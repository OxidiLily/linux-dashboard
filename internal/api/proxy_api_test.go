package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestProxyAPIButuhSudo(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/hosts", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("GET non-sudo = %d, ingin 403", w.Code)
	}
	if len(tiruan.riwayatOperasi()) != 0 {
		t.Fatalf("helper tetap dipanggil: %v", tiruan.riwayatOperasi())
	}
}

func TestProxyAPICRUDMeneruskanKontrakHelper(t *testing.T) {
	host := helperproto.ProxyHost{ID: "abc123abc123", Domain: "app.example.test", TargetHost: "127.0.0.1", TargetPort: 3000, Scheme: "http", Enabled: true}
	t.Run("list", func(t *testing.T) {
		tiruan := &helperTiruan{balas: []helperproto.ProxyHost{host}}
		r, st := buatServerCron(t, tiruan)
		sess := buatSesi(t, st, "ani", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/proxy/hosts", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyList {
			t.Fatalf("list status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
		}
	})
	t.Run("save", func(t *testing.T) {
		tiruan := &helperTiruan{balas: host}
		r, st := buatServerCron(t, tiruan)
		sess := buatSesi(t, st, "ani", true)
		b, _ := json.Marshal(host)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/proxy/hosts/abc123abc123", strings.NewReader(string(b)))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxySave {
			t.Fatalf("save status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
		}
		var args helperproto.ProxyHost
		if json.Unmarshal(tiruan.args, &args) != nil || args.ID != host.ID {
			t.Fatalf("save args=%s", tiruan.args)
		}
	})
	t.Run("delete", func(t *testing.T) {
		tiruan := &helperTiruan{}
		r, st := buatServerCron(t, tiruan)
		sess := buatSesi(t, st, "ani", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/proxy/hosts/abc123abc123", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyDelete {
			t.Fatalf("delete status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
		}
	})
}

func TestProxyAPICertIssueMeneruskanEmailDanStaging(t *testing.T) {
	host := helperproto.ProxyHost{ID: "abc123abc123", Domain: "app.example.test", TLSMode: "certbot"}
	tiruan := &helperTiruan{balas: host}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/hosts/abc123abc123/cert", strings.NewReader(`{"email":"admin@example.test","staging":true}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyCertIssue {
		t.Fatalf("cert status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
	}
	var args helperproto.ProxyCertArgs
	if json.Unmarshal(tiruan.args, &args) != nil || args.ID != host.ID || args.Email != "admin@example.test" || !args.Staging {
		t.Fatalf("cert args=%s", tiruan.args)
	}
}

func TestProxyAPICloudflareDNSMeneruskanTokenTanpaMencatatnya(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.CloudflareDNSRecord{ID: "record123", Type: "A", Name: "app.example.test", Content: "203.0.113.10", Proxied: true}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/proxy/cloudflare/dns", strings.NewReader(`{"token":"rahasia-uji","domain":"app.example.test","content":"203.0.113.10","proxied":true}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyCloudflareDNS {
		t.Fatalf("cloudflare status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
	}
	var args helperproto.CloudflareDNSArgs
	if json.Unmarshal(tiruan.args, &args) != nil || args.Token != "rahasia-uji" || args.Domain != "app.example.test" || !args.Proxied {
		t.Fatalf("cloudflare args=%s", tiruan.args)
	}
	if strings.Contains(w.Body.String(), "rahasia-uji") {
		t.Fatal("token bocor ke respons")
	}
}

func TestProxyAPICloudflareListDanDeleteMeneruskanKontrak(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		tiruan := &helperTiruan{balas: []helperproto.CloudflareDNSRecord{{ID: "rec1", Type: "A", Name: "app.example.test", Content: "203.0.113.10"}}}
		r, st := buatServerCron(t, tiruan)
		sess := buatSesi(t, st, "ani", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/proxy/cloudflare/list", strings.NewReader(`{"token":"rahasia-uji","domain":"app.example.test"}`))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyCloudflareList {
			t.Fatalf("list status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
		}
		var args helperproto.CloudflareDNSArgs
		if json.Unmarshal(tiruan.args, &args) != nil || args.Token != "rahasia-uji" {
			t.Fatalf("args=%s", tiruan.args)
		}
		if strings.Contains(w.Body.String(), "rahasia-uji") {
			t.Fatal("token bocor ke respons")
		}
	})
	t.Run("delete", func(t *testing.T) {
		tiruan := &helperTiruan{balas: map[string]int{"deleted": 1}}
		r, st := buatServerCron(t, tiruan)
		sess := buatSesi(t, st, "ani", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/proxy/cloudflare/delete", strings.NewReader(`{"token":"rahasia-uji","domain":"app.example.test","record_id":"rec1"}`))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || tiruan.cmd != helperproto.CmdProxyCloudflareDelete {
			t.Fatalf("delete status=%d cmd=%q body=%s", w.Code, tiruan.cmd, w.Body.String())
		}
		var args helperproto.CloudflareDNSArgs
		if json.Unmarshal(tiruan.args, &args) != nil || args.RecordID != "rec1" {
			t.Fatalf("args=%s", tiruan.args)
		}
	})
}

func TestCloudflareTokenAPISudoDanTanpaEcho(t *testing.T) {
	for _, tc := range []struct{ method, path, body, cmd string }{
		{"GET", "/api/proxy/cloudflare/token", "", helperproto.CmdProxyCloudflareTokenStatus},
		{"PUT", "/api/proxy/cloudflare/token", `{"token":"test-secret"}`, helperproto.CmdProxyCloudflareTokenSave},
		{"DELETE", "/api/proxy/cloudflare/token", "", helperproto.CmdProxyCloudflareTokenDelete},
	} {
		t.Run(tc.method, func(t *testing.T) {
			tiruan := &helperTiruan{balas: helperproto.CloudflareTokenStatus{Saved: true}}
			r, st := buatServerCron(t, tiruan)
			for _, sudo := range []bool{false, true} {
				sess := buatSesi(t, st, "ani", sudo)
				w := httptest.NewRecorder()
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
				r.ServeHTTP(w, req)
				if !sudo {
					if w.Code != http.StatusForbidden {
						t.Fatalf("non sudo: %d", w.Code)
					}
					continue
				}
				if w.Code != http.StatusOK || tiruan.cmd != tc.cmd || strings.Contains(w.Body.String(), "test-secret") {
					t.Fatalf("status=%d cmd=%s body=%s", w.Code, tiruan.cmd, w.Body.String())
				}
				if tc.method == "PUT" && !strings.Contains(string(tiruan.args), "test-secret") {
					t.Fatal("token not sent to helper")
				}
			}
		})
	}
}

func TestProxyAPIKonflikPortJadi409(t *testing.T) {
	tiruan := &helperTiruan{balasE: &helperclient.Error{Code: helperproto.ErrPortKonflik, Msg: "port dipakai"}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/hosts", strings.NewReader(`{"domain":"app.test","target_host":"127.0.0.1","target_port":3000,"scheme":"http","enabled":true}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("konflik status=%d body=%s", w.Code, w.Body.String())
	}
}
