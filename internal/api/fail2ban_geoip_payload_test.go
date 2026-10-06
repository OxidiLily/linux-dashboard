package api

import (
	"encoding/json"
	"fmt"
	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/metrics"
	"linux-dashboard/OxidiLily/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeoIPProviderNesting(t *testing.T) {
	for _, depth := range []int{32, 33, 6001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			raw := `{"ip":"8.8.8.8","success":true,"extra":` + strings.Repeat("[", depth-1) + `0` + strings.Repeat("]", depth-1) + `}`
			if !json.Valid([]byte(raw)) {
				t.Fatal("invalid fixture")
			}
			w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { rw.Write([]byte(raw)) })
			got := w.fetch("8.8.8.8")
			if depth <= 32 {
				if got.Status != "ready" || string(got.Data) != raw {
					t.Fatal(got.Status)
				}
			} else if got.Status != "error" || len(got.Data) != 0 || got.RetryAt == 0 {
				t.Fatal("provider accepted excessive nesting", got.Status)
			}
			if _, ok, err := st.GeoIP("8.8.8.8"); err != nil || ok {
				t.Fatal("fetch persisted payload", err)
			}
		})
	}
}

func TestGeoIPPayloadEndpoint(t *testing.T) {
	fake := &helperTiruan{balas: []helperproto.Fail2banJail{{Name: "sshd", BannedIPs: []string{"::ffff:8.8.8.8", "192.0.2.1"}}, {Name: "other", BannedIPs: []string{"1.1.1.1"}}}}
	router, st := buatServerCron(t, fake)
	raw := json.RawMessage(`{"ip":"8.8.8.8","success":true,"flag":{"emoji":""},"timezone":{"is_dst":false,"offset":0}}`)
	if err := st.PutGeoIP(store.GeoIP{IP: "8.8.8.8", Status: "ready", Data: raw, FetchedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		jail, ip string
		sudo     bool
		code     int
		data     bool
	}{
		{"sshd", "8.8.8.8", false, 403, false}, {"sshd", "8.8.8.8", true, 200, true}, {"sshd", "::ffff:8.8.8.8", true, 200, true}, {"sshd", "192.0.2.1", true, 200, false}, {"sshd", "1.1.1.1", true, 404, false}, {"missing", "8.8.8.8", true, 404, false}, {"sshd", "999.0.0.1", true, 400, false}, {"sshd", "fe80::1%25eth0", true, 400, false}, {"!bad", "8.8.8.8", true, 400, false},
	} {
		sess := buatSesi(t, st, fmt.Sprintf("user%d", len(fake.riwayatOperasi())), tc.sudo)
		req := httptest.NewRequest("GET", "/api/security/fail2ban/"+tc.jail+"/geoip?ip="+tc.ip, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		rw := httptest.NewRecorder()
		before := len(fake.riwayatOperasi())
		router.ServeHTTP(rw, req)
		if rw.Code != tc.code {
			t.Fatalf("%+v: %d %s", tc, rw.Code, rw.Body.String())
		}
		if tc.code == 403 || tc.code == 400 {
			if len(fake.riwayatOperasi()) != before {
				t.Fatal("invalid query called helper")
			}
		}
		if tc.code == 200 {
			var got map[string]json.RawMessage
			if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 5 || (tc.data && string(got["data"]) != string(raw)) || (!tc.data && string(got["data"]) != "null") {
				t.Fatal(rw.Body.String())
			}
		}
	}
}

func TestGeoIPPayloadEndpointPending(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		rw.Write([]byte(`{"ip":"8.8.8.8","success":true,"flag":{"emoji":""}}`))
	})
	fake := &helperTiruan{balas: []helperproto.Fail2banJail{{Name: "sshd", BannedIPs: []string{"8.8.8.8"}}}}
	srv := New(config.Config{}, st, pasangHelperTiruan(t, fake), metrics.NewCollector(), http.NotFoundHandler())
	srv.geoip = w
	router := srv.Routes()
	sess := buatSesi(t, st, "pending", true)
	request := func() map[string]json.RawMessage {
		req := httptest.NewRequest("GET", "/api/security/fail2ban/sshd/geoip?ip=8.8.8.8", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		rw := httptest.NewRecorder()
		start := time.Now()
		router.ServeHTTP(rw, req)
		if rw.Code != 200 || time.Since(start) > time.Second {
			t.Fatal(rw.Code, rw.Body.String())
		}
		var got map[string]json.RawMessage
		json.Unmarshal(rw.Body.Bytes(), &got)
		return got
	}
	for _, legacy := range []bool{false, true} {
		if legacy {
			if err := st.PutGeoIP(store.GeoIP{IP: "8.8.8.8", Country: "Old", Status: "ready", FetchedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
		}
		got := request()
		if string(got["status"]) != `"pending"` || string(got["data"]) != "null" {
			t.Fatal(got)
		}
	}
	release <- struct{}{}
	waitGeo(t, w, []string{"8.8.8.8"})
	got := request()
	if string(got["status"]) != `"ready"` || string(got["data"]) == "null" {
		t.Fatal(got)
	}
}

func TestGeoIPFullPayloadUpgrade(t *testing.T) {
	var calls atomic.Int32
	raw := `{"ip":"8.8.8.8","success":true,"country":"US","latitude":0,"longitude":0,"flag":{"img":"https://example.invalid/us.svg","emoji":""},"timezone":{"id":"UTC","is_dst":false,"offset":0,"current_time":""},"connection":{"asn":0,"isp":""},"extra":{"items":[],"value":null}}`
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RawQuery != "" {
			t.Error(r.URL)
		}
		rw.Write([]byte(raw))
	})
	if err := st.PutGeoIP(store.GeoIP{IP: "8.8.8.8", Status: "ready", Country: "Old", FetchedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.items([]string{"::ffff:8.8.8.8"}) }()
	}
	wg.Wait()
	got := waitGeo(t, w, []string{"8.8.8.8"})
	cached, _, err := st.GeoIP("8.8.8.8")
	if err != nil || string(cached.Data) != raw || calls.Load() != 1 || got.Items[0].Country != "US" {
		t.Fatalf("calls=%d cached=%+v err=%v", calls.Load(), cached, err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Items []map[string]json.RawMessage }
	json.Unmarshal(b, &list)
	if _, ok := list.Items[0]["data"]; ok {
		t.Fatal("list leaked payload")
	}
}
