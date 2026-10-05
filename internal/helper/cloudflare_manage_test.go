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

func TestCloudflareProxyOnly(t *testing.T) {
	for _, cmd := range []string{helperproto.CmdProxyCloudflareRecordSave, helperproto.CmdProxyCloudflareRecordDelete} {
		if !sudoRequired[cmd] {
			t.Fatalf("sudo missing: %s", cmd)
		}
	}
	const zone = "0123456789abcdef0123456789abcdef"
	const id = "abcdef0123456789abcdef0123456789"
	for _, tc := range []struct {
		name, typ, payload        string
		proxiable, want, mismatch bool
	}{
		{"enable", "A", `{"proxied":true}`, true, true, false},
		{"disable", "AAAA", `{"proxied":false}`, true, true, false},
		{"cname", "CNAME", `{"proxied":true}`, true, true, false},
		{"txt", "TXT", `{"proxied":true}`, true, false, false},
		{"not-proxiable", "A", `{"proxied":true}`, false, false, false},
		{"null", "A", `{"proxied":null}`, true, false, false},
		{"string", "A", `{"proxied":"true"}`, true, false, false},
		{"extra", "A", `{"proxied":true,"ttl":1}`, true, false, false},
		{"readback", "A", `{"proxied":true}`, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patches, gets := 0, 0
			proxied := tc.name == "disable"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/zones":
					fmt.Fprintf(w, `{"success":true,"result":[{"id":%q,"name":"example.test"}]}`, zone)
				case r.Method == "GET" && r.URL.Path == "/zones/"+zone+"/dns_records/"+id:
					gets++
					fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"type":%q,"name":"example.test","proxiable":%t,"proxied":%t}}`, id, tc.typ, tc.proxiable, proxied)
				case r.Method == "PATCH" && r.URL.Path == "/zones/"+zone+"/dns_records/"+id:
					patches++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					value, ok := body["proxied"].(bool)
					if len(body) != 1 || !ok {
						t.Errorf("not narrow: %v", body)
					}
					if !tc.mismatch {
						proxied = value
					}
					fmt.Fprintf(w, `{"success":true,"result":{"id":%q}}`, id)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
			}))
			defer srv.Close()
			oldURL, oldClient := cloudflareAPIBase, cloudflareHTTPClient
			cloudflareAPIBase, cloudflareHTTPClient = srv.URL, srv.Client()
			defer func() { cloudflareAPIBase, cloudflareHTTPClient = oldURL, oldClient }()
			_, err := managedSave("test-token", helperproto.CloudflareManagedArgs{ZoneID: zone, RecordID: id, Record: json.RawMessage(tc.payload)})
			if (err == nil) != tc.want {
				t.Fatalf("success=%t err=%v", tc.want, err)
			}
			if tc.want || tc.mismatch {
				if patches != 1 || gets != 2 {
					t.Fatalf("patches=%d gets=%d", patches, gets)
				}
			} else if patches != 0 {
				t.Fatal("invalid input mutated DNS")
			}
		})
	}
}

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
