package api

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"io"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/store"
)

func (s *Server) handleFail2banGeoIP(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var jails []helperproto.Fail2banJail
	if err := s.helper.Call(helperproto.CmdFail2banList, sessionFrom(r).HelperToken, nil, &jails); err != nil {
		writeHelperErr(w, err)
		return
	}
	ips := []string{}
	for _, jail := range jails {
		ips = append(ips, jail.BannedIPs...)
	}
	writeJSON(w, http.StatusOK, s.geoip.items(ips))
}

// Payload lookups accept only current helper bans; history uses /detail and cached summaries.
func (s *Server) handleFail2banGeoIPDetail(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	jail := chi.URLParam(r, "jail")
	addr, err := netip.ParseAddr(r.URL.Query().Get("ip"))
	if !detailJailRE.MatchString(jail) || err != nil || addr.Zone() != "" {
		writeErr(w, http.StatusBadRequest, "jail atau alamat IP tidak valid")
		return
	}
	var jails []helperproto.Fail2banJail
	if err := s.helper.Call(helperproto.CmdFail2banList, sessionFrom(r).HelperToken, nil, &jails); err != nil {
		writeHelperErr(w, err)
		return
	}
	for _, entry := range jails {
		if entry.Name != jail {
			continue
		}
		for _, raw := range entry.BannedIPs {
			banned, err := netip.ParseAddr(raw)
			if err != nil || banned.Zone() != "" || banned.Unmap() != addr.Unmap() {
				continue
			}
			result := s.geoip.items([]string{raw})
			if len(result.Items) == 0 {
				writeErr(w, http.StatusServiceUnavailable, "GeoIP unavailable")
				return
			}
			g := result.Items[0]
			status := g.Status
			if len(g.Data) == 0 && status == "ready" {
				status = "unavailable"
				if result.Pending {
					status = "pending"
				}
			}
			var data json.RawMessage
			if len(g.Data) > 0 {
				data = g.Data
			}
			writeJSON(w, http.StatusOK, struct {
				IP        string          `json:"ip"`
				Status    string          `json:"status"`
				Source    string          `json:"source"`
				FetchedAt string          `json:"fetched_at"`
				Data      json.RawMessage `json:"data"`
			}{g.IP, status, g.Source, g.FetchedAt, data})
			return
		}
	}
	writeErr(w, http.StatusNotFound, "IP tidak diblokir pada jail ini")
}

func (s *Server) enrichFail2banLocation(out *helperproto.Fail2banDetail) {
	warnings := []string{}
	for _, warning := range out.Warnings {
		if !strings.Contains(warning, "no local Geo-IP facility") && !strings.Contains(warning, "IP was not shared externally") {
			warnings = append(warnings, warning)
		}
	}
	out.Warnings = warnings
	out.Location = "Unavailable"
	a, err := netip.ParseAddr(out.IP)
	if err != nil || a.Zone() != "" || !publicGeoIP(a) {
		return
	}
	g, ok, err := s.store.GeoIP(a.Unmap().String())
	if err != nil || !ok || g.Status != "ready" {
		return
	}
	parts := []string{}
	for _, part := range []string{g.City, g.Region, g.Country} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) > 0 {
		out.Location = strings.Join(parts, ", ")
	}
}

const geoIPTTL = 30 * 24 * time.Hour

// Conservative special-use exclusions; never disclose nonpublic addresses.
var geoIPReserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}()

func publicGeoIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || (a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a)) {
		return false
	}
	for _, p := range geoIPReserved {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

type geoIPResponse struct {
	Items   []store.GeoIP `json:"items"`
	Pending bool          `json:"pending"`
}

type geoIPWorker struct {
	mu       sync.Mutex
	store    *store.Store
	client   *http.Client
	baseURL  string
	queue    chan string
	inflight map[string]bool
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	started  bool
	closed   bool
	cooldown int64
}

func newGeoIPWorker(st *store.Store) *geoIPWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &geoIPWorker{store: st, client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, baseURL: "https://ipwho.is/", queue: make(chan string, 256), inflight: map[string]bool{}, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (w *geoIPWorker) close() error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		w.cancel()
		clear(w.inflight)
		if !w.started {
			close(w.done)
		}
	}
	w.mu.Unlock()
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-w.done:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	}
}

func (w *geoIPWorker) items(ips []string) geoIPResponse {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := geoIPResponse{Items: []store.GeoIP{}}
	if w.closed {
		return out
	}
	if !w.started {
		w.started = true
		w.cooldown, _ = w.store.GeoIPCooldown()
		go w.run()
	}
	seen := map[string]bool{}
	now := time.Now()
	for _, raw := range ips {
		a, err := netip.ParseAddr(raw)
		if err != nil || a.Zone() != "" {
			continue
		}
		ip := a.Unmap().String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		if !publicGeoIP(a) {
			out.Items = append(out.Items, store.GeoIP{IP: ip, Status: "private", Source: "none"})
			continue
		}
		g, ok, err := w.store.GeoIP(ip)
		if err != nil {
			out.Items = append(out.Items, store.GeoIP{IP: ip, Status: "error", Source: "none"})
			continue
		}
		fetched, _ := time.Parse(time.RFC3339, g.FetchedAt)
		if !ok {
			g = store.GeoIP{IP: ip, Status: "pending", Source: "none"}
		}
		fresh := ok && g.Status == "ready" && len(g.Data) > 0 && now.Sub(fetched) < geoIPTTL
		if !fresh && g.RetryAt <= now.Unix() && w.cooldown <= now.Unix() {
			if !w.inflight[ip] {
				select {
				case w.queue <- ip:
					w.inflight[ip] = true
				default:
				}
			}
			out.Pending = true
			if g.Status != "ready" {
				g.Status = "pending"
			}
		} else if !fresh && w.cooldown > now.Unix() && g.Status != "ready" {
			g.Status = "rate_limited"
		}
		if w.inflight[ip] {
			out.Pending = true
		}
		out.Items = append(out.Items, g)
	}
	return out
}

func (w *geoIPWorker) run() {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case ip := <-w.queue:
			w.mu.Lock()
			if w.closed {
				w.mu.Unlock()
				return
			}
			cooldown := w.cooldown
			w.mu.Unlock()
			g := store.GeoIP{IP: ip, Status: "rate_limited", Source: "ipwho.is", RetryAt: cooldown, Error: "rate_limited"}
			if cooldown <= time.Now().Unix() {
				g = w.fetch(ip)
			}
			w.mu.Lock()
			if w.closed {
				w.mu.Unlock()
				return
			}
			if g.Status == "rate_limited" {
				w.cooldown = g.RetryAt
			}
			old, ok, err := w.store.GeoIP(ip)
			if err == nil && ok && old.Status == "ready" && g.Status != "ready" {
				old.Error = g.Status
				old.RetryAt = g.RetryAt
				g = old
			}
			if err := w.store.PutGeoIP(g); err != nil {
				log.Printf("GeoIP cache write: %v", err)
			}
			delete(w.inflight, ip)
			w.mu.Unlock()
		}
	}
}

func (w *geoIPWorker) fetch(ip string) store.GeoIP {
	now := time.Now().UTC()
	g := store.GeoIP{IP: ip, Status: "error", Source: "ipwho.is", FetchedAt: now.Format(time.RFC3339), RetryAt: now.Add(5 * time.Minute).Unix()}
	req, err := http.NewRequestWithContext(w.ctx, http.MethodGet, w.baseURL+ip, nil)
	if err != nil {
		g.Error = "lookup failed"
		return g
	}
	resp, err := w.client.Do(req)
	if err != nil {
		g.Error = "lookup failed"
		return g
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		until := now.Add(24 * time.Hour)
		header := resp.Header.Get("Retry-After")
		if seconds, err := strconv.ParseInt(header, 10, 64); err == nil && seconds >= 0 && seconds <= int64((1<<63-1)/int64(time.Second)) {
			until = now.Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(header); err == nil {
			until = date
		}
		if !until.After(now) {
			until = now.Add(time.Second)
		}
		g.Status = "rate_limited"
		g.Error = "rate_limited"
		g.RetryAt = until.Unix()
		return g
	}
	if resp.StatusCode != http.StatusOK {
		g.Error = "provider HTTP error"
		return g
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(b) > 65536 {
		g.Error = "invalid response size"
		return g
	}
	if err := store.ValidateGeoIPNesting(b); err != nil {
		g.Error = "invalid response nesting or JSON"
		return g
	}
	var data struct {
		IP          string `json:"ip"`
		Success     bool   `json:"success"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
		City        string `json:"city"`
		Connection  struct {
			ASN int64  `json:"asn"`
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if json.Unmarshal(b, &data) != nil {
		g.Error = "invalid response"
		return g
	}
	if !data.Success {
		g.Status = "unavailable"
		g.Error = "provider unavailable"
		return g
	}
	a, err := netip.ParseAddr(data.IP)
	if err != nil || a.Zone() != "" || a.Unmap().String() != ip {
		g.Error = "response IP mismatch"
		return g
	}
	g.Data = json.RawMessage(b)
	g.Country = data.Country
	g.CountryCode = data.CountryCode
	g.Region = data.Region
	g.City = data.City
	g.ISP = data.Connection.ISP
	g.Org = data.Connection.Org
	g.ASN = data.Connection.ASN
	g.Timezone = data.Timezone.ID
	g.Status = "ready"
	g.RetryAt = 0
	return g
}
