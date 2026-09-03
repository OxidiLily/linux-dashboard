package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// WorkerArg adalah argv[1] yang menandakan proses ini adalah worker anak.
const WorkerArg = "__worker"

// Kenapa fork worker, bukan syscall.Setuid di daemon:
// Setuid di Go berlaku untuk seluruh proses, jadi daemon yang melayani banyak
// request paralel tidak boleh mengubah uid-nya sendiri. Fork anak dengan
// SysProcAttr.Credential membuat kernel yang menegakkan izin — daemon tidak
// perlu meniru logika permission Unix (ACL, supplementary group, sticky bit)
// yang gampang salah kalau ditulis ulang.

type workerOp struct {
	Op        string `json:"op"`
	Path      string `json:"path,omitempty"`
	Dest      string `json:"dest,omitempty"`
	Mode      uint32 `json:"mode,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	// SaringAkses menyembunyikan entri yang tidak bisa dibaca user ini.
	// Dipakai untuk semua listing non-sudo (mis. folder yang di-share Samba
	// atau home yang izinnya longgar): nama file milik orang lain bukan
	// urusan yang membukanya.
	SaringAkses bool `json:"saring_akses,omitempty"`
	Append      bool `json:"append,omitempty"`
	PID         int  `json:"pid,omitempty"`
	Signal      int  `json:"signal,omitempty"`
	// Offset/Length dipakai op "read" untuk melayani HTTP Range. Length 0 =
	// sampai akhir berkas.
	Offset int64 `json:"offset,omitempty"`
	Length int64 `json:"length,omitempty"`
}

type workerResult struct {
	OK    bool            `json:"ok"`
	Code  string          `json:"code,omitempty"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// RunWorker dijalankan di proses anak (setelah kernel menurunkan privilege ke
// user target). fd 3 = op masuk, fd 4 = hasil keluar, stdin/stdout = data.
func RunWorker() int {
	opFile := os.NewFile(3, "op")
	resFile := os.NewFile(4, "res")
	if opFile == nil || resFile == nil {
		fmt.Fprintln(os.Stderr, "worker: fd 3/4 tidak tersedia")
		return 2
	}
	defer resFile.Close()

	var op workerOp
	if err := json.NewDecoder(opFile).Decode(&op); err != nil {
		writeResult(resFile, workerResult{Code: helperproto.ErrInternal, Error: err.Error()})
		return 2
	}
	opFile.Close()

	data, err := execWorkerOp(op, resFile)
	var done streamDone
	if errors.As(err, &done) {
		// Result sudah dikirim sebelum data mengalir — jangan kirim dua kali.
		if done.err != nil {
			fmt.Fprintln(os.Stderr, "worker: stream terputus:", done.err)
			return 1
		}
		return 0
	}
	if err != nil {
		writeResult(resFile, workerResult{Code: classify(err), Error: err.Error()})
		return 1
	}
	writeResult(resFile, workerResult{OK: true, Data: data})
	return 0
}

func writeResult(w io.Writer, r workerResult) {
	if r.Data == nil {
		r.Data = json.RawMessage("null")
	}
	_ = json.NewEncoder(w).Encode(r)
}

func classify(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return helperproto.ErrNotFound
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return helperproto.ErrDenied
	default:
		return helperproto.ErrInternal
	}
}

func execWorkerOp(op workerOp, res *os.File) (json.RawMessage, error) {
	switch op.Op {
	case "list":
		entries, err := listDir(op.Path, op.SaringAkses)
		if err != nil {
			return nil, err
		}
		return json.Marshal(entries)
	case "usage":
		return json.Marshal(hitungUsage(op.Path))
	// Dipakai handleFileRead sebelum streaming: ukuran + apakah direktori.
	// (Command `file.stat` sendiri sudah tidak ada; op ini murni internal.)
	case "stat":
		e, err := statEntry(op.Path)
		if err != nil {
			return nil, err
		}
		// Unduh/preview memakai hasil stat sebagai header, lalu byte-nya
		// mengalir setelah response OK terkirim. Kalau izin baca baru ketahuan
		// gagal saat streaming, user menerima HTTP 200 dengan isi KOSONG —
		// berkas kosong yang terlihat sah. Jadi izinnya diperiksa di sini,
		// selagi error masih bisa dilaporkan.
		if op.SaringAkses && !bisaDibaca(op.Path, e.IsDir) {
			return nil, &os.PathError{Op: "open", Path: op.Path, Err: syscall.EACCES}
		}
		return json.Marshal(e)
	case "mkdir":
		mode := os.FileMode(0o755)
		if op.Mode != 0 {
			mode = os.FileMode(op.Mode)
		}
		return nil, os.MkdirAll(op.Path, mode)
	case "remove":
		if op.Recursive {
			return nil, os.RemoveAll(op.Path)
		}
		return nil, os.Remove(op.Path)
	case "rename", "move":
		if err := os.Rename(op.Path, op.Dest); err != nil {
			// Rename lintas filesystem gagal dengan EXDEV — fallback copy+hapus.
			var le *os.LinkError
			if errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV) {
				if err := copyPath(op.Path, op.Dest); err != nil {
					return nil, err
				}
				return nil, os.RemoveAll(op.Path)
			}
			return nil, err
		}
		return nil, nil
	case "copy":
		return nil, copyPath(op.Path, op.Dest)
	case "chmod":
		return nil, os.Chmod(op.Path, os.FileMode(op.Mode))
	case "read":
		f, err := os.Open(op.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			return nil, fmt.Errorf("%s adalah direktori", op.Path)
		}
		meta, _ := json.Marshal(map[string]any{"size": st.Size()})
		// Hasil dikirim duluan supaya parent tahu ukuran, baru data mengalir.
		writeResult(res, workerResult{OK: true, Data: meta})
		// Offset di luar ukuran berkas bukan error: parent sudah memvalidasi
		// rentangnya terhadap ukuran hasil stat, dan Seek melewati EOF hanya
		// menghasilkan nol byte. Menjawab dengan badan kosong lebih baik
		// daripada memutus koneksi di tengah respons HTTP yang header-nya
		// sudah terkirim.
		if op.Offset > 0 {
			if _, err := f.Seek(op.Offset, io.SeekStart); err != nil {
				return nil, streamDone{err}
			}
		}
		if op.Length > 0 {
			_, err = io.CopyN(os.Stdout, f, op.Length)
			if errors.Is(err, io.EOF) {
				err = nil // berkas menyusut setelah stat — bukan kegagalan worker
			}
		} else {
			_, err = io.Copy(os.Stdout, f)
		}
		return nil, streamDone{err}
	case "write":
		flags := os.O_CREATE | os.O_WRONLY
		if op.Append {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		mode := os.FileMode(0o644)
		if op.Mode != 0 {
			mode = os.FileMode(op.Mode)
		}
		f, err := os.OpenFile(op.Path, flags, mode)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		writeResult(res, workerResult{OK: true})
		_, err = io.Copy(f, os.Stdin)
		return nil, streamDone{err}
	case "kill":
		// Kernel yang menegakkan "hanya boleh proses sendiri" — worker sudah
		// berjalan sebagai user yang login, jadi tidak ada cek manual di sini.
		return nil, syscall.Kill(op.PID, syscall.Signal(op.Signal))
	}
	return nil, fmt.Errorf("op tidak dikenal: %s", op.Op)
}

// streamDone menandai stream selesai; parent sudah menerima result awal
// jadi tidak boleh ada result kedua.
type streamDone struct{ err error }

func (s streamDone) Error() string { return "stream selesai" }

// maxUsageDirs membatasi jumlah direktori per penelusuran. Tanpa batas ini,
// menghitung ukuran "/" berarti menyusuri seluruh filesystem dan worker tidak
// pernah selesai.
const maxUsageDirs = 50000

// hitungUsage menjumlahkan ukuran isi satu direktori — setara `du -xb`.
// Seluruh penelusuran terjadi di dalam SATU worker: memanggil helper per
// direktori berarti satu fork+exec per direktori, dan folder dengan puluhan
// ribu subdirektori (mis. /DATA, /usr) butuh semenit hanya untuk spawn.
//
// Lintas filesystem sengaja tidak diikuti, seperti `du -x`: tanpa itu
// menghitung "/" ikut menyusuri /proc dan /sys yang ukurannya semu dan
// jumlah direktorinya puluhan ribu.
func hitungUsage(root string) helperproto.UsageHasil {
	var h helperproto.UsageHasil
	if fsSemu(root) {
		return h
	}
	rootDev, adaDev := perangkat(root)
	h.Dirs = 1
	tumpuk := []string{root}
	for len(tumpuk) > 0 {
		dir := tumpuk[len(tumpuk)-1]
		tumpuk = tumpuk[:len(tumpuk)-1]
		f, err := os.Open(dir)
		if err != nil {
			// Direktori tanpa izin baca cukup dilewati — hasilnya jadi ukuran
			// atas apa yang memang terlihat user, bukan error untuk semuanya.
			continue
		}
		// ReadDir milik *os.File TIDAK mengurutkan hasilnya, berbeda dari
		// os.ReadDir dan filepath.WalkDir. Untuk menjumlah ukuran, urutan tidak
		// ada gunanya — dan mengurutkan hampir 100 ribu entri memakan sekitar
		// sepertiga waktu penelusuran (/DATA di mesin uji: 3,0 s → 2,2 s).
		ents, _ := f.ReadDir(-1)
		f.Close()
		for _, e := range ents {
			nama := dir + "/" + e.Name()
			if e.IsDir() {
				if h.Dirs >= maxUsageDirs {
					h.Partial = true
					return h
				}
				// Lintas filesystem tidak diikuti, seperti `du -x`.
				if adaDev {
					if dev, ok := perangkat(nama); !ok || dev != rootDev {
						continue
					}
				}
				h.Dirs++
				tumpuk = append(tumpuk, nama)
				continue
			}
			if !e.Type().IsRegular() {
				// Symlink, socket, dan device node tidak punya ukuran isi yang
				// berarti; menghitungnya membuat total menyesatkan.
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			h.Size += ruangTerpakai(fi)
			h.Files++
		}
	}
	return h
}

// fsSemu menandai filesystem yang isinya dibangkitkan kernel, bukan disimpan
// di disk: ukurannya memang nol, tapi menyusurinya mahal — /proc di mesin uji
// butuh 2,8 detik hanya untuk sampai pada jawaban 0. Dicek sekali di akar
// penelusuran; sublevelnya sudah tertutup aturan tidak melintasi filesystem.
func fsSemu(path string) bool {
	// Nilai magic dari statfs(2); daftarnya bagian dari ABI kernel.
	const (
		procMagic       = 0x9fa0
		sysfsMagic      = 0x62656572
		debugfsMagic    = 0x64626720
		tracefsMagic    = 0x74726163
		cgroupMagic     = 0x27e0eb
		cgroup2Magic    = 0x63677270
		securityfsMagic = 0x73636673
		devptsMagic     = 0x1cd1
		bpfMagic        = 0xcafe4a11
	)
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	switch int64(st.Type) {
	case procMagic, sysfsMagic, debugfsMagic, tracefsMagic,
		cgroupMagic, cgroup2Magic, securityfsMagic, devptsMagic, bpfMagic:
		return true
	}
	return false
}

// ruangTerpakai memakai blok yang benar-benar dialokasikan, bukan st_size,
// persis seperti `du`. st_size berbohong untuk berkas sparse dan untuk berkas
// semu di /proc: /proc/kcore mengaku 128 TB padahal nol blok, sehingga
// menjumlahkan st_size membuat folder proc tampil 128 TB.
func ruangTerpakai(fi os.FileInfo) int64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return st.Blocks * 512
}

func perangkat(path string) (uint64, bool) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

func listDir(path string, saring bool) ([]helperproto.FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]helperproto.FileEntry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(path, e.Name())
		fe, err := statEntry(full)
		if err != nil {
			// File bisa hilang antara ReadDir dan Lstat — lewati, jangan gagalkan
			// seluruh listing.
			continue
		}
		if saring && !bisaDibaca(full, fe.IsDir) {
			continue
		}
		out = append(out, fe)
	}
	return out, nil
}

// Konstanta access(2) tidak diekspor paket syscall di Linux; nilainya bagian
// dari ABI kernel (POSIX), sama untuk semua arsitektur Linux.
const (
	akesR = 0x4 // R_OK
	akesX = 0x1 // X_OK
)

// bisaDibaca memakai access(2), bukan membandingkan mode dengan UID sendiri:
// hasilnya sudah memperhitungkan seluruh keanggotaan grup dan ACL, dan proses
// ini memang berjalan sebagai user yang bersangkutan (real UID = user itu),
// yang justru merupakan syarat access(2).
//
// Direktori butuh x juga — tanpa itu isinya tidak bisa dibuka, jadi
// menampilkannya cuma memberi pintu yang pasti tertutup.
func bisaDibaca(path string, dir bool) bool {
	mode := uint32(akesR)
	if dir {
		mode |= akesX
	}
	return syscall.Access(path, mode) == nil
}

func statEntry(path string) (helperproto.FileEntry, error) {
	fi, err := os.Lstat(path)
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
		if target, err := os.Readlink(path); err == nil {
			e.Symlink = target
		}
		if st, err := os.Stat(path); err == nil {
			e.IsDir = st.IsDir()
		}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.Owner = lookupUID(st.Uid)
		e.Group = lookupGID(st.Gid)
	}
	return e, nil
}

func lookupUID(uid uint32) string {
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		return u.Username
	}
	return strconv.FormatUint(uint64(uid), 10)
}

func lookupGID(gid uint32) string {
	if g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10)); err == nil {
		return g.Name
	}
	return strconv.FormatUint(uint64(gid), 10)
}

func copyPath(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ---- sisi parent (daemon root) ----

// runAsUser menjalankan satu op sebagai user target dan menunggu hasilnya.
// stdinData/stdoutW dipakai untuk op stream (read/write).
func runAsUser(u *userInfo, op workerOp, stdin io.Reader, stdout io.Writer) (json.RawMessage, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	opR, opW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer opW.Close()
	resR, resW, err := os.Pipe()
	if err != nil {
		opR.Close()
		return nil, err
	}
	defer resR.Close()

	cmd := exec.Command(self, WorkerArg)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + u.Home}
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{opR, resW}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}
	cmd.Stderr = os.Stderr
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}

	if err := cmd.Start(); err != nil {
		opR.Close()
		resW.Close()
		return nil, err
	}
	opR.Close()
	resW.Close()

	if err := json.NewEncoder(opW).Encode(op); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	opW.Close()

	var res workerResult
	decErr := json.NewDecoder(resR).Decode(&res)
	waitErr := cmd.Wait()
	if decErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("worker gagal: %w", waitErr)
		}
		return nil, fmt.Errorf("worker tidak mengirim hasil: %w", decErr)
	}
	if !res.OK {
		return nil, &helperErr{code: res.Code, msg: res.Error}
	}
	// Op stream mengirim result OK sebelum datanya mengalir, jadi kegagalan di
	// tengah penyalinan (disk penuh, kuota habis) hanya terlihat dari exit code
	// worker. Tanpa cek ini penulisan yang terpotong dilaporkan sukses.
	if waitErr != nil {
		return nil, &helperErr{code: helperproto.ErrInternal, msg: "penulisan terputus di tengah jalan: " + waitErr.Error()}
	}
	return res.Data, nil
}

type helperErr struct {
	code string
	msg  string
	// kodeUI + params dipakai frontend untuk menyusun kalimatnya sendiri;
	// msg tetap kalimat bahasa Indonesia sebagai cadangan.
	kodeUI string
	params []string
}

func (e *helperErr) Error() string    { return e.msg }
func (e *helperErr) Code() string     { return e.code }
func (e *helperErr) Params() []string { return e.params }
