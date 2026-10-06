package api

import (
	"encoding/json"
	"fmt"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/store"
)

func geoFixture(t *testing.T, handler http.HandlerFunc) (*geoIPWorker, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	worker := newGeoIPWorker(st)
	worker.baseURL = server.URL + "/"
	t.Cleanup(func() { worker.close(); server.Close(); st.Close() })
	return worker, st
}

func waitGeo(t *testing.T, w *geoIPWorker, ips []string) geoIPResponse {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		got := w.items(ips)
		if !got.Pending {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("GeoIP did not finish")
	return geoIPResponse{}
}

func TestGeoIPAPIAuthAndTrustedBans(t *testing.T) {
	fake := &helperTiruan{balas: []helperproto.Fail2banJail{{Name: "sshd", BannedIPs: []string{"192.0.2.1", "::ffff:192.0.2.1"}}, {Name: "other", BannedIPs: []string{"192.0.2.1"}}}}
	router, st := buatServerCron(t, fake)
	path := "/api/security/fail2ban/geoip?ip=8.8.8.8"
	for _, sudo := range []bool{false, true} {
		sess := buatSesi(t, st, fmt.Sprintf("user%v", sudo), sudo)
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if !sudo {
			if rw.Code != 403 || len(fake.riwayatOperasi()) != 0 {
				t.Fatal(rw.Code)
			}
			continue
		}
		var got geoIPResponse
		if rw.Code != 200 || json.Unmarshal(rw.Body.Bytes(), &got) != nil || got.Pending || len(got.Items) != 1 || got.Items[0].IP != "192.0.2.1" || got.Items[0].Status != "private" {
			t.Fatalf("status=%d body=%s", rw.Code, rw.Body.String())
		}
		if fake.cmd != helperproto.CmdFail2banList {
			t.Fatal(fake.cmd)
		}
	}
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, httptest.NewRequest("GET", path, nil))
	if rw.Code != 401 {
		t.Fatal(rw.Code)
	}
}

func TestGeoIPDetailCachedOnly(t *testing.T) {
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { t.Error("detail performed lookup") })
	if err := st.PutGeoIP(store.GeoIP{IP: "8.8.8.8", Country: "United States", Region: "California", City: "Mountain View", Status: "ready", FetchedAt: time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, geoip: w}
	detail := helperproto.Fail2banDetail{IP: "::ffff:8.8.8.8", Location: "Unavailable", Warnings: []string{"Location unavailable: no local Geo-IP facility configured; IP was not shared externally.", "History limited"}}
	s.enrichFail2banLocation(&detail)
	if detail.Location != "Mountain View, California, United States" || len(detail.Warnings) != 1 || detail.Warnings[0] != "History limited" {
		t.Fatal(detail)
	}
	detail.IP = "1.1.1.1"
	s.enrichFail2banLocation(&detail)
	if detail.Location != "Unavailable" {
		t.Fatal(detail)
	}
}

func TestGeoIPTimeoutAndBoundedQueue(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	w, _ := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	if w.client.Timeout != 5*time.Second {
		t.Fatal(w.client.Timeout)
	}
	// Exercise timeout cheaply; production deadline checked above.
	w.client.Timeout = 50 * time.Millisecond
	start := time.Now()
	got := w.items([]string{"8.8.8.8"})
	if !got.Pending || time.Since(start) > 100*time.Millisecond {
		t.Fatal("blocking lookup")
	}
	got = waitGeo(t, w, []string{"8.8.8.8"})
	if got.Items[0].Status != "error" || calls.Load() != 1 {
		t.Fatal(got)
	}
	ips := []string{}
	for i := 1; i <= 600; i++ {
		ips = append(ips, fmt.Sprintf("8.9.%d.%d", i/250, i%250+1))
	}
	w.items(ips)
	w.mu.Lock()
	n := len(w.inflight)
	w.mu.Unlock()
	if n > 257 {
		t.Fatalf("unbounded=%d", n)
	}
}

func TestGeoIPStaleRefreshPreservesSuccess(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-release; rw.WriteHeader(503) })
	old := store.GeoIP{IP: "8.8.8.8", Country: "Old", Status: "ready", FetchedAt: time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)}
	if err := st.PutGeoIP(old); err != nil {
		t.Fatal(err)
	}
	got := w.items([]string{old.IP})
	if !got.Pending || got.Items[0].Country != "Old" || got.Items[0].Status != "ready" {
		t.Fatal(got)
	}
	<-started
	release <- struct{}{}
	got = waitGeo(t, w, []string{old.IP})
	if got.Items[0].Country != "Old" || got.Items[0].Status != "ready" {
		t.Fatal(got)
	}
	stored, _, err := st.GeoIP(old.IP)
	if err != nil || stored.FetchedAt != old.FetchedAt || stored.RetryAt <= time.Now().Unix() {
		t.Fatal(stored, err)
	}
}

func TestGeoIPRateLimitCooldown(t *testing.T) {
	for i, header := range []string{"120", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat), ""} {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			var calls atomic.Int32
			w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				rw.Header().Set("Retry-After", header)
				rw.WriteHeader(429)
			})
			got := waitGeo(t, w, []string{"8.8.8.8", "1.1.1.1"})
			if calls.Load() != 1 {
				t.Fatalf("cooldown calls=%d", calls.Load())
			}
			for _, g := range got.Items {
				if g.Status != "rate_limited" {
					t.Fatalf("%+v", g)
				}
			}
			stored, _, _ := st.GeoIP("8.8.8.8")
			if stored.RetryAt < time.Now().Add(time.Minute).Unix() {
				t.Fatal(stored)
			}
			w2 := newGeoIPWorker(st)
			w2.baseURL = w.baseURL
			defer w2.close()
			got = w2.items([]string{"9.9.9.9"})
			if got.Pending || got.Items[0].Status != "rate_limited" {
				t.Fatal(got)
			}
		})
	}
}

func TestGeoIPFailuresNegativeCache(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		code               int
	}{
		{"unavailable", `{"ip":"8.8.8.8","success":false}`, "unavailable", 200},
		{"missing_ip", `{"success":false}`, "unavailable", 200},
		{"mismatch", `{"ip":"1.1.1.1","success":true}`, "error", 200},
		{"malformed", `{`, "error", 200},
		{"oversized", strings.Repeat(" ", 65537), "error", 200},
		{"http", `{}`, "error", 503},
		{"redirect", `{}`, "error", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			w, _ := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				rw.Header().Set("Location", "http://127.0.0.1/")
				rw.WriteHeader(tc.code)
				rw.Write([]byte(tc.body))
			})
			got := waitGeo(t, w, []string{"8.8.8.8"})
			if got.Items[0].Status != tc.status {
				t.Fatalf("%+v", got)
			}
			for i := 0; i < 10; i++ {
				w.items([]string{"8.8.8.8"})
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}

func TestGeoIPNonpublicNeverLeavesHost(t *testing.T) {
	var calls atomic.Int32
	w, _ := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { calls.Add(1); rw.Write([]byte(`{"success":false}`)) })
	ips := []string{"10.0.0.1", "127.0.0.1", "169.254.1.1", "224.0.0.1", "0.0.0.0", "192.0.2.1", "198.51.100.1", "203.0.113.1", "100.64.0.1", "198.18.0.1", "240.0.0.1", "192.0.0.1", "192.88.99.1", "::", "::1", "fc00::1", "fe80::1", "ff02::1", "2001:db8::1", "2001:2::1", "2001:20::1", "2001::1", "2002:a00:1::1", "64:ff9b::a00:1", "100::1", "3fff::1", "::ffff:127.0.0.1"}
	got := waitGeo(t, w, ips)
	if calls.Load() != 0 {
		t.Fatalf("private lookups=%d", calls.Load())
	}
	for _, g := range got.Items {
		if g.Status != "private" || g.Source != "none" {
			t.Fatalf("%+v", g)
		}
	}
}

func TestGeoIPOnlyCanonicalCacheMisses(t *testing.T) {
	var calls atomic.Int32
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RawQuery != "" {
			t.Error(r.URL)
		}
		fmt.Fprintf(rw, `{"ip":%q,"success":true,"country":"United States","country_code":"US","region":"California","city":"Mountain View","connection":{"asn":15169,"isp":"Google","org":"Google LLC"},"timezone":{"id":"America/Los_Angeles"}}`, strings.TrimPrefix(r.URL.Path, "/"))
	})
	ips := []string{}
	for i := 1; i <= 20; i++ {
		ip := fmt.Sprintf("8.8.8.%d", i)
		for repeat := 0; repeat < 100; repeat++ {
			ips = append(ips, ip, "::ffff:"+ip)
		}
		if i <= 10 {
			if err := st.PutGeoIP(store.GeoIP{IP: ip, Country: "Cached", Status: "ready", Data: json.RawMessage(fmt.Sprintf(`{"ip":%q,"success":true}`, ip)), FetchedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := waitGeo(t, w, ips)
	if len(got.Items) != 20 || calls.Load() != 10 {
		t.Fatalf("items=%d calls=%d", len(got.Items), calls.Load())
	}
	for _, g := range got.Items {
		if g.Status != "ready" || g.Source != "cache" {
			t.Fatalf("%+v", g)
		}
	}
	if got.Items[10].ASN != 15169 || got.Items[10].Timezone != "America/Los_Angeles" {
		t.Fatal(got.Items[10])
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.items([]string{"1.1.1.1", "::ffff:1.1.1.1"}) }()
	}
	wg.Wait()
	got = waitGeo(t, w, []string{"1.1.1.1"})
	if calls.Load() != 11 || got.Items[0].Status != "ready" {
		t.Fatalf("calls=%d got=%+v", calls.Load(), got)
	}
}
