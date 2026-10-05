package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestCloudflareManagerAPISudoDanKontrak(t *testing.T) {
	for _, tc := range []struct{ method, path, body, cmd string }{
		{"GET", "/api/proxy/cloudflare/zones", "", helperproto.CmdProxyCloudflareZones},
		{"POST", "/api/proxy/cloudflare/records", `{"zone_id":"zone","page":2}`, helperproto.CmdProxyCloudflareRecords},
		{"PUT", "/api/proxy/cloudflare/records", `{"zone_id":"zone","record_id":"rec","record":{"type":"TXT","name":"example.test","content":"hello"}}`, helperproto.CmdProxyCloudflareRecordSave},
		{"PUT", "/api/proxy/cloudflare/records", `{"zone_id":"zone","record_id":"rec","record":{"proxied":false}}`, helperproto.CmdProxyCloudflareRecordSave},
		{"POST", "/api/proxy/cloudflare/records/delete", `{"zone_id":"zone","record_id":"rec"}`, helperproto.CmdProxyCloudflareRecordDelete},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			var response any = map[string]any{"deleted": true}
			if tc.cmd == helperproto.CmdProxyCloudflareZones {
				response = []helperproto.CloudflareZone{}
			}
			fake := &helperTiruan{balas: response}
			r, st := buatServerCron(t, fake)
			unauthed := httptest.NewRecorder()
			r.ServeHTTP(unauthed, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if unauthed.Code == http.StatusOK || fake.cmd != "" {
				t.Fatalf("unauthorized: %d %s", unauthed.Code, fake.cmd)
			}
			noSudo := buatSesi(t, st, "budi", false)
			restricted := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			restricted.AddCookie(&http.Cookie{Name: sessionCookie, Value: noSudo})
			denied := httptest.NewRecorder()
			r.ServeHTTP(denied, restricted)
			if denied.Code != http.StatusForbidden || len(fake.riwayatOperasi()) != 0 {
				t.Fatal("non-sudo allowed")
			}
			sess := buatSesi(t, st, "ani", true)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK || fake.cmd != tc.cmd {
				t.Fatalf("status=%d cmd=%q body=%s", w.Code, fake.cmd, w.Body.String())
			}
			if tc.body != "" {
				var got helperproto.CloudflareManagedArgs
				var expected helperproto.CloudflareManagedArgs
				if err := json.Unmarshal([]byte(tc.body), &expected); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(fake.args, &got); err != nil || string(got.Record) != string(expected.Record) || got.RecordID != expected.RecordID {
					t.Fatalf("payload changed: %s", fake.args)
				}
				if err := json.Unmarshal(fake.args, &got); err != nil || got.ZoneID != "zone" {
					t.Fatalf("args=%s err=%v", fake.args, err)
				}
			}
		})
	}
}
