package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var cloudflareID = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func managedZones(token string) ([]helperproto.CloudflareZone, error) {
	zones := []helperproto.CloudflareZone{}
	for page := 1; ; page++ {
		var result struct {
			cloudflareEnvelope[[]helperproto.CloudflareZone]
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if err := cloudflareCall(token, http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &result); err != nil {
			return nil, err
		}
		if !result.Success {
			return nil, errors.New("pencarian zone ditolak")
		}
		zones = append(zones, result.Result...)
		if page >= result.ResultInfo.TotalPages {
			return zones, nil
		}
	}
}

// ponytail: daftar zone diverifikasi per operasi; jika akun punya ribuan zone, gunakan cache berumur pendek.
func managedZone(token, id string) error {
	if !cloudflareID.MatchString(id) {
		return errors.New("ID zone tidak valid")
	}
	zones, err := managedZones(token)
	if err != nil {
		return err
	}
	for _, z := range zones {
		if z.ID == id {
			return nil
		}
	}
	return errors.New("zone tidak diizinkan")
}

func managedRecords(token string, args helperproto.CloudflareManagedArgs) (helperproto.CloudflareRecordPage, error) {
	var out helperproto.CloudflareRecordPage
	if err := managedZone(token, args.ZoneID); err != nil {
		return out, err
	}
	if args.Page < 1 || args.Page > 100000 {
		return out, errors.New("halaman tidak valid")
	}
	var result struct {
		cloudflareEnvelope[[]json.RawMessage]
		ResultInfo struct {
			TotalPages int `json:"total_pages"`
			TotalCount int `json:"total_count"`
		} `json:"result_info"`
	}
	path := fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", args.ZoneID, args.Page)
	if err := cloudflareCall(token, http.MethodGet, path, nil, &result); err != nil {
		return out, err
	}
	if !result.Success {
		return out, errors.New("pembacaan DNS ditolak")
	}
	out = helperproto.CloudflareRecordPage{Records: result.Result, Page: args.Page, TotalPages: result.ResultInfo.TotalPages, TotalCount: result.ResultInfo.TotalCount}
	if out.Records == nil {
		out.Records = []json.RawMessage{}
	}
	return out, rejectCloudflareTokenEcho(token, out)
}

func managedRecord(token string, args helperproto.CloudflareManagedArgs) (json.RawMessage, error) {
	if !cloudflareID.MatchString(args.RecordID) {
		return nil, errors.New("ID record tidak valid")
	}
	var result cloudflareEnvelope[json.RawMessage]
	path := "/zones/" + args.ZoneID + "/dns_records/" + args.RecordID
	if err := cloudflareCall(token, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	if !result.Success || len(result.Result) == 0 {
		return nil, errors.New("record tidak ditemukan")
	}
	return result.Result, nil
}

func managedSave(token string, args helperproto.CloudflareManagedArgs) (json.RawMessage, error) {
	if err := managedZone(token, args.ZoneID); err != nil {
		return nil, err
	}
	if args.RecordID != "" {
		if _, err := managedRecord(token, args); err != nil {
			return nil, err
		}
	}
	var input map[string]json.RawMessage
	if len(args.Record) > 32768 || json.Unmarshal(args.Record, &input) != nil || input == nil {
		return nil, errors.New("JSON record tidak valid")
	}
	allowed := map[string]bool{"type": true, "name": true, "content": true, "data": true, "ttl": true, "proxied": true, "priority": true, "comment": true, "tags": true, "settings": true}
	for key := range input {
		if !allowed[key] {
			return nil, fmt.Errorf("field record tidak didukung: %s", key)
		}
	}
	var typ, name string
	if json.Unmarshal(input["type"], &typ) != nil || !regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}$`).MatchString(typ) {
		return nil, errors.New("jenis record tidak valid")
	}
	if json.Unmarshal(input["name"], &name) != nil || name == "" || len(name) > 253 || strings.ContainsAny(name, "/\\\r\n\x00") {
		return nil, errors.New("nama record tidak valid")
	}
	var recordZone string
	zones, err := managedZones(token)
	if err != nil {
		return nil, err
	}
	for _, z := range zones {
		if z.ID == args.ZoneID {
			recordZone = z.Name
			break
		}
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	recordZone = strings.ToLower(recordZone)
	if recordZone == "" || (name != recordZone && !strings.HasSuffix(name, "."+recordZone)) {
		return nil, errors.New("nama record di luar zone")
	}
	if _, ok := input["content"]; !ok {
		if _, ok := input["data"]; !ok {
			return nil, errors.New("content atau data wajib diisi")
		}
	}
	path := "/zones/" + args.ZoneID + "/dns_records"
	method := http.MethodPost
	if args.RecordID != "" {
		method, path = http.MethodPatch, path+"/"+args.RecordID
	}
	var result cloudflareEnvelope[json.RawMessage]
	if err := cloudflareCall(token, method, path, input, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, errors.New("perubahan DNS ditolak")
	}
	if err := rejectCloudflareTokenEcho(token, result.Result); err != nil {
		return nil, err
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(result.Result, &saved); err != nil || !cloudflareID.MatchString(saved.ID) {
		return nil, errors.New("respons record tidak valid")
	}
	args.RecordID = saved.ID
	verified, err := managedRecord(token, args)
	if err != nil {
		return nil, err
	}
	return verified, rejectCloudflareTokenEcho(token, verified)
}

func managedDelete(token string, args helperproto.CloudflareManagedArgs) error {
	if err := managedZone(token, args.ZoneID); err != nil {
		return err
	}
	if _, err := managedRecord(token, args); err != nil {
		return err
	}
	var result cloudflareEnvelope[json.RawMessage]
	if err := cloudflareCall(token, http.MethodDelete, "/zones/"+args.ZoneID+"/dns_records/"+args.RecordID, nil, &result); err != nil {
		return err
	}
	if !result.Success {
		return errors.New("penghapusan DNS ditolak")
	}
	if _, err := managedRecord(token, args); err == nil {
		return errors.New("record masih ada setelah penghapusan")
	} else if !strings.HasPrefix(err.Error(), "Cloudflare API HTTP 404:") {
		return err
	}
	return nil
}

func managedToken() (string, error) { return cloudflareTokenRead() }
func managedError(err error) error {
	if err != nil {
		return errors.New("permintaan Cloudflare DNS gagal")
	}
	return nil
}
