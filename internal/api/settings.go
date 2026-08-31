package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

// ---- Akun ----

type passwordBody struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body passwordBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "Password baru minimal 8 karakter")
		return
	}
	err := s.helper.Call(helperproto.CmdAuthPasswd, sess.Username, helperproto.PasswdArgs{
		OldPassword: body.OldPassword, NewPassword: body.NewPassword,
	}, nil)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "password_change", "ganti password sendiri", nil, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type hostnameBody struct {
	Hostname string `json:"hostname"`
}

func (s *Server) handleSetHostname(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body hostnameBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdSysHostnameSet, sess.Username,
		helperproto.PathArgs{Path: body.Hostname}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "hostname_change", "set hostname",
		map[string]any{"hostname": body.Hostname}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "hostname": body.Hostname})
}

// ---- Manajemen user Linux (khusus root/sudo) ----

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var users []helperproto.LinuxUser
	if err := s.helper.Call(helperproto.CmdUserList, sessionFrom(r).Username, nil, &users); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.UserCreateArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Password wajib dan panjang minimalnya sama dengan reset password.
	// Tanpa ini panel bisa membuat akun tanpa password sama sekali — akun yang
	// tidak bisa dipakai login, atau lebih buruk, bisa dipakai tanpa password
	// tergantung konfigurasi PAM.
	if len(body.Password) < 8 {
		writeErrKode(w, http.StatusBadRequest, helperproto.ErrPasswordPendek, "Password minimal 8 karakter", "8")
		return
	}
	body.MakeHome = true
	if err := s.helper.Call(helperproto.CmdUserCreate, sess.Username, body, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "user_create", "buat user",
		map[string]any{"username": body.Username, "groups": body.Groups}, clientIP(r))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) handleUserModify(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.UserModifyArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// UID/GID sengaja tidak bisa diubah: UID adalah identitas permanen yang
	// menentukan kepemilikan file — mengubahnya merusak ownership yang sudah ada.
	body.Username = chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdUserModify, sess.Username, body, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "user_modify", "ubah user",
		map[string]any{"username": body.Username}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	target := chi.URLParam(r, "name")
	if target == sess.Username {
		writeErr(w, http.StatusBadRequest, "Tidak bisa menghapus akun yang sedang dipakai login")
		return
	}
	removeHome := r.URL.Query().Get("remove_home") == "true"
	if err := s.helper.Call(helperproto.CmdUserDelete, sess.Username,
		helperproto.UserDeleteArgs{Username: target, RemoveHome: removeHome}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "user_delete", "hapus user",
		map[string]any{"username": target, "remove_home": removeHome}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUserResetPassword(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body passwordBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.NewPassword) < 8 {
		writeErrKode(w, http.StatusBadRequest, helperproto.ErrPasswordPendek, "Password minimal 8 karakter", "8")
		return
	}
	target := chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdAuthPasswd, sess.Username, helperproto.PasswdArgs{
		Target: target, NewPassword: body.NewPassword,
	}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "user_password_reset", "reset password user lain",
		map[string]any{"username": target}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Network ----

type ifaceInfo struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac"`
	IPs   []string `json:"ips"`
	MTU   int      `json:"mtu"`
	Up    bool     `json:"up"`
	Flags string   `json:"flags"`
}

// handleInterfaces hanya menampilkan interface yang punya IP DAN MAC —
// interface tanpa IP sengaja disembunyikan dari ringkasan utama.
func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	out := []ifaceInfo{}
	cmd := exec.Command("ip", "-j", "addr")
	if data, err := cmd.Output(); err == nil {
		var list []struct {
			Ifname    string   `json:"ifname"`
			Address   string   `json:"address"`
			Mtu       int      `json:"mtu"`
			Operstate string   `json:"operstate"`
			Flags     []string `json:"flags"`
			AddrInfo  []struct {
				Family string `json:"family"`
				Local  string `json:"local"`
				Prefix int    `json:"prefixlen"`
			} `json:"addr_info"`
		}
		if err := json.Unmarshal(data, &list); err == nil {
			for _, item := range list {
				if item.Address == "" || item.Address == "00:00:00:00:00:00" {
					continue
				}
				ips := []string{}
				for _, a := range item.AddrInfo {
					if a.Local != "" {
						ips = append(ips, a.Local)
					}
				}
				if len(ips) == 0 {
					continue
				}
				up := false
				for _, f := range item.Flags {
					if f == "UP" {
						up = true
						break
					}
				}
				out = append(out, ifaceInfo{
					Name: item.Ifname, MAC: item.Address, MTU: item.Mtu,
					Up: up, Flags: strings.Join(item.Flags, ","),
					IPs: ips,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type dnsBody struct {
	Nameservers []string `json:"nameservers"`
}

func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, dnsBody{Nameservers: readNameservers()})
}

func readNameservers() []string {
	out := []string{}
	b, err := os.ReadFile("/etc/resolv.conf")
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "nameserver" {
				out = append(out, f[1])
			}
		}
	}
	// 127.0.0.53 adalah stub systemd-resolved — server sebenarnya ada di
	// balik resolvectl, jadi resolv.conf saja tidak informatif.
	if len(out) == 1 && strings.HasPrefix(out[0], "127.0.0.5") {
		if res, err := exec.Command("resolvectl", "status").Output(); err == nil {
			for _, line := range strings.Split(string(res), "\n") {
				line = strings.TrimSpace(line)
				if after, ok := strings.CutPrefix(line, "DNS Servers:"); ok {
					out = append(out, strings.Fields(after)...)
				}
			}
		}
	}
	return out
}

func (s *Server) handleSetDNS(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body dnsBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, ns := range body.Nameservers {
		if net.ParseIP(ns) == nil {
			writeErr(w, http.StatusBadRequest, "Alamat DNS tidak valid: "+ns)
			return
		}
	}
	if err := s.helper.Call(helperproto.CmdSysDNSSet, sess.Username,
		helperproto.DNSArgs{Nameservers: body.Nameservers}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "dns_change", "ubah DNS",
		map[string]any{"nameservers": body.Nameservers}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "nameservers": body.Nameservers})
}

func (s *Server) handleVPNStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.VPNStatus
	if err := s.helper.Call(helperproto.CmdVPNStatus, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVPNConfigure(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.VPNArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	body.Name = chi.URLParam(r, "name")
	var st helperproto.VPNStatus
	if err := s.helper.Call(helperproto.CmdVPNConfigure, sess.Username, body, &st); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Auth key/token TIDAK ikut dicatat ke log — hanya nama & aksi.
	s.store.LogActivity(sess.Username, "vpn_configure", body.Action,
		map[string]any{"vpn": body.Name}, clientIP(r))
	writeJSON(w, http.StatusOK, st)
}

// ---- Firewall (ufw) ----

func (s *Server) handleFirewallList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var st struct {
		Enabled bool                  `json:"enabled"`
		Rules   []helperproto.UfwRule `json:"rules"`
	}
	if err := s.helper.Call(helperproto.CmdUfwStatus, sessionFrom(r).Username, nil, &st); err != nil {
		writeHelperErr(w, err)
		return
	}
	if st.Rules == nil {
		st.Rules = []helperproto.UfwRule{}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleFirewallAdd(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var rule helperproto.UfwRule
	if err := decodeBody(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdUfwAdd, sess.Username, rule, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "firewall_rule_add", "tambah rule",
		map[string]any{"action": rule.Action, "port": rule.Port, "proto": rule.Proto}, clientIP(r))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// handleFirewallUpdate mengganti isi satu rule. ufw tidak punya perintah edit,
// jadi helper menghapus rule lama lalu menambahkan yang baru — rule baru
// divalidasi lebih dulu supaya kegagalan tidak menyisakan port terbuka/tertutup
// yang tidak diminta.
func (s *Server) handleFirewallUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var rule helperproto.UfwRule
	if err := decodeBody(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	args := helperproto.UfwUpdateArgs{
		Num:  chi.URLParam(r, "num"),
		Spec: r.URL.Query().Get("spec"),
		Rule: rule,
	}
	if err := s.helper.Call(helperproto.CmdUfwUpdate, sess.Username, args, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "firewall_rule_update", "ubah rule",
		map[string]any{"num": args.Num, "action": rule.Action, "port": rule.Port, "proto": rule.Proto}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFirewallDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	num := chi.URLParam(r, "num")
	// spec dipakai saat ufw nonaktif — rule tidak punya nomor di kondisi itu.
	spec := r.URL.Query().Get("spec")
	if err := s.helper.Call(helperproto.CmdUfwDelete, sess.Username,
		helperproto.UfwDeleteArgs{Num: num, Spec: spec}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "firewall_rule_delete", "hapus rule",
		map[string]any{"num": num, "spec": spec}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleFirewallToggle nyalakan/matikan ufw (sudo). Frontend pakai ini
// untuk switch "UFW Aktif" di header panel Firewall; tanpa endpoint ini
// status cuma bisa dibaca, tidak ada cara menyalakan dari UI.
func (s *Server) handleFirewallToggle(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.UfwToggleArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdUfwToggle, sess.Username, body, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	verb := "disable"
	if body.Enable {
		verb = "enable"
	}
	s.store.LogActivity(sess.Username, "firewall_toggle", "ufw "+verb,
		map[string]any{"enable": body.Enable}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Alert thresholds ----

func (s *Server) handleThresholds(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Thresholds()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSetThresholds bersifat global — berlaku sama untuk setiap akun yang
// membuka dashboard, jadi hanya sudo yang boleh mengubahnya.
func (s *Server) handleSetThresholds(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var list []store.Threshold
	if err := decodeBody(r, &list); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Validasi SELURUH daftar dulu, baru simpan. Menyimpan sambil jalan
	// membuat satu nilai invalid di tengah daftar meninggalkan sebagian ambang
	// sudah berubah dan sebagian belum — user melihat error tapi sistem sudah
	// separuh berubah.
	for _, t := range list {
		if err := s.store.ValidateThreshold(t); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, t := range list {
		if err := s.store.SetThreshold(t); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.store.LogActivity(sess.Username, "alert_threshold_change", "ubah ambang",
		map[string]any{"thresholds": list}, clientIP(r))
	out, _ := s.store.Thresholds()
	writeJSON(w, http.StatusOK, out)
}

// ---- Preferensi user ----

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Preferences(sessionFrom(r).Username))
}

func (s *Server) handleSetPreferences(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	prefs := s.store.Preferences(sess.Username)
	if err := decodeBody(r, &prefs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prefs.Username = sess.Username
	if err := s.store.SavePreferences(prefs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyFastestInterval()
	writeJSON(w, http.StatusOK, prefs)
}

type pollingBody struct {
	IntervalMS int `json:"polling_interval_ms"`
}

func (s *Server) handleGetPollingInterval(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pollingBody{IntervalMS: s.store.Preferences(sessionFrom(r).Username).PollingInterval})
}

func (s *Server) handleSetPollingInterval(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body pollingBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prefs := s.store.Preferences(sess.Username)
	prefs.Username = sess.Username
	prefs.PollingInterval = body.IntervalMS
	if err := s.store.SavePreferences(prefs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyFastestInterval()
	writeJSON(w, http.StatusOK, pollingBody{IntervalMS: prefs.PollingInterval})
}

// applyFastestInterval menyetel collector ke interval tercepat yang sedang
// diminta client yang terhubung. Client dengan interval lebih lambat tetap
// dilayani sesuai preferensinya oleh broadcaster.
func (s *Server) applyFastestInterval() {
	fastest := time.Second
	s.wsMu.Lock()
	for _, d := range s.wsIntervals {
		if d < fastest {
			fastest = d
		}
	}
	s.wsMu.Unlock()
	s.collector.SetInterval(fastest)
}
