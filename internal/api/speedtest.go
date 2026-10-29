package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// speedtestResult adalah hasil satu sesi speed test.
type speedtestResult struct {
	// ID unik test — dipakai frontend untuk membedakan polling.
	ID         string  `json:"id"`
	Download   float64 `json:"download_mbps"`   // Mbps
	Upload     float64 `json:"upload_mbps"`     // Mbps
	Ping       float64 `json:"ping_ms"`         // ms
	Jitter     float64 `json:"jitter_ms"`       // ms
	Server     string  `json:"server"`          // nama server uji
	ServerURL  string  `json:"server_url"`      // URL server uji
	Phase      string  `json:"phase"`           // idle|ping|download|upload|done|error
	Progress   float64 `json:"progress"`        // 0..1 progress fase saat ini
	Error      string  `json:"error,omitempty"` // pesan error jika gagal
	StartedAt  int64   `json:"started_at"`
	FinishedAt int64   `json:"finished_at,omitempty"`
}

// serverUji adalah satu server uji publik: endpoint unduh, endpoint unggah
// ("" bila tidak tersedia), batas byte per request unggah, dan napas antar
// request supaya tidak kena rate limit.
type serverUji struct {
	Nama        string
	URL         string
	UnggahURL   string
	ChunkUnggah int64
	Jeda        time.Duration
}

// Server uji bawaan — semua endpoint terverifikasi 2026-10-28 dari mesin ini
// (unduh GET/HEAD 200, unggah POST 200). Bila satu server gagal atau kena
// rate limit (429), test otomatis pindah ke server berikutnya (failover).
var speedTestServers = []serverUji{
	{"Cloudflare", "https://speed.cloudflare.com/__down?bytes=73400320",
		"https://speed.cloudflare.com/__up", 25 << 20, 500 * time.Millisecond},
	{"SpeedtestDoctor", "https://api.speedtest.doctor/down",
		"https://api.speedtest.doctor/up", 5 << 20, 200 * time.Millisecond},
	{"OVH (FR)", "https://proof.ovh.net/files/100Mb.dat", "", 0, 0},
	{"Vultr (US)", "https://lax-ca-us-ping.vultr.com/vultr.com.100MB.bin", "", 0, 0},
}

// Hanya satu test berjalan sekaligus per server.
var (
	stMu      sync.Mutex
	stCurrent *speedtestResult
	stCancel  context.CancelFunc
)

func (s *Server) handleSpeedtestStart(w http.ResponseWriter, r *http.Request) {
	stMu.Lock()
	if stCurrent != nil && stCurrent.Phase != "done" && stCurrent.Phase != "error" && stCurrent.Phase != "idle" {
		stMu.Unlock()
		writeErr(w, http.StatusConflict, "Speed test sedang berjalan")
		return
	}

	// ponytail: context.Background, bukan r.Context() — request selesai = test mati.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	stCancel = cancel

	id := randomHex(8)
	stCurrent = &speedtestResult{
		ID:        id,
		Phase:     "ping",
		StartedAt: time.Now().Unix(),
	}
	result := stCurrent
	stMu.Unlock()

	sess := sessionFrom(r)
	s.store.LogActivity(sess.Username, "speedtest_start", "mulai speed test", nil, clientIP(r))

	go runSpeedTest(ctx, cancel, result)

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSpeedtestStatus(w http.ResponseWriter, r *http.Request) {
	stMu.Lock()
	result := stCurrent
	stMu.Unlock()
	if result == nil {
		writeJSON(w, http.StatusOK, speedtestResult{Phase: "idle"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSpeedtestStop(w http.ResponseWriter, r *http.Request) {
	stMu.Lock()
	if stCancel != nil {
		stCancel()
	}
	stMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// runSpeedTest menjalankan fase ping, download, upload secara berurutan dengan
// failover antar server uji bila server yang sedang dipakai gagal.
func runSpeedTest(ctx context.Context, cancel context.CancelFunc, result *speedtestResult) {
	defer cancel()

	// Urutkan server dari latensi terbaik — gagal di tengah = pindah berikutnya.
	urutan := urutkanServer(ctx, result)
	if ctx.Err() != nil {
		setError(result, "Dibatalkan")
		return
	}

	updateResult(result, func(r *speedtestResult) {
		r.Phase = "download"
		r.Progress = 0
	})

	// Download test.
	dlMbps := measureDownload(ctx, urutan, result)
	if hasError(result) {
		return
	}
	if ctx.Err() != nil {
		setError(result, "Dibatalkan")
		return
	}

	updateResult(result, func(r *speedtestResult) {
		r.Download = dlMbps
		r.Phase = "upload"
		r.Progress = 0
	})

	// Upload test — kirim data acak ke endpoint yang menerima POST.
	ulMbps := measureUpload(ctx, urutan, result)
	if hasError(result) {
		return
	}
	if ctx.Err() != nil {
		setError(result, "Dibatalkan")
		return
	}

	updateResult(result, func(r *speedtestResult) {
		r.Upload = ulMbps
		r.Phase = "done"
		r.Progress = 1
		r.FinishedAt = time.Now().Unix()
	})
}

func updateResult(r *speedtestResult, fn func(*speedtestResult)) {
	stMu.Lock()
	fn(r)
	stMu.Unlock()
}

func setError(r *speedtestResult, msg string) {
	updateResult(r, func(r *speedtestResult) {
		r.Phase = "error"
		r.Error = msg
		r.FinishedAt = time.Now().Unix()
	})
}

// hasError menandai hasil sudah masuk status error — fase berikutnya tidak
// boleh menimpa status error dengan "done"/"upload".
func hasError(r *speedtestResult) bool {
	stMu.Lock()
	defer stMu.Unlock()
	return r.Phase == "error"
}

// urutkanServer mengukur latensi tiap server uji lalu mengembalikan urutan
// indeks dari yang tercepat. Ping/jitter terbaik dicatat ke hasil.
func urutkanServer(ctx context.Context, result *speedtestResult) []int {
	type nilai struct {
		idx    int
		ping   float64
		jitter float64
	}
	terukur := make([]nilai, 0, len(speedTestServers))
	tanpaPing := make([]int, 0)
	for i, srv := range speedTestServers {
		if ctx.Err() != nil {
			break
		}
		updateResult(result, func(r *speedtestResult) {
			r.Phase = "ping"
			r.Progress = float64(i) / float64(len(speedTestServers))
		})
		pings := make([]float64, 0, 3)
		for j := 0; j < 3; j++ {
			if ctx.Err() != nil {
				break
			}
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, "HEAD", srv.URL, nil)
			if err != nil {
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			pings = append(pings, float64(time.Since(start).Microseconds())/1000)
		}
		if len(pings) == 0 {
			tanpaPing = append(tanpaPing, i) // tetap bisa dicoba saat failover
			continue
		}
		terukur = append(terukur, nilai{i, avg(pings), stddev(pings)})
	}
	sort.Slice(terukur, func(a, b int) bool { return terukur[a].ping < terukur[b].ping })
	if len(terukur) > 0 {
		terbaik := speedTestServers[terukur[0].idx]
		updateResult(result, func(r *speedtestResult) {
			r.Ping = round2(terukur[0].ping)
			r.Jitter = terukur[0].jitter
			r.Server = terbaik.Nama
			r.ServerURL = terbaik.URL
		})
	}
	urutan := make([]int, 0, len(speedTestServers))
	for _, n := range terukur {
		urutan = append(urutan, n.idx)
	}
	urutan = append(urutan, tanpaPing...)
	return urutan
}

// speedTestPhase adalah durasi tetap tiap fase ukur (unduh/unggah) supaya kedua
// fase selesai dalam waktu yang sama, seperti speed test pada umumnya.
const speedTestPhase = 10 * time.Second

// progressFase mengubah waktu berjalan menjadi kemajuan fase 0..0.99.
func progressFase(elapsed float64) float64 {
	p := elapsed / speedTestPhase.Seconds()
	if p > 0.99 {
		return 0.99
	}
	return p
}

// hitungReader menghitung byte yang benar-benar dialirkan ke jaringan.
type hitungReader struct {
	r io.Reader
	n *atomic.Int64
}

func (h hitungReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	h.n.Add(int64(n))
	return n, err
}

// jedaFase menunggu d; false bila fase keburu habis / dibatalkan.
func jedaFase(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// unduhDari mengunduh dari satu server sampai fase habis. Mengembalikan true
// bila server cukup sampai akhir fase; false = server gagal dan caller harus
// pindah ke server berikutnya (failover).
func unduhDari(ctx context.Context, srv serverUji, totalBytes *atomic.Int64, result *speedtestResult) bool {
	buf := make([]byte, 256<<10)
	kena429 := 0
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return true // fase habis / dibatalkan — normal
			}
			return false // gagal jaringan — pindah server
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			kena429++
			if kena429 >= 2 {
				return false // rate limit menetap — failover ke server lain
			}
			if !jedaFase(ctx, time.Second) {
				return true
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return false
		}
		kena429 = 0
		updateResult(result, func(r *speedtestResult) {
			r.Server = srv.Nama
			r.ServerURL = srv.URL
		})
		for ctx.Err() == nil {
			n, readErr := resp.Body.Read(buf)
			totalBytes.Add(int64(n))
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()
		if !jedaFase(ctx, srv.Jeda) {
			return true
		}
	}
	return true
}

// measureDownload mengukur unduhan berkelanjutan selama speedTestPhase dengan
// failover antar server uji. Angka live dan angka akhir dihitung dari
// akumulasi byte / waktu yang sama.
func measureDownload(ctx context.Context, urutan []int, result *speedtestResult) float64 {
	phaseCtx, cancel := context.WithTimeout(ctx, speedTestPhase)
	defer cancel()

	start := time.Now()
	var totalBytes atomic.Int64

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				if elapsed > 0.5 {
					updateResult(result, func(r *speedtestResult) {
						r.Download = round2(float64(totalBytes.Load()*8) / elapsed / 1e6)
						r.Progress = progressFase(elapsed)
					})
				}
			case <-stop:
				return
			}
		}
	}()

	for _, i := range urutan {
		if phaseCtx.Err() != nil {
			break
		}
		if unduhDari(phaseCtx, speedTestServers[i], &totalBytes, result) {
			break
		}
	}

	if totalBytes.Load() == 0 && ctx.Err() == nil {
		setError(result, "Unduh gagal: semua server uji tidak merespons")
		return 0
	}
	elapsed := time.Since(start).Seconds()
	if elapsed < 0.01 {
		elapsed = 0.01
	}
	return round2(float64(totalBytes.Load()*8) / elapsed / 1e6)
}

// unggahDari mengunggah ke satu server sampai fase habis. Mengembalikan true
// bila server cukup sampai akhir fase; false = caller pindah server berikutnya.
func unggahDari(ctx context.Context, srv serverUji, payload []byte, totalBytes *atomic.Int64, result *speedtestResult) bool {
	chunk := srv.ChunkUnggah
	if chunk <= 0 {
		chunk = 5 << 20
	}
	if chunk > int64(len(payload)) {
		chunk = int64(len(payload))
	}
	kena429 := 0
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, "POST", srv.UnggahURL,
			hitungReader{r: bytes.NewReader(payload[:chunk]), n: totalBytes})
		if err != nil {
			return false
		}
		req.ContentLength = chunk
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return true // fase habis / dibatalkan — normal
			}
			return false // gagal jaringan — pindah server
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			kena429++
			if kena429 >= 2 {
				return false
			}
			if !jedaFase(ctx, time.Second) {
				return true
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return false
		}
		kena429 = 0
		updateResult(result, func(r *speedtestResult) {
			r.Server = srv.Nama
			r.ServerURL = srv.UnggahURL
		})
		// Baca response body agar koneksi bisa di-reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if !jedaFase(ctx, srv.Jeda) {
			return true
		}
	}
	return true
}

// measureUpload mengukur unggahan berkelanjutan selama speedTestPhase dengan
// failover antar server uji yang menerima POST.
func measureUpload(ctx context.Context, urutan []int, result *speedtestResult) float64 {
	phaseCtx, cancel := context.WithTimeout(ctx, speedTestPhase)
	defer cancel()

	start := time.Now()
	var totalBytes atomic.Int64

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				if elapsed > 0.5 {
					updateResult(result, func(r *speedtestResult) {
						r.Upload = round2(float64(totalBytes.Load()*8) / elapsed / 1e6)
						r.Progress = progressFase(elapsed)
					})
				}
			case <-stop:
				return
			}
		}
	}()

	payload := make([]byte, 25<<20) // maksimum chunk antar server
	_, _ = rand.Read(payload)       // isi acak — sekali buat, dipakai ulang

	for _, i := range urutan {
		srv := speedTestServers[i]
		if srv.UnggahURL == "" || phaseCtx.Err() != nil {
			continue
		}
		if unggahDari(phaseCtx, srv, payload, &totalBytes, result) {
			break
		}
	}

	if totalBytes.Load() == 0 && ctx.Err() == nil {
		setError(result, "Upload gagal: semua server uji menolak unggahan")
		return 0
	}
	elapsed := time.Since(start).Seconds()
	if elapsed < 0.01 {
		elapsed = 0.01
	}
	return round2(float64(totalBytes.Load()*8) / elapsed / 1e6)
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stddev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := avg(vals)
	var ss float64
	for _, v := range vals {
		d := v - mean
		ss += d * d
	}
	return round2(math.Sqrt(ss / float64(len(vals)-1)))
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// handleSpeedtestHistory mengembalikan riwayat speed test (saat ini hanya
// hasil terakhir — riwayat SQLite bisa ditambahkan nanti).
func (s *Server) handleSpeedtestHistory(w http.ResponseWriter, r *http.Request) {
	stMu.Lock()
	cur := stCurrent
	stMu.Unlock()
	var list []speedtestResult
	if cur != nil && cur.Phase == "done" {
		list = append(list, *cur)
	}
	if list == nil {
		list = []speedtestResult{}
	}
	writeJSON(w, http.StatusOK, list)
}
