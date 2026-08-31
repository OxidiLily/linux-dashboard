package helper

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Progres instalasi komponen.
//
// Angkanya datang dari apt sendiri lewat APT::Status-Fd, bukan dari stopwatch.
// Perkiraan berbasis waktu selalu berbohong justru di keadaan yang paling butuh
// kejujuran — mesin lambat dan paket besar — dan bar yang penuh sebelum
// pekerjaannya selesai lebih buruk daripada tidak ada bar sama sekali.
//
// Perintah install tetap sinkron seperti sebelumnya. Yang ditambahkan hanya
// tempat menaruh kemajuan supaya bisa dibaca perintah lain sementara install
// masih berjalan; UI memanggilnya berkala selama menunggu.

var progres struct {
	sync.Mutex
	helperproto.ComponentProgress
}

func mulaiProgres(nama string) {
	progres.Lock()
	defer progres.Unlock()
	progres.ComponentProgress = helperproto.ComponentProgress{
		Name: nama, Aktif: true, Persen: 0, Fase: "indeks",
	}
}

func selesaiProgres() {
	progres.Lock()
	defer progres.Unlock()
	progres.ComponentProgress = helperproto.ComponentProgress{}
}

// setProgres tidak pernah menurunkan angka. apt melaporkan dua deret persen
// yang masing-masing mulai dari nol (unduh lalu pasang), dan bar yang mundur
// di tengah jalan terbaca sebagai proses yang gagal lalu diulang.
func setProgres(persen int, fase, pesan string) {
	progres.Lock()
	defer progres.Unlock()
	if !progres.Aktif {
		return
	}
	if persen > progres.Persen {
		progres.Persen = persen
	}
	if fase != "" {
		progres.Fase = fase
	}
	progres.Pesan = pesan
}

func ambilProgres() helperproto.ComponentProgress {
	progres.Lock()
	defer progres.Unlock()
	return progres.ComponentProgress
}

// Pembagian rentang antar tahap. apt-get update tidak melaporkan persen yang
// berguna untuk dipakai bar, jadi ia hanya menempati potongan awal; sisanya
// dibagi antara mengunduh dan memasang, dua tahap yang memang punya angka.
const (
	batasIndeks = 10 // 0..10   : apt-get update
	batasUnduh  = 55 // 10..55  : dlstatus
	batasPasang = 99 // 55..99  : pmstatus (100 disisakan untuk "selesai")
)

// aptDenganProgres menjalankan apt-get sambil membaca laporan kemajuannya.
//
// apt menulis status terstruktur ke file descriptor yang ditunjuk
// APT::Status-Fd. fd 3 dipakai karena ExtraFiles menempatkan berkas pertama di
// sana — 0,1,2 sudah dipakai stdin/stdout/stderr.
func aptDenganProgres(args []string, awal, akhir int) error {
	r, w, err := os.Pipe()
	if err != nil {
		// Tanpa pipe, instalasi tetap harus jalan — yang hilang cuma barnya.
		_, e := run("apt-get", args...)
		return e
	}
	penuh := append([]string{"-o", "APT::Status-Fd=3"}, args...)
	cmd := exec.Command("apt-get", penuh...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C",
	}
	cmd.ExtraFiles = []*os.File{w}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return err
	}
	// Salinan milik induk ditutup setelah anak jalan; kalau tidak, pembacaan di
	// bawah tidak pernah melihat EOF dan goroutine-nya menggantung selamanya.
	w.Close()

	selesai := make(chan struct{})
	go func() {
		defer close(selesai)
		bacaStatusAPT(r, awal, akhir)
	}()

	errJalan := cmd.Wait()
	r.Close()
	<-selesai
	if errJalan != nil {
		return errInvalid("%s", strings.TrimSpace(firstNonEmpty(stderr.String(), errJalan.Error())))
	}
	return nil
}

// bacaStatusAPT menerjemahkan baris status apt jadi persen keseluruhan.
//
// Bentuk barisnya "<jenis>:<paket>:<persen>:<pesan>", mis.
//
//	dlstatus:1:20.0000:Retrieving file 1 of 3
//	pmstatus:cups:16.6667:Unpacking cups
//
// dlstatus dan pmstatus masing-masing berjalan 0..100 untuk tahapnya sendiri,
// jadi keduanya dipetakan ke potongan rentang yang berbeda supaya bar hanya
// bergerak maju.
func bacaStatusAPT(r io.Reader, awal, akhir int) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		bagian := strings.SplitN(sc.Text(), ":", 4)
		if len(bagian) < 3 {
			continue
		}
		nilai, err := strconv.ParseFloat(bagian[2], 64)
		if err != nil {
			continue
		}
		var mulai, henti int
		var fase string
		switch bagian[0] {
		case "dlstatus":
			mulai, henti, fase = awal, batasUnduh, "unduh"
		case "pmstatus":
			mulai, henti, fase = batasUnduh, akhir, "pasang"
		default:
			// "status" dan jenis lain tidak membawa kemajuan yang bisa dipakai.
			continue
		}
		pesan := ""
		if len(bagian) == 4 {
			pesan = strings.TrimSpace(bagian[3])
		}
		setProgres(mulai+int(nilai/100*float64(henti-mulai)), fase, pesan)
	}
}
