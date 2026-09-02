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

	"linux-dashboard/OxidiLily/internal/helperclient"
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
// untuk komponen yang punya antarmuka web sendiri — 9router (:20128) dan
// Technitium DNS (:5380). Dipakai tombol "Buka" di halaman Components — tanpa
// ini user harus mengingat port dan mengetik manual, yang sering salah di
// WSL/lxc yang tidak punya hostname tetap.
func (s *Server) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	component := chi.URLParam(r, "name")
	var port int
	switch component {
	case "9router":
		port = 20128
	case "technitium-dns":
		port = 5380
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
	// pakai http di sini — konsol web Technitium juga melayani plain HTTP
	// di 5380 (HTTPS-nya ada di port lain, 53443, dengan cert sendiri).
	// Tambahkan kasus khusus kalau ada komponen yang hanya bicara HTTPS.
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

// handleTerminalReset menutup semua sesi terminal sehingga hitungan sesi
// kembali 0. Sesi yang tergantung (tab ditutup paksa, jaringan putus di
// tengah) kalau tidak begini hanya hilang saat helper-nya sendiri menyerah,
// dan sampai itu terjadi kuota ikut terpakai.
//
// Password akun diverifikasi lewat PAM (jalur yang sama dengan login) sebelum
// satu sesi pun ditutup — menutup shell orang lain bukan aksi yang boleh
// terjadi hanya karena satu klik nyasar. Password TIDAK pernah ikut ke log.
func (s *Server) handleTerminalReset(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ip := clientIP(r)
	key := sess.Username + "|" + ip
	if ok, retry := s.throttle.allowed(key); !ok {
		writeJSON(w, http.StatusTooManyRequests, errBody{
			Error: "Terlalu banyak percobaan. Coba lagi dalam " + retry.Round(time.Second).String(),
		})
		return
	}
	err := s.helper.Call(helperproto.CmdAuthLogin, sess.Username,
		helperproto.LoginArgs{Username: sess.Username, Password: body.Password}, nil)
	if err != nil {
		// Sama seperti login: hanya `denied` dari PAM yang berarti password
		// salah. Helper mati dilaporkan apa adanya supaya user tidak mengetik
		// ulang password yang sebenarnya benar.
		if helperclient.Code(err) != helperproto.ErrDenied {
			writeErr(w, http.StatusServiceUnavailable,
				"Layanan autentikasi tidak tersedia. Cek status linux-dashboard-helper.service.")
			return
		}
		s.throttle.record(key)
		writeErr(w, http.StatusUnauthorized, "Password salah")
		return
	}
	s.throttle.reset(key)

	n := s.terminals.CloseAll()
	s.store.LogActivity(sess.Username, "terminal_reset", "hapus semua sesi terminal",
		map[string]any{"ditutup": n}, ip)
	writeJSON(w, http.StatusOK, map[string]any{"closed": n})
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
