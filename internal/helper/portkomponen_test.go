package helper

import (
	"strings"
	"testing"
)

func TestPortSSHDefault(t *testing.T) {
	ports := portSSH()
	if len(ports) == 0 {
		t.Fatalf("portSSH() tidak boleh kosong")
	}
	found := false
	for _, p := range ports {
		if p == "22" || portRe.MatchString(p) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("portSSH() tidak memuat port valid: %v", ports)
	}
}

func TestKomponenPortsValid(t *testing.T) {
	harusPunyaPort := map[string][]string{
		"samba":          {"445", "139", "137:138"},
		"nfs-server":     {"2049", "111"},
		"avahi":          {"5353"},
		"technitium-dns": {"53", "5380"},
		"print-server":   {"631"},
		"9router":        {"20128"},
		"supabase":       {"8000", "5432", "6543"},
		"arkon":          {"5055", "3119"},
		"wireguard":      {"51820"},
		"tailscale":      {"41641"},
	}

	for nama, daftarPort := range harusPunyaPort {
		c, ok := components[nama]
		if !ok {
			t.Errorf("komponen %q tidak ditemukan di katalog", nama)
			continue
		}
		if len(c.ports) == 0 {
			t.Errorf("komponen %q harus punya port terdaftar", nama)
			continue
		}
		for _, target := range daftarPort {
			ada := false
			for _, p := range c.ports {
				if p.Port == target {
					ada = true
					break
				}
			}
			if !ada {
				t.Errorf("komponen %q kehilangan port %q", nama, target)
			}
		}

		for _, p := range c.ports {
			if !portRe.MatchString(p.Port) {
				t.Errorf("komponen %q punya port tidak valid: %s", nama, p.Port)
			}
			if p.Proto != "tcp" && p.Proto != "udp" {
				t.Errorf("komponen %q punya proto tidak valid: %s", nama, p.Proto)
			}
			rule := aturanPort(p, "")
			if rule.Action != "allow" {
				t.Errorf("aturanPort harus allow, didapat: %s", rule.Action)
			}
			if rule.From != "" {
				t.Errorf("aturanPort bawaan harus Anywhere (kosong), didapat: %s", rule.From)
			}
			args, err := ufwArgs(rule)
			if err != nil {
				t.Errorf("ufwArgs gagal untuk port %s/%s: %v", p.Port, p.Proto, err)
			}
			if len(args) != 2 || args[0] != "allow" || !strings.Contains(args[1], p.Port) {
				t.Errorf("ufwArgs format salah: %v", args)
			}
		}
	}
}

func TestPortAksesAdmin(t *testing.T) {
	ports := portAksesAdmin()
	if len(ports) == 0 {
		t.Fatalf("portAksesAdmin() tidak boleh kosong (harus minimal SSH)")
	}
	hasSSH := false
	for _, p := range ports {
		if p.Guna == "SSH" {
			hasSSH = true
			if !portRe.MatchString(p.Port) {
				t.Errorf("port SSH tidak valid: %s", p.Port)
			}
		}
	}
	if !hasSSH {
		t.Errorf("portAksesAdmin() harus memuat port SSH")
	}
}
