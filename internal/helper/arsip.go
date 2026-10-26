package helper

// arsip.go — ekstrak dan buat arsip untuk file manager.
//
// Ekstrak berjalan sepenuhnya di dalam proses worker (stdlib archive/zip,
// archive/tar, compress/*, plus rardecode untuk RAR) sehingga tidak ada
// program eksternal yang harus terpasang. Pembuatan .zip juga in-proses;
// pembuatan .rar harus memanggil program `rar` karena format RAR hanya bisa
// DITULIS oleh perangkat resmi RARLAB.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// ekstrakArsip mengeluarkan isi arsip src ke folder dest. Format dideteksi
// dari magic bytes, bukan ekstensi: berkas .zip yang ternyata rar tetap bisa
// diekstrak, dan berkas sampah tidak akan diobrak-abrik berdasarkan nama.
func ekstrakArsip(r resolusiPath, src, dest string) error {
	f, err := r.buka(src, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return errInvalid("%s adalah direktori, bukan arsip", src)
	}
	kepala := make([]byte, 512)
	n, _ := io.ReadFull(f, kepala)
	kepala = kepala[:n]
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := r.mkdirAll(dest, 0o755); err != nil {
		return err
	}
	switch {
	case bytes.HasPrefix(kepala, []byte("PK\x03\x04")), bytes.HasPrefix(kepala, []byte("PK\x05\x06")):
		return ekstrakZip(r, f, st.Size(), dest)
	case bytes.HasPrefix(kepala, []byte("Rar!\x1a\x07")):
		return ekstrakRar(r, f, dest)
	}
	var isi io.Reader = f
	switch {
	case len(kepala) >= 2 && kepala[0] == 0x1f && kepala[1] == 0x8b:
		gz, err := gzip.NewReader(f)
		if err != nil {
			return errInvalid("berkas gzip rusak: %v", err)
		}
		defer gz.Close()
		isi = gz
	case bytes.HasPrefix(kepala, []byte("BZh")):
		isi = bzip2.NewReader(f)
	case len(kepala) >= 262 && string(kepala[257:262]) == "ustar":
		// tar murni tanpa lapisan kompresi.
	default:
		return errInvalid("format arsip tidak dikenali (didukung: zip, rar, tar, tar.gz, tar.bz2)")
	}
	return ekstrakTar(r, isi, dest)
}

// namaAman memetakan nama entri di dalam arsip ke path di bawah dest.
// Entri absolut dinormalkan jadi relatif; entri yang tetap bisa keluar dari
// dest (zip-slip) ditolak.
func namaAman(dest, nama string) (string, error) {
	n := strings.ReplaceAll(nama, `\`, "/")
	n = strings.TrimPrefix(path.Clean("/"+n), "/")
	if n == "" || n == "." {
		return "", errInvalid("nama entri arsip tidak valid: %q", nama)
	}
	target := filepath.Join(dest, filepath.FromSlash(n))
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errInvalid("entri arsip mencoba keluar dari folder tujuan: %q", nama)
	}
	return target, nil
}

// tulisEntriArsip menulis satu entri hasil ekstrak lewat resolver yang sama
// dengan operasi file lain, sehingga jail non-sudo tetap menutup jalan keluar
// sekalipun ada bug di penyaringan nama entri.
//
// Symlink, hardlink, dan device node tidak pernah dibuat: entri semacam itu
// bisa dipakai untuk menulis ke luar folder tujuan lewat entri berikutnya.
func tulisEntriArsip(r resolusiPath, dest, nama string, dir bool, mode os.FileMode, isi io.Reader) error {
	target, err := namaAman(dest, nama)
	if err != nil {
		return err
	}
	if dir {
		return r.mkdirAll(target, 0o755)
	}
	if err := r.mkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	f, err := r.buka(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, isi)
	return err
}

func ekstrakZip(r resolusiPath, f *os.File, size int64, dest string) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return errInvalid("berkas zip rusak: %v", err)
	}
	for _, zf := range zr.File {
		fi := zf.FileInfo()
		if fi.IsDir() {
			if err := tulisEntriArsip(r, dest, zf.Name, true, 0, nil); err != nil {
				return err
			}
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = tulisEntriArsip(r, dest, zf.Name, false, fi.Mode(), rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func ekstrakTar(r resolusiPath, isi io.Reader, dest string) error {
	tr := tar.NewReader(isi)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errInvalid("berkas tar rusak: %v", err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := tulisEntriArsip(r, dest, hdr.Name, true, hdr.FileInfo().Mode(), nil); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := tulisEntriArsip(r, dest, hdr.Name, false, hdr.FileInfo().Mode(), tr); err != nil {
				return err
			}
		default:
			// Symlink, hardlink, device node, dan fifo dilewati.
		}
	}
}

func ekstrakRar(r resolusiPath, f *os.File, dest string) error {
	rr, err := rardecode.NewReader(f, "")
	if err != nil {
		return errInvalid("berkas rar rusak: %v", err)
	}
	for {
		hdr, err := rr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errInvalid("gagal membaca arsip rar (rusak atau memakai kata sandi): %v", err)
		}
		if hdr.IsDir {
			if err := tulisEntriArsip(r, dest, hdr.Name, true, 0, nil); err != nil {
				return err
			}
			continue
		}
		if err := tulisEntriArsip(r, dest, hdr.Name, false, hdr.Mode(), rr); err != nil {
			return err
		}
	}
}

// buatArsip membuat arsip baru berisi src pada jalur dest (.zip atau .rar).
func buatArsip(r resolusiPath, src, dest string) error {
	e, err := r.stat(src)
	if err != nil {
		return err
	}
	if _, err := r.stat(dest); err == nil {
		// `rar a` akan menggabung ke arsip lama alih-alih membuat baru;
		// menolak lebih dulu membuat kedua format berperilaku sama.
		return errInvalid("arsip tujuan sudah ada: %s", dest)
	}
	if e.IsDir {
		if rel, err := filepath.Rel(src, dest); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errInvalid("arsip tidak boleh berada di dalam folder yang diarsipkan")
		}
	}
	if err := r.mkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var errBuat error
	switch strings.ToLower(filepath.Ext(dest)) {
	case ".zip":
		errBuat = buatZip(r, src, dest, e.IsDir, os.FileMode(e.ModePct&0o777))
	case ".rar":
		errBuat = buatRar(src, dest)
	default:
		return errInvalid("ekstensi arsip tidak didukung: %s (pakai .zip atau .rar)", filepath.Ext(dest))
	}
	if errBuat != nil {
		// Arsip setengah jadi tidak boleh tertinggal: selain membingungkan,
		// keberadaannya memblokir percobaan ulang lewat cek "sudah ada".
		_ = r.hapus(dest, false)
	}
	return errBuat
}

func buatZip(r resolusiPath, src, dest string, dir bool, mode os.FileMode) error {
	f, err := r.buka(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	err = zipTambah(zw, r, src, filepath.Base(src), dir, mode)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	return err
}

// zipTambah memasukkan satu path ke arsip. Strukturnya sama dengan zipPath di
// sisi API (unduhan .zip), tetapi sumbernya langsung disk lewat resolver —
// tidak ada fork worker per berkas.
func zipTambah(zw *zip.Writer, r resolusiPath, abs, rel string, dir bool, mode os.FileMode) error {
	if !dir {
		src, err := r.buka(abs, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer src.Close()
		if mode.Perm() == 0 {
			mode = 0o644
		}
		// Deflate, bukan Store (berbeda dari unduhan .zip): tujuan menu ini
		// memang mengecilkan isi folder, bukan sekadar menggabungkan.
		hdr := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		hdr.SetMode(mode.Perm())
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		return err
	}
	entries, err := r.listDir(abs, false)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		_, err := zw.Create(rel + "/")
		return err
	}
	for _, e := range entries {
		if err := zipTambah(zw, r, e.Path, rel+"/"+e.Name, e.IsDir, os.FileMode(e.ModePct&0o777)); err != nil {
			return err
		}
	}
	return nil
}

// buatRar memanggil program `rar`. Tidak ada pustaka Go yang bisa menulis
// format RAR — hanya perangkat resmi RARLAB.
func buatRar(src, dest string) error {
	bin := helperproto.BinRar()
	if bin == "" {
		return errInvalid("pembuatan .rar butuh program rar yang belum terpasang di server")
	}
	// cmd.Dir + argumen relatif supaya nama yang tersimpan di arsip hanya
	// basename-nya, bukan path lengkap sumber.
	cmd := exec.Command(bin, "a", "-idq", dest, filepath.Base(src))
	cmd.Dir = filepath.Dir(src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errInvalid("gagal membuat arsip rar: %s", msg)
	}
	return nil
}
