package api

import (
	"net/http"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func (s *Server) handleCertificatesGet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.CertificatesStatus
	if err := s.helper.Call(helperproto.CmdCertificatesGet, sessionFrom(r).HelperToken, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCertificatesSet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.CertificatesSetArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var out helperproto.CertificatesStatus
	if err := s.helper.Call(helperproto.CmdCertificatesSet, sess.HelperToken, body, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "certificates_change", "ubah TLS panel",
		map[string]any{"active": out.Active, "cert_path": body.CertPath, "key_path": body.KeyPath}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCertificatesUpload(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.CertificatesUploadArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var out helperproto.CertificatesStatus
	if err := s.helper.Call(helperproto.CmdCertificatesUpload, sess.HelperToken, body, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "certificates_change", "unggah TLS panel",
		map[string]any{"active": out.Active, "cert_path": out.CertPath}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCertificatesSelfSigned(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.CertificatesSelfSignedArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var out helperproto.CertificatesStatus
	if err := s.helper.Call(helperproto.CmdCertificatesSelfSigned, sess.HelperToken, body, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "certificates_change", "buat TLS self-signed panel",
		map[string]any{"active": out.Active, "common_name": body.CommonName, "days": body.Days}, clientIP(r))
	writeJSON(w, http.StatusOK, out)
}
