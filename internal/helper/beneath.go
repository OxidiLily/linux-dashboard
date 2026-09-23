package helper

// Resolver berkas descriptor-relative untuk worker non-sudo.
//
// Masalah yang ditutup di sini (TOCTOU): validasi path terjadi di parent
// (checkPath di authz.go) dan pemakaian nama path terjadi di worker — dua
// langkah waktu yang terpisah. Penyerang yang bisa membuat symlink di dalam
// home-nya sendiri dapat menukar symlink di antara keduanya, sehingga operasi
// worker menyentuh path di luar home walau checkPath sudah meloloskannya.
//
// Jalan keluarnya bukan memvalidasi ulang nama (celah waktunya tetap ada),
// melainkan berhenti memakai nama sama sekali untuk menjalankan operasi:
//
//  1. Parent membuka direktori home sebagai fd (O_PATH|O_DIRECTORY) dan
//     mengirimnya ke worker sebagai fd 5, bareng penanda HELPER_JAIL_HOME.
//     Direktori home milik root, jadi penyerang tidak bisa menggantinya.
//  2. Worker menyelesaikan SETIAP path relatif terhadap fd itu dengan
//     openat2(2) + RESOLVE_BENEATH|RESOLVE_NO_MAGICLINKS. Kernel memeriksa
//     seluruh komponen (termasuk symlink) terhadap fd akar pada saat yang
//     sama dengan resolusi — tidak ada lagi jendela untuk menukar komponen.
//  3. Operasinya memakai syscall *at / fd (openat2, mkdirat, unlinkat,
//     renameat, fchmodat, fstatat, readdir lewat fd), bukan nama absolut.
//
// RESOLVE_NO_SYMLINKS sengaja TIDAK dipakai: symlink yang tetap menunjuk di
// dalam home adalah perilaku lama yang harus tetap jalan. RESOLVE_BENEATH
// sudah menjamin symlink yang keluar home ditolak (EXDEV).
//
// Jalur sudo tidak disentuh: userInfo.Sudo tetap memakai resolusi berbasis
// nama seperti sebelumnya (path-nya memang di luar home, dan RESOLVE_BENEATH
// justru akan memutus symlink absolut yang lazim di /etc dan /usr).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

const (
	// fdJail adalah fd protokol tempat parent mengirim deskriptor direktori
	// home (ExtraFiles ke-3: 3 = op, 4 = hasil, 5 = jail).
	fdJail = 5

	// jailHomeEnv mengangkut path absolut direktori home yang fd-nya dikirim
	// di fdJail. Dua-duanya diisi parent di tempat yang sama (runAsUser) dan
	// tidak pernah berasal dari klien. Dipisah dari HOME supaya jalur sudo —
	// yang justru tidak boleh dijail — tidak ikut terjebak oleh env bawaan.
	jailHomeEnv = "HELPER_JAIL_HOME"

	// devtmpfsMagic: bagian dari ABI kernel, sama dengan yang dipakai pseudoFs
	// di carifile.go untuk jalur berbasis nama.
	devtmpfsMagic = 0x01021997
)

// penjaga menyelesaikan path relatif terhadap fd direktori home.
type penjaga struct {
	fd   int    // fd direktori home (O_PATH|O_DIRECTORY), milik parent
	akar string // path absolutnya, untuk path tampilan & pesan error
}

// penjagaWorker menyiapkan jail dari fd protokol. Nil tanpa error berarti
// op ini bukan jalur jail (sudo) dan worker memakai resolusi berbasis nama.
//
// Penanda datang dari env, bukan dari field op, karena fd 5 dan path akarnya
// adalah SATU kontrak yang dibuat parent di tempat yang sama — memisahkannya
// ke dua tempat (struct op + ExtraFiles) hanya menambah cara untuk tidak
// sinkron. Efek sampingnya menguntungkan: protokolnya bisa diuji apa adanya.
func penjagaWorker() (*penjaga, error) {
	akar := os.Getenv(jailHomeEnv)
	if akar == "" {
		return nil, nil
	}
	akar = filepath.Clean(akar)
	if !filepath.IsAbs(akar) || akar == "/" {
		return nil, errJailInternal("akar jail tidak valid: %q", akar)
	}
	// Tanpa fd 5 jail tidak bisa ditegakkan. Menjalankan op-nya tetap
	// (dengan resolusi nama) berarti membatalkan penjagaan tanpa suara —
	// jadi ini kegagalan, bukan fallback.
	var st syscall.Stat_t
	if err := syscall.Fstat(fdJail, &st); err != nil {
		return nil, errJailInternal("jail %s diminta tapi fd %d tidak dikirim parent: %v", akar, fdJail, err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return nil, errJailInternal("fd %d bukan direktori", fdJail)
	}
	return &penjaga{fd: fdJail, akar: akar}, nil
}

// rel mengubah path absolut menjadi path relatif terhadap akar jail, dan
// menolak apa pun di luarnya. Pemeriksaan ini LEKSIKAL — sama seperti
// checkPath di parent; penegakan sebenarnya ada di openat2 di bawahnya.
func (p *penjaga) rel(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", errInvalid("path harus absolut: %s", path)
	}
	if !dalamHome(clean, p.akar) {
		return "", errJailDiLuarHome(clean)
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(clean, p.akar), "/")
	if rel == "" {
		return ".", nil
	}
	return rel, nil
}

// tampilan mengembalikan path absolut untuk pesan error dan kolom Path.
func (p *penjaga) tampilan(rel string) string {
	if rel == "." || rel == "" {
		return p.akar
	}
	return filepath.Join(p.akar, rel)
}

// buka membuka rel (relatif terhadap home) dengan penjagaan jail. Symlink
// absolut yang tetap menunjuk di dalam home ikut ditangani — lihat bukaAman.
func (p *penjaga) buka(rel string, flags int, mode uint32) (*os.File, error) {
	return p.bukaAman(rel, flags, mode)
}

// batasJailSymlink membatasi berapa kali pengarahan ulang symlink boleh
// terjadi untuk satu path. Kernel membatasi rantai symlink (40 langkah);
// angka ini membuat rantai/lingkaran symlink berhenti dengan penolakan, bukan
// berputar.
const batasJailSymlink = 32

// bukaAman membuka rel dengan penjagaan BENEATH, dan TETAP mengizinkan
// symlink ABSOLUT yang menunjuk di dalam home.
//
// Kebijakan lama (dan harapan user biasa) adalah: symlink yang menunjuk di
// dalam home boleh, yang menunjuk keluar home ditolak. RESOLVE_BENEATH hanya
// bisa menyatakan separuh pertama: symlink absolut dianggap keluar dari akar
// dan ditolak dengan EXDEV, padahal file manager membuat symlink seperti itu
// dengan santai (`ln -s /home/ani/Dokumen ~/dokumen`) — menolaknya berarti
// seluruh fitur berkas rusak untuk pola yang sah.
//
// Pengarahan ulang di bawah ini TIDAK melemahkan penjagaan. Yang berubah
// hanyalah STRING rel yang diserahkan ke openat2 berikutnya; setiap openat2
// tetap menegakkan RESOLVE_BENEATH, jadi percobaan menukar symlink di tengah
// jalan paling banter mengalihkan operasi ke path lain DI DALAM home. Symlink
// yang menunjuk keluar home ditolak secara leksikal sebelum openat2, dan
// penolakan itu yang dilaporkan (path yang disebut tetap path asli yang
// diminta user).
func (p *penjaga) bukaAman(rel string, flags int, mode uint32) (*os.File, error) {
	var errAwal error
	for sisa := 0; sisa < batasJailSymlink; sisa++ {
		f, err := p.bukaSekali(rel, flags, mode)
		if err == nil {
			return f, nil
		}
		if errAwal == nil {
			errAwal = err
		}
		// O_NOFOLLOW berarti pemanggil memang ingin melihat entri apa adanya
		// (lstat): EXDEV di situ berarti komponen ANTARA yang keluar home, dan
		// itu penolakan.
		if flags&unix.O_NOFOLLOW != 0 || !keluarJail(err) {
			return nil, err
		}
		baru, ok := p.arahkanAbsolut(rel)
		if !ok {
			return nil, errAwal
		}
		rel = baru
	}
	return nil, errAwal
}

// bukaSekali adalah satu panggilan openat2: resolusi dan pemakaian terjadi
// pada operasi yang sama, tanpa nama absolut di antaranya.
func (p *penjaga) bukaSekali(rel string, flags int, mode uint32) (*os.File, error) {
	return p.bukaD(p.fd, rel, p.tampilan(rel), flags, mode)
}

// keluarJail menandai error penolakan karena resolusi keluar dari home.
func keluarJail(err error) bool {
	var he *helperErr
	return errors.As(err, &he) && he.kodeUI == helperproto.ErrSymlinkKeluar
}

// arahkanAbsolut menelusuri rel komponen demi komponen (lewat fd, bukan path)
// untuk menemukan symlink ABSOLUT pertama yang menunjuk di dalam home, lalu
// mengembalikan rel baru dengan komponen itu diganti targetnya. Symlink
// relatif tidak perlu diurus: kernel sudah menyelesaikannya di dalam penjagaan
// BENEATH.
//
// ok=false berarti path ini bukan penjelasan symlink absolut yang sah —
// pemanggil memakai penolakan aslinya.
func (p *penjaga) arahkanAbsolut(rel string) (string, bool) {
	bagian := strings.Split(rel, "/")
	dirfd := p.fd
	tutup := func() {
		if dirfd != p.fd {
			unix.Close(dirfd)
		}
	}
	var prefix []string
	for i, nama := range bagian {
		last := i == len(bagian)-1
		// lstat per komponen: komponen terakhir tidak diikuti, jadi langkah
		// ini tidak pernah EXDEV karena symlink absolut.
		fd, err := unix.Openat2(dirfd, nama, &unix.OpenHow{
			Flags:   unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
		})
		if err != nil {
			tutup()
			return "", false
		}
		var st syscall.Stat_t
		if ferr := syscall.Fstat(fd, &st); ferr != nil {
			unix.Close(fd)
			tutup()
			return "", false
		}
		if st.Mode&syscall.S_IFMT != syscall.S_IFLNK {
			// Bukan symlink: lanjut ke komponen berikutnya lewat fd-nya.
			if last {
				unix.Close(fd)
				tutup()
				return "", false
			}
			if st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
				unix.Close(fd)
				tutup()
				return "", false
			}
			tutup()
			dirfd = fd
			prefix = append(prefix, nama)
			continue
		}
		buf := make([]byte, 4096)
		n, rerr := unix.Readlinkat(dirfd, nama, buf)
		unix.Close(fd)
		if rerr != nil {
			tutup()
			return "", false
		}
		target := filepath.Clean(string(buf[:n]))
		if !filepath.IsAbs(target) {
			// Symlink relatif: kernel yang menyelesaikan, dan BENEATH yang
			// menjaganya. Untuk melanjutkan penelusuran, buka hasilnya lewat
			// openat2 yang sama-sama dijaga.
			if last {
				tutup()
				return "", false
			}
			dfd, derr := unix.Openat2(dirfd, nama, &unix.OpenHow{
				Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
				Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
			})
			if derr != nil {
				tutup()
				return "", false
			}
			tutup()
			dirfd = dfd
			prefix = append(prefix, nama)
			continue
		}
		tutup()
		if !dalamHome(target, p.akar) {
			// Menunjuk keluar home: bukan urusan pengarahan ulang — penolakan
			// aslinya yang berlaku.
			return "", false
		}
		baru := append([]string{}, prefix...)
		if relTarget := strings.TrimPrefix(strings.TrimPrefix(target, p.akar), "/"); relTarget != "" {
			baru = append(baru, relTarget)
		}
		if !last {
			baru = append(baru, bagian[i+1:]...)
		}
		if len(baru) == 0 {
			return ".", true
		}
		return strings.Join(baru, "/"), true
	}
	return "", false
}

// bukaD membuka `name` di dalam direktori dirfd yang SUDAH diresolusi —
// dipakai untuk menelusuri isi direktori tanpa menyusun ulang path lengkapnya.
func (p *penjaga) bukaD(dirfd int, name, tampilan string, flags int, mode uint32) (*os.File, error) {
	how := unix.OpenHow{
		Flags:   uint64(flags) | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	}
	// openat2 menolak Mode != 0 tanpa O_CREAT/O_TMPFILE.
	if flags&os.O_CREATE != 0 {
		how.Mode = uint64(mode) & 0o7777
	}
	fd, err := unix.Openat2(dirfd, name, &how)
	if err != nil {
		return nil, terjemahJail("open", tampilan, err)
	}
	return os.NewFile(uintptr(fd), tampilan), nil
}

// terjemahJail menyeragamkan errno sistem *at menjadi error yang bisa
// diklasifikasi worker: EXDEV dan ELOOP dari openat2 berarti ada komponen yang
// keluar jail (symlink absolut, "..", atau magic link /proc), dan itu
// dilaporkan sebagai penolakan yang sama dengan penolakan parent di
// checkPath — kode + kodeUI yang persis sama, supaya frontend menerjemahkan
// kalimatnya dengan benar.
func terjemahJail(op, tampilan string, err error) error {
	var en unix.Errno
	if errors.As(err, &en) {
		switch en {
		case unix.EXDEV, unix.ELOOP:
			return errJailSymlinkKeluar(tampilan)
		}
		return &os.PathError{Op: op, Path: tampilan, Err: syscall.Errno(en)}
	}
	return err
}

// lstat mengambil metadata entri TANPA mengikuti symlink terakhir (semantik
// os.Lstat). Direktori induknya diresolusi lewat bukaAman, lalu komponen
// terakhir dibuka O_PATH|O_NOFOLLOW relatif ke fd induk itu — jadi symlink
// absolut di dalam home pada komponen ANTARA tetap bekerja, sedangkan
// komponen terakhir tidak pernah diikuti.
func (p *penjaga) lstat(rel string) (os.FileInfo, error) {
	return p.lstatD(p.fd, rel, p.tampilan(rel))
}

// lstatD sama dengan lstat. Kalau dirfd bukan fd home (dipakai penelusuran
// yang sudah menggenggam fd direktori), `name` harus satu komponen.
func (p *penjaga) lstatD(dirfd int, name, tampilan string) (os.FileInfo, error) {
	clean := filepath.Clean(name)
	if clean != "." && dirfd == p.fd && strings.Contains(clean, "/") {
		induk, base, err := p.induk(clean)
		if err != nil {
			return nil, err
		}
		defer induk.Close()
		return p.lstatD(int(induk.Fd()), base, tampilan)
	}
	f, err := p.bukaD(dirfd, clean, tampilan, unix.O_PATH|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// fstat pada fd hasil openat2: inode-nya sudah dipastikan di bawah jail.
	return f.Stat()
}

// stat mengikuti symlink terakhir (semantik os.Stat), tetap di bawah jail.
func (p *penjaga) stat(rel string) (os.FileInfo, error) {
	f, err := p.buka(rel, unix.O_PATH, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// bacaLink membaca target symlink. Komponen terakhir dibaca lewat
// readlinkat(2) relatif ke fd direktori induknya yang sudah diresolusi, jadi
// tidak ada path yang diresolusi ulang dari nama absolut. Targetnya sendiri
// boleh menunjuk keluar home — yang dilarang adalah mengikuti target itu pada
// operasi berkas.
func (p *penjaga) bacaLink(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == "/" {
		return "", &os.PathError{Op: "readlink", Path: p.tampilan(clean), Err: syscall.EINVAL}
	}
	induk, base, err := p.induk(clean)
	if err != nil {
		return "", err
	}
	defer induk.Close()
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(int(induk.Fd()), base, buf)
	if err != nil {
		return "", terjemahJail("readlink", p.tampilan(clean), err)
	}
	return string(buf[:n]), nil
}

// induk memecah rel menjadi fd direktori induk + nama entri terakhir, untuk
// syscall *at. Direktori induk di-resolusi lewat openat2, jadi tidak ada
// komponen nama yang diresolusi dua kali di antara pemeriksaan dan pemakaian.
func (p *penjaga) induk(rel string) (*os.File, string, error) {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "/" {
		// Direktori home terakhir kali dihapus/renama/dipindah oleh parent
		// (root). Worker tidak diberi fd di atas home, dan itulah yang membuat
		// jail-nya berarti.
		return nil, "", errJailAkarHome(p.akar)
	}
	dir, err := p.buka(filepath.Dir(rel), unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, "", err
	}
	return dir, filepath.Base(rel), nil
}

// bisaDibaca memakai faccessat relatif terhadap fd home — semantiknya sama
// dengan access(2) yang dipakai jalur berbasis nama (termasuk mengikuti
// symlink dan menghitung ACL/grup), hanya saja resolusinya tidak lagi lewat
// nama absolut.
func (p *penjaga) bisaDibaca(path string, dir bool) bool {
	rel, err := p.rel(path)
	if err != nil {
		return false
	}
	mode := uint32(akesR)
	if dir {
		mode |= akesX
	}
	return unix.Faccessat(p.fd, rel, mode, 0) == nil
}

// errJailDiLuarHome / errJailSymlinkKeluar: bentuknya sengaja identik dengan
// penolakan parent di authz.go (kode + kodeUI + params yang sama), supaya user
// melihat kalimat yang sama entah penolakannya datang dari parent atau dari
// worker.
func errJailDiLuarHome(path string) error {
	return &helperErr{
		code:   helperproto.ErrDenied,
		msg:    fmt.Sprintf("akses ke %s butuh sudo (di luar home directory)", path),
		kodeUI: helperproto.ErrDiLuarHome,
		params: []string{path},
	}
}

func errJailSymlinkKeluar(path string) error {
	return &helperErr{
		code:   helperproto.ErrDenied,
		msg:    fmt.Sprintf("akses ke %s butuh sudo (symlink menunjuk keluar home directory)", path),
		kodeUI: helperproto.ErrSymlinkKeluar,
		params: []string{path},
	}
}

func errJailAkarHome(path string) error {
	return errDenied("operasi ini tidak diizinkan pada direktori home itu sendiri (%s)", path)
}

func errJailInternal(format string, a ...any) error {
	return &helperErr{code: helperproto.ErrInternal, msg: fmt.Sprintf(format, a...)}
}

// gabungRel menyambung nama entri ke path relatif home.
func gabungRel(rel, name string) string {
	if rel == "." || rel == "" {
		return name
	}
	return rel + "/" + name
}

// ---- operasi berkas lewat deskriptor ----

// resolusiJail mengerjakan op berkas tanpa pernah menyerahkan path absolut ke
// syscall: semuanya relatif terhadap fd direktori home.
type resolusiJail struct{ p *penjaga }

func (r resolusiJail) buka(path string, flags int, mode os.FileMode) (*os.File, error) {
	rel, err := r.p.rel(path)
	if err != nil {
		return nil, err
	}
	return r.p.buka(rel, flags, uint32(mode))
}

func (r resolusiJail) listDir(path string, saring bool) ([]helperproto.FileEntry, error) {
	rel, err := r.p.rel(path)
	if err != nil {
		return nil, err
	}
	d, err := r.p.buka(rel, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	ents, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	// os.ReadDir mengurutkan hasilnya, ReadDir milik *os.File tidak. Urutan
	// listing adalah perilaku lama, jadi dipertahankan.
	urutkanEntri(ents)
	out := make([]helperproto.FileEntry, 0, len(ents))
	for _, e := range ents {
		childRel := gabungRel(rel, e.Name())
		full := filepath.Join(path, e.Name())
		fe, err := r.statEntri(childRel, full)
		if err != nil {
			// Berkas bisa hilang antara ReadDir dan fstat — lewati, jangan
			// gagalkan seluruh listing.
			continue
		}
		if saring && !r.p.bisaDibaca(full, fe.IsDir) {
			continue
		}
		out = append(out, fe)
	}
	return out, nil
}

// statEntri sama dengan statEntry, tetapi ambil metadatanya lewat fd.
func (r resolusiJail) statEntri(rel, path string) (helperproto.FileEntry, error) {
	fi, err := r.p.lstat(rel)
	if err != nil {
		return helperproto.FileEntry{}, err
	}
	e := helperproto.FileEntry{
		Name:    fi.Name(),
		Path:    path,
		IsDir:   fi.IsDir(),
		Size:    fi.Size(),
		Mode:    fi.Mode().String(),
		ModePct: uint32(fi.Mode().Perm()),
		ModTime: fi.ModTime().Unix(),
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if target, err := r.p.bacaLink(rel); err == nil {
			e.Symlink = target
		}
		// Symlink yang menunjuk keluar home tidak punya stat yang sah di
		// dalam jail; IsDir dibiarkan false seperti perilaku lama.
		if st, err := r.p.stat(rel); err == nil {
			e.IsDir = st.IsDir()
		}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.Owner = lookupUID(st.Uid)
		e.Group = lookupGID(st.Gid)
	}
	return e, nil
}

func (r resolusiJail) stat(path string) (helperproto.FileEntry, error) {
	rel, err := r.p.rel(path)
	if err != nil {
		return helperproto.FileEntry{}, err
	}
	// stat yang diminta EKSPLISIT untuk satu path: symlink yang resolusinya
	// keluar home ditolak di sini (parent juga menolaknya di checkPath), bukan
	// dilaporkan sebagai entri biasa. Ini sengaja tidak berlaku di listDir,
	// yang memang harus tetap menampilkan entri symlink apa adanya.
	if fi, err := r.p.lstat(rel); err != nil {
		return helperproto.FileEntry{}, err
	} else if fi.Mode()&os.ModeSymlink != 0 {
		if _, err := r.p.stat(rel); err != nil {
			return helperproto.FileEntry{}, err
		}
	}
	return r.statEntri(rel, path)
}

func (r resolusiJail) bisaDibaca(path string, dir bool) bool { return r.p.bisaDibaca(path, dir) }

func (r resolusiJail) chmod(path string, mode os.FileMode) error {
	rel, err := r.p.rel(path)
	if err != nil {
		return err
	}
	// fchmod(2) tidak menerima fd O_PATH (EBADF), dan fchmodat(nama) akan
	// mengikuti symlink TANPA penjagaan BENEATH. Jembatan /proc/self/fd
	// menunjuk inode yang SUDAH diresolusi openat2 di baris di atas —
	// termasuk symlink terakhirnya, dengan penjagaan — jadi tidak ada
	// resolusi nama milik penyerang yang tersisa di jalur ini.
	f, err := r.p.buka(rel, unix.O_PATH, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.Chmod("/proc/self/fd/"+strconv.Itoa(int(f.Fd())), mode); err != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: err}
	}
	return nil
}

func (r resolusiJail) mkdirAll(path string, mode os.FileMode) error {
	rel, err := r.p.rel(path)
	if err != nil {
		return err
	}
	return r.mkdirAllRel(rel, path, mode)
}

func (r resolusiJail) mkdirAllRel(rel, path string, mode os.FileMode) error {
	// Sudah ada? os.MkdirAll mengembalikan ENOTDIR kalau yang ada bukan
	// direktori, dan nil kalau memang direktori.
	if fi, err := r.p.stat(rel); err == nil {
		if fi.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	}
	perm := uint32(mode) & 0o7777
	cur := "."
	for _, name := range strings.Split(rel, "/") {
		if name == "" || name == "." {
			continue
		}
		dir, err := r.p.buka(cur, unix.O_PATH|unix.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		mkErr := unix.Mkdirat(int(dir.Fd()), name, perm)
		dir.Close()
		// EEXIST bukan kegagalan: komponennya sudah ada (bisa juga symlink ke
		// direktori lain di dalam home — sama seperti MkdirAll dulu).
		if mkErr != nil && !errors.Is(mkErr, unix.EEXIST) {
			return terjemahJail("mkdir", filepath.Join(r.p.akar, cur, name), mkErr)
		}
		cur = gabungRel(cur, name)
	}
	fi, err := r.p.stat(rel)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	}
	return nil
}

func (r resolusiJail) hapus(path string, recursive bool) error {
	rel, err := r.p.rel(path)
	if err != nil {
		return err
	}
	if !recursive {
		return r.hapusSatu(rel)
	}
	return r.hapusAll(rel)
}

// hapusSatu setara os.Remove: coba unlink, dan kalau ditolak karena direktori,
// coba rmdir.
func (r resolusiJail) hapusSatu(rel string) error {
	dir, name, err := r.p.induk(rel)
	if err != nil {
		return err
	}
	defer dir.Close()
	err = unix.Unlinkat(int(dir.Fd()), name, 0)
	if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
		err = unix.Unlinkat(int(dir.Fd()), name, unix.AT_REMOVEDIR)
	}
	if err != nil {
		return terjemahJail("remove", r.p.tampilan(rel), err)
	}
	return nil
}

// hapusAll setara os.RemoveAll: rekursif, symlink dihapus sebagai symlink
// (tidak pernah diikuti), dan path yang sudah tidak ada bukan kegagalan.
func (r resolusiJail) hapusAll(rel string) error {
	fi, err := r.p.lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !fi.IsDir() {
		return r.hapusSatu(rel)
	}
	dir, name, err := r.p.induk(rel)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := r.hapusIsi(int(dir.Fd()), rel, name); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(dir.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return terjemahJail("remove", r.p.tampilan(rel), err)
	}
	return nil
}

// hapusIsi mengosongkan direktori `name` di dalam direktori pdir (fd yang
// sudah diresolusi), lalu menghapus direktori itu sendiri. Isinya ditelusuri
// lewat fd direktori yang baru dibuka, bukan lewat path yang disusun ulang.
func (r resolusiJail) hapusIsi(pdir int, rel, name string) error {
	child, err := r.p.bukaD(pdir, name, r.p.tampilan(rel), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		// os.RemoveAll memberi jalan keluar untuk direktori yang izinnya
		// dicabut pemiliknya sendiri: chmod 0700 lalu buka lagi. Tanpa ini
		// folder ber-mode 000 milik user tidak bisa dihapus dari panel.
		if !errors.Is(err, syscall.EACCES) {
			return err
		}
		if cerr := unix.Fchmodat(pdir, name, 0o700, 0); cerr != nil {
			return err
		}
		if child, err = r.p.bukaD(pdir, name, r.p.tampilan(rel), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0); err != nil {
			return err
		}
	}
	defer child.Close()
	ents, err := child.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, e := range ents {
		isRel := gabungRel(rel, e.Name())
		fi, err := r.p.lstatD(int(child.Fd()), e.Name(), r.p.tampilan(isRel))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if fi.IsDir() {
			// Isinya dikosongkan lebih dulu, baru direktori itu sendiri —
			// pasangan hapusIsi + rmdir, sama seperti RemoveAll.
			if err := r.hapusIsi(int(child.Fd()), isRel, e.Name()); err != nil {
				return err
			}
			if err := unix.Unlinkat(int(child.Fd()), e.Name(), unix.AT_REMOVEDIR); err != nil {
				return terjemahJail("remove", r.p.tampilan(isRel), err)
			}
			continue
		}
		if err := unix.Unlinkat(int(child.Fd()), e.Name(), 0); err != nil {
			return terjemahJail("remove", r.p.tampilan(isRel), err)
		}
	}
	return nil
}

func (r resolusiJail) gantiNama(src, dst string) error {
	srel, err := r.p.rel(src)
	if err != nil {
		return err
	}
	drel, err := r.p.rel(dst)
	if err != nil {
		return err
	}
	sdir, sbase, err := r.p.induk(srel)
	if err != nil {
		return err
	}
	defer sdir.Close()
	ddir, dbase, err := r.p.induk(drel)
	if err != nil {
		return err
	}
	defer ddir.Close()
	if err := unix.Renameat(int(sdir.Fd()), sbase, int(ddir.Fd()), dbase); err != nil {
		// Errno dibiarkan apa adanya (tanpa terjemahJail): pemanggil memakai
		// EXDEV untuk fallback salin+lalu-hapus lintas filesystem.
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.Errno(err.(unix.Errno))}
	}
	return nil
}

func (r resolusiJail) kopi(src, dst string) error {
	srel, err := r.p.rel(src)
	if err != nil {
		return err
	}
	drel, err := r.p.rel(dst)
	if err != nil {
		return err
	}
	return r.kopiRel(srel, drel)
}

// kopiRel menyalin satu entri; isi direktori ditelusuri lewat fd direktori
// sumber. Symlink disalin sebagai symlink (targetnya apa adanya), sama dengan
// copyPath sebelumnya.
func (r resolusiJail) kopiRel(srel, drel string) error {
	fi, err := r.p.lstat(srel)
	if err != nil {
		return err
	}
	switch {
	case fi.IsDir():
		if err := r.mkdirAllRel(drel, r.p.tampilan(drel), fi.Mode().Perm()); err != nil {
			return err
		}
		d, err := r.p.buka(srel, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		ents, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := r.kopiRel(gabungRel(srel, e.Name()), gabungRel(drel, e.Name())); err != nil {
				return err
			}
		}
		return nil
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := r.p.bacaLink(srel)
		if err != nil {
			return err
		}
		dir, base, err := r.p.induk(drel)
		if err != nil {
			return err
		}
		defer dir.Close()
		if err := unix.Symlinkat(target, int(dir.Fd()), base); err != nil {
			return terjemahJail("symlink", r.p.tampilan(drel), err)
		}
		return nil
	}
	in, err := r.p.buka(srel, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := r.p.buka(drel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, uint32(fi.Mode().Perm()))
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ---- penelusuran ----

func (r resolusiJail) usage(path string) (helperproto.UsageHasil, error) {
	var h helperproto.UsageHasil
	rootRel, err := r.p.rel(path)
	if err != nil {
		return h, err
	}
	// Akar penelusuran diperiksa sekali di depan: penolakan jail (mis. akarnya
	// symlink yang menunjuk keluar home) harus sampai ke user, bukan diam-diam
	// menjadi hasil kosong.
	if f, err := r.p.buka(rootRel, unix.O_RDONLY|unix.O_DIRECTORY, 0); err != nil {
		if keluarJail(err) {
			return h, err
		}
	} else {
		f.Close()
	}
	// Jenis filesystem diperiksa lewat fstatfs PADA FD, bukan statfs(path):
	// filesystem semu berukuran nol tapi mahal disusuri (/proc puluhan ribu
	// direktori), persis alasan hitungUsage melewatinya.
	if f, err := r.p.buka(rootRel, unix.O_PATH|unix.O_DIRECTORY, 0); err == nil {
		semu := fsSemuFd(f)
		f.Close()
		if semu {
			return h, nil
		}
	}
	h.Dirs = 1
	// Aturan "jangan melintasi filesystem" (seperti `du -x`) butuh device
	// akar penelusuran; diambil dari lstat fd, sama seperti perangkat() dulu.
	var adaDev bool
	var rootDev uint64
	if fi, err := r.p.lstat(rootRel); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			rootDev, adaDev = uint64(st.Dev), true
		}
	}
	tumpuk := []string{rootRel}
	for len(tumpuk) > 0 {
		rel := tumpuk[len(tumpuk)-1]
		tumpuk = tumpuk[:len(tumpuk)-1]
		f, err := r.p.buka(rel, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			// Direktori tanpa izin baca cukup dilewati — hasilnya ukuran atas
			// apa yang memang terlihat user, bukan error untuk semuanya.
			continue
		}
		// Sama seperti ReadDir milik *os.File di hitungUsage: urutan tidak
		// berguna untuk menjumlah ukuran, jadi tidak diurutkan.
		ents, _ := f.ReadDir(-1)
		dirfd := int(f.Fd())
		for _, e := range ents {
			var st unix.Stat_t
			if err := unix.Fstatat(dirfd, e.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				continue
			}
			switch st.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				if h.Dirs >= maxUsageDirs {
					h.Partial = true
					f.Close()
					return h, nil
				}
				if adaDev && uint64(st.Dev) != rootDev {
					continue
				}
				h.Dirs++
				tumpuk = append(tumpuk, gabungRel(rel, e.Name()))
			case unix.S_IFREG:
				// Blok yang benar-benar dialokasikan, bukan st_size: angka
				// st_size berbohong untuk berkas sparse dan berkas semu di
				// /proc. Sama dengan ruangTerpakai.
				h.Size += st.Blocks * 512
				h.Files++
			}
		}
		f.Close()
	}
	return h, nil
}

func (r resolusiJail) cari(akar, kueri string, saring bool, maks int) (helperproto.SearchHasil, error) {
	out := helperproto.SearchHasil{Hits: []helperproto.SearchHit{}}
	berhenti := func(alasan string) {
		out.Alasan = alasan
		out.Truncated = true
	}
	kueri = batasiKueri(kueri)
	if kueri == "" {
		return out, nil
	}
	if maks <= 0 {
		maks = maksHasilCari
	}
	rootRel, err := r.p.rel(akar)
	if err != nil {
		return out, err
	}
	// Akar penelusuran yang ditolak jail (mis. symlink ke luar home) harus
	// dilaporkan, bukan menjadi hasil kosong yang terlihat seperti "tidak ada
	// yang cocok".
	if f, err := r.p.buka(rootRel, unix.O_RDONLY|unix.O_DIRECTORY, 0); err != nil {
		if keluarJail(err) {
			return out, err
		}
	} else {
		f.Close()
	}
	needle := strings.ToLower(kueri)
	mulai := time.Now()

	// Tiga bentuk path disimpan berdampingan: relHome untuk resolusi fd, abs
	// untuk laporan, dan rel untuk kolom Rel (relatif ke akar pencarian) —
	// yang terakhir harus tetap sama dengan cariRekursif.
	type item struct {
		relHome string
		abs     string
		rel     string
		akar    bool
	}
	tumpuk := []item{{rootRel, akar, "", true}}

	for len(tumpuk) > 0 {
		kini := tumpuk[len(tumpuk)-1]
		tumpuk = tumpuk[:len(tumpuk)-1]

		if out.Dirs >= maksDirCari {
			berhenti(AlasanFolder)
			return out, nil
		}
		// Diperiksa sebelum membaca: yang dilindungi adalah waktu user, dan
		// satu iterasi di mount yang lambat bisa jauh lebih lama dari plafon.
		if time.Since(mulai) > batasWaktuCari {
			berhenti(AlasanWaktu)
			return out, nil
		}
		out.Dirs++

		f, err := r.p.buka(kini.relHome, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			// Folder tanpa izin baca/cari cukup dilewati.
			continue
		}
		ents, _ := f.ReadDir(-1)
		urutkanEntri(ents)
		sistem := dirSistemFd(f, ents)
		if sistem && !kini.akar {
			f.Close()
			continue
		}

		for _, e := range ents {
			childRel := gabungRel(kini.relHome, e.Name())
			abs := filepath.Join(kini.abs, e.Name())
			rel := e.Name()
			if kini.rel != "" {
				rel = kini.rel + "/" + e.Name()
			}
			isDir := e.IsDir()
			if saring && !r.p.bisaDibaca(abs, isDir) {
				continue
			}
			if strings.Contains(strings.ToLower(e.Name()), needle) {
				if len(out.Hits) >= maks {
					berhenti(AlasanHasil)
					f.Close()
					return out, nil
				}
				hit := helperproto.SearchHit{
					Name:  e.Name(),
					Path:  abs,
					Rel:   rel,
					IsDir: isDir,
				}
				// Metadata diambil lewat fd; e.Info() milik *os.File justru
				// lstat lewat nama (parent.name + "/" + name).
				if fi, err := r.p.lstat(childRel); err == nil {
					hit.Size = fi.Size()
					hit.ModTime = fi.ModTime().Unix()
				}
				out.Hits = append(out.Hits, hit)
			}
			// Menurun ke pseudo-filesystem / direktori sistem tidak pernah
			// dilakukan — pemeriksaannya juga memakai fd, bukan nama.
			if isDir && !sistem && !r.p.pseudoFs(childRel) {
				tumpuk = append(tumpuk, item{childRel, abs, rel, false})
			}
		}
		f.Close()
	}
	return out, nil
}

// pseudoFs menandai pseudo-filesystem lewat fd hasil openat2.
func (p *penjaga) pseudoFs(rel string) bool {
	f, err := p.buka(rel, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	return fsSemuFd(f)
}

// fsSemuFd memeriksa jenis filesystem pada fd yang sudah dibuka. Fstatfs
// dipakai, bukan Statfs(path), supaya tidak ada resolusi nama lagi.
func fsSemuFd(f *os.File) bool {
	var st syscall.Statfs_t
	if err := syscall.Fstatfs(int(f.Fd()), &st); err != nil {
		return false
	}
	return fsSemuStat(&st) || int64(st.Type) == devtmpfsMagic
}

// dirSistemFd sama dengan dirSistem, tetapi jenis filesystem diperiksa lewat
// fd direktori yang sedang dibuka.
func dirSistemFd(f *os.File, ents []os.DirEntry) bool {
	if fsSemuFd(f) {
		return true
	}
	for _, e := range ents {
		if e.Type()&os.ModeDevice != 0 {
			return true
		}
	}
	return false
}
