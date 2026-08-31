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
	if !pathRe.MatchString(a.Mountpoint) || a.Mountpoint == "/" {
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
