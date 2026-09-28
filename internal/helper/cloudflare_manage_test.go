package helper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestCloudflareManagerZonesRecordsAndMutations(t *testing.T) {
	const zone = "0123456789abcdef0123456789abcdef"
	const id = "abcdef0123456789abcdef0123456789"
	var calls []string
	present := true
	content := "one"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("wrong auth")
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/zones":
			fmt.Fprintf(w, `{"success":true,"result":[{"id":%q,"name":"example.test"}],"result_info":{"total_pages":1}}`, zone)
		case r.Method == "GET" && r.URL.Path == "/zones/"+zone+"/dns_records":
			if r.URL.Query().Get("type") != "" || r.URL.Query().Get("name") != "" {
				t.Error("unexpected filtering")
			}
			fmt.Fprintf(w, `{"success":true,"result":[{"id":%q,"type":"TXT","name":"example.test","content":"one"},{"id":"2","type":"MX","name":"example.test","content":"mail.example.test","priority":10}],"result_info":{"total_pages":2,"total_count":102}}`, id)
		case r.Method == "GET" && r.URL.Path == "/zones/"+zone+"/dns_records/"+id:
			if !present {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"errors":[]}`)
				return
			}
			fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"type":"TXT","name":"example.test","content":%q}}`, id, content)
		case r.Method == "PATCH" || r.Method == "POST":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["type"] != "TXT" || body["content"] != "two" {
				t.Errorf("body: %v", body)
			}
			present, content = true, "two"
			fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"type":"TXT","name":"example.test","content":"two"}}`, id)
		case r.Method == "DELETE" && r.URL.Path == "/zones/"+zone+"/dns_records/"+id:
			present = false
			fmt.Fprint(w, `{"success":true,"result":{"id":"ok"}}`)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()
	oldURL, oldClient := cloudflareAPIBase, cloudflareHTTPClient
	cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { cloudflareAPIBase, cloudflareHTTPClient = oldURL, oldClient })
	zones, err := managedZones("test-token")
	if err != nil || len(zones) != 1 {
		t.Fatalf("zones: %v %v", zones, err)
	}
	page, err := managedRecords("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, Page: 1})
	if err != nil || len(page.Records) != 2 || page.TotalPages != 2 || page.TotalCount != 102 {
		t.Fatalf("records: %+v %v", page, err)
	}
	if _, err := managedSave("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, RecordID: id, Record: json.RawMessage(`{"type":"TXT","name":"example.test","content":"two"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := managedDelete("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, RecordID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := managedSave("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, Record: json.RawMessage(`{"type":"TXT","name":"example.test","content":"two"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := managedSave("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, Record: json.RawMessage(`{"type":"TXT","name":"example.test","content":"two","id":"attacker"}`)}); err == nil {
		t.Error("injected field accepted")
	}
	if err := managedDelete("test-token", helperproto.CloudflareManagedArgs{ZoneID: "../../etc", RecordID: id}); err == nil {
		t.Error("path traversal accepted")
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"GET /zones/" + zone + "/dns_records?per_page=100&page=1", "PATCH /zones/" + zone + "/dns_records/" + id, "DELETE /zones/" + zone + "/dns_records/" + id, "POST /zones/" + zone + "/dns_records"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
}
