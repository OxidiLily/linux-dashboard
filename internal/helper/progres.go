package helper

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// Fase sengaja kosong: sebelum ada laporan pertama, panel belum tahu
	// pemasangan ini lewat apt atau bukan. Menebak "indeks" membuat kartu
	// 9router — yang dipasang lewat npm dan tidak pernah menyentuh daftar
	// paket — mengaku sedang memperbarui indeks selama satu menit penuh.
	progres.ComponentProgress = helperproto.ComponentProgress{
		Name: nama, Aktif: true, Persen: 0, Pesan: "menyiapkan pemasangan",
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

// tahapBaru memulai tahap yang angkanya tidak bisa dibaca panel — npm, pipx,
// skrip vendor yang diam. Persennya dikembalikan ke nol supaya bar kembali ke
// bentuk "belum tahu posisi", bukan bertahan di 99% sisa tahap sebelumnya:
// prasyarat apt (mis. Node.js untuk 9router) sudah menghabiskan seluruh
// rentang, dan bar penuh yang masih berdetak lebih menyesatkan daripada bar
// yang jujur mengaku tidak tahu.
func tahapBaru(pesan string) {
	progres.Lock()
	defer progres.Unlock()
	if !progres.Aktif {
		return
	}
	progres.Persen, progres.Fase, progres.Pesan = 0, "", pesan
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
	penuh := append([]string{"-o", "APT::Status-Fd=3"}, args...)
	cmd := exec.Command("apt-get", penuh...)
	cmd.Env = envInstall()
	return jalankanDenganStatusAPT(cmd, awal, akhir)
}

// skripDenganProgres menjalankan skrip installer vendor sambil tetap membaca
// kemajuan apt yang dipanggil DI DALAM skrip itu.
//
// Skrip vendor (Tailscale, NodeSource) memanggil apt-get sendiri, jadi tidak
// ada tempat menyisipkan `-o APT::Status-Fd`. APT_CONFIG menunjuk berkas
// konfigurasi tambahan yang dibaca setiap apt-get anak skrip tersebut — hasil
// akhirnya sama: baris status mengalir ke fd 3 yang diwarisi dari sini.
// Skrip yang tidak menyentuh apt sama sekali cuma tidak mengirim baris apa pun.
func skripDenganProgres(shell, path string, awal, akhir int) error {
	konf := filepath.Join(filepath.Dir(path), "apt-status.conf")
	if err := os.WriteFile(konf, []byte("APT::Status-Fd \"3\";\n"), 0o600); err != nil {
		// Instalasi lebih penting daripada barnya.
		_, e := run(shell, path)
		return e
	}
	cmd := exec.Command(shell, path)
	cmd.Env = append(envInstall(), "APT_CONFIG="+konf)
	return jalankanDenganStatusAPT(cmd, awal, akhir)
}

// envInstall adalah lingkungan tetap untuk semua pemasangan: PATH yang tidak
// bergantung shell pemanggil, dan apt yang tidak pernah menunggu jawaban user.
func envInstall() []string {
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C",
	}
}

// jalankanDenganStatusAPT menjalankan cmd dengan fd 3 tersambung ke pembaca
// status apt.
func jalankanDenganStatusAPT(cmd *exec.Cmd, awal, akhir int) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
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
