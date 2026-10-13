package api

import (
	"linux-dashboard/OxidiLily/internal/helperproto"
	"net/http"
)

func (s *Server) handlePostfixRead(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.PostfixConfig
	if e := s.helper.Call(helperproto.CmdPostfixRead, sessionFrom(r).HelperToken, nil, &out); e != nil {
		writeHelperErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handlePostfixSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in, out helperproto.PostfixConfig
	if e := decodeBody(r, &in); e != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: e.Error(), Code: helperproto.ErrInvalid})
		return
	}
	sess := sessionFrom(r)
	if e := s.helper.Call(helperproto.CmdPostfixSave, sess.HelperToken, in, &out); e != nil {
		writeHelperErr(w, e)
		return
	}
	s.store.LogActivity(sess.Username, "postfix_save", "/etc/postfix/main.cf", map[string]any{"restart": in.Restart, "revision": out.Revision}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}
