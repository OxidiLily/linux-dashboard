package deploy

import (
	"os"
	"strings"
	"testing"
)

func TestHelperTidakMengambilKepemilikanStateWeb(t *testing.T) {
	b, err := os.ReadFile("linux-dashboard-helper.service")
	if err != nil {
		t.Fatal(err)
	}
	isi := string(b)
	if strings.Contains(isi, "StateDirectory=linux-dashboard linux-dashboard-helper") {
		t.Fatal("helper mengubah ownership state web ke root dan membuat start pertama web gagal")
	}
	if !strings.Contains(isi, "StateDirectory=linux-dashboard-helper") {
		t.Fatal("state directory rahasia helper hilang")
	}
}

func TestInstallerBaruMemulaiPanelHTTPDanMenjagaPilihanTLSLama(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `if [[ "$config_baru" == true ]]`) || !strings.Contains(s, `set_env_dashboard DASHBOARD_ALLOW_PLAINTEXT true`) || !strings.Contains(s, `set_env_dashboard DASHBOARD_SECURE_COOKIE false`) {
		t.Fatal("first install harus eksplisit HTTP agar login bisa dilakukan, bukan self-signed")
	}
	if !strings.Contains(s, `if [[ "$config_baru" == true ]]; then`) || !strings.Contains(s, `tls_cert=$(ambil_env_dashboard DASHBOARD_TLS_CERT)`) {
		t.Fatal("update tidak boleh diam-diam mengubah TLS lama")
	}
}

func TestInstallerMenyiapkanPluginDNSCloudflareUntukCertbotTerpasang(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `if command -v certbot >/dev/null 2>&1 && ! paket_terpasang python3-certbot-dns-cloudflare; then`) || !strings.Contains(s, `apt-get install -y -qq --no-install-recommends python3-certbot-dns-cloudflare`) {
		t.Fatal("plugin DNS-01 wajib dipasang untuk Certbot existing")
	}
}

func TestInstallerMembuatStateWebMilikService(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 /var/lib/linux-dashboard`) {
		t.Fatal("installer tidak menyiapkan state web yang writable sebelum start pertama")
	}
}

func TestInstallerMenyiapkanStateHelperTerpisahTanpaMempromosikanStateWeb(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `install -d -o root -g "$SERVICE_USER" -m 0750 "$helper_state"`) ||
		!strings.Contains(s, `[[ ! -L "$helper_state" ]]`) {
		t.Fatal("state helper harus root-owned dan bukan symlink")
	}
	if strings.Contains(s, `cp /var/lib/linux-dashboard/docker-ports.json`) ||
		strings.Contains(s, `cp /var/lib/linux-dashboard/komponen-ports.json`) {
		t.Fatal("state firewall web-writable lama tidak boleh dipercaya")
	}
}

func TestInstallerMengembalikanKepemilikanArtefakBuild(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	isi := string(b)
	if !strings.Contains(isi, "make build\n") {
		t.Fatal("jalur build installer tidak ditemukan")
	}
	// Build berjalan sebagai root di checkout milik user. Tanpa chown balik,
	// web/dist + node_modules jadi root:root dan `npm run test` /
	// `npm run build` lokal gagal EACCES sampai user membersihkannya dengan sudo.
	if !strings.Contains(isi, "chown -R \"${SUDO_USER}\" bin web/dist web/ui/node_modules") {
		t.Fatal("installer tidak mengembalikan kepemilikan dependency build ke user")
	}
	idxBuild := strings.Index(isi, "make build\n")
	idxChown := strings.Index(isi, "chown -R \"${SUDO_USER}\" bin web/dist web/ui/node_modules")
	if idxChown < idxBuild {
		t.Fatal("chown harus dijalankan setelah make build")
	}
}

func TestInstallerPindahKeRootSaatDijalankanDariStdin(t *testing.T) {
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	isi := string(b)
	guard := "if [[ ! -f \"${BASH_SOURCE[0]:-}\" ]]; then\n  cd /"
	posGuard := strings.Index(isi, guard)
	if posGuard < 0 {
		t.Fatal("installer dari stdin tidak menetralkan working directory yang mungkin sudah dihapus")
	}
	posOSCheck := strings.Index(isi, "grep -qiE 'ubuntu|debian' /etc/os-release")
	if posOSCheck < 0 || posGuard > posOSCheck {
		t.Fatal("guard working directory harus berjalan sebelum command eksternal pertama")
	}
}
