package api

import (
	"net/http"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// WireGuard mode server: menyiapkan config server, menambah, dan menghapus
// klien. Mode klien tetap lewat /settings/network/vpn/{name}.

func (s *Server) handleWGServerInfo(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var info helperproto.WGServerInfo
	if err := s.helper.Call(helperproto.CmdWGServerInfo, sessionFrom(r).Username, nil, &info); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleWGServerInit(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.WGServerArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var info helperproto.WGServerInfo
	if err := s.helper.Call(helperproto.CmdWGServerInit, sess.Username, body, &info); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "wireguard_server_init", "siapkan server WireGuard",
		map[string]any{"subnet": body.Subnet, "port": body.Port}, clientIP(r))
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleWGPeerAdd(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.WGPeerArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var hasil helperproto.WGPeerBaru
	if err := s.helper.Call(helperproto.CmdWGPeerAdd, sess.Username, body, &hasil); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Config klien memuat private key — yang dicatat hanya nama & alamatnya.
	s.store.LogActivity(sess.Username, "wireguard_peer_add", "tambah klien WireGuard",
		map[string]any{"nama": hasil.Peer.Nama, "ip": hasil.Peer.IP}, clientIP(r))
	writeJSON(w, http.StatusOK, hasil)
}

func (s *Server) handleWGPeerDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.WGPeerArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var info helperproto.WGServerInfo
	if err := s.helper.Call(helperproto.CmdWGPeerDelete, sess.Username, body, &info); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "wireguard_peer_delete", "hapus klien WireGuard",
		map[string]any{"nama": body.Nama}, clientIP(r))
	writeJSON(w, http.StatusOK, info)
}
