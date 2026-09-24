package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// TestMain membuat binary test ini bisa dipakai sebagai worker: parent
// (runAsUser) menjalankan ulang binary helper dengan argv[1] = __worker.
// Test di berkas ini meniru pemanggilan itu PERSIS — termasuk fd protokol —
// supaya yang diuji adalah worker yang benar-benar berjalan di produksi,
// bukan salinan logikanya.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == WorkerArg {
		os.Exit(RunWorker())
	}
	os.Exit(m.Run())
}

// jalankanWorker menjalankan satu op worker sebagai proses terpisah, sama
// seperti runAsUser: fd 3 = op masuk, fd 4 = hasil keluar, fd 5 = direktori
// home (jail). jailHome "" berarti tanpa jail — itulah jalur sudo.
func jalankanWorker(t *testing.T, jailHome string, op workerOp, stdin []byte) (workerResult, []byte) {
	t.Helper()
	res, out, err := cobaJalankanWorker(jailHome, op, stdin)
	if err != nil {
		t.Fatalf("worker tidak bisa dijalankan: %v", err)
	}
	return res, out
}

// cobaJalankanWorker tidak menggagalkan test; dipakai test yang memang
// mengharapkan worker menolak sebelum mengirim hasil apa pun.
func cobaJalankanWorker(jailHome string, op workerOp, stdin []byte) (workerResult, []byte, error) {
	opR, opW, err := os.Pipe()
	if err != nil {
		return workerResult{}, nil, err
	}
	defer opW.Close()
	resR, resW, err := os.Pipe()
	if err != nil {
		opR.Close()
		return workerResult{}, nil, err
	}
	defer resR.Close()

	files := []*os.File{opR, resW}
	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	var jail *os.File
	if jailHome != "" {
		fd, err := unix.Open(jailHome, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			opR.Close()
			resW.Close()
			return workerResult{}, nil, err
		}
		jail = os.NewFile(uintptr(fd), jailHome)
		files = append(files, jail)
		env = append(env, modeEnv+"="+modeJail, jailHomeEnv+"="+jailHome)
	} else {
		env = append(env, modeEnv+"="+modeSudo)
	}

	cmd := exec.Command(os.Args[0], WorkerArg)
	cmd.Env = env
	cmd.ExtraFiles = files
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Start(); err != nil {
		opR.Close()
		resW.Close()
		if jail != nil {
			jail.Close()
		}
		return workerResult{}, nil, err
	}
	opR.Close()
	resW.Close()
	if jail != nil {
		jail.Close()
	}

	if err := json.NewEncoder(opW).Encode(op); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return workerResult{}, nil, err
	}
	opW.Close()

	var res workerResult
	decErr := json.NewDecoder(resR).Decode(&res)
	// Exit code worker 1 berarti op gagal — hasilnya sudah dikirim lewat fd 4
	// dan itulah yang dibaca test, sama seperti runAsUser di produksi. Yang
	// menggagalkan test hanyalah worker yang tidak mengirim hasil apa pun.
	_ = cmd.Wait()
	return res, stdout.Bytes(), decErr
}

// siapkanHome membuat pohon uji:
//
//	home/
//	  catatan.txt        berkas biasa
//	  sub/               direktori biasa
//	  dalam.txt          symlink -> home/catatan.txt      (tetap di dalam home)
//	  sub-alias          symlink -> sub                   (tetap di dalam home)
//	  keluar.txt         symlink -> luar/rahasia.txt      (keluar home)
//	  keluar-dir         symlink -> luar/folder           (keluar home)
//	  bertingkat.txt     symlink -> home/keluar.txt -> luar/rahasia.txt
//
// Semua symlink boleh ADA di dalam home; yang tidak boleh adalah operasi yang
// menembusnya lewat resolusi nama.
func siapkanHome(t *testing.T) (home, luar string) {
	t.Helper()
	home = t.TempDir()
	luar = t.TempDir()
	for _, d := range []string{home, luar} {
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tulis := func(p, isi string) {
		if err := os.WriteFile(p, []byte(isi), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tulis(filepath.Join(home, "catatan.txt"), "isi dalam home")
	tulis(filepath.Join(home, "sub", "berkas-sub.txt"), "isi sub")
	tulis(filepath.Join(luar, "rahasia.txt"), "isi luar home")
	tulis(filepath.Join(luar, "sub", "berkas.txt"), "isi luar")
	link := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
			t.Skipf("symlink tidak didukung: %v", err)
		}
	}
	link(filepath.Join(home, "catatan.txt"), "dalam.txt")
	link("sub", "sub-alias")
	link(filepath.Join(luar, "rahasia.txt"), "keluar.txt")
	link(filepath.Join(luar, "sub"), "keluar-dir")
	link(filepath.Join(home, "keluar.txt"), "bertingkat.txt")
	return home, luar
}

func opWorker(t *testing.T, jailHome string, op workerOp, stdin []byte) workerResult {
	t.Helper()
	res, _ := jalankanWorker(t, jailHome, op, stdin)
	return res
}

// (1) Operasi di dalam home harus tetap berjalan — perbaikan tidak boleh
// mematikan fungsinya.
func TestWorkerJailPathDiDalamHomeLolos(t *testing.T) {
	home, _ := siapkanHome(t)

	res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: filepath.Join(home, "catatan.txt")}, nil)
	if !res.OK {
		t.Fatalf("baca berkas di dalam home harus lolos, dapat %+v", res)
	}
	if string(out) != "isi dalam home" {
		t.Fatalf("isi berkas salah: %q", out)
	}

	res, _ = jalankanWorker(t, home, workerOp{Op: "list", Path: home}, nil)
	if !res.OK {
		t.Fatalf("daftar home harus lolos, dapat %+v", res)
	}
	var entries []helperproto.FileEntry
	if err := json.Unmarshal(res.Data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("daftar home tidak boleh kosong")
	}

	if res := opWorker(t, home, workerOp{Op: "mkdir", Path: filepath.Join(home, "baru", "dalam")}, nil); !res.OK {
		t.Fatalf("mkdir di dalam home harus lolos, dapat %+v", res)
	}
	if res, _ := jalankanWorker(t, home, workerOp{Op: "write", Path: filepath.Join(home, "baru", "berkas.txt")}, []byte("halo")); !res.OK {
		t.Fatalf("tulis di dalam home harus lolos, dapat %+v", res)
	}
	if isi, err := os.ReadFile(filepath.Join(home, "baru", "berkas.txt")); err != nil || string(isi) != "halo" {
		t.Fatalf("berkas hasil tulis salah: %q %v", isi, err)
	}
	if res := opWorker(t, home, workerOp{Op: "search", Path: home, Query: "berkas", SaringAkses: true}, nil); !res.OK {
		t.Fatalf("pencarian di dalam home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "usage", Path: home}, nil); !res.OK {
		t.Fatalf("usage home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "rename", Path: filepath.Join(home, "catatan.txt"), Dest: filepath.Join(home, "sub", "pindah.txt")}, nil); !res.OK {
		t.Fatalf("rename di dalam home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "copy", Path: filepath.Join(home, "sub", "pindah.txt"), Dest: filepath.Join(home, "salin.txt")}, nil); !res.OK {
		t.Fatalf("copy di dalam home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "remove", Path: filepath.Join(home, "salin.txt"), Recursive: true}, nil); !res.OK {
		t.Fatalf("hapus di dalam home harus lolos, dapat %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "salin.txt")); !os.IsNotExist(err) {
		t.Fatal("berkas yang dihapus masih ada")
	}
}

// (4) Symlink yang tetap menunjuk DI DALAM home adalah kebijakan lama yang
// harus tetap jalan: RESOLVE_NO_SYMLINKS tidak boleh dipakai.
func TestWorkerJailSymlinkDiDalamHomeTetapBoleh(t *testing.T) {
	home, _ := siapkanHome(t)

	res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: filepath.Join(home, "dalam.txt")}, nil)
	if !res.OK {
		t.Fatalf("symlink yang menunjuk di dalam home harus tetap bisa dibaca, dapat %+v", res)
	}
	if string(out) != "isi dalam home" {
		t.Fatalf("isi berkas lewat symlink salah: %q", out)
	}

	// Symlink relatif ke direktori di dalam home: membuka daftarnya harus boleh.
	res, _ = jalankanWorker(t, home, workerOp{Op: "list", Path: filepath.Join(home, "sub-alias")}, nil)
	if !res.OK {
		t.Fatalf("daftar lewat symlink direktori di dalam home harus boleh, dapat %+v", res)
	}

	// Menulis lewat symlink yang menunjuk di dalam home juga sah — targetnya
	// berkas milik user itu sendiri di dalam home.
	if res := opWorker(t, home, workerOp{Op: "write", Path: filepath.Join(home, "dalam.txt")}, []byte("lewat symlink")); !res.OK {
		t.Fatalf("tulis lewat symlink di dalam home harus boleh, dapat %+v", res)
	}
	if isi, err := os.ReadFile(filepath.Join(home, "catatan.txt")); err != nil || string(isi) != "lewat symlink" {
		t.Fatalf("isi berkas target salah: %q %v", isi, err)
	}
}

// (2) Symlink yang menunjuk keluar home harus DITOLAK untuk operasi apa pun,
// termasuk penulisan — inilah kerentanan TOCTOU yang sedang ditutup.
func TestWorkerJailTolakSymlinkKeluarHome(t *testing.T) {
	home, luar := siapkanHome(t)

	kasus := []struct {
		nama string
		op   workerOp
	}{
		{"read lewat symlink", workerOp{Op: "read", Path: filepath.Join(home, "keluar.txt")}},
		{"read lewat symlink bertingkat", workerOp{Op: "read", Path: filepath.Join(home, "bertingkat.txt")}},
		{"write lewat symlink", workerOp{Op: "write", Path: filepath.Join(home, "keluar.txt")}},
		{"stat lewat symlink", workerOp{Op: "stat", Path: filepath.Join(home, "keluar.txt"), SaringAkses: true}},
		{"list lewat symlink direktori", workerOp{Op: "list", Path: filepath.Join(home, "keluar-dir")}},
		{"search lewat symlink direktori", workerOp{Op: "search", Path: filepath.Join(home, "keluar-dir"), Query: "berkas"}},
		{"usage lewat symlink direktori", workerOp{Op: "usage", Path: filepath.Join(home, "keluar-dir")}},
		{"mkdir lewat symlink direktori", workerOp{Op: "mkdir", Path: filepath.Join(home, "keluar-dir", "baru")}},
		{"remove lewat symlink direktori", workerOp{Op: "remove", Path: filepath.Join(home, "keluar-dir", "berkas.txt"), Recursive: true}},
		{"rename menembus symlink direktori", workerOp{Op: "rename", Path: filepath.Join(home, "catatan.txt"), Dest: filepath.Join(home, "keluar-dir", "pindah.txt")}},
		{"copy menembus symlink direktori", workerOp{Op: "copy", Path: filepath.Join(home, "catatan.txt"), Dest: filepath.Join(home, "keluar-dir", "salin.txt")}},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			res, _ := jalankanWorker(t, home, k.op, []byte("ditulis dari dalam home"))
			if res.OK {
				t.Fatalf("operasi %q seharusnya ditolak, bukan berhasil", k.nama)
			}
			if res.Code != helperproto.ErrDenied {
				t.Fatalf("kode error harus %q, dapat %q (%s)", helperproto.ErrDenied, res.Code, res.Error)
			}
		})
	}

	// Bukti bahwa berkas di luar home tidak tersentuh sama sekali.
	if isi, err := os.ReadFile(filepath.Join(luar, "rahasia.txt")); err != nil || string(isi) != "isi luar home" {
		t.Fatalf("berkas di luar home berubah: %q %v", isi, err)
	}
	if _, err := os.Stat(filepath.Join(luar, "sub", "salin.txt")); err == nil {
		t.Fatal("copy berhasil menembus home")
	}
	if _, err := os.Stat(filepath.Join(luar, "sub", "pindah.txt")); err == nil {
		t.Fatal("rename berhasil menembus home")
	}
	if _, err := os.Stat(filepath.Join(luar, "sub", "berkas.txt")); err != nil {
		t.Fatal("berkas di luar home terhapus")
	}
}

// (3) Escape lewat path absolut dan lewat ".." juga harus ditolak, walau
// checkPath di parent sudah menolaknya lebih dulu — worker adalah penegak
// terakhir dan tidak boleh bergantung pada urutan pemeriksaan itu.
func TestWorkerJailTolakPathAbsolutDanTitikTitik(t *testing.T) {
	home, luar := siapkanHome(t)

	kasus := []struct {
		nama string
		op   workerOp
	}{
		{"read path absolut di luar home", workerOp{Op: "read", Path: filepath.Join(luar, "rahasia.txt")}},
		{"write path absolut di luar home", workerOp{Op: "write", Path: filepath.Join(luar, "tulis.txt")}},
		{"mkdir path absolut di luar home", workerOp{Op: "mkdir", Path: filepath.Join(luar, "dir-baru")}},
		{"remove path absolut di luar home", workerOp{Op: "remove", Path: filepath.Join(luar, "sub"), Recursive: true}},
		{"list path absolut di luar home", workerOp{Op: "list", Path: luar}},
		{"read lewat titik-titik", workerOp{Op: "read", Path: filepath.Join(home, "..", filepath.Base(luar), "rahasia.txt")}},
		{"write lewat titik-titik", workerOp{Op: "write", Path: filepath.Join(home, "sub", "..", "..", filepath.Base(luar), "tembus.txt")}},
		{"read di atas home", workerOp{Op: "read", Path: filepath.Dir(home)}},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			res, _ := jalankanWorker(t, home, k.op, []byte("ditulis dari dalam home"))
			if res.OK {
				t.Fatalf("operasi %q seharusnya ditolak, bukan berhasil", k.nama)
			}
			if res.Code != helperproto.ErrDenied {
				t.Fatalf("kode error harus %q, dapat %q (%s)", helperproto.ErrDenied, res.Code, res.Error)
			}
		})
	}

	if isi, err := os.ReadFile(filepath.Join(luar, "rahasia.txt")); err != nil || string(isi) != "isi luar home" {
		t.Fatalf("berkas di luar home berubah: %q %v", isi, err)
	}
	if _, err := os.Stat(filepath.Join(luar, "tulis.txt")); err == nil {
		t.Fatal("tulis berhasil menembus home")
	}
	if _, err := os.Stat(filepath.Join(luar, "sub")); err != nil {
		t.Fatal("direktori di luar home terhapus")
	}
}

// Tanpa jail (jalur sudo) perilaku lama harus utuh: worker tetap boleh
// menyentuh path di luar home, karena itulah yang dilakukan sudoer.
func TestWorkerTanpaJailTetapBisaPathLuarHome(t *testing.T) {
	_, luar := siapkanHome(t)

	res, out := jalankanWorker(t, "", workerOp{Op: "read", Path: filepath.Join(luar, "rahasia.txt")}, nil)
	if !res.OK {
		t.Fatalf("jalur tanpa jail (sudo) harus tetap berjalan, dapat %+v", res)
	}
	if string(out) != "isi luar home" {
		t.Fatalf("isi berkas salah: %q", out)
	}
	if res := opWorker(t, "", workerOp{Op: "write", Path: filepath.Join(luar, "tulis-sudo.txt")}, []byte("sudo")); !res.OK {
		t.Fatalf("tulis tanpa jail harus tetap berjalan, dapat %+v", res)
	}
}

// ---- satuan: resolver & penjaga ----

// Mode sudo = resolusi berbasis nama tanpa jail. Itulah yang membuat jalur
// sudo tetap utuh (path-nya memang di luar home).
func TestPenjagaWorkerModeSudoTanpaJail(t *testing.T) {
	t.Setenv(modeEnv, modeSudo)
	t.Setenv(jailHomeEnv, "")
	p, err := penjagaWorker()
	if err != nil {
		t.Fatalf("mode sudo tidak boleh error: %v", err)
	}
	if p != nil {
		t.Fatal("mode sudo tidak boleh menghasilkan jail")
	}
}

// Mode yang tidak dikirim adalah kegagalan, bukan "anggap saja jalur sudo":
// situs spawn baru yang lupa menyatakan kontraknya akan tampak seperti jalur
// sudo padahal op-nya milik user non-sudo — penjagaan hilang tanpa suara.
func TestPenjagaWorkerTanpaModeGagalKeras(t *testing.T) {
	t.Setenv(modeEnv, "")
	t.Setenv(jailHomeEnv, "")
	if _, err := penjagaWorker(); err == nil {
		t.Fatal("mode yang tidak dikirim harus gagal")
	}

	t.Setenv(modeEnv, "mode-karangan")
	if _, err := penjagaWorker(); err == nil {
		t.Fatal("mode yang tidak dikenal harus gagal")
	}
}

// Mode sudo tidak boleh membawa penanda jail: dua kontrak yang saling
// bertentangan berarti parent dan worker tidak sepakat soal penjagaan.
func TestPenjagaWorkerModeSudoDenganPenandaJailGagal(t *testing.T) {
	t.Setenv(modeEnv, modeSudo)
	t.Setenv(jailHomeEnv, t.TempDir())
	if _, err := penjagaWorker(); err == nil {
		t.Fatal("mode sudo + penanda jail harus gagal")
	}
}

// Penanda tanpa fd 5 adalah kegagalan, bukan fallback diam-diam ke resolusi
// berbasis nama: menjalankannya berarti membatalkan penjagaan tanpa suara.
func TestPenjagaWorkerPenandaTanpaFdGagal(t *testing.T) {
	var st syscall.Stat_t
	if err := syscall.Fstat(fdJail, &st); err == nil {
		t.Skipf("fd %d kebetulan terbuka di proses test", fdJail)
	}
	t.Setenv(modeEnv, modeJail)
	t.Setenv(jailHomeEnv, t.TempDir())
	if _, err := penjagaWorker(); err == nil {
		t.Fatal("penanda jail tanpa fd harus gagal")
	}
}

// Akar jail "/" ditolak: RESOLVE_BENEATH di atas "/" tidak menjepit apa pun,
// jadi itu bukan jail.
func TestPenjagaWorkerTolakAkarRoot(t *testing.T) {
	t.Setenv(modeEnv, modeJail)
	t.Setenv(jailHomeEnv, "/")
	if _, err := penjagaWorker(); err == nil {
		t.Fatal("akar jail \"/\" harus ditolak")
	}
}

// Path relatif ditolak sebelum sampai ke openat2: worker hanya menerima path
// absolut yang sudah dibersihkan parent.
func TestPenjagaTolakPathRelatif(t *testing.T) {
	home, _ := siapkanHome(t)
	p := &penjaga{fd: fdJail, akar: home}
	if _, err := p.rel("catatan.txt"); err == nil {
		t.Fatal("path relatif harus ditolak")
	}
	if rel, err := p.rel(home); err != nil || rel != "." {
		t.Fatalf("akar jail harus menjadi %q, dapat %q (%v)", ".", rel, err)
	}
	if rel, err := p.rel(filepath.Join(home, "sub", "berkas-sub.txt")); err != nil || rel != "sub/berkas-sub.txt" {
		t.Fatalf("rel salah: %q (%v)", rel, err)
	}
}

// bukaJail: hanya user non-sudo yang dapat jail, dan home yang tidak masuk
// akal ditolak di situ — bukan dijalankan tanpa penjagaan.
func TestBukaJailHanyaUntukNonSudo(t *testing.T) {
	home, _ := siapkanHome(t)

	if f, err := bukaJail(&userInfo{Name: "admin", Home: home, Sudo: true}); err != nil || f != nil {
		t.Fatalf("sudoer tidak boleh dapat jail: %v %v", f, err)
	}
	f, err := bukaJail(&userInfo{Name: "biasa", Home: home})
	if err != nil {
		t.Fatalf("user biasa harus dapat jail: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.IsDir() {
		t.Fatalf("fd jail harus direktori: %v %v", fi, err)
	}
	for _, h := range []string{"", "/"} {
		if _, err := bukaJail(&userInfo{Name: "biasa", Home: h}); err == nil {
			t.Fatalf("home %q harus ditolak", h)
		}
	}
}

// ---- perilaku operasi (lanjutan) ----

// Daftar direktori tetap MEMPERLIHATKAN entri symlink yang menunjuk keluar
// home (dengan targetnya), sama seperti sebelumnya; yang ditolak adalah
// memakai symlink itu untuk operasi berkas.
func TestWorkerJailDaftarTetapMenampilkanEntriSymlink(t *testing.T) {
	home, luar := siapkanHome(t)

	res, _ := jalankanWorker(t, home, workerOp{Op: "list", Path: home, SaringAkses: true}, nil)
	if !res.OK {
		t.Fatalf("daftar harus lolos, dapat %+v", res)
	}
	var entries []helperproto.FileEntry
	if err := json.Unmarshal(res.Data, &entries); err != nil {
		t.Fatal(err)
	}
	var ada bool
	for _, e := range entries {
		if e.Name == "keluar.txt" {
			ada = true
			if e.Symlink != filepath.Join(luar, "rahasia.txt") {
				t.Fatalf("target symlink salah: %q", e.Symlink)
			}
		}
	}
	if !ada {
		t.Fatal("entri symlink harus tetap tampil di daftar")
	}
}

// Symlink ABSOLUT yang menunjuk di dalam home (pola yang dibuat file manager)
// harus tetap bisa dipakai — termasuk saat komponen antaranya yang berupa
// symlink, bukan hanya komponen terakhir.
func TestWorkerJailSymlinkAbsolutDalamHomeLewatPerantara(t *testing.T) {
	home, _ := siapkanHome(t)
	dokumen := filepath.Join(home, "Dokumen")
	if err := os.MkdirAll(filepath.Join(dokumen, "laporan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dokumen, "laporan", "q1.txt"), []byte("q1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Targetnya ABSOLUT dan berada di dalam home.
	if err := os.Symlink(dokumen, filepath.Join(home, "dokumen")); err != nil {
		t.Skipf("symlink tidak didukung: %v", err)
	}

	res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: filepath.Join(home, "dokumen", "laporan", "q1.txt")}, nil)
	if !res.OK {
		t.Fatalf("baca lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
	if string(out) != "q1" {
		t.Fatalf("isi salah: %q", out)
	}
	res, _ = jalankanWorker(t, home, workerOp{Op: "list", Path: filepath.Join(home, "dokumen")}, nil)
	if !res.OK {
		t.Fatalf("daftar lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
	// Daftarnya harus BENAR-BENAR berisi entri: lstat entri lewat fd tidak
	// boleh gagal hanya karena komponen antaranya symlink absolut.
	var entries []helperproto.FileEntry
	if err := json.Unmarshal(res.Data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("daftar lewat symlink absolut di dalam home tidak boleh kosong")
	}
	var adaLaporan bool
	for _, e := range entries {
		if e.Name == "laporan" && e.IsDir {
			adaLaporan = true
		}
	}
	if !adaLaporan {
		t.Fatalf("entri laporan harus tampil sebagai direktori, dapat %+v", entries)
	}
	if res := opWorker(t, home, workerOp{Op: "search", Path: filepath.Join(home, "dokumen"), Query: "q1", SaringAkses: true}, nil); !res.OK {
		t.Fatalf("pencarian lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "usage", Path: filepath.Join(home, "dokumen")}, nil); !res.OK {
		t.Fatalf("usage lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
	if res := opWorker(t, home, workerOp{Op: "mkdir", Path: filepath.Join(home, "dokumen", "baru")}, nil); !res.OK {
		t.Fatalf("mkdir lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dokumen, "baru")); err != nil {
		t.Fatalf("direktori tidak dibuat di tempat yang benar: %v", err)
	}
	// Symlink absolut ke DIBUAT lewat symlink dalam home juga harus bekerja.
	if res := opWorker(t, home, workerOp{Op: "write", Path: filepath.Join(home, "dokumen", "laporan", "q2.txt")}, []byte("q2")); !res.OK {
		t.Fatalf("tulis lewat symlink absolut di dalam home harus lolos, dapat %+v", res)
	}
}

// Rantai symlink yang menunjuk keluar home tetap ditolak walau bentuknya
// berlapis (symlink -> symlink -> luar).
func TestWorkerJailTolakRantaiSymlinkKeluar(t *testing.T) {
	home, luar := siapkanHome(t)
	if err := os.Symlink(filepath.Join(home, "bertingkat.txt"), filepath.Join(home, "rantai.txt")); err != nil {
		t.Skipf("symlink tidak didukung: %v", err)
	}
	res, _ := jalankanWorker(t, home, workerOp{Op: "read", Path: filepath.Join(home, "rantai.txt")}, nil)
	if res.OK {
		t.Fatal("rantai symlink ke luar home harus ditolak")
	}
	if res.Code != helperproto.ErrDenied {
		t.Fatalf("kode harus %q, dapat %q (%s)", helperproto.ErrDenied, res.Code, res.Error)
	}
	if isi, err := os.ReadFile(filepath.Join(luar, "rahasia.txt")); err != nil || string(isi) != "isi luar home" {
		t.Fatalf("berkas di luar home berubah: %q %v", isi, err)
	}
}

// Copy mempertahankan symlink sebagai symlink (tidak pernah mengikuti
// targetnya), sama seperti copyPath dulu.
func TestWorkerJailCopyMempertahankanSymlink(t *testing.T) {
	home, luar := siapkanHome(t)

	if res := opWorker(t, home, workerOp{Op: "copy", Path: filepath.Join(home, "keluar.txt"), Dest: filepath.Join(home, "salinan.txt")}, nil); !res.OK {
		t.Fatalf("copy symlink harus lolos, dapat %+v", res)
	}
	target, err := os.Readlink(filepath.Join(home, "salinan.txt"))
	if err != nil {
		t.Fatalf("hasil copy harus berupa symlink: %v", err)
	}
	if target != filepath.Join(luar, "rahasia.txt") {
		t.Fatalf("target symlink hasil copy salah: %q", target)
	}
}

// Menghapus folder yang izinnya dicabut pemiliknya sendiri harus tetap bisa
// (os.RemoveAll melakukan chmod 0700 lalu mencoba lagi; jalur fd menirunya).
func TestWorkerJailHapusFolderTanpaIzin(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root melewati pemeriksaan izin")
	}
	home, _ := siapkanHome(t)
	tertutup := filepath.Join(home, "tertutup")
	if err := os.MkdirAll(filepath.Join(tertutup, "dalam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tertutup, "dalam", "berkas.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tertutup, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tertutup, 0o755) })

	if res := opWorker(t, home, workerOp{Op: "remove", Path: tertutup, Recursive: true}, nil); !res.OK {
		t.Fatalf("hapus folder tanpa izin harus lolos, dapat %+v", res)
	}
	if _, err := os.Stat(tertutup); !os.IsNotExist(err) {
		t.Fatal("folder tidak terhapus")
	}
}

// chmod di jalur jail mengubah berkas yang benar — dan symlink yang menunjuk
// keluar home tidak bisa dipakai untuk mengubah mode berkas di luar home.
func TestWorkerJailChmod(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root mengubah mode apa pun")
	}
	home, luar := siapkanHome(t)
	berkas := filepath.Join(home, "mode.txt")
	if err := os.WriteFile(berkas, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := opWorker(t, home, workerOp{Op: "chmod", Path: berkas, Mode: 0o600}, nil); !res.OK {
		t.Fatalf("chmod harus lolos, dapat %+v", res)
	}
	fi, err := os.Stat(berkas)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode tidak berubah: %v %v", fi.Mode(), err)
	}

	// Berkas di luar home tidak boleh berubah mode-nya lewat symlink.
	luarBerkas := filepath.Join(luar, "rahasia.txt")
	if err := os.Chmod(luarBerkas, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := opWorker(t, home, workerOp{Op: "chmod", Path: filepath.Join(home, "keluar.txt"), Mode: 0o600}, nil); res.OK {
		t.Fatal("chmod lewat symlink keluar home harus ditolak")
	}
	if fi, err := os.Stat(luarBerkas); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode berkas di luar home berubah: %v %v", fi.Mode(), err)
	}
}

// Offset/Length pada op read harus tetap bekerja di jalur fd (HTTP Range).
func TestWorkerJailReadRentang(t *testing.T) {
	home, _ := siapkanHome(t)
	berkas := filepath.Join(home, "rentang.txt")
	if err := os.WriteFile(berkas, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: berkas, Offset: 4, Length: 3}, nil)
	if !res.OK {
		t.Fatalf("baca rentang harus lolos, dapat %+v", res)
	}
	if string(out) != "456" {
		t.Fatalf("rentang salah: %q", out)
	}
	// Offset di luar ukuran bukan error: badan kosong (perilaku lama).
	if res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: berkas, Offset: 99}, nil); !res.OK || len(out) != 0 {
		t.Fatalf("offset di luar ukuran harus menghasilkan badan kosong: %+v %q", res, out)
	}
}

// Kontrak parent → worker: fd 5 + penanda jail hanya untuk user non-sudo.
// Diuji pada perintah yang benar-benar disusun runAsUser, tanpa perlu
// menjalankan prosesnya (penurunan privilege butuh root).
func TestPerintahWorkerMembawaJailHanyaUntukNonSudo(t *testing.T) {
	home, _ := siapkanHome(t)
	opR, opW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer opR.Close()
	defer opW.Close()
	resR, resW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer resR.Close()
	defer resW.Close()

	u := &userInfo{Name: "biasa", UID: 1000, GID: 1000, Home: home}
	jail, err := bukaJail(u)
	if err != nil {
		t.Fatal(err)
	}
	defer jail.Close()
	cmd := perintahWorker(os.Args[0], u, jail, opR, resW)
	if len(cmd.ExtraFiles) != 3 {
		t.Fatalf("worker user biasa harus menerima 3 fd (op, hasil, jail), dapat %d", len(cmd.ExtraFiles))
	}
	if cmd.ExtraFiles[2] != jail {
		t.Fatal("fd jail harus di posisi ketiga (fd 5)")
	}
	var adaPenanda, adaMode bool
	for _, e := range cmd.Env {
		if e == jailHomeEnv+"="+home {
			adaPenanda = true
		}
		if e == modeEnv+"="+modeJail {
			adaMode = true
		}
	}
	if !adaPenanda {
		t.Fatalf("env worker harus memuat %s=%s, dapat %v", jailHomeEnv, home, cmd.Env)
	}
	if !adaMode {
		t.Fatalf("env worker harus menyatakan %s=%s, dapat %v", modeEnv, modeJail, cmd.Env)
	}

	// Sudoer: tanpa jail sama sekali. Kalau penanda ikut terkirim tanpa fd,
	// worker akan menolak op — dan itu berarti jalur sudo mati.
	cmdSudo := perintahWorker(os.Args[0], &userInfo{Name: "admin", UID: 0, GID: 0, Home: home, Sudo: true}, nil, opR, resW)
	if len(cmdSudo.ExtraFiles) != 2 {
		t.Fatalf("worker sudoer harus menerima 2 fd, dapat %d", len(cmdSudo.ExtraFiles))
	}
	var adaModeSudo bool
	for _, e := range cmdSudo.Env {
		if strings.HasPrefix(e, jailHomeEnv+"=") {
			t.Fatalf("worker sudoer tidak boleh membawa penanda jail: %v", cmdSudo.Env)
		}
		if e == modeEnv+"="+modeSudo {
			adaModeSudo = true
		}
	}
	if !adaModeSudo {
		t.Fatalf("worker sudoer harus menyatakan %s=%s, dapat %v", modeEnv, modeSudo, cmdSudo.Env)
	}
}

// Inti TOCTOU: symlink yang menunjuk keluar home ditukar-tukar terus selama
// operasi berjalan. Tidak ada satu pun interleaving yang boleh menghasilkan
// isi berkas di luar home — hasil yang sah hanyalah isi di dalam home atau
// penolakan.
func TestWorkerJailSymlinkDitukarTidakPernahMenembusHome(t *testing.T) {
	home, luar := siapkanHome(t)
	if err := os.MkdirAll(filepath.Join(home, "dalam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dalam", "isi.txt"), []byte("AMAN"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(luar, "isi.txt"), []byte("LUAR"), 0o644); err != nil {
		t.Fatal(err)
	}
	tukar := filepath.Join(home, "tukar")
	if err := os.Symlink("dalam", tukar); err != nil {
		t.Skipf("symlink tidak didukung: %v", err)
	}

	berhenti := make(chan struct{})
	selesai := make(chan struct{})
	go func() {
		defer close(selesai)
		target := []string{
			"dalam",                               // relatif, di dalam home
			filepath.Join(home, "dalam"),          // absolut, di dalam home
			filepath.Join(luar, "isi.txt"),        // di luar home
			filepath.Join(home, "keluar.txt"),     // symlink ke luar home
			filepath.Join(home, "bertingkat.txt"), // rantai ke luar home
		}
		for i := 0; ; i++ {
			select {
			case <-berhenti:
				return
			default:
			}
			_ = os.Remove(tukar)
			_ = os.Symlink(target[i%len(target)], tukar)
		}
	}()

	jumlah := 60
	for i := 0; i < jumlah; i++ {
		res, out := jalankanWorker(t, home, workerOp{Op: "read", Path: filepath.Join(home, "tukar", "isi.txt")}, nil)
		// target symlink berupa berkas (bukan direktori) juga sah: bacaannya
		// harus ditolak, bukan mengembalikan isi di luar home.
		if res.OK && string(out) == "LUAR" {
			close(berhenti)
			<-selesai
			t.Fatalf("percobaan ke-%d membaca berkas di luar home (%q)", i, out)
		}
		if res.OK && string(out) != "AMAN" {
			close(berhenti)
			<-selesai
			t.Fatalf("percobaan ke-%d mengembalikan isi tak terduga: %q", i, out)
		}
		// Dua kegagalan ini sah di tengah balapan: symlink-nya ditolak (keluar
		// home) atau sedang tidak ada saat ditukar (penukaran symlink bukan
		// operasi atomik). Yang tidak boleh terjadi hanyalah isi "LUAR".
		if !res.OK && res.Code != helperproto.ErrDenied && res.Code != helperproto.ErrNotFound {
			close(berhenti)
			<-selesai
			t.Fatalf("percobaan ke-%d gagal dengan kode %q: %s", i, res.Code, res.Error)
		}
	}
	close(berhenti)
	<-selesai

	if isi, err := os.ReadFile(filepath.Join(luar, "isi.txt")); err != nil || string(isi) != "LUAR" {
		t.Fatalf("berkas di luar home berubah: %q %v", isi, err)
	}
}

// ---------------------------------------------------------------------------
// Regression tests for the two remaining descriptor‑relative behaviours that are
// still unguarded. They are written to *fail* with the current implementation
// – the fix will make them pass.

// (A) chmod on a directory via the worker should be blocked when the path is
// resolved relative to the parent‑directory descriptor. The existing worker
// implementation permits the operation, so this test expects failure.
func TestWorkerChmodDirViaDescriptorShouldFail(t *testing.T) {
	home, _ := siapkanHome(t)
	dir := filepath.Join(home, "sub")
	// Attempt to chmod the directory inside the jail. The correct behaviour (as
	// per the regression note) is to reject the operation.
	res := opWorker(t, home, workerOp{Op: "chmod", Path: dir, Mode: 0o700}, nil)
	if res.OK {
		t.Fatalf("chmod on directory via descriptor should be rejected, got OK")
	}
	// Verify the mode was not changed.
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir failed: %v", err)
	}
	if fi.Mode().Perm() == 0o700 {
		t.Fatalf("directory mode changed unexpectedly")
	}
}

// (B) probe "bisa dibaca" pada berkas reguler di dalam jail HARUS berhasil —
// berkas yang memang bisa dibuka user tetap lolos. Tes ini menggantikan
// versi sebelumnya yang salah (mengharapkan false untuk berkas biasa di
// dalam home).

// ---- probe "bisa dibaca" & chmod deskriptor ----

// penjagaUji membuka home sebagai fd jail supaya resolver satuan (probe baca,
// chmod) bisa diuji langsung — tanpa menjalankan proses worker.
func penjagaUji(t *testing.T, home string) *penjaga {
	t.Helper()
	fd, err := unix.Open(home, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("buka home: %v", err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return &penjaga{fd: fd, akar: home}
}

// cobaJalankanWorkerBatas sama dengan cobaJalankanWorker, tetapi worker yang
// tidak selesai dalam `batas` dibunuh. Probe baca yang menggantung membuat
// SELURUH listing macet; itu kegagalan yang harus terbaca sebagai kegagalan
// test, bukan test yang ikut menunggu selamanya.
func cobaJalankanWorkerBatas(jailHome string, op workerOp, stdin []byte, batas time.Duration) (workerResult, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), batas)
	defer cancel()

	opR, opW, err := os.Pipe()
	if err != nil {
		return workerResult{}, nil, err
	}
	defer opW.Close()
	resR, resW, err := os.Pipe()
	if err != nil {
		opR.Close()
		return workerResult{}, nil, err
	}
	defer resR.Close()

	files := []*os.File{opR, resW}
	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	var jail *os.File
	if jailHome != "" {
		fd, err := unix.Open(jailHome, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			opR.Close()
			resW.Close()
			return workerResult{}, nil, err
		}
		jail = os.NewFile(uintptr(fd), jailHome)
		files = append(files, jail)
		env = append(env, modeEnv+"="+modeJail, jailHomeEnv+"="+jailHome)
	} else {
		env = append(env, modeEnv+"="+modeSudo)
	}

	cmd := exec.CommandContext(ctx, os.Args[0], WorkerArg)
	cmd.Env = env
	cmd.ExtraFiles = files
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Start(); err != nil {
		opR.Close()
		resW.Close()
		if jail != nil {
			jail.Close()
		}
		return workerResult{}, nil, err
	}
	opR.Close()
	resW.Close()
	if jail != nil {
		jail.Close()
	}

	if err := json.NewEncoder(opW).Encode(op); err != nil {
		_ = cmd.Wait()
		return workerResult{}, nil, err
	}
	opW.Close()

	var res workerResult
	decErr := json.NewDecoder(resR).Decode(&res)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return workerResult{}, stderr.Bytes(), ctx.Err()
	}
	_ = waitErr
	return res, stdout.Bytes(), decErr
}

// Probe "bisa dibaca" jalur jail dulu memakai faccessat(fdHome, rel): syscall
// itu MENGGIKUTI symlink dan tidak punya penjagaan BENEATH, jadi symlink yang
// menunjuk keluar home dilaporkan "bisa dibaca" — status berkas di luar home
// direkam, dan jawabannya menempel pada inode yang beda dari yang nanti
// dibuka pemanggil. Probe baru memakai resolusi yang sama dengan operasi
// berkasnya, jadi yang keluar home ditolak, bukan dilaporkan terbaca.
func TestPenjagaBisaDibacaTolakSymlinkKeluarHome(t *testing.T) {
	home, luar := siapkanHome(t)
	p := penjagaUji(t, home)

	// Sasaran di luar home memang bisa dibaca user ini, jadi jawaban lama
	// "bisa dibaca" bukan karena berkasnya tidak ada / tidak terbaca.
	for _, k := range []struct {
		path string
		dir  bool
	}{
		{filepath.Join(luar, "rahasia.txt"), false},
		{filepath.Join(luar, "sub"), true},
	} {
		if !bisaDibaca(k.path, k.dir) {
			t.Skipf("sasaran di luar home tidak bisa dibaca test ini: %s", k.path)
		}
	}

	kasus := []struct {
		nama string
		path string
		dir  bool
	}{
		{"symlink berkas ke luar home", filepath.Join(home, "keluar.txt"), false},
		{"symlink direktori ke luar home", filepath.Join(home, "keluar-dir"), true},
		{"rantai symlink ke luar home", filepath.Join(home, "bertingkat.txt"), false},
	}
	for _, k := range kasus {
		if p.bisaDibaca(k.path, k.dir) {
			t.Errorf("%s dilaporkan bisa dibaca, padahal resolusinya keluar home", k.nama)
		}
	}

	// Yang benar-benar ada DI DALAM home tetap lolos: perbaikan tidak boleh
	// menyaring berkas yang memang bisa dibuka user.
	for _, k := range []struct {
		nama string
		path string
		dir  bool
	}{
		{"berkas biasa", filepath.Join(home, "catatan.txt"), false},
		{"direktori biasa", filepath.Join(home, "sub"), true},
		{"symlink di dalam home", filepath.Join(home, "dalam.txt"), false},
		{"symlink relatif ke direktori di dalam home", filepath.Join(home, "sub-alias"), true},
	} {
		if !p.bisaDibaca(k.path, k.dir) {
			t.Errorf("%s harus tetap dilaporkan bisa dibaca", k.nama)
		}
	}
}

// Probe ini dijalankan untuk SETIAP entri listing. Membuka FIFO tanpa
// O_NONBLOCK menunggu penulis, jadi satu FIFO di dalam home akan menggantung
// seluruh listing (dan worker-nya). Batas waktu di bawah mengubah kegagalan
// itu menjadi kegagalan test yang terbaca.
func TestPenjagaBisaDibacaFIFOTidakMenggantung(t *testing.T) {
	home, _ := siapkanHome(t)
	p := penjagaUji(t, home)
	pipa := filepath.Join(home, "pipa")
	if err := unix.Mkfifo(pipa, 0o644); err != nil {
		t.Skipf("mkfifo tidak didukung: %v", err)
	}

	selesai := make(chan bool, 1)
	go func() { selesai <- p.bisaDibaca(pipa, false) }()
	select {
	case bisa := <-selesai:
		if !bisa {
			t.Fatal("FIFO milik user sendiri harus dilaporkan bisa dibaca")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe bisa dibaca menggantung pada FIFO — open-nya tidak memakai O_NONBLOCK")
	}
}

// Satu FIFO di dalam home tidak boleh menggantung op listing yang menyaring
// akses: listing berjalan di worker terpisah, dan worker yang macet akan
// memblokir request user selamanya.
func TestWorkerJailListingDenganFIFOTidakMenggantung(t *testing.T) {
	home, _ := siapkanHome(t)
	pipa := filepath.Join(home, "pipa")
	if err := unix.Mkfifo(pipa, 0o644); err != nil {
		t.Skipf("mkfifo tidak didukung: %v", err)
	}

	res, _, err := cobaJalankanWorkerBatas(home, workerOp{Op: "list", Path: home, SaringAkses: true}, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("list dengan FIFO tidak selesai (worker menggantung di probe baca FIFO?): %v", err)
	}
	if !res.OK {
		t.Fatalf("list dengan FIFO harus lolos, dapat %+v", res)
	}
	var entries []helperproto.FileEntry
	if err := json.Unmarshal(res.Data, &entries); err != nil {
		t.Fatal(err)
	}
	var ada bool
	for _, e := range entries {
		if e.Name == "pipa" {
			ada = true
		}
	}
	if !ada {
		t.Fatalf("FIFO milik user sendiri harus tetap tampil di daftar, dapat %+v", entries)
	}
}

// Entri yang tidak bisa dibaca user tetap disaring dari listing dan pencarian;
// entri yang bisa dibaca tetap muncul. Ini perilaku yang harus utuh setelah
// probe diganti.
func TestWorkerJailSaringEntriTidakTerbaca(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root melewati pemeriksaan izin")
	}
	home, _ := siapkanHome(t)
	bisa := filepath.Join(home, "bisa.txt")
	tutup := filepath.Join(home, "tertutup.txt")
	if err := os.WriteFile(bisa, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tutup, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tutup, 0o000); err != nil {
		t.Fatal(err)
	}

	namaDari := func(op workerOp) map[string]bool {
		t.Helper()
		res, _ := jalankanWorker(t, home, op, nil)
		if !res.OK {
			t.Fatalf("op %s harus lolos, dapat %+v", op.Op, res)
		}
		out := map[string]bool{}
		if op.Op == "list" {
			var entries []helperproto.FileEntry
			if err := json.Unmarshal(res.Data, &entries); err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				out[e.Name] = true
			}
			return out
		}
		var h helperproto.SearchHasil
		if err := json.Unmarshal(res.Data, &h); err != nil {
			t.Fatal(err)
		}
		for _, hit := range h.Hits {
			out[hit.Name] = true
		}
		return out
	}

	disaring := namaDari(workerOp{Op: "list", Path: home, SaringAkses: true})
	if !disaring["bisa.txt"] {
		t.Fatal("berkas yang bisa dibaca tersaring dari daftar")
	}
	if disaring["tertutup.txt"] {
		t.Fatal("berkas tanpa izin baca tidak tersaring dari daftar")
	}
	// Tanpa penyaringan, entri yang tidak terbaca tetap terlihat — itu yang
	// membedakan "disaring" dari "hilang".
	penuh := namaDari(workerOp{Op: "list", Path: home})
	if !penuh["tertutup.txt"] {
		t.Fatal("tanpa SaringAkses entri harus tetap tampil")
	}

	cariSaring := namaDari(workerOp{Op: "search", Path: home, Query: "txt", SaringAkses: true})
	if !cariSaring["bisa.txt"] {
		t.Fatal("berkas yang bisa dibaca tersaring dari hasil pencarian")
	}
	if cariSaring["tertutup.txt"] {
		t.Fatal("berkas tanpa izin baca tersaring dari hasil pencarian seharusnya tersaring")
	}
}

// chmod jalur jail harus mengubah mode lewat DESKRIPTOR — fchmodat2(2) dengan
// AT_EMPTY_PATH pada fd O_PATH hasil openat2 — bukan lewat resolusi nama
// "/proc/self/fd/N". Varian "fd direktori induk + nama basis" dengan flags=0
// justru MENGIKUTI symlink tanpa penjagaan BENEATH, jadi symlink yang menunjuk
// keluar home akan mengubah mode berkas di luar home.
func TestPenjagaChmodLewatDescriptorBukanProcSelfFd(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root mengubah mode apa pun")
	}
	home, luar := siapkanHome(t)
	p := penjagaUji(t, home)
	berkas := filepath.Join(home, "mode.txt")
	if err := os.WriteFile(berkas, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dipakaiProc := false
	lama := chmodProc
	chmodProc = func(fd int, mode os.FileMode) error {
		dipakaiProc = true
		return lama(fd, mode)
	}
	t.Cleanup(func() { chmodProc = lama })

	rel, err := p.rel(berkas)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.chmodFd(rel, berkas, 0o600); err != nil {
		t.Fatalf("chmod di dalam home harus lolos: %v", err)
	}
	if dipakaiProc {
		t.Error("chmod jalur jail masih lewat /proc/self/fd, bukan fchmodat2 pada fd")
	}
	fi, err := os.Stat(berkas)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode berkas tidak berubah: %v %v", fi.Mode(), err)
	}

	// Symlink yang menunjuk keluar home tetap ditolak, dan berkas di luar
	// home tidak boleh berubah mode-nya.
	luarBerkas := filepath.Join(luar, "rahasia.txt")
	if err := os.Chmod(luarBerkas, 0o644); err != nil {
		t.Fatal(err)
	}
	krel, err := p.rel(filepath.Join(home, "keluar.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.chmodFd(krel, filepath.Join(home, "keluar.txt"), 0o600); err == nil {
		t.Fatal("chmod lewat symlink keluar home harus ditolak")
	}
	if fi, err := os.Stat(luarBerkas); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode berkas di luar home berubah: %v %v", fi.Mode(), err)
	}
}
