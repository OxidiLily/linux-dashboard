package helper

import (
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestValidasiSambaShareMenolakGuestOK(t *testing.T) {
	err := validasiSambaShare(helperproto.SambaShare{
		Name:     "Data",
		Path:     "/srv/data",
		Writable: true,
		Public:   true,
	})
	if err == nil {
		t.Fatal("share Guest OK diterima; akses tulis anonim membuka jalur ransomware dari seluruh klien LAN")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "guest ok") {
		t.Fatalf("pesan error tidak menjelaskan Guest OK: %v", err)
	}
}

func TestGlobalSambaMematikanPemetaanGuest(t *testing.T) {
	if sambaBarisMapToGuest != "   map to guest = Never" {
		t.Fatalf("map to guest = %q; harus Never agar username tak dikenal tidak dipetakan ke guest", sambaBarisMapToGuest)
	}
}
