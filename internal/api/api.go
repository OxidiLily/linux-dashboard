// Package api menyusun seluruh HTTP handler web app.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/metrics"
	"linux-dashboard/OxidiLily/internal/store"
	"linux-dashboard/OxidiLily/internal/terminal"
)

type Server struct {
	cfg       config.Config
	store     *store.Store
	helper    *helperclient.Client
	collector *metrics.Collector
	terminals *terminal.Registry
	throttle  *throttle
	static    http.Handler
	usage     *usageCache

	// wsIntervals melacak interval yang diminta tiap koneksi WebSocket aktif,
	// dipakai untuk menentukan tick tercepat yang harus dijalankan collector.
	wsMu        sync.Mutex
	wsIntervals map[int64]time.Duration
	wsNextID    int64
}

func New(cfg config.Config, st *store.Store, hc *helperclient.Client, col *metrics.Collector, static http.Handler) *Server {
	return &Server{
		cfg:         cfg,
		store:       st,
		helper:      hc,
		collector:   col,
		terminals:   terminal.NewRegistry(),
		throttle:    newThrottle(),
		usage:       newUsageCache(),
		static:      static,
		wsIntervals: map[int64]time.Duration{},
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(sameOriginOnly)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		// Nama host dipakai halaman login, jadi harus bisa dibaca sebelum ada
		// sesi. Yang dibuka hanya nama mesin — sama dengan yang sudah terlihat
		// dari sertifikat TLS, DNS, atau banner SSH di jaringan yang sama.
		r.Get("/hostname", s.handleHostname)
		r.Get("/open-url/{name}", s.handleOpenURL)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)

			r.Get("/system/info", s.handleSystemInfo)
			r.Get("/system/metrics", s.handleMetricsSnapshot)

			r.Get("/processes", s.handleProcesses)
			r.Post("/processes/{pid}/kill", s.handleKillProcess)

			r.Get("/services/{name}", s.handleServiceStatus)
			r.Post("/services/{name}/{action}", s.handleServiceAction)

			r.Get("/files", s.handleFileList)
			r.Post("/files/upload", s.handleUpload)
			r.Get("/files/download", s.handleDownload)
			r.Get("/files/archive", s.handleArchive)
			r.Get("/files/usage", s.handleUsage)
			r.Get("/files/preview", s.handlePreview)
			r.Get("/files/content", s.handleFileContent)
			r.Put("/files/content", s.handleFileSave)
			r.Put("/files/permissions", s.handlePermissions)
			r.Post("/files/mkdir", s.handleMkdir)
			r.Post("/files/rename", s.handleRename)
			r.Post("/files/delete", s.handleDelete)
			r.Post("/files/copy", s.handleCopy)
			r.Post("/files/move", s.handleMove)
			r.Get("/files/roots", s.handleFileRoots)

			r.Get("/bookmarks", s.handleBookmarks)
			r.Post("/bookmarks", s.handleBookmarkCreate)
			r.Put("/bookmarks/{id}", s.handleBookmarkUpdate)
			r.Delete("/bookmarks/{id}", s.handleBookmarkDelete)

			r.Get("/storage/nfs", s.handleNFSList)
			r.Post("/storage/nfs", s.handleNFSSave)
			r.Put("/storage/nfs", s.handleNFSSave)
			r.Delete("/storage/nfs", s.handleNFSDelete)

			r.Get("/security/fail2ban", s.handleFail2banList)
			r.Post("/security/fail2ban", s.handleFail2banSave)
			r.Put("/security/fail2ban/{jail}", s.handleFail2banSave)
			r.Delete("/security/fail2ban/{jail}", s.handleFail2banDelete)
			r.Post("/security/fail2ban/{jail}/unban", s.handleFail2banUnban)

			r.Post("/storage/disks/prepare", s.handleDiskPrepare)

			r.Get("/storage/mergerfs", s.handleMergerfsList)
			r.Post("/storage/mergerfs", s.handleMergerfsSave)
			r.Put("/storage/mergerfs", s.handleMergerfsSave)
			r.Post("/storage/mergerfs/mount", s.handleMergerfsMount)
			r.Delete("/storage/mergerfs", s.handleMergerfsDelete)

			r.Get("/samba/shares", s.handleSambaList)
			r.Post("/samba/shares", s.handleSambaSave)
			r.Put("/samba/shares", s.handleSambaSave)
			r.Delete("/samba/shares/{name}", s.handleSambaDelete)
			r.Get("/samba/users", s.handleSambaUserList)
			r.Post("/samba/users", s.handleSambaUserSave)
			r.Put("/samba/users/{name}", s.handleSambaUserSave)
			r.Delete("/samba/users/{name}", s.handleSambaUserDelete)

			r.Get("/print/printers", s.handlePrinterList)
			r.Post("/print/printers", s.handlePrinterAdd)
			r.Delete("/print/printers/{name}", s.handlePrinterDelete)
			r.Post("/print/printers/{name}/default", s.handlePrinterDefault)
			r.Post("/print/printers/{name}/enable", s.handlePrinterEnable)
			r.Get("/print/devices", s.handlePrinterDevices)
			r.Get("/print/detect", s.handlePrinterDeteksi)
			r.Post("/print/drivers", s.handlePrinterDriverInstall)
			r.Get("/print/models", s.handlePrinterModels)
			r.Get("/print/jobs", s.handlePrintJobs)
			r.Delete("/print/jobs/{id}", s.handlePrintCancel)
			r.Post("/print/file", s.handlePrintFile)

			r.Get("/logs/file-operations", s.handleLogFileOps)
			r.Get("/logs/activity", s.handleLogActivity)
			r.Get("/logs/notifications", s.handleLogNotifikasiList)
			r.Post("/logs/notifications", s.handleLogNotifikasi)

			r.Put("/settings/account/password", s.handleChangePassword)
			r.Put("/settings/account/hostname", s.handleSetHostname)
			r.Get("/settings/account/users", s.handleUserList)
			r.Post("/settings/account/users", s.handleUserCreate)
			r.Put("/settings/account/users/{name}", s.handleUserModify)
			r.Delete("/settings/account/users/{name}", s.handleUserDelete)
			r.Put("/settings/account/users/{name}/password", s.handleUserResetPassword)

			r.Get("/settings/network/interfaces", s.handleInterfaces)
			r.Get("/settings/network/dns", s.handleGetDNS)
			r.Put("/settings/network/dns", s.handleSetDNS)
			r.Get("/settings/network/vpn", s.handleVPNStatus)
			r.Put("/settings/network/vpn/{name}", s.handleVPNConfigure)
			r.Get("/settings/network/wireguard", s.handleWGServerInfo)
			r.Post("/settings/network/wireguard/server", s.handleWGServerInit)
			r.Post("/settings/network/wireguard/peers", s.handleWGPeerAdd)
			r.Delete("/settings/network/wireguard/peers", s.handleWGPeerDelete)

			r.Get("/firewall/rules", s.handleFirewallList)
			r.Post("/firewall/rules", s.handleFirewallAdd)
			r.Put("/firewall/rules/{num}", s.handleFirewallUpdate)
			r.Delete("/firewall/rules/{num}", s.handleFirewallDelete)
			r.Post("/firewall/toggle", s.handleFirewallToggle)

			r.Get("/settings/alert-thresholds", s.handleThresholds)
			r.Put("/settings/alert-thresholds", s.handleSetThresholds)

			r.Get("/settings/preferences", s.handleGetPreferences)
			r.Put("/settings/preferences", s.handleSetPreferences)
			r.Get("/settings/polling-interval", s.handleGetPollingInterval)
			r.Put("/settings/polling-interval", s.handleSetPollingInterval)

			r.Get("/settings/update", s.handleUpdateStatus)
			r.Post("/settings/update", s.handleUpdateStart)
			r.Post("/settings/uninstall", s.handleUninstall)

			r.Get("/components", s.handleComponents)
			r.Get("/components/progress", s.handleComponentProgress)
			r.Post("/components/{name}/install", s.handleComponentInstall)
			r.Post("/components/{name}/uninstall", s.handleComponentUninstall)
			r.Post("/components/{name}/{action}", s.handleComponentService)

			r.Get("/docker/containers", s.handleDockerContainers)
			r.Get("/docker/containers/{id}/logs", s.handleContainerLogs)
			r.Post("/docker/containers/{id}/{action}", s.handleContainerAction)
			r.Get("/docker/stacks", s.handleStackList)
			r.Post("/docker/stacks", s.handleStackCreate)
			r.Put("/docker/stacks/{id}", s.handleStackUpdate)
			r.Delete("/docker/stacks/{id}", s.handleStackDelete)
			r.Post("/docker/stacks/{id}/{action}", s.handleStackAction)
			r.Get("/docker/stacks/{id}/compose", s.handleStackComposeGet)
			r.Put("/docker/stacks/{id}/compose", s.handleStackComposeSet)
			r.Get("/docker/stacks/{id}/env", s.handleStackEnvGet)
			r.Put("/docker/stacks/{id}/env", s.handleStackEnvSet)
			r.Get("/docker/df", s.handleDockerDiskUsage)
			r.Get("/docker/images", s.handleDockerImages)
			r.Get("/docker/volumes", s.handleDockerVolumes)
			r.Get("/docker/networks", s.handleDockerNetworks)
			// {daya} = images|volumes|networks; dipetakan ke subcommand docker
			// lewat dayaDocker, tidak pernah diteruskan mentah.
			r.Delete("/docker/{daya}/{id}", s.handleDockerDayaDelete)
			r.Post("/docker/{daya}/prune", s.handleDockerDayaPrune)

			r.Get("/terminal/capacity", s.handleTerminalCapacity)
			r.Post("/terminal/sessions/reset", s.handleTerminalReset)
		})
	})

	// WebSocket dicek auth-nya di dalam handler karena browser tidak bisa
	// memasang header Authorization pada koneksi WebSocket — cookie session
	// tetap terkirim otomatis.
	r.Get("/ws/metrics", s.handleWSMetrics)
	r.Get("/ws/terminal", s.handleWSTerminal)

	// SPA fallback: apa pun di luar /api dan /ws dilayani dari asset embed.
	// Path /api dan /ws yang tidak dikenal TIDAK boleh ikut jatuh ke sini —
	// klien akan menerima index.html dengan status 200 dan salah ketik URL
	// endpoint terlihat seperti respons sukses yang gagal di-parse.
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/ws/") {
			writeErr(w, http.StatusNotFound, "endpoint tidak ditemukan")
			return
		}
		s.static.ServeHTTP(w, req)
	})
	return r
}

// ---- helper response ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("gagal menulis response: %v", err)
		}
	}
}

type errBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	// Params mengisi placeholder pada kalimat yang disusun frontend sesuai
	// bahasa user; Error tetap kalimat Indonesia untuk klien non-browser.
	Params []string `json:"params,omitempty"`
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errBody{Error: msg})
}

// writeErrKode dipakai untuk kegagalan yang dibaca user langsung: frontend
// menyusun kalimatnya sesuai bahasa yang dipilih, `msg` cuma cadangan.
func writeErrKode(w http.ResponseWriter, status int, kode, msg string, params ...string) {
	writeJSON(w, status, errBody{Error: msg, Code: kode, Params: params})
}

// writeHelperErr memetakan kode error helper daemon ke HTTP status.
// requires_sudo jadi 403 dengan pesan jelas — tidak pernah gagal diam-diam.
func writeHelperErr(w http.ResponseWriter, err error) {
	code := helperclient.Code(err)
	status := http.StatusInternalServerError
	switch code {
	case helperproto.ErrRequiresSudo, helperproto.ErrDenied, helperproto.ErrDiLuarHome, helperproto.ErrSymlinkKeluar:
		status = http.StatusForbidden
	case helperproto.ErrNotFound, helperproto.ErrFolderTidakAda, helperproto.ErrKomponenTidakAda:
		status = http.StatusNotFound
	case helperproto.ErrSudahAda, helperproto.ErrMasihTersambung, helperproto.ErrDikelolaLuar:
		status = http.StatusConflict
	case helperproto.ErrInvalid, helperproto.ErrPathTidakValid, helperproto.ErrNilaiTidakValid,
		helperproto.ErrPasswordPendek, helperproto.ErrGuestOKKonflik, helperproto.ErrBelumTerpasang,
		helperproto.ErrKredensialTidakOK, helperproto.ErrFuseTidakAda:
		status = http.StatusBadRequest
	}
	writeJSON(w, status, errBody{Error: err.Error(), Code: code, Params: helperclient.Params(err)})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("body request tidak valid: " + err.Error())
	}
	return nil
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, ok := strings.Cut(ip, ":"); ok && strings.Count(ip, ":") == 1 {
		return host
	}
	return ip
}
