package helper

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Share yang dikelola panel ditulis ke file include terpisah, bukan langsung
// ke smb.conf — supaya konfigurasi manual milik admin tidak pernah tertimpa.
const (
	sambaIncludePath = "/etc/samba/lindash-shares.conf"
	sambaMainConf    = "/etc/samba/smb.conf"
	sambaIncludeLine = "include = " + sambaIncludePath
)

var shareNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)

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
	_, err = fmt.Fprintf(f, "\n# ditambahkan oleh linux-dashboard\n%s\n", sambaIncludeLine)
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

func sambaSave(share helperproto.SambaShare) error {
	if !shareNameRe.MatchString(share.Name) {
		return errInvalid("nama share tidak valid")
	}
	if share.Path == "" || !strings.HasPrefix(share.Path, "/") {
		return errInvalid("path share harus absolut")
	}
	if err := cekPathShare(share.Path); err != nil {
		return err
	}
	// Nilai yang ditulis apa adanya ke berkas include tidak boleh mengandung
	// baris baru: satu "\n" di dalamnya menyisipkan baris konfigurasi Samba
	// sendiri ke dalam section share — mis. `force user = root`, yang memberi
	// semua klien SMB akses sebagai root. Komentar sudah lama dibersihkan saat
	// ditulis; path dan valid users ikut diperiksa di sini karena path bermakro
	// (`%U`) tidak lagi tertahan os.Stat.
	for _, v := range append([]string{share.Path, share.SmbUser}, share.ValidUsers...) {
		if strings.ContainsAny(v, "\r\n") {
			return errInvalid("nilai share tidak boleh mengandung baris baru")
		}
	}
	// "guest ok" mematikan autentikasi untuk share ini, jadi daftar valid users
	// tidak pernah dipakai smbd. Dulu daftar itu dibuang diam-diam saat menulis
	// smb.conf — user yang sudah dipilih hilang tanpa pesan apa pun.
	if share.Public && (len(share.ValidUsers) > 0 || share.SmbUser != "") {
		return errKode(helperproto.ErrGuestOKKonflik, "share Guest OK tidak bisa dibatasi ke user tertentu — matikan Guest OK dulu")
	}
	// Share dengan nama yang sudah dipakai definisi di luar panel tidak boleh
	// ditulis ke file include: smbd akan melihat dua section bernama sama dan
	// memakai yang pertama, jadi perubahan dari panel diam-diam tidak berefek.
	for _, ext := range sambaShareSistem() {
		if ext.Name == share.Name && !punyaPanel(share.Name) {
			return errInvalid("share %q sudah didefinisikan di smb.conf di luar panel — "+
				"hapus definisi itu dulu kalau ingin dikelola dari sini", share.Name)
		}
	}
	if err := ensureSambaInclude(); err != nil {
		return err
	}
	// Kredensial Samba terpisah dari akun Linux (konvensi Samba) — user harus
	// sudah ada sebagai user Unix, lalu diberi password Samba sendiri.
	if share.SmbUser != "" && share.SmbPass != "" {
		if err := setSambaPassword(share.SmbUser, share.SmbPass); err != nil {
			return err
		}
		if !contains(share.ValidUsers, share.SmbUser) {
			share.ValidUsers = append(share.ValidUsers, share.SmbUser)
		}
	}

	existing, err := sambaList()
	if err != nil {
		return err
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
	return writeSambaShares(existing)
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

func sambaDelete(name string) error {
	if !shareNameRe.MatchString(name) {
		return errInvalid("nama share tidak valid")
	}
	existing, err := sambaList()
	if err != nil {
		return err
	}
	out := existing[:0]
	for _, s := range existing {
		if s.Name != name {
			out = append(out, s)
		}
	}
	return writeSambaShares(out)
}

func writeSambaShares(shares []helperproto.SambaShare) error {
	var b bytes.Buffer
	b.WriteString("# File ini dikelola oleh linux-dashboard. Perubahan manual akan tertimpa.\n")
	for _, s := range shares {
		fmt.Fprintf(&b, "\n[%s]\n", s.Name)
		fmt.Fprintf(&b, "   path = %s\n", s.Path)
		fmt.Fprintf(&b, "   browseable = yes\n")
		fmt.Fprintf(&b, "   writable = %s\n", yesNo(s.Writable))
		fmt.Fprintf(&b, "   guest ok = %s\n", yesNo(s.Public))
		if s.Comment != "" {
			fmt.Fprintf(&b, "   comment = %s\n", strings.ReplaceAll(s.Comment, "\n", " "))
		}
		if len(s.ValidUsers) > 0 && !s.Public {
			fmt.Fprintf(&b, "   valid users = %s\n", strings.Join(s.ValidUsers, " "))
		}
	}
	if err := os.WriteFile(sambaIncludePath, b.Bytes(), 0o644); err != nil {
		return err
	}
	// testparm menolak config rusak sebelum smbd dijalankan ulang.
	if _, err := run("testparm", "-s"); err != nil {
		return errInvalid("konfigurasi Samba ditolak: %v", err)
	}
	// restart, bukan reload: smbd memberi tiap klien proses anak sendiri yang
	// memegang salinan konfigurasi dari saat klien menyambung. reload hanya
	// dibaca proses induk, jadi klien yang sedang terhubung tetap memakai
	// konfigurasi lama sampai ia menyambung ulang — dan Windows menahan sesi
	// SMB-nya berjam-jam. Efeknya `valid users` dan password yang baru ditulis
	// tidak berlaku untuk klien itu, sementara panel melapor sukses: share
	// terlihat sudah dibatasi padahal sesi lama masih jalan sebagai guest.
	// Harganya, transfer yang sedang berjalan ikut terputus saat share diubah.
	_, err := run("systemctl", "restart", "smbd")
	return err
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
	cmd := exec.Command("smbpasswd", "-a", "-s", username)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errInvalid("set password Samba gagal: %s", strings.TrimSpace(stderr.String()))
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
		out = append(out, helperproto.SambaUser{Username: name, Enabled: sambaUserEnabled(name)})
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

func sambaUserSet(args helperproto.SambaUserArgs) error {
	if !usernameRe.MatchString(args.Username) {
		return errInvalid("username Samba tidak valid")
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
	if _, err := run("smbpasswd", "-x", username); err != nil {
		return err
	}
	// Share yang memakai user ini jadi menunjuk akun yang tidak ada lagi.
	shares, err := sambaList()
	if err != nil {
		return nil
	}
	ubah := false
	for i := range shares {
		sisa := shares[i].ValidUsers[:0]
		for _, u := range shares[i].ValidUsers {
			if u != username {
				sisa = append(sisa, u)
			} else {
				ubah = true
			}
		}
		shares[i].ValidUsers = sisa
	}
	if !ubah {
		return nil
	}
	return writeSambaShares(shares)
}

// ---- prasyarat audit autentikasi (dipakai jail fail2ban) ----

const (
	sambaTandaGlobal = "# ---- linux-dashboard: audit autentikasi (untuk fail2ban) ----"
	sambaBackupConf  = sambaMainConf + ".lindash.bak"

	sambaBarisMapToGuest = "   map to guest = Bad User"
	// Persis seperti yang ditulis versi panel terdahulu — dicocokkan apa
	// adanya supaya baris milik admin yang kebetulan bernilai sama tidak
	// ikut tersentuh.
	sambaBarisMapToGuestLama = "   map to guest = Never"
)

// perbaikiMapToGuestLama mengganti satu baris `map to guest = Never` yang
// pernah ditulis panel di dalam bloknya sendiri. Dijalankan tiap kali blok
// sudah ada, jadi server yang terlanjur dipatch versi lama ikut sembuh tanpa
// perlu admin menghapus bloknya dengan tangan.
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
//   - map to guest = Bad User. Nilai ini menentukan apa yang terjadi pada
//     username yang tidak dikenal: "Bad User" memetakannya ke akun guest,
//     "Never" menolaknya.
//
//     Versi pertama blok ini memakai Never, dengan alasan yang benar tapi
//     akibat yang tidak diperiksa: percobaan login memang jadi tercatat
//     sebagai NT_STATUS_LOGON_FAILURE, TAPI share "guest ok = yes" berhenti
//     bekerja sama sekali. Klien yang menyambung tanpa kredensial datang
//     sebagai sesi anonim, dan Never menolaknya — Windows lalu menampilkan
//     kotak minta username/password untuk share yang justru dibuat supaya
//     tidak perlu login. Satu setelan audit mematikan satu fitur produk.
//
//     Bad User mengembalikan guest tanpa membuat fail2ban buta: brute force
//     nyata menyasar akun yang ADA (root, admin, nama user server), dan untuk
//     username yang dikenal dengan password salah Samba tetap menolak dan
//     tetap mencetak NT_STATUS_LOGON_FAILURE. Yang lolos dari catatan hanya
//     percobaan dengan username yang tidak ada sama sekali — dan itu memang
//     tidak bisa menembus apa pun selain share yang sengaja dibuka untuk
//     umum.
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
	// Satu pengecualian: `map to guest = Never` yang ditulis versi panel
	// terdahulu. Itu bukan keputusan admin melainkan bug panel, dan selama
	// baris itu ada tidak ada satu pun share guest yang bisa dipakai. Yang
	// diperbaiki hanya baris itu, hanya kalau masih persis seperti yang
	// dulu ditulis panel sendiri.
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
