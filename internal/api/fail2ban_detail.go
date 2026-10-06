package api

import (
	"net/http"
	"net/netip"
	"regexp"

	"github.com/go-chi/chi/v5"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

var detailJailRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func (s *Server) handleFail2banDetail(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	args := helperproto.Fail2banUnbanArgs{Jail: chi.URLParam(r, "jail"), IP: r.URL.Query().Get("ip")}
	addr, err := netip.ParseAddr(args.IP)
	if !detailJailRE.MatchString(args.Jail) || err != nil || addr.Zone() != "" {
		writeErr(w, http.StatusBadRequest, "jail atau alamat IP tidak valid")
		return
	}
	var out helperproto.Fail2banDetail
	if err := s.helper.Call(helperproto.CmdFail2banDetail, sessionFrom(r).HelperToken, args, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.enrichFail2banLocation(&out)
	writeJSON(w, http.StatusOK, out)
}
