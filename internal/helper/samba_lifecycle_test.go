package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// ---- perkakas uji samba ----

// ujiSamba menyiapkan binary tiruan (useradd, getent, setfacl, …) plus berkas
// config/manifest Samba di direktori sementara, sehingga alur sambaSave/
// sambaDelete bisa dijalankan utuh tanpa menyentuh /etc/samba, akun, atau ACL
// mesin pengembang.
type ujiSamba struct {
	t   *testing.T
	dir string
	log string
}

func siapkanUjiSamba(t *testing.T) *ujiSamba {
	t.Helper()
	dir := t.TempDir()
	u := &ujiSamba{t: t, dir: dir, log: filepath.Join(dir, "panggilan.log")}

	// PATH proses (untuk exec.LookPath) dan PATH anak proses (untuk run()).
	lamaPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+lamaPath)
	lamaExec := pathExec
	pathExec = dir + ":" + lamaExec

	// Path Samba diarahkan ke berkas sementara. Keempatnya variabel di kode
	// produksi — nilai defaultnya tidak berubah untuk mesin nyata.
	lamaInc, lamaMain, lamaManifest := sambaIncludePath, sambaMainConf, sambaManifestPath
	sambaIncludePath = filepath.Join(dir, "lindash-shares.conf")
	sambaMainConf = filepath.Join(dir, "smb.conf")
	sambaManifestPath = filepath.Join(dir, "samba-manifest.json")
	t.Cleanup(func() {
		pathExec = lamaExec
		sambaIncludePath, sambaMainConf, sambaManifestPath = lamaInc, lamaMain, lamaManifest
	})

	// smb.conf sudah memuat include-nya: ensureSambaInclude cukup no-op.
	if err := os.WriteFile(sambaMainConf, []byte("include = "+sambaIncludePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// useradd tiruan MENIRU shadow-utils: menolak ':' pada comment (rc=3).
	// Penolakan ini terverifikasi empiris — tanpanya bug GECOS tidak teruji.
	u.tulisBin("useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "`+u.log+`"
prev=
for a in "$@"; do
  case "$prev" in
    -c|--comment)
      case "$a" in
        *:*) echo "useradd: invalid comment '$a'" >&2; exit 3;;
      esac
      ;;
  esac
  prev=$a
done
exit 0
`)
	u.tulisBin("getent", `#!/bin/sh
printf 'getent %s\n' "$*" >> "`+u.log+`"
if [ -f "`+filepath.Join(dir, "getent-passwd.txt")+`" ]; then
  cat "`+filepath.Join(dir, "getent-passwd.txt")+`"
  exit 0
fi
exit 2
`)
	u.tulisBin("id", `#!/bin/sh
printf 'id %s\n' "$*" >> "`+u.log+`"
echo 12345
exit 0
`)
	u.tulisBin("smbpasswd", `#!/bin/sh
printf 'smbpasswd %s\n' "$*" >> "`+u.log+`"
exit 0
`)
	u.tulisBin("setfacl", `#!/bin/sh
printf 'setfacl %s\n' "$*" >> "`+u.log+`"
if [ -f "`+filepath.Join(dir, "setfacl-gagal.txt")+`" ]; then
  echo 'setfacl: Io error' >&2
  exit 1
fi
exit 0
`)
	u.tulisBin("find", `#!/bin/sh
printf 'find %s\n' "$*" >> "`+u.log+`"
exit 0
`)
	u.tulisBin("testparm", `#!/bin/sh
printf 'testparm %s\n' "$*" >> "`+u.log+`"
exit 0
`)
	u.tulisBin("systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "`+u.log+`"
exit 0
`)
	u.tulisBin("userdel", `#!/bin/sh
printf 'userdel %s\n' "$*" >> "`+u.log+`"
exit 0
`)
	return u
}

func (u *ujiSamba) tulisBin(nama, skrip string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(skrip), 0o755); err != nil {
		u.t.Fatal(err)
	}
}

func (u *ujiSamba) tulis(nama, isi string) {
	u.t.Helper()
	if err := os.WriteFile(filepath.Join(u.dir, nama), []byte(isi), 0o644); err != nil {
		u.t.Fatal(err)
	}
}

// setfaclGagal membuat SEMUA panggilan setfacl keluar dengan status 1 —
// bentuk yang sama dengan paket acl tidak terpasang atau permission ditolak.
func (u *ujiSamba) setfaclGagal() { u.tulis("setfacl-gagal.txt", "1\n") }

// panggilan mengembalikan setiap baris argumen yang diterima binary tiruan.
func (u *ujiSamba) panggilan() []string {
	u.t.Helper()
	b, err := os.ReadFile(u.log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (u *ujiSamba) dipanggil(awalan string) bool {
	for _, p := range u.panggilan() {
		if strings.HasPrefix(p, awalan+" ") || p == awalan {
			return true
		}
	}
	return false
}

func (u *ujiSamba) jumlahPanggilan(awalan string) int {
	jumlah := 0
	for _, p := range u.panggilan() {
		if strings.HasPrefix(p, awalan+" ") || p == awalan {
			jumlah++
		}
	}
	return jumlah
}

// tidakDipanggil memastikan binary TERTENTU tidak pernah dijalankan — inti
// dari dua temuan review: jangan membuat akun sebelum config terbaca, dan
// jangan membebaskan UID selama ACE masih menempel.
func (u *ujiSamba) tidakDipanggil(nama string) {
	u.t.Helper()
	if u.dipanggil(nama) {
		u.t.Fatalf("%s terpanggil padahal tidak boleh:\n%s", nama, strings.Join(u.panggilan(), "\n"))
	}
}

// ---- uji GECOS ----

// TestBuatAkunShareGECOSDiterimaUseradd menjaga format GECOS tetap kompatibel
// dengan /etc/passwd: kolon adalah pemisah field, jadi GECOS yang memuat kolon
// membuat useradd menolak pembuatan akun (shadow-utils: "invalid comment",
// rc=3) dan parser getent memotong field-nya. Keduanya mematikan seluruh
// siklus akun managed — validasi identitas akan selalu gagal atau akun tidak
// pernah terbentuk.
func TestBuatAkunShareGECOSDiterimaUseradd(t *testing.T) {
	u := siapkanUjiSamba(t)
	share := helperproto.SambaShare{Name: "Data", Path: filepath.Join(u.dir, "data"), Writable: true}

	entry, pass, err := buatAkunShare(share, sambaManifest{})
	if err != nil {
		t.Fatalf("buatAkunShare gagal — useradd menolak GECOS yang dipakai: %v", err)
	}
	if strings.Contains(entry.GECOS, ":") {
		t.Fatalf("GECOS memuat ':' — /etc/passwd memakai ':' sebagai pemisah field: %q", entry.GECOS)
	}
	if entry.Username == "" || entry.UID == 0 || pass == "" {
		t.Fatalf("akun tidak lengkap: %+v pass=%q", entry, pass)
	}
	// Identitas yang terekam di manifest harus lolos validasi yang sama dengan
	// yang dipakai jalur save/delete.
	u.tulis("getent-passwd.txt", entry.Username+":x:12345:1000:"+entry.GECOS+":/nonexistent:/usr/sbin/nologin\n")
	if err := validasiIdentitasAkunManaged(entry); err != nil {
		t.Fatalf("identitas hasil pembuatan tidak lolos validasi: %v", err)
	}
}

// ---- uji urutan save ----

// TestSambaSaveBacaConfigGagalSebelumAkunDibuat menjaga urutan baca-manifest/
// config SEBELUM akun dan ACL dibuat. Sebelum perbaikan, sambaList() dipanggil
// setelah buatAkunShare+pasangACLShare, sehingga kegagalan membaca config
// meninggalkan akun system tanpa entri manifest — tak terlihat di panel, tak
// bisa dirotasi, dan tak pernah dibersihkan.
func TestSambaSaveBacaConfigGagalSebelumAkunDibuat(t *testing.T) {
	u := siapkanUjiSamba(t)
	// Config tidak terbaca: path-nya direktori, jadi ReadFile gagal dengan
	// EISDIR (bukan IsNotExist) — bentuk error yang membuat sambaList gagal.
	// Berkas konfignya sendiri memang belum pernah dibuat oleh harness.
	if err := os.Mkdir(sambaIncludePath, 0o755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(u.dir, "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := sambaSave(helperproto.SambaShare{Name: "Data", Path: data, Writable: true})
	if err == nil {
		t.Fatal("sambaSave harus gagal ketika config tidak terbaca")
	}
	u.tidakDipanggil("useradd")
	u.tidakDipanggil("setfacl")
	u.tidakDipanggil("smbpasswd")
}

// ---- uji urutan delete ----

// TestSambaDeleteACLGagalTidakMenghapusAkunDanShare menjaga tiga hal sekaligus
// ketika pencabutan ACL gagal: (1) kegagalan dikembalikan, bukan ditelan;
// (2) akun tidak dihapus — membebaskan UID selama ACE masih menempel membuat
// user baru dengan UID sama mewarisi akses share; (3) config dan manifest
// tidak disentuh supaya operasi bisa diulang dari panel.
func TestSambaDeleteACLGagalTidakMenghapusAkunDanShare(t *testing.T) {
	u := siapkanUjiSamba(t)
	data := filepath.Join(u.dir, "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := renderSambaShares([]helperproto.SambaShare{{
		Name: "Data", Path: data, Writable: true, ValidUsers: []string{"lds-tes"},
	}})
	if err := os.WriteFile(sambaIncludePath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tulisManifestSamba(sambaManifest{Shares: map[string]sambaManagedShare{
		"Data": {Username: "lds-tes", UID: 12345, Path: data,
			GECOS: "linux-dashboard samba share Data", Ancestors: []string{u.dir}},
	}}); err != nil {
		t.Fatal(err)
	}
	u.tulis("getent-passwd.txt",
		"lds-tes:x:12345:1000:linux-dashboard samba share Data:/nonexistent:/usr/sbin/nologin\n")
	u.setfaclGagal()

	err := sambaDelete("Data")
	if err == nil {
		t.Fatal("sambaDelete harus mengembalikan error ketika pencabutan ACL gagal — kegagalan ini menentukan apakah share benar-benar bersih")
	}
	u.tidakDipanggil("userdel")
	u.tidakDipanggil("smbpasswd")

	// Config harus kembali/konsisten: share masih ada supaya bisa dicoba ulang.
	b, err := os.ReadFile(sambaIncludePath)
	if err != nil || !strings.Contains(string(b), "[Data]") {
		t.Fatalf("config tidak utuh setelah kegagalan ACL (err=%v):\n%s", err, b)
	}
	m, err := bacaManifestSamba()
	if err != nil {
		t.Fatal(err)
	}
	if _, ada := m.Shares["Data"]; !ada {
		t.Fatalf("manifest kehilangan entry setelah kegagalan ACL: %+v", m.Shares)
	}
	// Satu write menonaktifkan share, satu lagi memulihkannya. Ini membuktikan
	// urutan config-off → revoke ACL → restore, bukan sekadar config tak berubah.
	if got := u.jumlahPanggilan("testparm"); got != 2 {
		t.Fatalf("config harus divalidasi dua kali (hapus + restore), got %d:\n%s", got, strings.Join(u.panggilan(), "\n"))
	}
	panggilan := u.panggilan()
	idxConfig, idxACL, idxRestore := -1, -1, -1
	for i, p := range panggilan {
		if strings.HasPrefix(p, "systemctl restart smbd") {
			if idxConfig < 0 {
				idxConfig = i
			} else if idxRestore < 0 {
				idxRestore = i
			}
		}
		if idxACL < 0 && (strings.HasPrefix(p, "setfacl ") || strings.Contains(p, " setfacl ")) {
			idxACL = i
		}
	}
	if idxConfig < 0 || idxACL < 0 || idxRestore < 0 || !(idxConfig < idxACL && idxACL < idxRestore) {
		t.Fatalf("urutan wajib config-off < revoke ACL < restore config:\n%s", strings.Join(panggilan, "\n"))
	}
}

// TestSambaSaveACLParsialGagalTidakMembebaskanUID menjaga call site pertama
// sesudah pasangACLShare. Bila rollback ACL parsial juga gagal, akun baru harus
// tetap ada agar UID yang masih tercatat di ACE tidak diberikan ke user lain.
func TestSambaSaveACLParsialGagalTidakMembebaskanUID(t *testing.T) {
	u := siapkanUjiSamba(t)
	data := filepath.Join(u.dir, "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	u.setfaclGagal()

	_, err := sambaSave(helperproto.SambaShare{Name: "Data", Path: data, Writable: true})
	if err == nil || !strings.Contains(err.Error(), "membersihkan ACL parsial juga gagal") {
		t.Fatalf("harus melaporkan rollback ACL parsial gagal, got: %v", err)
	}
	u.tidakDipanggil("userdel")
	for _, p := range u.panggilan() {
		if strings.HasPrefix(p, "smbpasswd -x ") {
			t.Fatalf("kredensial Samba tidak boleh dicabut saat UID masih memiliki ACE: %s", p)
		}
	}
}

// TestSambaSavePindahKeSubdirektoriTidakMencabutACLBaru menjaga path overlap.
// Setelah /data → /data/private, pencabutan ACL lama tidak boleh rekursif
// melewati /data/private karena itu akan menghapus ACL yang baru dipasang.
func TestSambaSavePindahKeSubdirektoriMemasangUlangACLBaru(t *testing.T) {
	u := siapkanUjiSamba(t)
	lama := filepath.Join(u.dir, "data")
	baru := filepath.Join(lama, "private")
	if err := os.MkdirAll(baru, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := renderSambaShares([]helperproto.SambaShare{{
		Name: "Data", Path: lama, Writable: true, ValidUsers: []string{"lds-tes"},
	}})
	if err := os.WriteFile(sambaIncludePath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := sambaManagedShare{Username: "lds-tes", UID: 12345, Path: lama,
		GECOS: "linux-dashboard samba share Data", Ancestors: ancestorShare(lama)}
	if err := tulisManifestSamba(sambaManifest{Shares: map[string]sambaManagedShare{"Data": entry}}); err != nil {
		t.Fatal(err)
	}
	u.tulis("getent-passwd.txt",
		"lds-tes:x:12345:1000:linux-dashboard samba share Data:/nonexistent:/usr/sbin/nologin\n")

	if _, err := sambaSave(helperproto.SambaShare{Name: "Data", Path: baru, Writable: true}); err != nil {
		t.Fatalf("pindah share gagal: %v", err)
	}
	panggilan := u.panggilan()
	idxCabut, idxPasangUlang := -1, -1
	for i, p := range panggilan {
		if strings.HasPrefix(p, "find "+lama+" -xdev ") && strings.Contains(p, "-execdir setfacl -P -x u:12345 {} +") {
			idxCabut = i
		}
		if idxCabut >= 0 && strings.HasPrefix(p, "find "+baru+" -xdev ") && strings.Contains(p, "-execdir setfacl -P -m u:lds-tes:rwX {} +") {
			idxPasangUlang = i
		}
	}
	if idxCabut < 0 || idxPasangUlang <= idxCabut {
		t.Fatalf("ACL baru harus dipasang ulang setelah revoke path lama overlap:\n%s", strings.Join(panggilan, "\n"))
	}
}

func TestACLShareTraversalTidakMenyerahkanSymlinkKeSetfacl(t *testing.T) {
	u := siapkanUjiSamba(t)
	data := filepath.Join(u.dir, "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	share := helperproto.SambaShare{Name: "Data", Path: data, Writable: true}
	if err := pasangACLShare(share, "lds-tes"); err != nil {
		t.Fatal(err)
	}
	if err := lepasACLShare(sambaManagedShare{Username: "lds-tes", UID: 12345, Path: data}); err != nil {
		t.Fatal(err)
	}
	for _, p := range u.panggilan() {
		if !strings.HasPrefix(p, "find "+data+" ") {
			continue
		}
		if !strings.Contains(p, " -xdev ") || !strings.Contains(p, " -execdir setfacl -P ") {
			t.Fatalf("traversal ACL wajib -xdev + -execdir + setfacl -P: %s", p)
		}
		if !strings.Contains(p, "-type d ! -samefile "+data+" -execdir mountpoint -q -- {} ; -prune") {
			t.Fatalf("nested mountpoint wajib dideteksi dan di-prune sebelum setfacl: %s", p)
		}
		// Selector leaf harus berada di cabang kanan sesudah prune, tepat sebelum
		// setfacl. `-type d` dari cabang mountpoint tidak boleh dianggap bukti.
		if strings.Contains(p, "d:u:") {
			if !strings.Contains(p, ") -o -type d -execdir setfacl -P ") {
				t.Fatalf("default ACL hanya boleh menerima direktori pada cabang leaf: %s", p)
			}
		} else if !strings.Contains(p, ") -o ( -type f -o -type d ) -execdir setfacl -P ") {
			t.Fatalf("access ACL hanya boleh menerima file/direktori pada cabang leaf: %s", p)
		}
	}
}

func TestSambaSaveManagedMenolakTransisiKePersenU(t *testing.T) {
	u := siapkanUjiSamba(t)
	lama := filepath.Join(u.dir, "data")
	if err := os.Mkdir(lama, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := renderSambaShares([]helperproto.SambaShare{{Name: "Data", Path: lama, Writable: true, ValidUsers: []string{"lds-tes"}}})
	if err := os.WriteFile(sambaIncludePath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := sambaManagedShare{Username: "lds-tes", UID: 12345, Path: lama,
		GECOS: "linux-dashboard samba share Data", Ancestors: ancestorShare(lama)}
	if err := tulisManifestSamba(sambaManifest{Shares: map[string]sambaManagedShare{"Data": entry}}); err != nil {
		t.Fatal(err)
	}
	u.tulis("getent-passwd.txt", "lds-tes:x:12345:1000:linux-dashboard samba share Data:/nonexistent:/usr/sbin/nologin\n")

	_, err := sambaSave(helperproto.SambaShare{Name: "Data", Path: "/home/%U/DATA/Documents", Writable: true})
	if err == nil || !strings.Contains(err.Error(), "%U") {
		t.Fatalf("transisi managed ke %%U harus ditolak jelas, got: %v", err)
	}
	u.tidakDipanggil("setfacl")
}
