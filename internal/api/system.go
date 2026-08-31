package api

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shirou/gopsutil/v4/process"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/platform"
	"linux-dashboard/OxidiLily/internal/terminal"
)

type systemInfo struct {
	Hostname string            `json:"hostname"`
	Time     string            `json:"server_time"`
	Platform platform.Info     `json:"platform"`
	Cores    int               `json:"cores"`
	Terminal terminal.Capacity `json:"terminal"`
}

// handleHostname adalah satu-satunya endpoint sistem yang tidak butuh sesi:
// halaman login menampilkan nama mesin supaya user tahu server mana yang
// sedang ia masuki.
func (s *Server) handleHostname(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]string{"hostname": host})
}

// handleOpenURL adalah endpoint diagnostik yang mengembalikan URL absolut
// untuk komponen 9router (default http://<hostname>:20128). Dipakai tombol
// "Buka 9router" di halaman Components — tanpa ini user harus mengingat
// port dan mengetik manual, yang sering salah di WSL/lxc yang tidak punya
// hostname tetap.
func (s *Server) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	component := chi.URLParam(r, "name")
	var port int
	switch component {
	case "9router":
		port = 20128
	default:
		writeErr(w, http.StatusNotFound, "tidak ada URL langsung untuk komponen "+component)
		return
	}
	// Ambil host dari header request, bukan dari os.Hostname(). Hostname
	// server sering kali nama internal yang tidak resolve dari device lain
	// (mis. WSL2 punya hostname acak "DESKTOP-ABC123", mobile di Wi-Fi
	// berbeda tidak akan menemukan itu). Request Host = persis apa yang
	// user ketik di address bar browser = bekerja di semua device.
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		// Reverse proxy (NPM, Caddy) — pakai host asli yang diakses user.
		host = h
	}
	if host == "" {
		host, _ = os.Hostname()
	}
	// Buang port dari host kalau ada, lalu tambahkan port komponen.
	if h, p, err := net.SplitHostPort(host); err == nil && h != "" {
		host = h
		_ = p
	}
	// Scheme harus mengikuti protokol yang dipakai komponen itu sendiri,
	// BUKAN protokol akses ke panel. Panel bisa diakses via HTTPS
	// (self-signed cert), tapi 9router adalah plain HTTP — kalau pakai
	// scheme panel, browser dapat "ERR_SSL_PROTOCOL_ERROR" atau
	// "invalid response" karena 9router tidak berbicara TLS. Selalu
	// pakai http untuk 9router; tambahkan kasus khusus untuk komponen
	// lain yang mungkin mendukung HTTPS.
	scheme := "http"
	portStr := strconv.Itoa(port)
	writeJSON(w, http.StatusOK, map[string]string{
		"url":    scheme + "://" + host + ":" + portStr,
		"scheme": scheme,
		"host":   host,
		"port":   portStr,
	})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	// Hari & tanggal diformat di frontend supaya mengikuti locale browser;
	// backend cukup mengirim waktu server dalam RFC3339 beserta offset-nya.
	writeJSON(w, http.StatusOK, systemInfo{
		Hostname: host,
		Time:     time.Now().Format(time.RFC3339),
		Platform: platform.Detect(),
		Cores:    s.terminals.Capacity().Cores,
		Terminal: s.terminals.Capacity(),
	})
}

func (s *Server) handleMetricsSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.collector.Last())
}

func (s *Server) handleTerminalCapacity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.terminals.Capacity())
}

// ---- processes ----

type procInfo struct {
	PID     int32   `json:"pid"`
	PPID    int32   `json:"ppid"`
	User    string  `json:"user"`
	CPUPct  float64 `json:"cpu_pct"`
	MemPct  float64 `json:"mem_pct"`
	MemRSS  uint64  `json:"mem_rss"`
	Command string  `json:"command"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	// Own menandai proses milik user yang login — UI memakainya untuk tahu
	// kill mana yang tidak butuh sudo.
	Own bool `json:"own"`
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	procs, err := process.Processes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca daftar proses: "+err.Error())
		return
	}
	out := make([]procInfo, 0, len(procs))
	for _, p := range procs {
		username, _ := p.Username()
		cmd, _ := p.Cmdline()
		name, _ := p.Name()
		if cmd == "" {
			cmd = name
		}
		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		ppid, _ := p.Ppid()
		var rss uint64
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			rss = mi.RSS
		}
		statuses, _ := p.Status()
		out = append(out, procInfo{
			PID: p.Pid, PPID: ppid, User: username,
			CPUPct: round1(cpuPct), MemPct: round1(float64(memPct)), MemRSS: rss,
			Command: cmd, Name: name, Status: strings.Join(statuses, ","),
			Own: username == sess.Username,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPct > out[j].CPUPct })
	writeJSON(w, http.StatusOK, out)
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

type killRequest struct {
	Signal int `json:"signal"`
}

func (s *Server) handleKillProcess(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil || pid <= 1 {
		writeErr(w, http.StatusBadRequest, "PID tidak valid")
		return
	}
	var req killRequest
	_ = decodeBody(r, &req)
	if req.Signal == 0 {
		req.Signal = 15
	}
	// Helper daemon yang memutuskan self-vs-sudo: kill proses sendiri tidak
	// butuh sudo (izin Unix standar), kill milik user lain butuh.
	err = s.helper.Call(helperproto.CmdProcKill, sess.Username,
		helperproto.KillArgs{PID: pid, Signal: req.Signal}, nil)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "kill_process", "kill",
		map[string]any{"pid": pid, "signal": req.Signal}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pid": pid})
}

// ---- services ----

var serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9@._\-]+$`)

type serviceStatus struct {
	Name        string `json:"name"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	LoadState   string `json:"load_state"`
	Description string `json:"description"`
	Enabled     string `json:"enabled"`
}

func (s *Server) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !serviceNameRe.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "nama service tidak valid")
		return
	}
	// Query status systemd bersifat read-only dan boleh dilakukan user biasa,
	// jadi tidak perlu lewat helper daemon.
	out, _ := exec.Command("systemctl", "show", name,
		"--property=ActiveState,SubState,LoadState,Description,UnitFileState").Output()
	st := serviceStatus{Name: name}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "ActiveState":
			st.ActiveState = v
		case "SubState":
			st.SubState = v
		case "LoadState":
			st.LoadState = v
		case "Description":
			st.Description = v
		case "UnitFileState":
			st.Enabled = v
		}
	}
	if st.LoadState == "not-found" || st.LoadState == "" {
		writeErr(w, http.StatusNotFound, "service tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name, action := chi.URLParam(r, "name"), chi.URLParam(r, "action")
	err := s.helper.Call(helperproto.CmdSvcAction, sess.Username,
		helperproto.ServiceArgs{Name: name, Action: action}, nil)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "service_"+action, action,
		map[string]any{"service": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
