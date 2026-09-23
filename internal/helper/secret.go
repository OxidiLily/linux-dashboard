package helper

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// batasSecret: panjang maksimum berkas secret yang masih dibaca. Secret yang
// sah panjangnya puluhan byte; berkas sebesar ini pasti salah tempat dan
// membacanya utuh hanya menghabiskan memori.
const batasSecret = 4096

// loadSecretFile membaca secret HMAC dari disk dengan syarat ketat.
//
// Secret ini adalah satu-satunya bukti bahwa sebuah request datang dari proses
// web: siapa pun yang membacanya bisa menandatangani request apa pun — termasuk
// request yang mengaku sebagai user sudo. Karena itu berkasnya:
//
//   - dibuka dengan O_NOFOLLOW, jadi symlink tidak diikuti. Symlink bisa
//     dipasang pihak yang punya akses tulis ke direktori, dan mengikuti symlink
//     berarti helper memakai berkas yang dipilih orang lain;
//   - harus berkas biasa (bukan FIFO, device, atau direktori);
//   - tidak boleh bisa ditulis grup atau user lain (mode & 022 == 0). Berkas
//     yang bisa ditulis pihak lain bukan lagi rahasia, dan helper akan rela
//     memuat secret yang ditanam penyerang;
//   - harus dimiliki uid yang diberikan pemanggil — helper berjalan sebagai
//     root, jadi pemiliknya harus root.
//
// Kegagalan di sini membuat helper TIDAK jalan. Itu disengaja: helper yang
// berjalan dengan secret yang tidak terpercaya sama saja membuka jalur root
// bagi siapa pun yang bisa menulis ke berkas itu.
func loadSecretFile(path string, expectUID int) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("secret %s bukan berkas biasa (mode %v)", path, st.Mode())
	}
	if perm := st.Mode().Perm(); perm&0o022 != 0 {
		return nil, fmt.Errorf("secret %s bisa ditulis grup/user lain (mode %04o)", path, perm)
	}
	if uid, ok := fileUID(st); ok && uid != expectUID {
		return nil, fmt.Errorf("secret %s dimiliki uid %d, bukan %d", path, uid, expectUID)
	}

	b, err := io.ReadAll(io.LimitReader(f, batasSecret+1))
	if err != nil {
		return nil, err
	}
	if len(b) > batasSecret {
		return nil, fmt.Errorf("secret %s melebihi %d byte", path, batasSecret)
	}
	b = bytes.TrimSpace(b)
	if len(b) < 32 {
		return nil, fmt.Errorf("secret %s terlalu pendek (%d byte)", path, len(b))
	}
	return b, nil
}

func fileUID(st os.FileInfo) (int, bool) {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(sys.Uid), true
}

// secretLegacyAda melaporkan apakah secret versi lama (di dalam state dir web)
// masih ada. Dipakai hanya untuk memberi pesan yang bisa ditindaklanjuti.
func secretLegacyAda(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

// tulisSecretBaru menulis secret ke lokasi barunya: mode 0640 milik root dan
// grup web app (web harus bisa MEMBACA untuk menandatangani request, tapi tidak
// boleh menulis), dan tidak pernah menimpa berkas yang sudah ada.
//
// O_EXCL dipakai justru karena berkas yang sudah ada berarti ada yang salah:
// secret yang sah sudah dibaca lebih dulu oleh loadSecretFile, jadi menemukan
// berkas di sini berarti ada symlink atau sisa berkas yang tidak terduga. Dalam
// keadaan itu menimpa berarti menyerahkan isi secret ke pihak yang menaruhnya.
func tulisSecretBaru(path string, isi []byte, group string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(isi); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if gid, err := lookupGroupID(group); err == nil {
		_ = os.Chown(path, 0, gid)
	}
	return nil
}
