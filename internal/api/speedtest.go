package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"io"
	"log"
	"net/http"
	"sync"
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

// Server speed test bawaan — file publik yang bisa diunduh untuk mengukur.
// Sengaja pakai beberapa mirror untuk memperkecil latensi ke satu geografi.
var speedTestServers = []struct {
	Name string
	URL  string
	Size int64 // perkiraan ukuran file, byte
}{
	// Cloudflare menolak (403) dimensi >= 100MB; 75MB terverifikasi 200 (2026-10-28).
	{"Cloudflare", "https://speed.cloudflare.com/__down?bytes=73400320", 75 << 20},
	{"Hetzner (DE)", "https://speed.hetzner.de/100MB.bin", 100 << 20},
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

// runSpeedTest menjalankan fase ping, download, upload secara berurutan.
func runSpeedTest(ctx context.Context, cancel context.CancelFunc, result *speedtestResult) {
	defer cancel()

	// Pilih server dengan latensi terbaik.
	bestIdx, bestPing, bestJitter := pickBestServer(ctx, result)
	if ctx.Err() != nil {
		setError(result, "Dibatalkan")
		return
	}

	srv := speedTestServers[bestIdx]
	updateResult(result, func(r *speedtestResult) {
		r.Ping = bestPing
		r.Jitter = bestJitter
		r.Server = srv.Name
		r.ServerURL = srv.URL
		r.Phase = "download"
		r.Progress = 0
	})

	// Download test.
	dlMbps := measureDownload(ctx, srv.URL, result)
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
	ulMbps := measureUpload(ctx, result)
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

func pickBestServer(ctx context.Context, result *speedtestResult) (bestIdx int, bestPing, bestJitter float64) {
	bestPing = 9999
	for i, srv := range speedTestServers {
		if ctx.Err() != nil {
			return
		}
		updateResult(result, func(r *speedtestResult) {
			r.Phase = "ping"
			r.Progress = float64(i) / float64(len(speedTestServers))
		})

		pings := make([]float64, 0, 5)
		for j := 0; j < 5; j++ {
			if ctx.Err() != nil {
				return
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
			continue
		}
		avgPing := avg(pings)
		jitter := stddev(pings)
		if avgPing < bestPing {
			bestPing = avgPing
			bestJitter = jitter
			bestIdx = i
		}
	}
	return
}

func measureDownload(ctx context.Context, url string, result *speedtestResult) float64 {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		setError(result, "Download request gagal: "+err.Error())
		return 0
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		setError(result, "Download gagal: "+err.Error())
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		setError(result, fmt.Sprintf("Download gagal: HTTP %d", resp.StatusCode))
		return 0
	}
	totalExpected := resp.ContentLength
	if totalExpected <= 0 {
		totalExpected = 75 << 20 // assume 75MB
	}

	buf := make([]byte, 256<<10) // 256KB buffer
	var totalBytes int64
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	// Update progress in background.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				updateResult(result, func(r *speedtestResult) {
					r.Progress = float64(totalBytes) / float64(totalExpected)
					if r.Progress > 1 {
						r.Progress = 0.99
					}
					elapsed := time.Since(start).Seconds()
					if elapsed > 0.5 {
						r.Download = float64(totalBytes*8) / elapsed / 1e6
					}
				})
			case <-done:
				return
			}
		}
	}()

	for {
		n, readErr := resp.Body.Read(buf)
		totalBytes += int64(n)
		if readErr != nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(done)

	elapsed := time.Since(start).Seconds()
	if elapsed < 0.01 {
		elapsed = 0.01
	}
	mbps := float64(totalBytes*8) / elapsed / 1e6
	return round2(mbps)
}

// measureUpload mengirim data acak ke Cloudflare speed test upload endpoint.
func measureUpload(ctx context.Context, result *speedtestResult) float64 {
	// Cloudflare menerima POST upload di __up.
	uploadURL := "https://speed.cloudflare.com/__up"
	uploadSize := int64(25 << 20) // 25MB upload

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		buf := make([]byte, 64<<10)
		var written int64
		for written < uploadSize {
			if ctx.Err() != nil {
				return
			}
			_, _ = rand.Read(buf)
			toWrite := int64(len(buf))
			if written+toWrite > uploadSize {
				toWrite = uploadSize - written
			}
			n, err := pw.Write(buf[:toWrite])
			written += int64(n)
			if err != nil {
				return
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, pr)
	if err != nil {
		setError(result, "Upload request gagal: "+err.Error())
		return 0
	}
	req.ContentLength = uploadSize
	req.Header.Set("Content-Type", "application/octet-stream")

	start := time.Now()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				if elapsed > 0.5 {
					updateResult(result, func(r *speedtestResult) {
						// Perkiraan progress: waktu / perkiraan total
						r.Progress = elapsed / 30 // max ~30 detik
						if r.Progress > 0.99 {
							r.Progress = 0.99
						}
					})
				}
			case <-done:
				return
			}
		}
	}()

	resp, err := http.DefaultClient.Do(req)
	close(done)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		setError(result, "Upload gagal: "+err.Error())
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		setError(result, fmt.Sprintf("Upload gagal: HTTP %d", resp.StatusCode))
		return 0
	}
	// Baca response body agar koneksi bisa di-reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	elapsed := time.Since(start).Seconds()
	if elapsed < 0.01 {
		elapsed = 0.01
	}

	// Coba parse header upload-speed dari Cloudflare jika ada.
	if cfSpeed := resp.Header.Get("Server-Timing"); cfSpeed != "" {
		log.Printf("speedtest upload Server-Timing: %s", cfSpeed)
	}

	mbps := float64(uploadSize*8) / elapsed / 1e6
	return round2(mbps)
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
