package helper

import (
	"os"
	"regexp"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Sisi KLIEN dari NFS: export milik server lain yang dipasang di mesin ini.
// Satu mount = satu baris /etc/fstab bertanda panel, persis seperti pool
// mergerfs — baris tanpa penanda hanya ditampilkan dan tidak pernah ditulis
// ulang, karena menulis ulang fstab orang lain bisa membuat sistem gagal boot.
const (
	nfsMountTanda = "# lindash-nfsmount"

	// _netdev + nofail wajib ada di default. Tanpa _netdev systemd mencoba
	// mount sebelum jaringan hidup; tanpa nofail server NFS yang mati membuat
	// MESIN INI gagal boot dan jatuh ke emergency mode — harga yang terlalu
	// mahal untuk satu folder jaringan.
	//
	// hard (bawaan NFS) sengaja TIDAK diganti soft: soft membuat tulisan yang
	// belum sampai ke server dibuang diam-diam saat jaringan putus, dan
	// kehilangan data tanpa pesan apa pun jauh lebih buruk daripada I/O yang
	// menunggu. retry=0 hanya membatasi PERCOBAAN MOUNT-nya (bawaannya
	// mencoba ulang sampai dua menit), bukan perilaku I/O sesudah ter-mount.
	opsiDefNfsMount = "_netdev,nofail,rw,hard,retry=0,timeo=600,retrans=2"

	// Batas waktu perintah yang bicara ke server lain. Server NFS yang mati
	// tidak menolak koneksi — ia diam, dan perintahnya menggantung. Permintaan
	// yang menggantung tidak memunculkan apa pun di panel, jadi lebih baik
	// gagal cepat dengan pesan yang jelas.
	batasNfsDetik = "20"
)

// Host boleh nama atau IPv4. IPv6 literal butuh kurung siku di fstab dan
// tidak diterima di sini.
// ponytail: hostname/IPv4 saja, tambahkan bentuk [::1] kalau ada yang memakainya.
var serverNfsRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$`)

func nfsMountList() ([]helperproto.NFSMount, error) {
	b, err := os.ReadFile(fstabPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := []helperproto.NFSMount{}
	tercatat := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		m, ok := parseBarisNFSMount(line)
		if !ok {
			continue
		}
		m.InFstab = true
		tercatat[m.Mountpoint] = true
		out = append(out, m)
	}
	// Mount yang hidup tanpa baris fstab — dipasang manual lewat `mount` —
	// tetap ditampilkan. Halaman ini harus menyebut SELURUH NFS yang sedang
	// terpasang; daftar yang diam soal salah satunya membuat user mencari
	// mount yang "hilang" di tempat yang salah.
	for _, m := range nfsMountAktif() {
		if !tercatat[m.Mountpoint] {
			out = append(out, m)
		}
	}
	for i := range out {
		out[i].Mounted = sudahTerMount(out[i].Mountpoint)
		if out[i].Mounted {
			out[i].Total, out[i].Used, out[i].Free = pemakaian(out[i].Mountpoint)
		}
	}
	return out, nil
}

// parseBarisNFSMount membaca satu baris fstab bertipe nfs/nfs4.
func parseBarisNFSMount(line string) (helperproto.NFSMount, bool) {
	bersih := strings.TrimSpace(line)
	if bersih == "" || strings.HasPrefix(bersih, "#") {
		return helperproto.NFSMount{}, false
	}
	punyaPanel := strings.Contains(bersih, nfsMountTanda)
	if i := strings.Index(bersih, "#"); i >= 0 {
		bersih = strings.TrimSpace(bersih[:i])
	}
	f := spasiRe.Split(bersih, -1)
	if len(f) < 3 || (f[2] != "nfs" && f[2] != "nfs4") {
		return helperproto.NFSMount{}, false
	}
	server, remote, ok := pisahSumberNFS(f[0])
	if !ok {
		return helperproto.NFSMount{}, false
	}
	opsi := ""
	if len(f) >= 4 {
		opsi = f[3]
	}
	return helperproto.NFSMount{
		Server: server, Remote: remote, Mountpoint: f[1],
		Options: opsi, External: !punyaPanel,
	}, true
}

// pisahSumberNFS memecah "server:/path". Host NFS tidak pernah mengandung
// titik dua (IPv6 literal ditolak di serverNfsRe), jadi pemisahnya yang pertama.
func pisahSumberNFS(s string) (server, remote string, ok bool) {
	server, remote, ok = strings.Cut(s, ":")
	if !ok || server == "" || !strings.HasPrefix(remote, "/") {
		return "", "", false
	}
	return server, remote, true
}

// nfsMountAktif membaca mount NFS yang benar-benar hidup di kernel.
func nfsMountAktif() []helperproto.NFSMount {
	res, err := run("findmnt", "-rn", "-t", "nfs,nfs4", "-o", "SOURCE,TARGET,OPTIONS")
	if err != nil {
		return nil
	}
	var out []helperproto.NFSMount
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 2 {
			continue
		}
		server, remote, ok := pisahSumberNFS(f[0])
		if !ok {
			continue
		}
		opsi := ""
		if len(f) >= 3 {
			opsi = f[2]
		}
		out = append(out, helperproto.NFSMount{
			Server: server, Remote: remote, Mountpoint: f[1],
			Options: opsi, External: true,
		})
	}
	return out
}

func nfsMountSave(m helperproto.NFSMount) error {
	// Dicek di awal, bukan lewat kegagalan mount di akhir: tanpa ini fstab
	// sudah terlanjur ditulis sebelum ketahuan kliennya belum terpasang.
	if !installed("mount.nfs") {
		return errKode(helperproto.ErrBelumTerpasang,
			"klien NFS belum terpasang — pasang komponen nfs-client lewat Components")
	}
	if !serverNfsRe.MatchString(m.Server) {
		return errInvalid("alamat server %q tidak valid — pakai hostname atau IPv4", m.Server)
	}
	if !pathRe.MatchString(m.Remote) {
		return errInvalid("path di server harus absolut, tanpa spasi atau titik dua")
	}
	if !pathRe.MatchString(m.Mountpoint) || m.Mountpoint == "/" {
		return errInvalid("mount point harus path absolut, bukan /")
	}
	opsi := strings.TrimSpace(m.Options)
	if opsi == "" {
		opsi = opsiDefNfsMount
	}
	if !opsiRe.MatchString(opsi) {
		return errInvalid("opsi mount mengandung karakter yang tidak diizinkan")
	}

	lama, err := nfsMountList()
	if err != nil {
		return err
	}
	for _, x := range lama {
		if x.Mountpoint == m.Mountpoint && x.External {
			return errInvalid("mount point %s sudah dipakai di luar panel", m.Mountpoint)
		}
	}
	if err := os.MkdirAll(m.Mountpoint, 0o755); err != nil {
		return err
	}

	baris := m.Server + ":" + m.Remote + "  " + m.Mountpoint + "  nfs  " + opsi + "  0 0  " + nfsMountTanda
	// Isi fstab sebelum perubahan disimpan untuk dikembalikan kalau mount
	// gagal: entri yang tidak bisa di-mount tidak boleh tertinggal di fstab.
	sebelum, _ := os.ReadFile(fstabPath)
	if err := gantiBarisFstab(nfsMountTanda, m.Mountpoint, baris); err != nil {
		return err
	}
	// Remount supaya perubahan server/path/opsi langsung berlaku.
	if sudahTerMount(m.Mountpoint) {
		_, _ = run("umount", m.Mountpoint)
	}
	lindungiMountPoint(m.Mountpoint)
	if err := mountNFS(m.Mountpoint); err != nil {
		if sebelum != nil {
			_ = os.WriteFile(fstabPath, sebelum, 0o644)
		}
		return err
	}
	return nil
}

// nfsMountToggle memasang atau melepas mount tanpa menyentuh /etc/fstab, jadi
// mount yang dilepas kembali terpasang saat boot. Idempoten: memasang yang
// sudah ter-mount, atau melepas yang sudah lepas, bukan kegagalan.
func nfsMountToggle(mountpoint string, lepas bool) error {
	m, err := nfsMountPanel(mountpoint)
	if err != nil {
		return err
	}
	if lepas {
		if sudahTerMount(m.Mountpoint) {
			if _, err := run("umount", m.Mountpoint); err != nil {
				return errInvalid("tidak bisa melepas %s: %v — pastikan tidak ada file yang sedang dipakai", m.Mountpoint, err)
			}
		}
		lindungiMountPoint(m.Mountpoint)
		return nil
	}
	if sudahTerMount(m.Mountpoint) {
		return nil
	}
	if err := os.MkdirAll(m.Mountpoint, 0o755); err != nil {
		return err
	}
	lindungiMountPoint(m.Mountpoint)
	return mountNFS(m.Mountpoint)
}

func nfsMountDelete(mountpoint string) error {
	if !pathRe.MatchString(mountpoint) {
		return errInvalid("mount point tidak valid")
	}
	if _, err := nfsMountPanel(mountpoint); err != nil {
		return err
	}
	if sudahTerMount(mountpoint) {
		if _, err := run("umount", mountpoint); err != nil {
			return errInvalid("tidak bisa melepas %s: %v — pastikan tidak ada file yang sedang dipakai", mountpoint, err)
		}
	}
	if err := gantiBarisFstab(nfsMountTanda, mountpoint, ""); err != nil {
		return err
	}
	// Folder mount point ikut dibuang — panel yang membuatnya, panel pula yang
	// membereskannya. os.Remove, bukan RemoveAll: folder yang tidak kosong
	// berarti ada berkas nyata di sana dan itu tidak boleh ikut terhapus.
	bukaMountPoint(mountpoint)
	_ = os.Remove(mountpoint)
	return nil
}

// nfsMountPanel mencari mount milik panel. Mount yang barisnya ditulis di luar
// panel — atau yang dipasang manual tanpa baris fstab — hanya ditampilkan.
func nfsMountPanel(mountpoint string) (helperproto.NFSMount, error) {
	if !pathRe.MatchString(mountpoint) {
		return helperproto.NFSMount{}, errInvalid("mount point tidak valid")
	}
	list, err := nfsMountList()
	if err != nil {
		return helperproto.NFSMount{}, err
	}
	for _, m := range list {
		if m.Mountpoint != mountpoint {
			continue
		}
		if m.External {
			return helperproto.NFSMount{}, errInvalid(
				"mount %s tidak dibuat panel — kelola sendiri lewat /etc/fstab atau perintah mount", mountpoint)
		}
		return m, nil
	}
	return helperproto.NFSMount{}, errKode(helperproto.ErrNotFound, "mount %s tidak ada di /etc/fstab", mountpoint)
}

// nfsDiscover menanyakan daftar export satu server, supaya path remote tidak
// perlu diketik dari ingatan.
func nfsDiscover(server string) ([]helperproto.NFSRemoteExport, error) {
	if !serverNfsRe.MatchString(server) {
		return nil, errInvalid("alamat server %q tidak valid — pakai hostname atau IPv4", server)
	}
	if !installed("showmount") {
		return nil, errKode(helperproto.ErrBelumTerpasang,
			"showmount belum ada — pasang komponen nfs-client lewat Components")
	}
	res, err := runBatas("showmount", "--no-headers", "-e", server)
	if err != nil {
		if res.ExitCode == 124 {
			return nil, errInvalid("server %s tidak menjawab dalam %s detik — cek alamat, firewall port 111/2049, dan apakah NFS-nya hidup", server, batasNfsDetik)
		}
		return nil, errInvalid("server %s tidak memberi daftar export: %v", server, err)
	}
	out := []helperproto.NFSRemoteExport{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) == 0 || !strings.HasPrefix(f[0], "/") {
			continue
		}
		out = append(out, helperproto.NFSRemoteExport{Path: f[0], Clients: strings.Join(f[1:], " ")})
	}
	return out, nil
}

// mountNFS menjalankan `mount <mountpoint>` dengan batas waktu dan
// menerjemahkan kegagalan yang paling sering muncul di homelab.
func mountNFS(mountpoint string) error {
	res, err := runBatas("mount", mountpoint)
	if err == nil {
		return nil
	}
	if res.ExitCode == 124 {
		return errInvalid("server tidak menjawab dalam %s detik — cek alamat, firewall port 2049, dan apakah NFS-nya hidup", batasNfsDetik)
	}
	pesan := err.Error()
	switch {
	case strings.Contains(pesan, "access denied"), strings.Contains(pesan, "Permission denied"):
		return errInvalid("server menolak mesin ini: %s — pastikan IP mesin ini termasuk klien yang diizinkan di /etc/exports server", pesan)
	case strings.Contains(pesan, "No such file or directory"):
		return errInvalid("path itu tidak diekspor server: %s — cek daftar export lewat tombol Cari export", pesan)
	}
	return errInvalid("mount gagal: %s", pesan)
}

// runBatas menjalankan perintah dengan batas waktu lewat coreutils `timeout`.
// Perintah NFS ke server yang mati tidak ditolak — ia diam sampai time-out
// bawaannya sendiri (mount.nfs mencoba ulang sampai dua menit), dan permintaan
// yang menggantung selama itu tidak memunculkan apa pun di panel.
func runBatas(name string, args ...string) (helperproto.ExecResult, error) {
	return run("timeout", append([]string{batasNfsDetik, name}, args...)...)
}
