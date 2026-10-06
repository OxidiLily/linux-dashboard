package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeoIPPayloadNesting(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, depth := range []int{32, 33, 6001} {
		raw := json.RawMessage(`{"ip":"8.8.8.8","success":true,"extra":` + strings.Repeat("[", depth-1) + `0` + strings.Repeat("]", depth-1) + `}`)
		if !json.Valid(raw) {
			t.Fatal("invalid fixture")
		}
		err := st.PutGeoIP(GeoIP{IP: "8.8.8.8", Status: "ready", Data: raw})
		if depth <= 32 && err != nil {
			t.Fatal(err)
		}
		if depth > 32 && err == nil {
			t.Errorf("accepted nesting depth %d", depth)
		}
	}
	got, ok, err := st.GeoIP("8.8.8.8")
	if err != nil || !ok || strings.Count(string(got.Data), "[") != 31 {
		t.Fatal("rejected payload replaced cache", err)
	}
}

func TestGeoIPPayloadMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE geoip_cache(ip TEXT PRIMARY KEY,country TEXT NOT NULL,country_code TEXT NOT NULL,region TEXT NOT NULL,city TEXT NOT NULL,isp TEXT NOT NULL,org TEXT NOT NULL,asn INTEGER NOT NULL,timezone TEXT NOT NULL,fetched_at TEXT NOT NULL,status TEXT NOT NULL,error TEXT NOT NULL,retry_at INTEGER NOT NULL); INSERT INTO geoip_cache VALUES('8.8.8.8','Old','','','','','',0,'','','ready','',0)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		old, ok, err := st.GeoIP("8.8.8.8")
		if err != nil || !ok || old.Country != "Old" || len(old.Data) != 0 {
			t.Fatal(old, err)
		}
		st.Close()
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	raw := json.RawMessage(` {"ip":"8.8.8.8","success":true,"flag":{"emoji":"","img":"https://example.invalid/flag.svg"},"timezone":{"is_dst":false,"offset":0},"unknown":null} `)
	if err := st.PutGeoIP(GeoIP{IP: "::ffff:8.8.8.8", Status: "ready", Data: raw}); err != nil {
		t.Fatal(err)
	}
	got, _, err := st.GeoIP("8.8.8.8")
	if err != nil || !json.Valid(got.Data) || strings.HasPrefix(string(got.Data), " ") {
		t.Fatal(got, err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(got.Data, &obj)
	if string(obj["timezone"]) != `{"is_dst":false,"offset":0}` || string(obj["unknown"]) != "null" {
		t.Fatal(string(got.Data))
	}
	for _, bad := range []string{`{`, `[]`, `null`, `{"ip":"1.1.1.1","success":true}`, `{"ip":"8.8.8.8","success":false}`, strings.Repeat(" ", 65537)} {
		if err := st.PutGeoIP(GeoIP{IP: "8.8.8.8", Status: "ready", Data: json.RawMessage(bad)}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
