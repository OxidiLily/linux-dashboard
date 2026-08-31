package api

import (
	"net/http"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pembaruan panel dari repo. Hanya sudoer: langkahnya memasang binary,
// menulis unit systemd, dan me-restart service.

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	// Pengecekan versi remote butuh jaringan, jadi hanya saat diminta —
	// polling log di modal tidak boleh ikut memanggil ls-remote tiap detik.
	args := helperproto.UpdateArgs{Cek: r.URL.Query().Get("cek") == "1"}
	var st helperproto.UpdateStatus
	if err := s.helper.Call(helperproto.CmdUpdateStatus, sessionFrom(r).Username, args, &st); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var st helperproto.UpdateStatus
	if err := s.helper.Call(helperproto.CmdUpdateStart, sess.Username, nil, &st); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "panel_update", "jalankan pembaruan panel",
		map[string]any{"lokal": st.Lokal}, clientIP(r))
	writeJSON(w, http.StatusOK, st)
}

// Uninstall panel. Password akun diverifikasi helper lewat PAM sebelum apa pun
// dijalankan, dan TIDAK pernah ikut ke log aktivitas.
func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.UninstallArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdUninstall, sess.Username, body, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "panel_uninstall", "jalankan uninstall panel",
		map[string]any{"mode": body.Mode}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
