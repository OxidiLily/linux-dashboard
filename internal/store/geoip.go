package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
)

type GeoIP struct {
	Data        json.RawMessage `json:"-"`
	IP          string          `json:"ip"`
	Country     string          `json:"country"`
	CountryCode string          `json:"country_code"`
	Region      string          `json:"region"`
	City        string          `json:"city"`
	ISP         string          `json:"isp"`
	Org         string          `json:"org"`
	ASN         int64           `json:"asn"`
	Timezone    string          `json:"timezone"`
	FetchedAt   string          `json:"fetched_at"`
	Status      string          `json:"status"`
	Source      string          `json:"source"`
	Error       string          `json:"-"`
	RetryAt     int64           `json:"-"`
}

func canonicalGeoIP(ip string) (string, error) {
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Zone() != "" {
		return "", fmt.Errorf("invalid IP")
	}
	return a.Unmap().String(), nil
}

func (s *Store) GeoIP(ip string) (GeoIP, bool, error) {
	ip, err := canonicalGeoIP(ip)
	if err != nil {
		return GeoIP{}, false, err
	}
	var g GeoIP
	var data string
	err = s.db.QueryRow(`SELECT ip,country,country_code,region,city,isp,org,asn,timezone,fetched_at,status,error,retry_at,data FROM geoip_cache WHERE ip=?`, ip).Scan(&g.IP, &g.Country, &g.CountryCode, &g.Region, &g.City, &g.ISP, &g.Org, &g.ASN, &g.Timezone, &g.FetchedAt, &g.Status, &g.Error, &g.RetryAt, &data)
	if err == sql.ErrNoRows {
		return GeoIP{}, false, nil
	}
	g.Data = json.RawMessage(data)
	g.Source = "cache"
	return g, err == nil, err
}

// ValidateGeoIPNesting bounds containers, including unknown provider fields.
func ValidateGeoIPNesting(data []byte) error {
	if len(data) > 65536 || !json.Valid(data) {
		return fmt.Errorf("invalid GeoIP payload")
	}
	depth := 0
	quoted, escaped := false, false
	for _, c := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > 32 {
				return fmt.Errorf("GeoIP payload nesting exceeds 32")
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

func (s *Store) PutGeoIP(g GeoIP) error {
	ip, err := canonicalGeoIP(g.IP)
	if err != nil {
		return err
	}
	if len(g.Data) > 0 {
		if err := ValidateGeoIPNesting(g.Data); err != nil {
			return err
		}
		var identity struct {
			IP      string `json:"ip"`
			Success bool   `json:"success"`
		}
		if json.Unmarshal(g.Data, &identity) != nil || !identity.Success {
			return fmt.Errorf("invalid GeoIP payload")
		}
		payloadIP, e := canonicalGeoIP(identity.IP)
		if e != nil || payloadIP != ip {
			return fmt.Errorf("GeoIP payload IP mismatch")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, g.Data); err != nil {
			return err
		}
		g.Data = compact.Bytes()
	}
	_, err = s.db.Exec(`INSERT INTO geoip_cache(ip,country,country_code,region,city,isp,org,asn,timezone,fetched_at,status,error,retry_at,data) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(ip) DO UPDATE SET country=excluded.country,country_code=excluded.country_code,region=excluded.region,city=excluded.city,isp=excluded.isp,org=excluded.org,asn=excluded.asn,timezone=excluded.timezone,fetched_at=excluded.fetched_at,status=excluded.status,error=excluded.error,retry_at=excluded.retry_at,data=excluded.data`, ip, g.Country, g.CountryCode, g.Region, g.City, g.ISP, g.Org, g.ASN, g.Timezone, g.FetchedAt, g.Status, g.Error, g.RetryAt, string(g.Data))
	return err
}

func (s *Store) GeoIPCooldown() (int64, error) {
	var until int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(retry_at),0) FROM geoip_cache WHERE status='rate_limited' OR error='rate_limited'`).Scan(&until)
	return until, err
}
