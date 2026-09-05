package helper

import (
	"os"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/metrics"
)

// Disk mentah yang baru ditambahkan di hypervisor tidak bisa dipakai sampai
// diformat dan di-mount. Sebelum ini user harus mengetik mkfs/fstab/mount
// sendiri di terminal; di sini panel yang mengerjakannya, dengan pagar:
// yang boleh disentuh hanya disk yang metrics akui benar-benar kosong
// (tanpa partisi, tanpa holder LVM/RAID, tidak ter-mount), dan memformat disk
// yang sudah berisi filesystem butuh izin eksplisit.
const diskTanda = "# lindash-disk"

// Filesystem yang ditawarkan panel, beserta flag "jangan tanya" masing-masing
// mkfs — helper tidak punya TTY, jadi prompt konfirmasi mkfs akan menggantung.
var fsDidukung = map[string][]string{
	"ext4":  {"-F"},
	"xfs":   {"-f"},
	"btrfs": {"-f"},
}

// Paket apt yang menyediakan mkfs untuk tiap filesystem, dipakai untuk pesan
// error yang bisa ditindaklanjuti ("pasang dulu lewat Components").
var paketFS = map[string]string{
	"ext4":  "e2fsprogs",
	"xfs":   "xfsprogs",
	"btrfs": "btrfs-progs",
}

func diskPrepare(a helperproto.DiskPrepareArgs) error {
	if !diskKosong(a.Path) {
		return errKode(helperproto.ErrDiskDipakai, "%s bukan disk kosong — sudah punya partisi, dipakai LVM/RAID, atau sedang ter-mount", a.Path)
	}
	if !pathAman(a.Mountpoint) || a.Mountpoint == "/" {
		return errKode(helperproto.ErrPathTidakValid, "mount point harus path absolut, bukan /")
	}
	if sudahTerMount(a.Mountpoint) {
		return errKode(helperproto.ErrSudahAda, "%s sudah dipakai mount lain", a.Mountpoint)
	}

	fsLama := blkidNilai(a.Path, "TYPE")
	switch {
	case a.Format && fsLama != "" && !a.Timpa:
		// Disk yang sudah berisi data tidak boleh hilang karena satu klik.
		return errKode(helperproto.ErrDiskAdaFS, "%s sudah berisi filesystem %s", a.Path, fsLama)
	case !a.Format && fsLama == "":
		return errInvalid("%s belum punya filesystem — pilih format dulu", a.Path)
	}

	fstype := fsLama
	if a.Format {
		opsi, ok := fsDidukung[a.FSType]
		if !ok {
			return errKode(helperproto.ErrNilaiTidakValid, "filesystem %s tidak didukung", a.FSType)
		}
		mkfs := "mkfs." + a.FSType
		if !installed(mkfs) {
			return errKode(helperproto.ErrBelumTerpasang, "%s belum terpasang", paketFS[a.FSType])
		}
		if _, err := run(mkfs, append(append([]string{}, opsi...), a.Path)...); err != nil {
			return errInvalid("format %s gagal: %v", a.Path, err)
		}
		fstype = a.FSType
	}

	// fstab menunjuk UUID, bukan /dev/sdb: urutan penamaan disk bisa berubah
	// antar boot, dan mount yang salah disk lebih buruk daripada gagal mount.
	uuid := blkidNilai(a.Path, "UUID")
	if uuid == "" {
		return errInvalid("UUID %s tidak terbaca setelah disiapkan", a.Path)
	}
	if err := os.MkdirAll(a.Mountpoint, 0o755); err != nil {
		return err
	}

	// nofail: disk data yang hilang (dicabut, dilepas dari VM) tidak boleh
	// menjatuhkan server ke emergency mode saat boot.
	baris := "UUID=" + uuid + "  " + a.Mountpoint + "  " + fstype + "  defaults,nofail  0 2  " + diskTanda
	sebelum, _ := os.ReadFile(fstabPath)
	if err := gantiBarisFstab(diskTanda, a.Mountpoint, baris); err != nil {
		return err
	}
	// Lubang yang sama dengan mount point pool: kalau disk ini gagal mount —
	// dicabut, UUID berubah, atau nofail melewatinya saat boot — mount point-nya
	// jadi folder biasa di disk sistem, dan berkas yang ditulis ke sana memenuhi
	// disk sistem lalu tersembunyi begitu disknya ter-mount lagi. Dikunci
	// sebelum mount, saat direktorinya masih telanjang.
	lindungiMountPoint(a.Mountpoint)
	if _, err := run("mount", a.Mountpoint); err != nil {
		// Entri yang tidak bisa di-mount tidak boleh tertinggal di fstab.
		if sebelum != nil {
			_ = os.WriteFile(fstabPath, sebelum, 0o644)
		}
		return errInvalid("mount %s gagal: %v", a.Mountpoint, err)
	}
	return nil
}

// diskKosong memakai daftar yang sama dengan yang ditampilkan dashboard,
// supaya tidak ada disk yang terlihat "belum dipakai" di UI tapi ditolak di
// helper — atau sebaliknya, yang jauh lebih berbahaya.
func diskKosong(path string) bool {
	for _, d := range metrics.UnusedDisks() {
		if d.Path == path {
			return true
		}
	}
	return false
}

// blkidNilai membaca satu atribut superblock. -p memaksa probe langsung ke
// device: tanpa itu blkid bisa menjawab dari cache dan melaporkan filesystem
// lama yang barusan ditimpa mkfs. Device tanpa filesystem membuat blkid keluar
// dengan status != 0 — itu jawaban "kosong", bukan kegagalan.
func blkidNilai(path, atribut string) string {
	res, err := run("blkid", "-p", "-s", atribut, "-o", "value", path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// diskUnmount melepas satu mount disk dari panel, dan — kalau lupakan=true —
// sekalian membuang jejaknya: baris /etc/fstab tulisan panel dan folder mount
// point-nya.
//
// Ini pasangan yang selama ini hilang dari diskPrepare: disk bisa disiapkan
// dari panel tapi tidak bisa dilepas dari panel, sehingga disk yang dicabut
// meninggalkan mount mati (setiap pembacaan dijawab EIO), baris fstab yang
// tidak menunjuk apa pun, dan folder immutable yang tidak bisa dihapus user.
func diskUnmount(mountpoint string, lupakan bool) error {
	if !pathAman(mountpoint) {
		return errKode(helperproto.ErrPathTidakValid, "mount point tidak valid")
	}
	// Pagar paling penting di berkas ini. Melepas /, /boot, atau /var membuat
	// mesin tidak bisa dipakai sampai reboot — dan panel tidak punya satu pun
	// alasan sah untuk menyentuhnya. Yang boleh hanya mount data.
	if !strings.HasPrefix(mountpoint, "/mnt/") && !strings.HasPrefix(mountpoint, "/media/") {
		return errInvalid("hanya mount di /mnt atau /media yang bisa dilepas dari panel — %s dikelola sistem", mountpoint)
	}
	switch tandaFstab(mountpoint) {
	case mergerfsTanda:
		return errInvalid("%s adalah pool mergerfs — lepas dari halaman Disk Pool supaya barisnya ikut terurus", mountpoint)
	case nfsMountTanda:
		return errInvalid("%s adalah mount NFS — lepas dari halaman NFS Exports → Klien NFS", mountpoint)
	}

	if sudahTerMount(mountpoint) {
		if _, err := run("umount", mountpoint); err != nil {
			// Disk yang dicabut saat masih ter-mount tidak bisa di-umount biasa:
			// kernel masih memegang mount-nya dan setiap akses dijawab EIO.
			// umount -l melepas pohonnya sekarang dan membereskan sisanya
			// begitu tidak ada yang memakai — satu-satunya jalan keluar yang
			// tidak menuntut reboot.
			if _, e := run("umount", "-l", mountpoint); e != nil {
				return errInvalid("tidak bisa melepas %s: %v — pastikan tidak ada berkas yang sedang dipakai", mountpoint, err)
			}
		}
	}
	if !lupakan {
		return nil
	}

	// Hanya baris tulisan panel yang dibuang. Baris fstab orang lain tetap
	// utuh — dan karena itu mount-nya akan kembali setelah reboot, jadi
	// kejadian itu dilaporkan alih-alih didiamkan.
	adaBarisPanel := tandaFstab(mountpoint) == diskTanda
	if adaBarisPanel {
		if err := gantiBarisFstab(diskTanda, mountpoint, ""); err != nil {
			return err
		}
	}
	// Kunci immutable dipasang diskPrepare; rmdir pada direktori immutable
	// selalu ditolak, jadi harus dilepas dulu. os.Remove, bukan RemoveAll:
	// folder yang tidak kosong berarti ada berkas nyata di sana — biasanya
	// tulisan yang mendarat saat disknya tidak ter-mount — dan itu tidak boleh
	// ikut terhapus.
	bukaMountPoint(mountpoint)
	_ = os.Remove(mountpoint)
	if !adaBarisPanel && barisFstabAda(mountpoint) {
		return errInvalid("%s dilepas, tapi baris /etc/fstab-nya bukan tulisan panel — hapus sendiri, kalau tidak mount-nya kembali setelah reboot", mountpoint)
	}
	return nil
}

// tandaFstab mengembalikan penanda panel pada baris fstab untuk mountpoint
// tertentu ("" kalau barisnya tidak ada atau tidak bertanda). Dipakai untuk
// menolak mount yang punya halaman pengelolanya sendiri.
func tandaFstab(mountpoint string) string {
	b, err := os.ReadFile(fstabPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		bersih := strings.TrimSpace(line)
		if bersih == "" || strings.HasPrefix(bersih, "#") {
			continue
		}
		isi := bersih
		if i := strings.Index(isi, "#"); i >= 0 {
			isi = strings.TrimSpace(isi[:i])
		}
		f := spasiRe.Split(isi, -1)
		if len(f) < 2 || f[1] != mountpoint {
			continue
		}
		for _, t := range []string{diskTanda, mergerfsTanda, nfsMountTanda} {
			if strings.Contains(bersih, t) {
				return t
			}
		}
		return ""
	}
	return ""
}

// barisFstabAda menjawab apakah mountpoint masih punya baris di fstab, tanpa
// peduli siapa yang menulisnya.
func barisFstabAda(mountpoint string) bool {
	b, err := os.ReadFile(fstabPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		bersih := strings.TrimSpace(line)
		if bersih == "" || strings.HasPrefix(bersih, "#") {
			continue
		}
		if i := strings.Index(bersih, "#"); i >= 0 {
			bersih = strings.TrimSpace(bersih[:i])
		}
		f := spasiRe.Split(bersih, -1)
		if len(f) >= 2 && f[1] == mountpoint {
			return true
		}
	}
	return false
}
