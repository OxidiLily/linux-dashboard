package helper

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Share yang dikelola panel ditulis ke file include terpisah, bukan langsung
// ke smb.conf — supaya konfigurasi manual milik admin tidak pernah tertimpa.
//
// Variabel, bukan konstanta: test menyisipkan berkas di direktori sementara
// sehingga alur save/delete bisa diuji utuh tanpa menyentuh /etc/samba mesin
// pengembang. Nilai defaultnya tetap path produksi.
var (
	sambaIncludePath = "/etc/samba/lindash-shares.conf"
	sambaMainConf    = "/etc/samba/smb.conf"
	// Ikut sebagai var karena diturunkan dari sambaMainConf — nilainya sama
	// persis dengan sebelum konversi const→var.
	sambaBackupConf = sambaMainConf + ".lindash.bak"
)

var shareNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)

// Manifest adalah bukti bahwa akun system memang dibuat panel. Karena web app
// memiliki /var/lib/linux-dashboard, menaruhnya di sana membuat proses web yang
// kompromi mampu memalsukan ownership lalu meminta helper root menghapus akun
// arbitrary. Lokasi helper ini root-owned dan tidak writable oleh web app.
var sambaManifestPath = "/var/lib/linux-dashboard-helper/samba-shares.json"

var sambaMu sync.Mutex

type sambaManagedShare struct {
	Username  string   `json:"username"`
	UID       int      `json:"uid"`
	Path      string   `json:"path"`
	GECOS     string   `json:"gecos"`
	Ancestors []string `json:"ancestors,omitempty"`
}

type sambaManifest struct {
	Shares map[string]sambaManagedShare `json:"shares"`
}

func bacaManifestSamba() (sambaManifest, error) {
	m := sambaManifest{Shares: map[string]sambaManagedShare{}}
	b, err := os.ReadFile(sambaManifestPath)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, errInvalid("manifest akun Samba rusak: %v", err)
	}
	if m.Shares == nil {
		m.Shares = map[string]sambaManagedShare{}
	}
	return m, nil
}

// tulisManifestSamba memakai rename pada filesystem yang sama: pembaca tidak
// pernah melihat JSON setengah tulis. Manifest berisi identitas, bukan password.
func tulisManifestSamba(m sambaManifest) error {
	if err := os.MkdirAll(filepath.Dir(sambaManifestPath), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(sambaManifestPath), ".samba-manifest-*")
	if err != nil {
		return err
	}
	nama := tmp.Name()
	defer os.Remove(nama)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if tutup := tmp.Close(); err == nil {
		err = tutup
	}
	if err != nil {
		return err
	}
	return os.Rename(nama, sambaManifestPath)
}

func namaAkunShare(name, path string, collision int) string {
	h := sha256.Sum256([]byte(name + "\x00" + path + "\x00" + strconv.Itoa(collision)))
	return "lds-" + hex.EncodeToString(h[:])[:16]
}

func passwordShare() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func perluAkunOtomatis(s helperproto.SambaShare, m sambaManifest) bool {
	if strings.Contains(s.Path, "%U") {
		return false
	}
	_, ada := m.Shares[s.Name]
	return !ada
}

func akunSudahAda(name string) bool { _, err := run("getent", "passwd", name); return err == nil }

func buatAkunShare(s helperproto.SambaShare, m sambaManifest) (sambaManagedShare, string, error) {
	var username string
	for i := 0; i < 100; i++ {
		username = namaAkunShare(s.Name, s.Path, i)
		if !akunSudahAda(username) {
			break
		}
		username = ""
	}
	if username == "" {
		return sambaManagedShare{}, "", errInvalid("tidak dapat memilih username Samba yang unik")
	}
	// Kolon adalah pemisah field /etc/passwd: shadow-utils menolaknya di
	// --comment ("invalid comment", rc=3), sehingga akun tak pernah terbentuk
	// di sistem nyata. Spasi diterima dan tetap terbaca utuh oleh getent.
	gecos := "linux-dashboard samba share " + s.Name
	if _, err := run("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--comment", gecos, username); err != nil {
		return sambaManagedShare{}, "", err
	}
	res, err := run("id", "-u", username)
	if err != nil {
		_, _ = run("userdel", username)
		return sambaManagedShare{}, "", err
	}
	uid, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil {
		_, _ = run("userdel", username)
		return sambaManagedShare{}, "", errInvalid("UID akun Samba tidak valid")
	}
	pass, err := passwordShare()
	if err != nil {
		_, _ = run("userdel", username)
		return sambaManagedShare{}, "", err
	}
	if err := setSambaPassword(username, pass); err != nil {
		_, _ = run("userdel", username)
		return sambaManagedShare{}, "", err
	}
	entry := sambaManagedShare{Username: username, UID: uid, Path: s.Path, GECOS: gecos, Ancestors: ancestorShare(s.Path)}
	return entry, pass, nil
}

func ancestorShare(path string) []string {
	clean := filepath.Clean(path)
	var out []string
	for p := filepath.Dir(clean); p != "/" && p != "."; p = filepath.Dir(p) {
		out = append(out, p)
	}
	return out
}

func pasangACLShare(s helperproto.SambaShare, username string) error {
	akses := "r-X"
	if s.Writable {
		akses = "rwX"
	}
	entryRollback := sambaManagedShare{Username: username, Path: s.Path, Ancestors: ancestorShare(s.Path)}
	gagal := func(err error, pesan string) error {
		if rbErr := lepasACLShare(entryRollback); rbErr != nil {
			return errKode(helperproto.ErrBelumTerpasang, pesan+": %v; membersihkan ACL parsial juga gagal: %v", err, rbErr)
		}
		return errKode(helperproto.ErrBelumTerpasang, pesan+": %v", err)
	}
	// Samba harus dapat menelusuri setiap ancestor menuju share tanpa mengubah
	// owner/mode. Hanya execute diberikan; entri persis ini dicatat di manifest
	// lalu dicabut ketika share dihapus.
	for _, p := range entryRollback.Ancestors {
		if _, err := run("setfacl", "-m", "u:"+username+":--x", p); err != nil {
			return gagal(err, "ACL ancestor gagal diterapkan")
		}
	}
	// Access ACL berlaku pada isi yang sudah ada tanpa mengikuti symlink atau
	// menyeberang filesystem/mount bersarang. setfacl -R tidak punya batas
	// filesystem, jadi gunakan find -xdev sebagai traversal tunggal.
	if _, err := run("find", s.Path, "-xdev",
		"(", "-type", "d", "!", "-samefile", s.Path, "-execdir", "mountpoint", "-q", "--", "{}", ";", "-prune", ")", "-o",
		"(", "-type", "f", "-o", "-type", "d", ")", "-execdir", "setfacl", "-P", "-m", "u:"+username+":"+akses, "{}", "+"); err != nil {
		return gagal(err, "ACL gagal; pastikan paket acl dan util-linux terpasang")
	}
	// Default ACL dipasang di setiap direktori yang sudah ada agar berkas baru
	// di subfolder ikut mewarisi akses. -execdir mengurangi race komponen path;
	// -P menjadi pertahanan tambahan agar setfacl tidak dereference symlink.
	// mountpoint+prune menolak root filesystem bersarang (termasuk bind mount
	// dengan device sama); -xdev saja hanya mencegah descent tetapi tetap
	// menyerahkan direktori mountpoint itu sendiri kepada setfacl.
	if _, err := run("find", s.Path, "-xdev",
		"(", "-type", "d", "!", "-samefile", s.Path, "-execdir", "mountpoint", "-q", "--", "{}", ";", "-prune", ")", "-o",
		"-type", "d", "-execdir", "setfacl", "-P", "-m", "d:u:"+username+":"+akses, "{}", "+"); err != nil {
		return gagal(err, "default ACL gagal diterapkan")
	}
	return nil
}

// lepasACLShare mencabut seluruh ACL yang dipasang pasangACLShare. Setiap
// kegagalan DIKEMBALIKAN, tidak ditelan: setfacl yang diam-diam gagal
// meninggalkan ACE menempel di folder, dan ketika share dihapus akunnya ikut
// dihapus — ACE yatim lalu diwarisi user lain yang memperoleh UID sama.
//
// Subjek ditulis sebagai UID numerik ketika diketahui. Bukti empiris di sesi
// ini: -x lewat nama keluar status 2 begitu nama tidak lagi ada di passwd
// (akun sudah terhapus duluan), sedangkan -x by-UID selalu bekerja dan
// membersihkan entri yang semula dipasang lewat nama, termasuk default ACL.
// Path yang sudah hilang dari disk dilewati: tidak ada lagi yang bisa
// dicabut di sana, dan setfacl pada path hilang selalu gagal (exit 1).
func lepasACLShare(e sambaManagedShare) error {
	subjek := "u:" + e.Username
	if e.UID > 0 {
		subjek = "u:" + strconv.Itoa(e.UID)
	}
	var errs []string
	if _, statErr := os.Stat(e.Path); statErr == nil {
		if _, err := run("find", e.Path, "-xdev",
			"(", "-type", "d", "!", "-samefile", e.Path, "-execdir", "mountpoint", "-q", "--", "{}", ";", "-prune", ")", "-o",
			"(", "-type", "f", "-o", "-type", "d", ")", "-execdir", "setfacl", "-P", "-x", subjek, "{}", "+"); err != nil {
			errs = append(errs, fmt.Sprintf("ACL isi %s: %v", e.Path, err))
		}
		if _, err := run("find", e.Path, "-xdev",
			"(", "-type", "d", "!", "-samefile", e.Path, "-execdir", "mountpoint", "-q", "--", "{}", ";", "-prune", ")", "-o",
			"-type", "d", "-execdir", "setfacl", "-P", "-x", "d:"+subjek, "{}", "+"); err != nil {
			errs = append(errs, fmt.Sprintf("default ACL %s: %v", e.Path, err))
		}
	}
	ancestors := e.Ancestors
	if len(ancestors) == 0 {
		ancestors = ancestorShare(e.Path)
	}
	for _, p := range ancestors {
		if _, statErr := os.Stat(p); statErr != nil {
			continue
		}
		if _, err := run("setfacl", "-x", subjek, p); err != nil {
			errs = append(errs, fmt.Sprintf("ACL ancestor %s: %v", p, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("pencabutan ACL gagal: %s", strings.Join(errs, "; "))
	}
	return nil
}

// hapusAkunShareBaru membalik pembuatan akun baru: cabut ACL lebih dulu, baru
// kredensial dan akunnya. Jika pencabutan ACL gagal, akun TIDAK dihapus —
// membebaskan UID selama ACE masih menempel membuat user lain yang kelak
// memperoleh UID sama mewarisi akses folder tersebut. Error yang dikembalikan
// selalu menyebut akun yang tertinggal supaya bisa dibersihkan manual.
func hapusAkunShareBaru(e sambaManagedShare) error {
	if err := lepasACLShare(e); err != nil {
		return fmt.Errorf("akun %s (UID %d) dibiarkan agar UID tidak dibebaskan dengan ACE menempel; %w", e.Username, e.UID, err)
	}
	_, _ = run("smbpasswd", "-x", e.Username)
	if _, err := run("userdel", e.Username); err != nil {
		return fmt.Errorf("akun %s gagal dihapus: %w", e.Username, err)
	}
	return nil
}

func validasiIdentitasAkunManaged(e sambaManagedShare) error {
	res, err := run("getent", "passwd", e.Username)
	if err != nil {
		return errInvalid("akun managed %s tidak ditemukan; operasi ditolak", e.Username)
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), ":")
	if len(fields) < 7 || fields[2] != strconv.Itoa(e.UID) || fields[4] != e.GECOS || fields[6] != "/usr/sbin/nologin" {
		return errInvalid("identitas akun %s berubah; operasi otomatis ditolak", e.Username)
	}
	return nil
}

func rotateSambaShare(name string) (helperproto.SambaCredential, error) {
	sambaMu.Lock()
	defer sambaMu.Unlock()
	m, err := bacaManifestSamba()
	if err != nil {
		return helperproto.SambaCredential{}, err
	}
	e, ok := m.Shares[name]
	if !ok {
		return helperproto.SambaCredential{}, errInvalid("share bukan akun otomatis milik panel")
	}
	if err := validasiIdentitasAkunManaged(e); err != nil {
		return helperproto.SambaCredential{}, err
	}
	pass, err := passwordShare()
	if err != nil {
		return helperproto.SambaCredential{}, err
	}
	if err := setSambaPassword(e.Username, pass); err != nil {
		return helperproto.SambaCredential{}, err
	}
	return helperproto.SambaCredential{Username: e.Username, Password: pass}, nil
}

func ensureSambaInclude() error {
	b, err := os.ReadFile(sambaMainConf)
	if err != nil {
		return errInvalid("smb.conf tidak ditemukan — pasang paket samba dulu")
	}
	if strings.Contains(string(b), sambaIncludePath) {
		return nil
	}
	f, err := os.OpenFile(sambaMainConf, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# ditambahkan oleh linux-dashboard\ninclude = %s\n", sambaIncludePath)
	return err
}

func sambaList() ([]helperproto.SambaShare, error) {
	b, err := os.ReadFile(sambaIncludePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []helperproto.SambaShare{}, nil
		}
		return nil, err
	}
	var out []helperproto.SambaShare
	var cur *helperproto.SambaShare
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &helperproto.SambaShare{Name: strings.Trim(line, "[]")}
			continue
		}
		if cur == nil {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "path":
			cur.Path = val
		case "writable", "read only":
			yes := val == "yes"
			if key == "read only" {
				cur.Writable = !yes
			} else {
				cur.Writable = yes
			}
		case "guest ok":
			cur.Public = val == "yes"
		case "comment":
			cur.Comment = val
		case "valid users":
			cur.ValidUsers = strings.Fields(val)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out, nil
}

// sambaListSemua menggabungkan share tulisan panel dengan share yang sudah
// ada di smb.conf sebelumnya. Tanpa ini, share yang dibuat manual (atau oleh
// panel versi lama) tidak terlihat sama sekali di UI, dan user mengira harus
// membuatnya lagi — padahal membuat ulang dengan nama sama akan bentrok.
func sambaListSemua() ([]helperproto.SambaShare, error) {
	milikPanel, err := sambaList()
	if err != nil {
		return nil, err
	}
	punya := map[string]bool{}
	for _, s := range milikPanel {
		punya[s.Name] = true
	}
	for _, s := range sambaShareSistem() {
		if !punya[s.Name] {
			milikPanel = append(milikPanel, s)
		}
	}
	return milikPanel, nil
}

// Share bawaan smbd yang bukan folder sharing user.
var shareBawaan = map[string]bool{"global": true, "printers": true, "print$": true, "IPC$": true}

// sambaShareSistem membaca konfigurasi efektif smbd lewat `testparm -s` —
// termasuk share yang ditulis manual di smb.conf, bukan hanya file include
// milik panel.
func sambaShareSistem() []helperproto.SambaShare {
	res, err := run("testparm", "-s")
	if err != nil && res.Stdout == "" {
		return nil
	}
	var out []helperproto.SambaShare
	var cur *helperproto.SambaShare
	simpan := func() {
		if cur != nil && !shareBawaan[cur.Name] && cur.Path != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			simpan()
			cur = &helperproto.SambaShare{Name: strings.Trim(line, "[]"), External: true, Writable: false}
			continue
		}
		if cur == nil {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.ToLower(strings.TrimSpace(val))
		switch key {
		case "path":
			cur.Path = strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
		case "read only":
			cur.Writable = val == "no"
		case "writable", "write ok":
			cur.Writable = val == "yes"
		case "guest ok":
			cur.Public = val == "yes"
		case "comment":
			cur.Comment = strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
		case "valid users":
			cur.ValidUsers = strings.Fields(strings.TrimSpace(strings.SplitN(line, "=", 2)[1]))
		}
	}
	simpan()
	return out
}

func validasiSambaShare(share helperproto.SambaShare) error {
	if !shareNameRe.MatchString(share.Name) {
		return errInvalid("nama share tidak valid")
	}
	if share.Path == "" || !strings.HasPrefix(share.Path, "/") {
		return errInvalid("path share harus absolut")
	}
	// Akses anonim ke folder jaringan membuat satu perangkat LAN yang terinfeksi
	// cukup untuk mengenkripsi seluruh isi share. Fitur Guest OK sengaja ditolak
	// di boundary helper (bukan hanya disembunyikan UI), termasuk untuk klien API
	// lama atau request yang dibuat langsung.
	if share.Public {
		return errInvalid("Guest OK dinonaktifkan demi keamanan — buat user Samba dan gunakan autentikasi")
	}
	// Nilai yang ditulis apa adanya ke berkas include tidak boleh mengandung
	// baris baru: satu "\n" di dalamnya menyisipkan directive Samba sendiri.
	for _, v := range append([]string{share.Path, share.Comment, share.SmbUser}, share.ValidUsers...) {
		if strings.ContainsAny(v, "\r\n") {
			return errInvalid("nilai share tidak boleh mengandung baris baru")
		}
	}
	return nil
}

func sambaSave(share helperproto.SambaShare) (helperproto.SambaCredential, error) {
	sambaMu.Lock()
	defer sambaMu.Unlock()
	var credential helperproto.SambaCredential
	if err := validasiSambaShare(share); err != nil {
		return credential, err
	}
	if err := cekPathShare(share.Path); err != nil {
		return credential, err
	}
	for _, ext := range sambaShareSistem() {
		if ext.Name == share.Name && !punyaPanel(share.Name) {
			return credential, errInvalid("share %q sudah didefinisikan di smb.conf di luar panel — hapus definisi itu dulu kalau ingin dikelola dari sini", share.Name)
		}
	}
	if err := ensureSambaInclude(); err != nil {
		return credential, err
	}
	// Config dibaca SEBELUM akun atau ACL disentuh. Posisi lama (setelah
	// buatAkunShare+pasangACLShare) membuat kegagalan membaca config
	// meninggalkan akun system tanpa entri manifest: tak terlihat di panel,
	// tak bisa dirotasi, tak pernah dibersihkan.
	existing, err := sambaList()
	if err != nil {
		return credential, err
	}

	m, err := bacaManifestSamba()
	if err != nil {
		return credential, err
	}
	entry, managed := m.Shares[share.Name]
	entryLama := entry
	pathBerubah := managed && entry.Path != share.Path
	baru := false
	if managed {
		// Manifest hanyalah bukti historis. Pastikan akun live masih identitas
		// system no-login yang sama sebelum memberinya ACL atau valid_users baru.
		if err := validasiIdentitasAkunManaged(entry); err != nil {
			return credential, err
		}
		// %U adalah mode legacy/manual: satu path dinamis per user yang login,
		// bukan resource konkret milik akun service per-share. Mengubah share
		// managed menjadi %U lewat update akan membuat ACL literal /home/%U gagal
		// sekaligus mengaburkan kapan akun managed lama boleh dibersihkan.
		if strings.Contains(share.Path, "%U") {
			return credential, errInvalid("share dengan akun managed tidak dapat diubah ke path %%U; hapus share lalu buat ulang sebagai share legacy/manual")
		}
	}
	if perluAkunOtomatis(share, m) {
		entry, credential.Password, err = buatAkunShare(share, m)
		if err != nil {
			return helperproto.SambaCredential{}, err
		}
		credential.Username, baru = entry.Username, true
		m.Shares[share.Name], managed = entry, true
	}
	if managed {
		share.ValidUsers = []string{entry.Username}
		share.SmbUser, share.SmbPass = "", ""
		if pathBerubah {
			entry.Path = share.Path
			entry.Ancestors = ancestorShare(share.Path)
			m.Shares[share.Name] = entry
		}
		// ACL path baru dipasang dahulu. ACL path lama baru dicabut setelah config
		// baru berhasil aktif, supaya kegagalan update tidak memutus share lama.
		if err := pasangACLShare(share, entry.Username); err != nil {
			if baru {
				if cbErr := hapusAkunShareBaru(entry); cbErr != nil {
					return helperproto.SambaCredential{}, fmt.Errorf("%v; %w", err, cbErr)
				}
			}
			return helperproto.SambaCredential{}, err
		}
	} else if share.SmbUser != "" && share.SmbPass != "" {
		if err := setSambaPassword(share.SmbUser, share.SmbPass); err != nil {
			return credential, err
		}
		if !contains(share.ValidUsers, share.SmbUser) {
			share.ValidUsers = append(share.ValidUsers, share.SmbUser)
		}
	}

	// Simpan snapshot sebelum slice dimutasi agar kegagalan persistence manifest
	// dapat memulihkan konfigurasi yang benar-benar aktif sebelumnya.
	existingLama := append([]helperproto.SambaShare(nil), existing...)
	var shareLama helperproto.SambaShare
	for _, s := range existingLama {
		if s.Name == share.Name {
			shareLama = s
			break
		}
	}
	// Rollback update path harus mencabut ACL path baru lalu memasang ulang ACL
	// path lama. Kedua path dapat overlap/berbagi ancestor; hanya mencabut path
	// baru dapat ikut menghapus ACE lama yang masih diperlukan config restore.
	rollbackACLPath := func() error {
		cabutErr := lepasACLShare(entry)
		var pasangErr error
		if pathBerubah {
			pasangErr = pasangACLShare(shareLama, entryLama.Username)
		}
		switch {
		case cabutErr != nil && pasangErr != nil:
			return fmt.Errorf("cabut ACL path baru gagal: %v; pulihkan ACL path lama juga gagal: %w", cabutErr, pasangErr)
		case cabutErr != nil:
			return cabutErr
		case pasangErr != nil:
			return fmt.Errorf("pulihkan ACL path lama gagal: %w", pasangErr)
		}
		return nil
	}
	replaced := false
	for i := range existing {
		if existing[i].Name == share.Name {
			existing[i] = share
			replaced = true
		}
	}
	if !replaced {
		existing = append(existing, share)
	}
	if err := writeSambaShares(existing); err != nil {
		if baru {
			if cbErr := hapusAkunShareBaru(entry); cbErr != nil {
				return helperproto.SambaCredential{}, fmt.Errorf("%v; %w", err, cbErr)
			}
		} else if pathBerubah {
			// Config lama dipulihkan writeSambaShares; cabut hanya ACL path baru.
			if cbErr := rollbackACLPath(); cbErr != nil {
				return helperproto.SambaCredential{}, fmt.Errorf("%v; %w", err, cbErr)
			}
		}
		return helperproto.SambaCredential{}, err
	}
	if managed {
		if err := tulisManifestSamba(m); err != nil {
			// Config sudah aktif, tetapi tanpa manifest helper tidak boleh menganggap
			// akun/ACL sebagai miliknya. Pulihkan config lama dan bersihkan hanya
			// resource baru; entry lama tetap utuh untuk retry.
			rollbackErr := writeSambaShares(existingLama)
			var cleanupErr error
			if baru {
				cleanupErr = hapusAkunShareBaru(entry)
			} else if pathBerubah {
				cleanupErr = rollbackACLPath()
			}
			switch {
			case rollbackErr != nil && cleanupErr != nil:
				return helperproto.SambaCredential{}, fmt.Errorf("tulis manifest gagal: %v; rollback config gagal: %v; pembersihan resource baru juga gagal: %w", err, rollbackErr, cleanupErr)
			case rollbackErr != nil:
				return helperproto.SambaCredential{}, fmt.Errorf("tulis manifest gagal: %v; rollback config juga gagal: %w", err, rollbackErr)
			case cleanupErr != nil:
				return helperproto.SambaCredential{}, fmt.Errorf("tulis manifest gagal: %v; rollback config sukses tetapi pembersihan resource baru gagal: %w", err, cleanupErr)
			}
			return helperproto.SambaCredential{}, err
		}
		if pathBerubah {
			if aclErr := lepasACLShare(entryLama); aclErr != nil {
				// Simpan sudah berhasil dan akunnya tetap (hanya path berubah),
				// jadi residu ACE bukan risiko pewarisan UID — tetap dilaporkan.
				return credential, fmt.Errorf("share tersimpan, tetapi ACL lama di %s gagal dicabut: %w", entryLama.Path, aclErr)
			}
			// Path lama dan baru dapat overlap serta berbagi ancestor. Revoke
			// rekursif path lama dapat ikut menghapus ACE path baru; revoke ancestor
			// lama juga dapat mencabut traversal yang masih diperlukan. Pasang ulang
			// ACL aktif setelah revoke agar state akhir selalu mengikuti path baru.
			if aclErr := pasangACLShare(share, entry.Username); aclErr != nil {
				return credential, fmt.Errorf("share tersimpan dan ACL lama dicabut, tetapi ACL path baru gagal dipulihkan: %w", aclErr)
			}
		}
	}
	return credential, nil
}

// cekPathShare memeriksa folder share. Samba mengganti makro `%U` dengan nama
// user yang menyambung, jadi satu share bisa menunjuk folder berbeda per user —
// itulah cara menjadikan `/home/%U/DATA/Documents` (folder data milik akun
// masing-masing) sebagai share. Path bermakro tidak bisa di-stat apa adanya:
// yang diperiksa adalah bagian literal sebelum makro pertama, mis. `/home`.
//
// Hanya `%U` yang diterima. Makro Samba lain (`%m` nama mesin klien, `%I` IP)
// membuat path bergantung pada data yang dikirim klien, dan itu bukan sesuatu
// yang layak dipakai memilih folder di disk.
func cekPathShare(path string) error {
	if i := strings.IndexByte(path, '%'); i >= 0 {
		if strings.Count(path, "%") != strings.Count(path, "%U") {
			return errInvalid("hanya makro %%U (nama user yang menyambung) yang boleh dipakai di path share")
		}
		induk := filepath.Clean(path[:i])
		if st, err := os.Stat(induk); err != nil || !st.IsDir() {
			return errKode(helperproto.ErrFolderTidakAda, "folder %s tidak ada", induk)
		}
		return nil
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return errKode(helperproto.ErrFolderTidakAda, "folder %s tidak ada", path)
	}
	return nil
}

// punyaPanel: apakah share ini ditulis di file include milik panel.
func punyaPanel(name string) bool {
	list, err := sambaList()
	if err != nil {
		return false
	}
	for _, s := range list {
		if s.Name == name {
			return true
		}
	}
	return false
}

// amankanShareGuestLama menutup Guest OK pada share yang pernah dibuat panel
// versi lama. Definisi manual di smb.conf tidak disentuh karena bukan milik
// panel. Share tetap tersedia bagi user Samba terautentikasi.
func amankanShareGuestLama() error {
	shares, err := sambaList()
	if err != nil {
		return err
	}
	diubah := false
	for i := range shares {
		if shares[i].Public {
			shares[i].Public = false
			diubah = true
		}
	}
	if !diubah {
		return nil
	}
	return writeSambaShares(shares)
}

func sambaDelete(name string) error {
	sambaMu.Lock()
	defer sambaMu.Unlock()
	if !shareNameRe.MatchString(name) {
		return errInvalid("nama share tidak valid")
	}
	existing, err := sambaList()
	if err != nil {
		return err
	}
	m, err := bacaManifestSamba()
	if err != nil {
		return err
	}
	e, managed := m.Shares[name]
	akunAda := false
	if managed {
		if _, lookupErr := run("getent", "passwd", e.Username); lookupErr == nil {
			akunAda = true
			// Guard ownership diperiksa sebelum share dinonaktifkan. Manifest rusak
			// atau username yang dipakai ulang tidak boleh menyebabkan outage dulu.
			if err := validasiIdentitasAkunManaged(e); err != nil {
				return err
			}
		}
	}
	// Snapshot sebelum out memutasi backing array yang sama — dipakai untuk
	// memulihkan config bila pencabutan ACL gagal.
	existingLama := append([]helperproto.SambaShare(nil), existing...)
	out := existing[:0]
	for _, s := range existing {
		if s.Name != name {
			out = append(out, s)
		}
	}
	// Fail closed: config/restart selesai dahulu, baru ACL dan kredensial dicabut.
	if err := writeSambaShares(out); err != nil {
		return err
	}
	if !managed {
		return nil
	}
	if err := lepasACLShare(e); err != nil {
		// ACE masih menempel: akun TIDAK boleh dihapus — membebaskan UID selama
		// ACE menempel membuat user baru yang memperoleh UID sama mewarisi
		// akses. Config dipulihkan supaya share tetap utuh dan operasi bisa
		// diulang setelah masalah ACL-nya beres.
		if restoreErr := writeSambaShares(existingLama); restoreErr != nil {
			return fmt.Errorf("pencabutan ACL gagal: %v; memulihkan config juga gagal: %w", err, restoreErr)
		}
		return fmt.Errorf("pencabutan ACL gagal; share tidak dihapus dan config dipulihkan: %w", err)
	}
	if akunAda {
		_, _ = run("smbpasswd", "-x", e.Username)
		if _, err := run("userdel", e.Username); err != nil {
			return err
		}
	}
	delete(m.Shares, name)
	return tulisManifestSamba(m)
}

func renderSambaShares(shares []helperproto.SambaShare) []byte {
	var b bytes.Buffer
	b.WriteString("# File ini dikelola oleh linux-dashboard. Perubahan manual akan tertimpa.\n")
	for _, s := range shares {
		fmt.Fprintf(&b, "\n[%s]\n", s.Name)
		fmt.Fprintf(&b, "   path = %s\n", s.Path)
		fmt.Fprintf(&b, "   browseable = yes\n")
		fmt.Fprintf(&b, "   writable = %s\n", yesNo(s.Writable))
		// Semua share yang dikelola panel wajib terautentikasi. Nilai lama yang
		// mungkin masih tersimpan juga ditulis ulang fail-closed saat konfigurasi
		// berikutnya disimpan.
		fmt.Fprintf(&b, "   guest ok = no\n")
		if s.Comment != "" {
			fmt.Fprintf(&b, "   comment = %s\n", strings.ReplaceAll(s.Comment, "\n", " "))
		}
		if len(s.ValidUsers) > 0 && !s.Public {
			fmt.Fprintf(&b, "   valid users = %s\n", strings.Join(s.ValidUsers, " "))
		}
	}
	return b.Bytes()
}

func writeSambaShares(shares []helperproto.SambaShare) error {
	baru := renderSambaShares(shares)
	lama, readErr := os.ReadFile(sambaIncludePath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	dir := filepath.Dir(sambaIncludePath)
	tmp, err := os.CreateTemp(dir, ".lindash-shares-*")
	if err != nil {
		return err
	}
	nama := tmp.Name()
	defer os.Remove(nama)
	if err = tmp.Chmod(0o644); err == nil {
		_, err = tmp.Write(baru)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if tutup := tmp.Close(); err == nil {
		err = tutup
	}
	if err == nil {
		err = os.Rename(nama, sambaIncludePath)
	}
	if err != nil {
		return err
	}
	rollback := func() {
		if readErr == nil {
			_ = os.WriteFile(sambaIncludePath, lama, 0o644)
		} else {
			_ = os.Remove(sambaIncludePath)
		}
	}
	if _, err := run("testparm", "-s"); err != nil {
		rollback()
		return errInvalid("konfigurasi Samba ditolak: %v", err)
	}
	if _, err := run("systemctl", "restart", "smbd"); err != nil {
		rollback()
		_, _ = run("systemctl", "restart", "smbd")
		return err
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// setSambaPassword memberi password lewat stdin — tidak pernah sebagai
// argumen command, supaya tidak bocor ke /proc/<pid>/cmdline.
func setSambaPassword(username, password string) error {
	if !usernameRe.MatchString(username) {
		return errInvalid("username Samba tidak valid")
	}
	if _, err := runStdin(password+"\n"+password+"\n", "smbpasswd", "-a", "-s", username); err != nil {
		return errInvalid("set password Samba gagal: %v", err)
	}
	_, err := run("smbpasswd", "-e", username)
	return err
}

// ---- user Samba (database smbpasswd) ----

// Samba menyimpan password sendiri di /var/lib/samba/private/passdb.tdb, jadi
// akun Linux yang baru dibuat TIDAK otomatis bisa login ke share — ia harus
// didaftarkan di sini dulu. `pdbedit -L` mencetak "user:uid:comment" per baris.
func sambaUserList() ([]helperproto.SambaUser, error) {
	res, err := run("pdbedit", "-L")
	if err != nil {
		return []helperproto.SambaUser{}, nil // samba belum terpasang / db kosong
	}
	managed := map[string]bool{}
	if m, manifestErr := bacaManifestSamba(); manifestErr == nil {
		for _, e := range m.Shares {
			managed[e.Username] = true
		}
	}
	var out []helperproto.SambaUser
	for _, line := range strings.Split(res.Stdout, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || name == "" {
			continue
		}
		// Akun root disembunyikan dari daftar — smbpasswd -x root selalu
		// gagal ("Failed to delete entry for user root"), dan menampilkan
		// tombol Hapus yang pasti gagal hanya membingungkan.
		if name == "root" {
			continue
		}
		out = append(out, helperproto.SambaUser{Username: name, Enabled: sambaUserEnabled(name), Managed: managed[name]})
	}
	return out, nil
}

// Akun yang di-disable punya flag "D" di kolom Account Flags dari `pdbedit -v`.
func sambaUserEnabled(name string) bool {
	res, err := run("pdbedit", "-L", "-v", "-u", name)
	if err != nil {
		return true
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "Account Flags:") {
			continue
		}
		_, flags, _ := strings.Cut(line, ":")
		return !strings.Contains(flags, "D")
	}
	return true
}

func akunManagedSamba(username string) (bool, error) {
	m, err := bacaManifestSamba()
	if err != nil {
		return false, err
	}
	for _, e := range m.Shares {
		if e.Username == username {
			return true, nil
		}
	}
	return false, nil
}

func sambaUserSet(args helperproto.SambaUserArgs) error {
	sambaMu.Lock()
	defer sambaMu.Unlock()
	if !usernameRe.MatchString(args.Username) {
		return errInvalid("username Samba tidak valid")
	}
	managed, err := akunManagedSamba(args.Username)
	if err != nil {
		return err
	}
	if managed {
		return errInvalid("akun Samba %q dikelola otomatis oleh share; gunakan rotasi password pada share", args.Username)
	}
	// smbpasswd hanya menerima user yang sudah ada di Unix — kalau tidak, entri
	// dibuat tapi share tetap menolak login karena tidak ada UID yang cocok.
	if _, err := user.Lookup(args.Username); err != nil {
		return errKode(helperproto.ErrBelumTerpasang, "user Linux %q belum ada — buat dulu di Settings → Akun", args.Username)
	}
	if args.Password != "" {
		if err := setSambaPassword(args.Username, args.Password); err != nil {
			return err
		}
	}
	if args.Disable {
		_, err := run("smbpasswd", "-d", args.Username)
		return err
	}
	if args.Password == "" {
		// Tanpa password baru, satu-satunya perubahan yang mungkin adalah enable.
		_, err := run("smbpasswd", "-e", args.Username)
		return err
	}
	return nil
}

func sambaUserDelete(username string) error {
	sambaMu.Lock()
	defer sambaMu.Unlock()
	if !usernameRe.MatchString(username) {
		return errInvalid("username Samba tidak valid")
	}
	// smbpasswd -x untuk user bernama "root" selalu gagal dengan
	// "Failed to delete entry for user root" — Samba melindungi akun
	// sistem yang dipakai proses internal. Pesan yang jelas lebih berguna
	// daripada stack trace yang sama setiap kali user mencoba.
	if username == "root" {
		return errInvalid("user Samba \"root\" tidak bisa dihapus — itu akun sistem yang dipakai proses internal Samba")
	}
	managed, err := akunManagedSamba(username)
	if err != nil {
		return err
	}
	if managed {
		return errInvalid("akun Samba %q dikelola otomatis oleh share; hapus share untuk membersihkan akun", username)
	}
	// Jangan pernah membuang kredensial lebih dahulu lalu mencoba memperbaiki
	// config. Jika user masih direferensikan, operator harus memindahkan share
	// secara eksplisit; ini menjaga valid_users tetap fail-closed.
	shares, err := sambaList()
	if err != nil {
		return err
	}
	for _, share := range shares {
		if contains(share.ValidUsers, username) {
			return errInvalid("user Samba %q masih dipakai share %q; ubah share terlebih dahulu", username, share.Name)
		}
	}
	_, err = run("smbpasswd", "-x", username)
	return err
}

// ---- prasyarat audit autentikasi (dipakai jail fail2ban) ----

const (
	sambaTandaGlobal = "# ---- linux-dashboard: audit autentikasi (untuk fail2ban) ----"

	sambaBarisMapToGuest = "   map to guest = Never"
	// Persis seperti yang ditulis versi panel terdahulu — dicocokkan apa
	// adanya supaya baris milik admin yang kebetulan bernilai sama tidak
	// ikut tersentuh.
	sambaBarisMapToGuestLama = "   map to guest = Bad User"
)

// perbaikiMapToGuestLama mengganti `map to guest = Bad User` yang pernah
// ditulis panel di dalam bloknya sendiri. Dijalankan tiap kali blok sudah ada,
// jadi server lama ikut menolak pemetaan username tak dikenal ke guest tanpa
// perlu admin mengedit smb.conf dengan tangan.
func perbaikiMapToGuestLama(isi string, asli []byte) error {
	baris := strings.Split(isi, "\n")
	tandaKetemu := false
	diubah := false
	for i, l := range baris {
		if strings.Contains(l, sambaTandaGlobal) {
			tandaKetemu = true
			continue
		}
		if !tandaKetemu {
			continue
		}
		// Berhenti di header section berikutnya: baris serupa di section lain
		// bukan milik panel.
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			break
		}
		if l == sambaBarisMapToGuestLama {
			baris[i] = sambaBarisMapToGuest
			diubah = true
			break
		}
	}
	if !diubah {
		return nil
	}
	if err := os.WriteFile(sambaMainConf, []byte(strings.Join(baris, "\n")), 0o644); err != nil {
		return err
	}
	if _, err := run("testparm", "-s"); err != nil {
		_ = os.WriteFile(sambaMainConf, asli, 0o644)
		return errInvalid("konfigurasi Samba ditolak setelah perbaikan map to guest: %v", err)
	}
	_, err := run("systemctl", "restart", "smbd")
	return err
}

// pastikanGlobalAuditSamba menyiapkan [global] supaya kegagalan login Samba
// benar-benar tercatat dan bisa dibaca fail2ban. Dua setelan dibutuhkan:
//
//   - map to guest = Never. Username yang tidak dikenal harus ditolak, bukan
//     diam-diam dipetakan ke akun guest. Share anonim tidak disediakan panel:
//     satu klien LAN yang terinfeksi ransomware tidak boleh memperoleh akses
//     tulis hanya karena mengetahui alamat server.
//
//   - log level = 0 auth_audit:3. Level umum tetap 0 supaya log tidak
//     membengkak; hanya kelas auth_audit yang dinaikkan, dan itulah yang
//     mencetak baris "Auth: ... status [NT_STATUS_...] ... remote host [...]".
//
// Blok disisipkan di AKHIR section [global], bukan di awalnya: Samba memakai
// nilai TERAKHIR dalam satu section, jadi menaruhnya di akhir membuat setelan
// panel menang tanpa perlu mengedit atau menghapus satu pun baris milik admin.
// Membuang blok ini mengembalikan konfigurasi lama persis seperti semula —
// itulah alasan pendekatan "sisipkan di akhir" dipilih ketimbang menimpa baris
// yang sudah ada.
func pastikanGlobalAuditSamba() error {
	b, err := os.ReadFile(sambaMainConf)
	if err != nil {
		return errInvalid("smb.conf tidak ditemukan — pasang paket samba dulu")
	}
	isi := string(b)
	// Sudah pernah disiapkan. Tidak ditulis ulang: kalau admin mengubah atau
	// membuang bloknya, itu keputusannya, bukan sesuatu yang panel pulihkan
	// diam-diam di belakangnya.
	//
	// Satu pengecualian: `map to guest = Bad User` yang ditulis versi panel
	// terdahulu. Baris milik panel itu dimigrasikan ke kebijakan fail-closed;
	// baris admin di luar blok tidak disentuh.
	if strings.Contains(isi, sambaTandaGlobal) {
		return perbaikiMapToGuestLama(isi, b)
	}
	// Cadangan dibuat sekali saja, sebelum perubahan pertama — kalau ditimpa
	// setiap kali, cadangannya justru ikut berisi perubahan panel dan tidak
	// ada gunanya sebagai jalan kembali.
	if _, err := os.Stat(sambaBackupConf); os.IsNotExist(err) {
		if err := os.WriteFile(sambaBackupConf, b, 0o644); err != nil {
			return err
		}
	}

	blok := []string{
		"",
		sambaTandaGlobal,
		"# Dibaca jail fail2ban \"samba\". Hapus blok ini untuk mengembalikan",
		"# perilaku bawaan; baris asli di atas tidak pernah diubah.",
		sambaBarisMapToGuest,
		"   log level = 0 auth_audit:3",
		"",
	}

	baris := strings.Split(isi, "\n")
	mulaiGlobal := -1
	sisip := -1
	for i, l := range baris {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "[") || !strings.HasSuffix(t, "]") {
			continue
		}
		if strings.EqualFold(t, "[global]") {
			mulaiGlobal = i
			continue
		}
		if mulaiGlobal >= 0 {
			sisip = i // header section pertama SETELAH [global]
			break
		}
	}
	if mulaiGlobal < 0 {
		return errInvalid("smb.conf tidak punya section [global]")
	}
	if sisip < 0 {
		sisip = len(baris) // [global] adalah section terakhir
	}
	baru := append([]string{}, baris[:sisip]...)
	baru = append(baru, blok...)
	baru = append(baru, baris[sisip:]...)

	if err := os.WriteFile(sambaMainConf, []byte(strings.Join(baru, "\n")), 0o644); err != nil {
		return err
	}
	if _, err := run("testparm", "-s"); err != nil {
		// Kembalikan dari cadangan: smb.conf yang ditolak testparm membuat smbd
		// gagal start sama sekali, jadi jangan tinggalkan dalam keadaan itu.
		_ = os.WriteFile(sambaMainConf, b, 0o644)
		return errInvalid("konfigurasi Samba ditolak setelah perubahan audit: %v", err)
	}
	_, err = run("systemctl", "restart", "smbd")
	return err
}

// bersihkanSambaKonfigurasi membersihkan seluruh jejak konfigurasi, share,
// audit, dan kredensial Samba yang pernah dikelola oleh panel.
func bersihkanSambaKonfigurasi() {
	_, _ = run("systemctl", "disable", "--now", "smbd.service", "nmbd.service")
	_ = os.Remove(sambaIncludePath)
	if _, err := os.Stat(sambaBackupConf); err == nil {
		if b, err := os.ReadFile(sambaBackupConf); err == nil {
			_ = os.WriteFile(sambaMainConf, b, 0o644)
		}
		_ = os.Remove(sambaBackupConf)
	} else if b, err := os.ReadFile(sambaMainConf); err == nil {
		isi := string(b)
		baris := strings.Split(isi, "\n")
		var hasil []string
		skipBlok := false
		for _, l := range baris {
			if strings.Contains(l, sambaTandaGlobal) {
				skipBlok = true
				continue
			}
			if skipBlok {
				if strings.HasPrefix(strings.TrimSpace(l), "[") {
					skipBlok = false
				} else {
					continue
				}
			}
			if strings.Contains(l, sambaIncludePath) || strings.Contains(l, "ditambahkan oleh linux-dashboard") {
				continue
			}
			hasil = append(hasil, l)
		}
		_ = os.WriteFile(sambaMainConf, []byte(strings.Join(hasil, "\n")), 0o644)
	}
	if res, err := run("pdbedit", "-L"); err == nil {
		for _, line := range strings.Split(res.Stdout, "\n") {
			name, _, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok && name != "" && name != "root" {
				_, _ = run("pdbedit", "-x", "-u", name)
			}
		}
	}
	_ = os.Remove("/var/lib/samba/private/passdb.tdb")
	_ = os.Remove("/var/lib/samba/private/secrets.tdb")
	_ = os.Remove("/var/lib/samba/passdb.tdb")
	_ = os.Remove(f2bJailSamba)
	_ = os.Remove(f2bFilterSamba)
	_, _ = run("fail2ban-client", "reload")
}

func installSamba() error {
	if err := aptInstall("samba"); err != nil {
		return err
	}
	_ = ensureSambaInclude()
	_ = pastikanGlobalAuditSamba()
	_, _ = run("systemctl", "enable", "--now", "smbd.service")
	return nil
}

func uninstallSamba() error {
	_, _ = run("systemctl", "disable", "--now", "smbd.service", "nmbd.service")
	return aptRemove("samba", "smbd", "nmbd")
}

func purgeSamba() error {
	bersihkanSambaKonfigurasi()
	return aptPurge("samba", "smbd", "nmbd", "samba-common", "samba-common-bin")
}
