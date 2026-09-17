package helper

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// urutkanEntri mengurutkan hasil *os.File.ReadDir menurut nama.
//
// os.ReadDir mengurutkan sendiri; ReadDir milik *os.File (yang dipakai di
// sini karena lebih murah) TIDAK. Untuk pencarian, urutannya bukan kosmetik:
// hasil yang dibatasi harus berhenti di tempat yang sama setiap kali, kalau
// tidak "500 hasil pertama" berubah antar-percobaan dan berkas yang sama bisa
// muncul-hilang tanpa sebab yang terlihat user.
func urutkanEntri(ents []os.DirEntry) {
	slices.SortFunc(ents, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
}

// pencarian nama berkas secara rekursif — sisi yang dipakai halaman File
// Manager saat user memilih "termasuk subfolder".
//
// Yang dicari adalah NAMA berkas/folder, bukan isinya. Itu bukan kekurangan
// sementara: folder data user bisa berisi puluhan GB, dan membaca seluruh isi
// berkas untuk satu kata kunci membuat pencarian tidak bisa dipakai pada
// folder sungguhan. `grep -r` melakukan hal yang sama dengan biaya jauh lebih
// besar, dan user yang memang butuh isi berkas punya terminal.
//
// Sama seperti listDir, penelusuran TIDAK mengikuti symlink yang menunjuk
// direktori. Symlink yang menunjuk ke atas pohonnya sendiri akan membuat
// penelusuran berputar tanpa ujung — persis alasan `find -L` bisa menggantung.

const (
	// maksKueriCari memotong kueri yang tidak masuk akal. Nama berkas
	// maksimum 255 byte di Linux, jadi kueri sepanjang ini sudah lebih dari
	// cukup; tanpa batas, klien bisa mengirim string raksasa dan setiap
	// perbandingan nama membayarnya.
	maksKueriCari = 128

	// maksHasilCari membatasi jumlah baris yang dikirim ke UI. Pohon besar
	// bisa punya puluhan ribu kecocokan; mengirim semuanya membuat respons
	// lambat dan halaman penuh baris yang tidak ada yang membacanya.
	maksHasilCari = 500

	// maksDirCari membatasi jumlah folder yang dikunjungi, sehingga pencarian
	// di "/" berhenti dengan sendirinya alih-alih berjalan tanpa ujung.
	// Angka ini sejalan dengan maxUsageDirs: keduanya menelusuri pohon yang
	// sama, hanya hasilnya yang berbeda.
	maksDirCari = 50000
)

// batasWaktuCari adalah plafon waktu penelusuran.
//
// Penghitung folder saja tidak cukup MENJAMIN respons: 50.000 folder di SSD
// lokal selesai dalam beberapa detik, tetapi di mount jaringan yang lambat —
// atau disk yang sedang sibuk — jumlah yang sama bisa berjalan menit-menitan.
// Berbeda dari hitungUsage yang dipicu di latar, pencarian ini MEMBLOKIR
// permintaan HTTP user: tanpa plafon waktu, satu penelusuran ke mount yang
// menggantung menahan permintaannya tanpa ujung, dan user yang menekan Cari
// lagi hanya menumpuk worker yang sama-sama menunggu.
//
// Berbentuk variabel supaya test bisa mempersempitnya tanpa menunggu 20 detik.
//
// Batasan yang jujur: plafon ini diperiksa DI ANTARA operasi direktori, jadi
// satu syscall yang benar-benar macet (mis. NFS server hilang di tengah
// pembacaan) tetap tidak bisa dipotong dari sini — hanya mount ber-opsi
// `soft`/`intr` yang memutusnya. Yang dijamin: penelusuran pohon besar berhenti
// dan melapor, bukan berjalan tanpa akhir.
var batasWaktuCari = 20 * time.Second

// Alasan berhentinya penelusuran. Dipisah, bukan sekadar boolean "terpotong",
// karena kalimat yang benar berbeda untuk tiap sebab: "persempit kata kunci"
// tidak menolong apa pun saat yang habis adalah waktunya.
//
// Nilainya berbahasa Inggris karena ini NILAI PROTOKOL yang sampai ke
// frontend, bukan teks yang dibaca user — pemeriksa terjemahan memindai semua
// literal di berkas .tsx, dan kata Indonesia di situ akan terbaca sebagai teks
// yang belum diterjemahkan (konvensi yang sama dipakai penanda baris di
// views/cron.tsx). Kalimat untuk user disusun di UI sesuai bahasanya.
const (
	AlasanHasil  = "results" // plafon jumlah hasil tercapai
	AlasanFolder = "folders" // plafon jumlah folder tercapai
	AlasanWaktu  = "time"    // plafon waktu tercapai
)

// batasiKueri membuang spasi tepi dan memotong kueri ke maksKueriCari rune.
// Pemotongan memakai rune, bukan byte: memotong di tengah karakter UTF-8
// menghasilkan nama yang tidak akan pernah cocok dengan apa pun.
func batasiKueri(q string) string {
	q = strings.TrimSpace(q)
	r := []rune(q)
	if len(r) > maksKueriCari {
		return string(r[:maksKueriCari])
	}
	return q
}

// pseudoFs menandai filesystem yang isinya dibangkitkan kernel, bukan berkas
// user: /proc, /sys, dan kerabatnya.
//
// Penelusuran rekursif WAJIB melewatinya. Isinya berubah setiap saat (setiap
// PID punya pohon sendiri di /proc), jadi hasil yang dilaporkan sudah basi saat
// dikirim; nama seperti "task" atau "fd" muncul ratusan kali sehingga
// membanjiri hasil dengan kecocokan yang tidak berarti; dan biayanya paling
// besar justru di situ — pencarian dari "/" menghabiskan jatah 50.000 folder di
// /proc dan /sys sebelum sampai ke berkas sungguhan.
//
// Daftar magicnya bagian dari ABI kernel dan sama untuk semua arsitektur Linux.
// fsSemu dipakai bersama hitungUsage; devtmpfs ditambahkan di sini karena
// hitungUsage sudah mengecualikannya lewat aturan "jangan melintasi
// filesystem", sedangkan pencarian justru HARUS melintas (bind mount dan mount
// di dalam folder data adalah isi sah).
func pseudoFs(path string) bool {
	if fsSemu(path) {
		return true
	}
	// DEVTMPFS_MAGIC, bagian dari ABI kernel.
	const devtmpfsMagic = 0x01021997
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return int64(st.Type) == devtmpfsMagic
}

// dirSistem menandai direktori yang isinya bukan berkas user, dan karena itu
// tidak ditelusuri.
//
// Jenis filesystem saja tidak cukup untuk mengenali direktori perangkat: di
// sebagian sistem (termasuk container) /dev adalah **tmpfs**, bukan devtmpfs —
// terverifikasi di mesin ini lewat `stat -f` (%T = tmpfs). sementara tmpfs juga
// dipakai untuk home di sebagian pemasangan, jadi menolak seluruh tmpfs akan
// mematikan pencarian di folder yang justru berisi data user.
//
// Yang menentukan adalah ISI-nya: direktori yang memuat device node
// (char/block) adalah direktori perangkat. Membuat device node menuntut root,
// jadi folder data user praktis tidak pernah memicunya.
//
// Dipanggil hanya untuk direktori yang BUKAN akar pencarian: user yang memang
// membuka /dev tetap melihat isinya alih-alih diam-diam kosong.
func dirSistem(path string, ents []os.DirEntry) bool {
	if pseudoFs(path) {
		return true
	}
	for _, e := range ents {
		if e.Type()&os.ModeDevice != 0 {
			return true
		}
	}
	return false
}

// cariRekursif menelusuri akar dan seluruh subfoldernya, mengembalikan entri
// yang namanya memuat kueri (kapital diabaikan) beserta SEBAB berhentinya.
//
// saring=true membuang entri yang tidak bisa dibaca user ini — aturan yang
// sama dengan daftar folder: nama berkas milik orang lain bukan urusan yang
// membukanya. Folder yang tidak bisa dibuka DILEWATI, bukan menggagalkan
// seluruh pencarian; itu perilaku `grep -r` juga.
func cariRekursif(akar, kueri string, saring bool, maks int) helperproto.SearchHasil {
	out := helperproto.SearchHasil{Hits: []helperproto.SearchHit{}}

	// Truncated diset di sini, bukan di pemanggil, supaya struct yang
	// dikembalikan konsisten di mana pun ia dipakai: Alasan terisi berarti
	// hasilnya memang tidak lengkap.
	berhenti := func(alasan string) helperproto.SearchHasil {
		out.Alasan = alasan
		out.Truncated = true
		return out
	}

	kueri = batasiKueri(kueri)
	// Kueri kosong TIDAK berarti "semua berkas": membuka kotak pencarian di
	// folder besar lalu mengembalikan seluruh isi pohon akan membanjiri UI
	// sebelum user sempat mengetik satu huruf.
	if kueri == "" {
		return out
	}
	if maks <= 0 {
		maks = maksHasilCari
	}
	needle := strings.ToLower(kueri)
	mulai := time.Now()

	// Tumpukan, bukan rekursi: pohon data user bisa sangat dalam, dan
	// kedalaman rekursi tidak sebanding dengan nilainya di sini.
	type item struct {
		dir  string
		rel  string
		akar bool
	}
	tumpuk := []item{{akar, "", true}}

	for len(tumpuk) > 0 {
		kini := tumpuk[len(tumpuk)-1]
		tumpuk = tumpuk[:len(tumpuk)-1]

		if out.Dirs >= maksDirCari {
			return berhenti(AlasanFolder)
		}
		// Diperiksa SEBELUM membaca, bukan sesudah: yang dilindungi adalah
		// waktu user, dan satu iterasi di mount yang lambat bisa jauh lebih
		// lama daripada plafonnya.
		if time.Since(mulai) > batasWaktuCari {
			return berhenti(AlasanWaktu)
		}
		out.Dirs++

		// ReadDir pada *os.File tidak mengurutkan hasilnya (berbeda dari
		// os.ReadDir). Untuk pencarian, urutan itu PENTING: hasil yang
		// terpotong harus berhenti di tempat yang sama setiap kali, kalau
		// tidak daftar "500 pertama" berubah-ubah antar-percobaan.
		f, err := os.Open(kini.dir)
		if err != nil {
			// Folder tanpa izin baca/cari cukup dilewati.
			continue
		}
		ents, _ := f.ReadDir(-1)
		f.Close()
		urutkanEntri(ents)

		// Direktori sistem (berisi device node, atau pseudo-filesystem) tidak
		// ditelusuri. Entri lapis pertamanya tetap dilaporkan saat direktori
		// itulah yang diminta user (mis. membuka /proc atau /dev sendiri),
		// tetapi turun ke bawahnya tidak pernah terjadi — di situlah ribuan
		// subdirektori per-PID dan per-perangkat berada.
		//
		// Pemeriksaan ini juga mencegah anak-anaknya masuk tumpukan sama
		// sekali, karena keputusannya dipakai di kedua tempat.
		sistem := dirSistem(kini.dir, ents)
		if sistem && !kini.akar {
			continue
		}

		for _, e := range ents {
			full := filepath.Join(kini.dir, e.Name())
			rel := e.Name()
			if kini.rel != "" {
				rel = kini.rel + "/" + e.Name()
			}
			// IsDir dari DirEntry dibaca dari direntnya sendiri, jadi symlink
			// yang menunjuk direktori TIDAK dianggap direktori di sini — dan
			// itulah yang mencegah penelusuran berputar.
			isDir := e.IsDir()
			if saring && !bisaDibaca(full, isDir) {
				continue
			}

			if strings.Contains(strings.ToLower(e.Name()), needle) {
				if len(out.Hits) >= maks {
					return berhenti(AlasanHasil)
				}
				hit := helperproto.SearchHit{
					Name:  e.Name(),
					Path:  full,
					Rel:   rel,
					IsDir: isDir,
				}
				if fi, err := e.Info(); err == nil {
					hit.Size = fi.Size()
					hit.ModTime = fi.ModTime().Unix()
				}
				out.Hits = append(out.Hits, hit)
			}

			// Menurun ke pseudo-filesystem atau direktori sistem TIDAK
			// dilakukan. Pemeriksaan ini penting ada di sini, bukan hanya saat
			// direktori diproses: /proc punya satu subdirektori per PID, dan
			// menaruh ratusan ribu di antaranya ke tumpukan hanya untuk
			// dibuang membuat pencarian dari "/" membakar jatah folder tanpa
			// membaca satu berkas user pun.
			if isDir && !sistem && !pseudoFs(full) {
				tumpuk = append(tumpuk, item{full, rel, false})
			}
		}
	}
	return out
}
