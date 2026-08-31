package helper

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pool mergerfs didefinisikan sebagai baris /etc/fstab, bukan unit systemd:
// itu cara resmi mergerfs dan membuat pool ikut hidup saat boot tanpa panel.
//
// Baris tulisan panel diberi penanda di ujungnya. Baris tanpa penanda —
// dibuat manual atau oleh tool lain — hanya DITAMPILKAN, tidak pernah diubah
// atau dihapus: menulis ulang fstab orang lain berisiko membuat sistem gagal
// boot, dan pemiliknya tidak pernah menyetujui panel mengelola baris itu.
// fstabPath variabel (bukan konstanta) supaya test bisa menunjuk file
// sementara alih-alih /etc/fstab sungguhan.
var fstabPath = "/etc/fstab"

const (
	mergerfsTanda  = "# lindash-mergerfs"
	mergerfsFstype = "fuse.mergerfs"
	// nofail wajib ada di default: entri fuse yang gagal mount saat boot bisa
	// menjatuhkan sistem ke emergency mode, dan pool data bukan alasan yang
	// sepadan untuk membuat server tidak bisa booting.
	//
	// category.create=pfrd, bukan mfs. mfs ("most free space") selalu memilih
	// disk dengan sisa ruang terbanyak, jadi seluruh berkas baru menumpuk di
	// satu disk sampai sisanya turun ke level disk berikutnya — di pool 50G+32G
	// yang masih kosong, 6 dari 6 berkas mendarat di disk yang sama dan pool
	// terlihat tidak menyebar sama sekali. pfrd memilih acak dengan bobot sisa
	// ruang, sehingga berkas tersebar sejak awal dan disk yang lebih besar
	// menerima bagian lebih banyak — semuanya penuh pada waktu yang berdekatan,
	// bukan satu per satu. Diukur di pool 50G+32G: 40 berkas terbagi 25/15,
	// mengikuti nisbah sisa ruang 47/30.
	opsiDefault = "defaults,nofail,allow_other,use_ino,category.create=pfrd,moveonenospc=true,minfreespace=10G"
)

var (
	// Opsi mount dibatasi ke karakter yang wajar untuk fstab; spasi dan tab
	// akan memecah baris jadi kolom baru dan merusak seluruh entri.
	opsiRe  = regexp.MustCompile(`^[A-Za-z0-9_.,=%:/+-]+$`)
	pathRe  = regexp.MustCompile(`^/[^\s:]*$`)
	spasiRe = regexp.MustCompile(`\s+`)
)

func mergerfsList() ([]helperproto.MergerfsPool, error) {
	b, err := os.ReadFile(fstabPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []helperproto.MergerfsPool{}, nil
		}
		return nil, err
	}
	out := []helperproto.MergerfsPool{}
	for _, line := range strings.Split(string(b), "\n") {
		p, ok := parseBarisMergerfs(line)
		if !ok {
			continue
		}
		p.Mounted = sudahTerMount(p.Mountpoint)
		if p.Mounted {
			p.Total, p.Used, p.Free = pemakaian(p.Mountpoint)
		}
		out = append(out, p)
	}
	return out, nil
}

// parseBarisMergerfs membaca satu baris fstab bertipe fuse.mergerfs.
func parseBarisMergerfs(line string) (helperproto.MergerfsPool, bool) {
	bersih := strings.TrimSpace(line)
	if bersih == "" || strings.HasPrefix(bersih, "#") {
		return helperproto.MergerfsPool{}, false
	}
	punyaPanel := strings.Contains(bersih, mergerfsTanda)
	if i := strings.Index(bersih, "#"); i >= 0 {
		bersih = strings.TrimSpace(bersih[:i])
	}
	f := spasiRe.Split(bersih, -1)
	if len(f) < 3 || f[2] != mergerfsFstype {
		return helperproto.MergerfsPool{}, false
	}
	opsi := ""
	if len(f) >= 4 {
		opsi = f[3]
	}
	return helperproto.MergerfsPool{
		Mountpoint: f[1],
		Branches:   strings.Split(f[0], ":"),
		Options:    opsi,
		External:   !punyaPanel,
	}, true
}

func mergerfsSave(pool helperproto.MergerfsPool, u *userInfo) error {
	if !installed("mergerfs") {
		return errKode(helperproto.ErrBelumTerpasang, "mergerfs belum terpasang — pasang dulu lewat Components")
	}
	if !pathRe.MatchString(pool.Mountpoint) || pool.Mountpoint == "/" {
		return errInvalid("mount point harus path absolut, bukan /")
	}
	if len(pool.Branches) < 2 {
		return errInvalid("pool butuh minimal dua folder sumber")
	}
	for _, b := range pool.Branches {
		if !pathRe.MatchString(b) {
			return errInvalid("folder sumber %q tidak valid (harus path absolut tanpa spasi/titik dua)", b)
		}
		if st, err := os.Stat(b); err != nil || !st.IsDir() {
			return errKode(helperproto.ErrFolderTidakAda, "folder sumber %s tidak ada", b)
		}
		if b == pool.Mountpoint {
			return errInvalid("folder sumber tidak boleh sama dengan mount point")
		}
	}
	opsi := strings.TrimSpace(pool.Options)
	if opsi == "" {
		opsi = opsiDefault
	}
	if !opsiRe.MatchString(opsi) {
		return errInvalid("opsi mount mengandung karakter yang tidak diizinkan")
	}

	lama, err := mergerfsList()
	if err != nil {
		return err
	}
	for _, p := range lama {
		if p.Mountpoint == pool.Mountpoint && p.External {
			return errInvalid("mount point %s sudah didefinisikan di fstab di luar panel", pool.Mountpoint)
		}
	}
	if err := os.MkdirAll(pool.Mountpoint, 0o755); err != nil {
		return err
	}
	// Pool baru harus langsung bisa ditulisi pembuatnya. Disk yang baru
	// di-mount umumnya masih root:root 0755, sementara penulisan lewat panel
	// berjalan sebagai user yang login — tanpa langkah ini setiap upload ke
	// pool ditolak 403 dan user harus chown sendiri lewat SSH.
	//
	// Yang menentukan izin di dalam pool adalah branch-nya, bukan mount point:
	// mergerfs meneruskan atribut direktori dari branch. chownJikaBaru
	// melewati direktori yang sudah bukan milik root, jadi disk yang sengaja
	// dimiliki user lain tidak diutak-atik. Sengaja TIDAK rekursif: berkas
	// lama tetap milik pemiliknya masing-masing.
	if u != nil {
		// Branch selalu direktori sungguhan — tidak pernah tertutup mount pool,
		// jadi chown-nya langsung mengenai disk. Mount point hanya disentuh
		// saat pool belum ter-mount: pada jalur Edit pool masih hidup di sini
		// (umount baru di bawah), sehingga stat/chown-nya akan diteruskan ke
		// mergerfs lewat FUSE dan bisa ditolak — itu akan menggagalkan
		// penyimpanan pool yang sebenarnya tidak bermasalah.
		sasaran := append([]string{}, pool.Branches...)
		if !sudahTerMount(pool.Mountpoint) {
			sasaran = append(sasaran, pool.Mountpoint)
		}
		for _, d := range sasaran {
			if err := chownJikaBaru(d, u.UID, u.GID); err != nil {
				return errInvalid("tidak bisa memberi hak tulis ke %s pada %s: %v", u.Name, d, err)
			}
		}
	}

	baris := strings.Join(pool.Branches, ":") + "  " + pool.Mountpoint + "  " +
		mergerfsFstype + "  " + opsi + "  0 0  " + mergerfsTanda

	// Isi fstab sebelum perubahan disimpan untuk dikembalikan kalau mount
	// gagal: entri yang tidak bisa di-mount tidak boleh tertinggal di fstab.
	sebelum, _ := os.ReadFile(fstabPath)
	if err := tulisFstab(pool.Mountpoint, baris); err != nil {
		return err
	}
	// Remount supaya perubahan branch/opsi langsung berlaku, bukan menunggu boot.
	if sudahTerMount(pool.Mountpoint) {
		_, _ = run("umount", pool.Mountpoint)
	}
	// Dikunci setelah umount, saat direktorinya pasti telanjang — termasuk
	// kalau mount di bawah gagal dan pool tidak jadi hidup.
	lindungiMountPoint(pool.Mountpoint)
	if _, err := run("mount", pool.Mountpoint); err != nil {
		if sebelum != nil {
			_ = os.WriteFile(fstabPath, sebelum, 0o644)
		}
		return errMount(err)
	}
	return nil
}

// errMount menerjemahkan kegagalan mount yang paling sering muncul di homelab
// menjadi keterangan yang bisa ditindaklanjuti.
func errMount(err error) error {
	pesan := err.Error()
	if strings.Contains(pesan, "device not found") || strings.Contains(pesan, "/dev/fuse") {
		return errKode(helperproto.ErrFuseTidakAda, "FUSE tidak tersedia di sistem ini — mergerfs butuh /dev/fuse. "+
			"Di container LXC, aktifkan fuse di konfigurasi container (features: fuse=1) lalu boot ulang")
	}
	return errInvalid("mount pool gagal: %s", pesan)
}

// mergerfsMount memasang atau melepas pool tanpa menyentuh /etc/fstab, jadi
// pool yang dilepas kembali terpasang saat boot berikutnya. Idempoten: memasang
// pool yang sudah ter-mount, atau melepas yang sudah lepas, bukan kegagalan —
// tombolnya bisa saja ditekan dua kali, atau keadaannya berubah sejak halaman
// terakhir dimuat.
func mergerfsMount(mountpoint string, lepas bool) error {
	pool, err := poolPanel(mountpoint)
	if err != nil {
		return err
	}
	if lepas {
		if sudahTerMount(pool.Mountpoint) {
			if _, err := run("umount", pool.Mountpoint); err != nil {
				return errInvalid("tidak bisa melepas %s: %v — pastikan tidak ada file yang sedang dipakai", pool.Mountpoint, err)
			}
		}
		bersihkanMountPoint(pool.Mountpoint)
		return nil
	}
	if sudahTerMount(pool.Mountpoint) {
		return nil
	}
	// Direktorinya dibuang saat pool dilepas, jadi harus ada lagi sebelum
	// mount: `mount` tidak pernah membuat mount point sendiri.
	if err := os.MkdirAll(pool.Mountpoint, 0o755); err != nil {
		return err
	}
	lindungiMountPoint(pool.Mountpoint)
	if _, err := run("mount", pool.Mountpoint); err != nil {
		return errMount(err)
	}
	return nil
}

// bersihkanMountPoint membuang direktori mount point yang sudah tidak dipakai
// supaya /mnt tidak menyimpan folder sisa. Tidak ada direktori sama sekali
// adalah proteksi terkuat terhadap upload nyasar: tulisan ke sana gagal karena
// tujuannya memang tidak ada, bukan sekadar ditolak izin.
//
// Aman karena pool-nya sendiri tetap terdefinisi di fstab: systemd membuat
// ulang mount point-nya saat boot, dan jalur Pasang di panel membuatnya sendiri
// sebelum mount.
//
// Kalau direktorinya TIDAK kosong, isinya berkas nyata — biasanya tulisan yang
// terlanjur mendarat sebelum proteksi terpasang. Direktorinya dipertahankan
// beserta isinya, lalu dikunci supaya tidak bertambah. Menghapus berkas orang
// diam-diam jauh lebih buruk daripada menyisakan satu folder.
func bersihkanMountPoint(path string) {
	bukaMountPoint(path)
	if err := os.Remove(path); err != nil {
		lindungiMountPoint(path)
	}
}

// poolPanel mencari pool milik panel untuk mount point tertentu. Pool yang
// barisnya ditulis di luar panel hanya ditampilkan, tidak pernah dikendalikan
// dari sini — pemiliknya tidak pernah menyetujui panel mengutak-atiknya.
func poolPanel(mountpoint string) (helperproto.MergerfsPool, error) {
	if !pathRe.MatchString(mountpoint) {
		return helperproto.MergerfsPool{}, errInvalid("mount point tidak valid")
	}
	pools, err := mergerfsList()
	if err != nil {
		return helperproto.MergerfsPool{}, err
	}
	for _, p := range pools {
		if p.Mountpoint != mountpoint {
			continue
		}
		if p.External {
			return helperproto.MergerfsPool{}, errInvalid("pool %s didefinisikan di luar panel — kelola sendiri lewat /etc/fstab", mountpoint)
		}
		return p, nil
	}
	return helperproto.MergerfsPool{}, errKode(helperproto.ErrNotFound, "pool %s tidak ada di /etc/fstab", mountpoint)
}

func mergerfsDelete(mountpoint string) error {
	if !pathRe.MatchString(mountpoint) {
		return errInvalid("mount point tidak valid")
	}
	lama, err := mergerfsList()
	if err != nil {
		return err
	}
	for _, p := range lama {
		if p.Mountpoint == mountpoint && p.External {
			return errInvalid("pool %s didefinisikan di luar panel — hapus barisnya sendiri di /etc/fstab", mountpoint)
		}
	}
	if sudahTerMount(mountpoint) {
		if _, err := run("umount", mountpoint); err != nil {
			return errInvalid("tidak bisa melepas %s: %v — pastikan tidak ada file yang sedang dipakai", mountpoint, err)
		}
	}
	if err := tulisFstab(mountpoint, ""); err != nil {
		return err
	}
	// Mount point ikut dibuang supaya tidak ada folder sisa di /mnt setelah
	// pool hilang — panel yang membuatnya saat pool dibuat, jadi panel pula
	// yang membereskannya.
	//
	// os.Remove, BUKAN RemoveAll: folder yang tidak kosong berarti ada berkas
	// nyata di sana — biasanya data yang ditulis ke mount point saat pool
	// sedang lepas — dan itu tidak boleh ikut terhapus. Kegagalannya sengaja
	// diabaikan: pool-nya sendiri sudah benar-benar terhapus, dan folder
	// tersisa hanya soal kerapian, bukan alasan melaporkan penghapusan gagal.
	//
	// Kunci immutable-nya dilepas dulu — rmdir pada direktori immutable selalu
	// ditolak, jadi tanpa ini folder sisa tidak pernah bisa dibuang.
	bukaMountPoint(mountpoint)
	_ = os.Remove(mountpoint)
	return nil
}

// tulisFstab mengganti baris mergerfs milik panel untuk mountpoint tertentu.
func tulisFstab(mountpoint, baris string) error {
	return gantiBarisFstab(mergerfsTanda, mountpoint, baris)
}

// gantiBarisFstab mengganti baris fstab milik panel — dikenali dari penanda di
// ujungnya — yang menunjuk mountpoint tertentu. baris kosong = hapus. Baris
// lain, termasuk entri yang ditulis manual atau oleh tool lain, disalin apa
// adanya: menulis ulang fstab orang lain berisiko membuat sistem gagal boot.
func gantiBarisFstab(tanda, mountpoint, baris string) error {
	b, err := os.ReadFile(fstabPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var keluar []string
	ketemu := false
	for _, line := range strings.Split(string(b), "\n") {
		if barisPanel(line, tanda, mountpoint) {
			ketemu = true
			if baris != "" {
				keluar = append(keluar, baris)
			}
			continue
		}
		keluar = append(keluar, line)
	}
	if !ketemu && baris != "" {
		// Buang baris kosong di ujung supaya file tidak menumpuk newline.
		for len(keluar) > 0 && strings.TrimSpace(keluar[len(keluar)-1]) == "" {
			keluar = keluar[:len(keluar)-1]
		}
		keluar = append(keluar, baris, "")
	}
	isi := strings.Join(keluar, "\n")
	if !strings.HasSuffix(isi, "\n") {
		isi += "\n"
	}
	// Tulis lewat file sementara di direktori yang sama lalu rename: fstab yang
	// terpotong di tengah penulisan membuat sistem gagal boot.
	tmp := filepath.Join(filepath.Dir(fstabPath), ".fstab.lindash")
	if err := os.WriteFile(tmp, []byte(isi), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fstabPath)
}

// barisPanel: baris fstab bertanda milik panel yang mount point-nya cocok.
func barisPanel(line, tanda, mountpoint string) bool {
	if !strings.Contains(line, tanda) {
		return false
	}
	f := spasiRe.Split(strings.TrimSpace(line), -1)
	return len(f) >= 2 && f[1] == mountpoint
}

// lindungiMountPoint mengunci direktori mount point dengan atribut immutable.
//
// Saat tidak ada yang ter-mount di atasnya, mount point hanyalah folder biasa
// di disk sistem. Berkas yang diunggah ke sana diam-diam memenuhi disk sistem
// alih-alih masuk ke pool, lalu TERSEMBUNYI begitu pool dipasang lagi —
// pemiliknya melihat foldernya kosong dan tidak punya cara tahu berkasnya ada
// di mana. Dengan immutable, tulisan itu ditolak keras dan kesalahannya
// terlihat saat itu juga.
//
// Mount tetap bisa dilakukan di atas direktori immutable, dan menulis ke dalam
// mount berjalan normal, jadi proteksi ini tidak mengganggu pool yang hidup.
//
// Kegagalannya dicatat tapi tidak menggagalkan operasi: sebagian filesystem
// tidak mendukung atribut ini, dan pool yang berjalan tanpa proteksi tetap
// lebih berguna daripada pool yang gagal dibuat.
func lindungiMountPoint(path string) {
	if _, err := run("chattr", "+i", path); err != nil {
		log.Printf("[helper] %s tidak bisa dikunci immutable: %v — berkas yang ditulis saat tidak ter-mount akan mengisi disk sistem", path, err)
	}
}

// bukaMountPoint melepas atribut immutable. Dipakai sebelum direktorinya
// dihapus: rmdir pada direktori immutable selalu ditolak.
func bukaMountPoint(path string) {
	_, _ = run("chattr", "-i", path)
}

func sudahTerMount(path string) bool {
	res, err := run("findmnt", "-rn", "-o", "TARGET", path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(res.Stdout) != ""
}

func pemakaian(path string) (total, used, free uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0
	}
	total = st.Blocks * uint64(st.Bsize)
	free = st.Bavail * uint64(st.Bsize)
	return total, total - st.Bfree*uint64(st.Bsize), free
}
