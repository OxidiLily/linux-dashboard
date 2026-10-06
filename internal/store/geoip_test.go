package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGeoIPCanonicalPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geo.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	record := GeoIP{IP: "::ffff:8.8.8.8", Country: "United States", CountryCode: "US", Region: "California", City: "Mountain View", ISP: "Google", Org: "Google LLC", ASN: 15169, Timezone: "America/Los_Angeles", FetchedAt: time.Now().UTC().Format(time.RFC3339), Status: "ready", Error: "", RetryAt: 0}
	if err := s.PutGeoIP(record); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.GeoIP("8.8.8.8")
	if err != nil || !ok || got.IP != "8.8.8.8" || got.Country != record.Country || got.ASN != record.ASN || got.Timezone != record.Timezone {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if err := s.PutGeoIP(GeoIP{IP: "invalid"}); err == nil {
		t.Fatal("invalid IP stored")
	}
}
