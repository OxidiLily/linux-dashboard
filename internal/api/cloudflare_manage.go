package api

import (
	"net/http"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func (s *Server) handleCloudflareZones(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.CloudflareZone
	if err := s.helper.Call(helperproto.CmdProxyCloudflareZones, sessionFrom(r).HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.CloudflareZone{}
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) cloudflareManaged(w http.ResponseWriter, r *http.Request, cmd string) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.CloudflareManagedArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: "Permintaan DNS tidak valid", Code: helperproto.ErrInvalid})
		return
	}
	var out any
	switch cmd {
	case helperproto.CmdProxyCloudflareRecords:
		out = &helperproto.CloudflareRecordPage{}
	case helperproto.CmdProxyCloudflareRecordSave:
		out = new(any)
	default:
		out = &map[string]bool{}
	}
	sess := sessionFrom(r)
	if err := s.helper.Call(cmd, sess.HelperToken, in, out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if cmd != helperproto.CmdProxyCloudflareRecords {
		s.store.LogActivity(sess.Username, cmd, in.ZoneID, map[string]any{"record_id": in.RecordID}, clientIP(r))
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleCloudflareRecords(w http.ResponseWriter, r *http.Request) {
	s.cloudflareManaged(w, r, helperproto.CmdProxyCloudflareRecords)
}
func (s *Server) handleCloudflareRecordSave(w http.ResponseWriter, r *http.Request) {
	s.cloudflareManaged(w, r, helperproto.CmdProxyCloudflareRecordSave)
}
func (s *Server) handleCloudflareRecordDelete(w http.ResponseWriter, r *http.Request) {
	s.cloudflareManaged(w, r, helperproto.CmdProxyCloudflareRecordDelete)
}
