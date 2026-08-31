package helper

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Export NFS didefinisikan di /etc/exports. Sama seperti fstab dan smb.conf:
// baris tulisan panel diberi penanda, baris milik admin hanya ditampilkan dan
// TIDAK pernah ditulis ulang — file ini menentukan siapa boleh membaca isi
// disk lewat jaringan, jadi salah tulis satu baris berarti membuka data ke
// host yang tidak pernah diizinkan pemiliknya.
var exportsPath = "/etc/exports"

const nfsTanda = "# lindash-nfs"

var (
	// Klien boleh berupa host, IP, CIDR, atau wildcard — tapi tidak boleh
	// mengandung spasi/kurung yang akan memecah format baris exports.
	klienRe    = regexp.MustCompile(`^[A-Za-z0-9_.:*/@-]+$`)
	opsiNfsRe  = regexp.MustCompile(`^[a-z0-9_,=.-]+$`)
	klausaRe   = regexp.MustCompile(`^([^()\s]+)\(([^)]*)\)$`)
	opsiDefNfs = "rw,sync,no_subtree_check"
)

func nfsList() ([]helperproto.NFSExport, error) {
	b, err := os.ReadFile(exportsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []helperproto.NFSExport{}, nil
		}
		return nil, err
	}
	aktif := nfsExportAktif()
	out := []helperproto.NFSExport{}
	for _, line := range strings.Split(string(b), "\n") {
		e, ok := parseBarisExport(line)
		if !ok {
			continue
		}
		e.Active = aktif[e.Path]
		out = append(out, e)
	}
	return out, nil
}

// nfsExportAktif membaca export yang BENAR-BENAR aktif di kernel, bukan yang
// sekadar tertulis di file — keduanya sering berbeda kalau `exportfs -ra`
// belum pernah dijalankan setelah file diubah manual.
func nfsExportAktif() map[string]bool {
	out := map[string]bool{}
	res, err := run("exportfs", "-s")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) > 0 && strings.HasPrefix(f[0], "/") {
			out[f[0]] = true
		}
	}
	return out
}

func parseBarisExport(line string) (helperproto.NFSExport, bool) {
	bersih := strings.TrimSpace(line)
	if bersih == "" || strings.HasPrefix(bersih, "#") {
		return helperproto.NFSExport{}, false
	}
	punyaPanel := strings.Contains(bersih, nfsTanda)
	if i := strings.Index(bersih, "#"); i >= 0 {
		bersih = strings.TrimSpace(bersih[:i])
	}
	f := strings.Fields(bersih)
	if len(f) < 2 || !strings.HasPrefix(f[0], "/") {
		return helperproto.NFSExport{}, false
	}
	e := helperproto.NFSExport{Path: f[0], External: !punyaPanel}
	for _, klausa := range f[1:] {
		m := klausaRe.FindStringSubmatch(klausa)
		if m == nil {
			// Klien tanpa opsi tetap sah di format exports.
			e.Clients = append(e.Clients, helperproto.NFSClient{Host: klausa})
			continue
		}
		e.Clients = append(e.Clients, helperproto.NFSClient{Host: m[1], Options: m[2]})
	}
	return e, len(e.Clients) > 0
}

func nfsSave(e helperproto.NFSExport) error {
	// Dicek di awal, bukan lewat kegagalan terapkanExports di akhir: tanpa ini
	// /etc/exports sudah terlanjur ditulis sebelum ketahuan exportfs tidak ada,
	// dan berkasnya ditinggal berubah meski panel membalas error.
	if !installed("exportfs") {
		return errKode(helperproto.ErrBelumTerpasang, "nfs-kernel-server belum terpasang — pasang dulu lewat Components")
	}
	// Whitespace apa pun ditolak, bukan cuma spasi dan tab: satu baris baru di
	// path memecah satu entri jadi dua baris di /etc/exports. Path
	// "/srv/x\n/" menghasilkan baris kedua "/  <klien>(<opsi>)" — seluruh
	// filesystem diekspor. Nama direktori berisi baris baru memang legal di
	// Linux, jadi os.Stat sebelumnya tidak menahannya.
	if !filepath.IsAbs(e.Path) || strings.ContainsFunc(e.Path, unicode.IsSpace) {
		return errInvalid("path export harus absolut dan tanpa spasi atau baris baru")
	}
	if st, err := os.Stat(e.Path); err != nil || !st.IsDir() {
		return errKode(helperproto.ErrFolderTidakAda, "folder %s tidak ada", e.Path)
	}
	if len(e.Clients) == 0 {
		return errInvalid("minimal satu klien harus diisi")
	}
	for i := range e.Clients {
		if !klienRe.MatchString(e.Clients[i].Host) {
			return errInvalid("klien %q tidak valid", e.Clients[i].Host)
		}
		opsi := strings.TrimSpace(e.Clients[i].Options)
		if opsi == "" {
			opsi = opsiDefNfs
		}
		if !opsiNfsRe.MatchString(opsi) {
			return errInvalid("opsi %q mengandung karakter yang tidak diizinkan", opsi)
		}
		e.Clients[i].Options = opsi
	}

	lama, err := nfsList()
	if err != nil {
		return err
	}
	for _, x := range lama {
		if x.Path == e.Path && x.External {
			return errInvalid("path %s sudah didefinisikan di /etc/exports di luar panel", e.Path)
		}
	}

	var b strings.Builder
	b.WriteString(e.Path)
	for _, c := range e.Clients {
		b.WriteString("  " + c.Host + "(" + c.Options + ")")
	}
	b.WriteString("  " + nfsTanda)
	if err := tulisExports(e.Path, b.String()); err != nil {
		return err
	}
	return terapkanExports()
}

func nfsDelete(path string) error {
	lama, err := nfsList()
	if err != nil {
		return err
	}
	for _, x := range lama {
		if x.Path == path && x.External {
			return errInvalid("export %s didefinisikan di luar panel — hapus barisnya sendiri di /etc/exports", path)
		}
	}
	if err := tulisExports(path, ""); err != nil {
		return err
	}
	return terapkanExports()
}

// terapkanExports memuat ulang tabel export kernel. Tanpa ini perubahan file
// baru berlaku setelah reboot, dan user mengira panelnya tidak bekerja.
func terapkanExports() error {
	if !installed("exportfs") {
		return errKode(helperproto.ErrBelumTerpasang, "nfs-kernel-server belum terpasang — pasang dulu lewat Components")
	}
	if _, err := run("exportfs", "-ra"); err != nil {
		return errInvalid("exportfs menolak konfigurasi: %v", err)
	}
	return nil
}

func tulisExports(path, baris string) error {
	b, err := os.ReadFile(exportsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var keluar []string
	ketemu := false
	for _, line := range strings.Split(string(b), "\n") {
		e, ok := parseBarisExport(line)
		if ok && !e.External && e.Path == path {
			ketemu = true
			if baris != "" {
				keluar = append(keluar, baris)
			}
			continue
		}
		keluar = append(keluar, line)
	}
	if !ketemu && baris != "" {
		for len(keluar) > 0 && strings.TrimSpace(keluar[len(keluar)-1]) == "" {
			keluar = keluar[:len(keluar)-1]
		}
		keluar = append(keluar, baris, "")
	}
	isi := strings.Join(keluar, "\n")
	if !strings.HasSuffix(isi, "\n") {
		isi += "\n"
	}
	tmp := filepath.Join(filepath.Dir(exportsPath), ".exports.lindash")
	if err := os.WriteFile(tmp, []byte(isi), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, exportsPath)
}
