package helperproto

import "os"

// BinRar mengembalikan path program rar di lokasi instal baku, atau "" kalau
// tidak terpasang. Dipakai dua sisi: worker helper (menjalankan `rar a`) dan
// endpoint /api/files/formats (menyembunyikan menu .rar di UI) — satu daftar
// supaya keduanya tidak berbeda pendapat.
//
// ponytail: hanya lokasi baku; kalau rar dipasang di tempat lain, tambahkan
// path-nya di sini. exec.LookPath sengaja tidak dipakai karena PATH proses
// web server dan PATH daemon helper berbeda.
func BinRar() string {
	for _, p := range []string{"/usr/bin/rar", "/usr/local/bin/rar"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
