package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

func (s *Server) handleProxyList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.ProxyHost
	if err := s.helper.Call(helperproto.CmdProxyList, sessionFrom(r).HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.ProxyHost{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxySave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.ProxyHost
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Code: helperproto.ErrInvalid})
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		in.ID = id
	}
	var out helperproto.ProxyHost
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxySave, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	action := "proxy_create"
	if chi.URLParam(r, "id") != "" {
		action = "proxy_update"
	}
	s.store.LogActivity(sess.Username, action, in.Domain,
		map[string]any{"id": out.ID, "domain": out.Domain, "target": out.TargetHost, "port": out.TargetPort}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxyDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyDelete, sess.HelperToken, helperproto.ProxyDeleteArgs{ID: id}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "proxy_delete", id, map[string]any{"id": id}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleProxyTest(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	if err := s.helper.Call(helperproto.CmdProxyTest, sessionFrom(r).HelperToken, nil, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleProxyCertIssue(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.ProxyCertArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Code: helperproto.ErrInvalid})
		return
	}
	in.ID = chi.URLParam(r, "id")
	var out helperproto.ProxyHost
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCertIssue, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "proxy_certificate_issue", out.Domain,
		map[string]any{"id": out.ID, "domain": out.Domain, "staging": in.Staging}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCloudflareTokenStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.CloudflareTokenStatus
	if err := s.helper.Call(helperproto.CmdProxyCloudflareTokenStatus, sessionFrom(r).HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCloudflareTokenSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.CloudflareTokenArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: "API token Cloudflare tidak valid", Code: helperproto.ErrInvalid})
		return
	}
	var out helperproto.CloudflareTokenStatus
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCloudflareTokenSave, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "proxy_cloudflare_token_save", "", nil, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCloudflareTokenDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.CloudflareTokenStatus
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCloudflareTokenDelete, sess.HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "proxy_cloudflare_token_delete", "", nil, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxyCloudflareDNS(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.CloudflareDNSArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Code: helperproto.ErrInvalid})
		return
	}
	var out helperproto.CloudflareDNSRecord
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCloudflareDNS, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Token sengaja tidak pernah masuk activity metadata.
	s.store.LogActivity(sess.Username, "proxy_cloudflare_dns", out.Name,
		map[string]any{"record_id": out.ID, "domain": out.Name, "content": out.Content, "proxied": out.Proxied}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxyCloudflareList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.CloudflareDNSArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Code: helperproto.ErrInvalid})
		return
	}
	var out []helperproto.CloudflareDNSRecord
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCloudflareList, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.CloudflareDNSRecord{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxyCloudflareDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var in helperproto.CloudflareDNSArgs
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Code: helperproto.ErrInvalid})
		return
	}
	var out map[string]int
	sess := sessionFrom(r)
	if err := s.helper.Call(helperproto.CmdProxyCloudflareDelete, sess.HelperToken, in, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Token tidak pernah masuk activity metadata.
	s.store.LogActivity(sess.Username, "proxy_cloudflare_delete", in.Domain,
		map[string]any{"domain": in.Domain, "record_id": in.RecordID, "deleted": out["deleted"]}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProxyStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.ProxyStatus
	if err := s.helper.Call(helperproto.CmdProxyStatus, sessionFrom(r).HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
