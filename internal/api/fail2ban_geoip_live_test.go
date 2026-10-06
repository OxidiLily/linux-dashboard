package api

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"linux-dashboard/OxidiLily/internal/store"
)

// Opt-in probe: sends only the explicitly supplied public IP to ipwho.is.
func TestGeoIPLiveSQLiteCache(t *testing.T) {
	ip := os.Getenv("FAIL2BAN_GEOIP_TEST_IP")
	if ip == "" {
		t.Skip("set FAIL2BAN_GEOIP_TEST_IP for an authorized external lookup")
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !publicGeoIP(addr) {
		t.Fatal("public IP required")
	}
	ip = addr.Unmap().String()
	path := filepath.Join(t.TempDir(), "geo.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := newGeoIPWorker(st)
	first := waitGeo(t, w, []string{ip, ip})
	if len(first.Items) != 1 || first.Items[0].Status != "ready" {
		t.Fatalf("lookup failed: %+v", first)
	}
	t.Logf("provider: IP=%s country=%s region=%s city=%s ASN=%d", ip, first.Items[0].Country, first.Items[0].Region, first.Items[0].City, first.Items[0].ASN)
	cached, ok, err := st.GeoIP(ip)
	if err != nil || !ok || !json.Valid(cached.Data) {
		t.Fatalf("full payload not persisted: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(cached.Data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"type", "continent", "latitude", "longitude", "is_eu", "flag", "connection", "timezone"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("provider field missing: %s", key)
		}
	}
	t.Logf("full provider payload persisted: %d top-level fields", len(fields))
	w.close()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w = newGeoIPWorker(st)
	defer w.close()
	w.baseURL = "http://127.0.0.1:1/" // Cache must succeed without provider connectivity.
	second := w.items([]string{ip, ip})
	if second.Pending || len(second.Items) != 1 || second.Items[0].Status != "ready" || second.Items[0].Source != "cache" {
		t.Fatalf("persistent cache failed: %+v", second)
	}
	reopened, ok, err := st.GeoIP(ip)
	if err != nil || !ok || !bytes.Equal(cached.Data, reopened.Data) {
		t.Fatal("full payload changed after SQLite reopen")
	}
	t.Log("SQLite reopen: exact full payload cache hit; no provider lookup needed")
}
