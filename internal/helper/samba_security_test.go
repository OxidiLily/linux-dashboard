package helper

import (
	"os"
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

// Auth wajib. Share path konkret selalu dapat akun managed, tetapi share %U
// tidak pernah — tanpa daftar user ia jatuh ke "semua user Samba", yaitu
// akses bagi siapa pun yang punya password Samba.
func TestValidasiSambaShareMenolakShareTanpaUser(t *testing.T) {
	err := validasiSambaShare(helperproto.SambaShare{
		Name: "Docs",
		Path: "/home/%U/DATA",
	})
	if err == nil {
		t.Fatal("share bermakro tanpa valid users diterima; share jatuh ke 'semua user Samba'")
	}
	if !strings.Contains(err.Error(), "minimal satu user") {
		t.Fatalf("pesan error tidak menjelaskan kewajiban user: %v", err)
	}

	if err := validasiSambaShare(helperproto.SambaShare{
		Name: "Docs", Path: "/home/%U/DATA", ValidUsers: []string{"budi"},
	}); err != nil {
		t.Fatalf("share %%U dengan valid users ditolak: %v", err)
	}
	// Kredensial legacy diisi bersamaan → turut dijadikan valid users oleh
	// sambaSave, jadi tetap terautentikasi.
	if err := validasiSambaShare(helperproto.SambaShare{
		Name: "Docs", Path: "/home/%U/DATA", SmbUser: "budi", SmbPass: "kunci-panjang-satu",
	}); err != nil {
		t.Fatalf("share %%U dengan kredensial legacy ditolak: %v", err)
	}
}

// Entri valid users yang kosong/spasi ditolak sebelum renderer menulis
// `valid users = ` yang dibiarkan smbd sebagai parameter tidak ada — share
// akan terbuka untuk SEMUA user Samba, bukan untuk daftar yang diminta.
func TestValidasiSambaShareMenolakUserKosong(t *testing.T) {
	for _, kasus := range [][]string{{" "}, {""}, {"alice", "   "}} {
		err := validasiSambaShare(helperproto.SambaShare{
			Name:       "Docs",
			Path:       "/home/%U/DATA",
			ValidUsers: kasus,
		})
		if err == nil {
			t.Fatalf("valid_users %q diterima, harap ditolak", kasus)
		}
		if !strings.Contains(err.Error(), "kosong") {
			t.Fatalf("pesan error tidak menjelaskan penolakan: %v", err)
		}
	}
}

// Renderer adalah lapisan terakhir sebelum config masuk smbd: `guest ok = no`
// harus selalu tertulis, dan valid users tidak boleh gugur walau struct masih
// membawa Public=true (mis. dari share lama yang belum dimigrasikan).
func TestRenderSambaSharesKunciAuth(t *testing.T) {
	out := string(renderSambaShares([]helperproto.SambaShare{{
		Name: "Data", Path: "/srv/data", Writable: true, Public: true,
		ValidUsers: []string{"lds-x"},
	}}))
	if strings.Contains(out, "guest ok = yes") {
		t.Fatalf("renderer menulis guest ok = yes:\n%s", out)
	}
	if !strings.Contains(out, "guest ok = no") {
		t.Fatalf("renderer tidak menulis guest ok = no:\n%s", out)
	}
	if !strings.Contains(out, "valid users = lds-x") {
		t.Fatalf("valid users hilang saat Public=true — share jatuh ke semua user Samba:\n%s", out)
	}
}

func bacaSmbConfUji(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(sambaMainConf)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Tanpa `restrict anonymous = 2` klien bisa membuka sesi tanpa kredensial ke
// IPC$ lalu mendaftar nama share — terbukti di mesin nyata dengan
// `net rpc share list -N` yang menjawab "Anonymous login successful".
func TestBlokAuditGlobalBaruMenutupNullSession(t *testing.T) {
	u := siapkanUjiSamba(t)
	u.tulis("smb.conf", "[global]\n   workgroup = WORKGROUP\n\n[printers]\n   path = /var/tmp\n")

	if err := pastikanGlobalAuditSamba(); err != nil {
		t.Fatalf("persiapan blok audit gagal: %v", err)
	}
	isi := bacaSmbConfUji(t)
	for _, wajib := range []string{sambaBarisMapToGuest, sambaBarisRestrictAnonymous} {
		if !strings.Contains(isi, wajib) {
			t.Errorf("blok audit tidak memuat %q:\n%s", strings.TrimSpace(wajib), isi)
		}
	}
	if strings.Index(isi, sambaBarisRestrictAnonymous) > strings.Index(isi, "[printers]") {
		t.Errorf("restrict anonymous berada di luar [global]:\n%s", isi)
	}
	if !u.dipanggil("systemctl restart smbd") {
		t.Error("smbd tidak dimuat ulang setelah blok audit ditulis")
	}
}

// Instalasi lama sudah punya blok panel tapi tanpa `restrict anonymous`.
// Baris itu harus disisipkan ke blok yang sama, dan baris admin di luar blok
// tidak boleh tersentuh.
func TestLengkapiBlokAuditMenambahRestrictAnonymous(t *testing.T) {
	u := siapkanUjiSamba(t)
	// Bentuk persis seperti yang ditulis versi panel sebelumnya: `Bad User`,
	// tanpa restrict anonymous, plus satu baris milik admin di luar blok.
	u.tulis("smb.conf",
		"[global]\n"+
			"   hosts deny = 10.0.0.0/8\n"+
			"\n"+sambaTandaGlobal+"\n"+
			"# Dibaca jail fail2ban \"samba\". Hapus blok ini untuk mengembalikan\n"+
			"# perilaku bawaan; baris asli di atas tidak pernah diubah.\n"+
			sambaBarisMapToGuestLama+"\n"+
			"   log level = 0 auth_audit:3\n"+
			"\n[printers]\n   path = /var/tmp\n")

	if err := pastikanGlobalAuditSamba(); err != nil {
		t.Fatalf("migrasi blok audit gagal: %v", err)
	}
	isi := bacaSmbConfUji(t)
	if !strings.Contains(isi, sambaBarisRestrictAnonymous) {
		t.Errorf("restrict anonymous tidak disisipkan ke blok lama:\n%s", isi)
	}
	if strings.Contains(isi, sambaBarisMapToGuestLama) {
		t.Errorf("map to guest = Bad User belum dimigrasikan:\n%s", isi)
	}
	if !strings.Contains(isi, "   hosts deny = 10.0.0.0/8") {
		t.Errorf("baris admin di luar blok ikut terhapus:\n%s", isi)
	}
	if strings.Index(isi, sambaBarisRestrictAnonymous) > strings.Index(isi, "[printers]") {
		t.Errorf("restrict anonymous disisipkan setelah [global]:\n%s", isi)
	}
}

// `restrict anonymous` di section LAIN bukan milik panel: keberadaannya di
// [arsip] tidak boleh membuat panel menganggap "sudah ada" dan melewatkan
// sisipan ke [global] — null session tetap terbuka kalau itu terjadi.
func TestLengkapiBlokAuditAbaikanRestrictAnonymousDiluarGlobal(t *testing.T) {
	u := siapkanUjiSamba(t)
	u.tulis("smb.conf",
		"[global]\n"+sambaTandaGlobal+"\n"+sambaBarisMapToGuest+"\n   log level = 0 auth_audit:3\n"+
			"\n[arsip]\n   path = /srv/arsip\n   restrict anonymous = 1\n")

	if err := pastikanGlobalAuditSamba(); err != nil {
		t.Fatalf("migrasi blok audit gagal: %v", err)
	}
	isi := bacaSmbConfUji(t)
	if strings.Count(isi, sambaBarisRestrictAnonymous) != 1 {
		t.Errorf("restrict anonymous = 2 tidak disisipkan ke [global] (jumlah %d):\n%s",
			strings.Count(isi, sambaBarisRestrictAnonymous), isi)
	}
	if strings.Index(isi, sambaBarisRestrictAnonymous) > strings.Index(isi, "[arsip]") {
		t.Errorf("restrict anonymous disisipkan di luar [global]:\n%s", isi)
	}
}

// Panggilan kedua tidak boleh menduplikasi baris — Samba memakai nilai
// terakhir, tetapi duplikat menandai migrasi yang tidak idempoten.
func TestLengkapiBlokAuditIdempoten(t *testing.T) {
	u := siapkanUjiSamba(t)
	u.tulis("smb.conf",
		"[global]\n"+sambaTandaGlobal+"\n"+sambaBarisMapToGuest+"\n"+
			sambaBarisRestrictAnonymous+"\n   log level = 0 auth_audit:3\n")

	if err := pastikanGlobalAuditSamba(); err != nil {
		t.Fatalf("blok audit sudah lengkap tetapi gagal: %v", err)
	}
	isi := bacaSmbConfUji(t)
	if strings.Count(isi, sambaBarisRestrictAnonymous) != 1 {
		t.Errorf("restrict anonymous muncul %d kali:\n%s",
			strings.Count(isi, sambaBarisRestrictAnonymous), isi)
	}
	if u.dipanggil("systemctl restart smbd") {
		t.Error("smbd direstart ulang padahal tidak ada perubahan")
	}
}
