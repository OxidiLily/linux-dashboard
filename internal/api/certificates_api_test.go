package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestCertificatesAPIButuhSudo(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/certificates", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("GET non-sudo = %d, ingin 403 (body: %s)", w.Code, w.Body.String())
	}
	if len(tiruan.riwayatOperasi()) != 0 {
		t.Fatalf("helper tetap dipanggil: %v", tiruan.riwayatOperasi())
	}
}

func TestCertificatesAPIPutButuhSudo(t *testing.T) {
	tiruan := &helperTiruan{}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/certificates", strings.NewReader(`{"cert_path":"/cert.pem","key_path":"/key.pem"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("PUT non-sudo = %d, ingin 403 (body: %s)", w.Code, w.Body.String())
	}
	if len(tiruan.riwayatOperasi()) != 0 {
		t.Fatalf("helper tetap dipanggil: %v", tiruan.riwayatOperasi())
	}
}

func TestCertificatesAPIGetMeneruskanStatus(t *testing.T) {
	want := helperproto.CertificatesStatus{CertPath: "/cert.pem", KeyPath: "/key.pem", Active: true, Valid: true, Subject: "panel.local"}
	tiruan := &helperTiruan{balas: want}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/certificates", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d, ingin 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdCertificatesGet {
		t.Fatalf("command = %q, ingin %q", tiruan.cmd, helperproto.CmdCertificatesGet)
	}
	var got helperproto.CertificatesStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CertPath != want.CertPath || got.KeyPath != want.KeyPath || got.Active != want.Active || got.Valid != want.Valid || got.Subject != want.Subject {
		t.Fatalf("status = %+v, ingin %+v", got, want)
	}
}

func TestCertificatesAPIPutMeneruskanKeduaPath(t *testing.T) {
	tiruan := &helperTiruan{balas: helperproto.CertificatesStatus{Active: true, Valid: true}}
	r, st := buatServerCron(t, tiruan)
	sess := buatSesi(t, st, "ani", true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/certificates", strings.NewReader(`{"cert_path":"/cert.pem","key_path":"/key.pem"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, ingin 200 (body: %s)", w.Code, w.Body.String())
	}
	if tiruan.cmd != helperproto.CmdCertificatesSet {
		t.Fatalf("command = %q, ingin %q", tiruan.cmd, helperproto.CmdCertificatesSet)
	}
	var got helperproto.CertificatesSetArgs
	if err := json.Unmarshal(tiruan.args, &got); err != nil {
		t.Fatal(err)
	}
	if got.CertPath != "/cert.pem" || got.KeyPath != "/key.pem" {
		t.Fatalf("args = %+v", got)
	}
}
