package helper

import (
	"strings"
	"testing"
)

func TestKatalogProxyManagerMemuatKomponenWajib(t *testing.T) {
	for _, name := range []string{"nginx", "certbot"} {
		found := false
		for _, listed := range ComponentNames() {
			if listed == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("komponen %s tidak tampil di Components", name)
		}
	}
	nginx, ok := components["nginx"]
	if !ok {
		t.Fatal("komponen nginx belum terdaftar")
	}
	if nginx.RequiredFor != "Settings → Proxy manager" {
		t.Fatalf("nginx RequiredFor = %q", nginx.RequiredFor)
	}
	if nginx.KelolaDi != "Settings → Proxy manager" {
		t.Fatalf("nginx KelolaDi = %q", nginx.KelolaDi)
	}
	if !nginx.portsPublik {
		t.Error("port nginx harus ditandai publik agar reverse proxy dapat diakses dari internet")
	}
	for _, target := range []string{"80", "443"} {
		ada := false
		for _, p := range nginx.ports {
			if p.Port == target && p.Proto == "tcp" {
				ada = true
			}
		}
		if !ada {
			t.Errorf("nginx kehilangan port publik %s/tcp", target)
		}
	}

	certbot, ok := components["certbot"]
	if !ok {
		t.Fatal("komponen certbot belum terdaftar")
	}
	if certbot.RequiredFor != "Settings → Proxy manager (TLS otomatis)" {
		t.Fatalf("certbot RequiredFor = %q", certbot.RequiredFor)
	}
}

func TestKonflikPortMenolakPemilikLainDenganAlasan(t *testing.T) {
	c := &component{Name: "nginx", Label: "Nginx", ports: []portKomponen{
		{Port: "80", Proto: "tcp"},
		{Port: "443", Proto: "tcp"},
	}}
	terpakai := []portTerpakai{
		{Port: "443", Proto: "tcp", Proses: "stalwart", PID: 321},
	}

	err := cekKonflikPort(c, terpakai)
	if err == nil {
		t.Fatal("port 443 yang dipakai Stalwart seharusnya ditolak")
	}
	pesan := err.Error()
	for _, fragmen := range []string{"nginx", "443/tcp", "stalwart", "321"} {
		if !strings.Contains(strings.ToLower(pesan), strings.ToLower(fragmen)) {
			t.Errorf("pesan konflik %q tidak memuat %q", pesan, fragmen)
		}
	}
	if kodeErr(err) != "port_conflict" {
		t.Fatalf("kode error = %q, harap port_conflict", kodeErr(err))
	}
}

func TestKonflikPortMengabaikanProtokolBerbeda(t *testing.T) {
	c := &component{Name: "dns-web", portsPublik: true,
}
	if err := cekKonflikPort(c, []portTerpakai{{Port: "80", Proto: "udp", Proses: "dns"}}); err != nil {
		t.Fatalf("80/udp tidak boleh bentrok dengan 80/tcp: %v", err)
	}
}

func TestParsePortTerpakaiDariSS(t *testing.T) {
	keluaran := `tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("stalwart",pid=321,fd=9))
` +
		`tcp LISTEN 0 4096 [::]:80 [::]:* users:(("nginx",pid=654,fd=7))
` +
		`udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("dns",pid=11,fd=4))`

	dapat := parsePortTerpakai(keluaran)
	if len(dapat) != 3 {
		t.Fatalf("jumlah listener = %d, harap 3: %#v", len(dapat), dapat)
	}
	if dapat[0] != (portTerpakai{Port: "443", Proto: "tcp", Proses: "stalwart", PID: 321}) {
		t.Fatalf("listener pertama salah: %#v", dapat[0])
	}
	if dapat[1].Port != "80" || dapat[1].Proses != "nginx" {
		t.Fatalf("listener IPv6 salah: %#v", dapat[1])
	}
	if dapat[2].Port != "53" || dapat[2].Proto != "udp" {
		t.Fatalf("listener UDP salah: %#v", dapat[2])
	}
}
