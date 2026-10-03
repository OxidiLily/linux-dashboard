package helper

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// ujiStreamLog adalah stream handleDockerLogs yang sudah membaca response
// awalnya, siap dibaca atau ditutup oleh test.
type ujiStreamLog struct {
	resp      helperproto.Response
	br        *bufio.Reader
	cli       net.Conn
	selesai   chan struct{}
	logDocker string
}

// skripDockerTiruan adalah template binary `docker` tiruan: "$DIR" diganti
// direktori uji oleh bukaStreamLog. Ia mencatat setiap argumen yang diterima
// (untuk memeriksa tail yang benar-benar dikirim), melayani `inspect`, lalu
// pada `logs` mencetak dua baris dan tidur — meniru container yang masih
// menghasilkan log.
const skripDockerTiruan = `#!/bin/sh
printf '%s\n' "$*" >> "$DIR/docker.log"
case "$1" in
  inspect) echo running ;;
  logs)
    printf 'baris-1\nbaris-2\n'
    # exec: supaya tidur MENGGANTIKAN shell — kalau sh tetap hidup sebagai
    # induk, mematikan prosesnya meninggalkan tidur yang menjaga pipe terbuka
    # dan cmd.Wait menggantung. (docker sungguhan memang satu proses.)
    exec sleep 30 ;;
esac
`

// pasangDockerTiruan menaruh binary `docker` tiruan di PATH proses DAN di
// pathExec (keduanya dipakai untuk menemukan binarinya), lalu mengembalikan
// jalur berkas catatan argumennya. Template wajib memuat "$DIR".
func pasangDockerTiruan(t *testing.T, skripTemplate string) string {
	t.Helper()
	dir := t.TempDir()
	if !strings.Contains(skripTemplate, "$DIR") {
		t.Fatal("template skrip tidak memuat $DIR — catatan argumen tidak akan terbaca")
	}
	skrip := strings.ReplaceAll(skripTemplate, "$DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(skrip), 0o755); err != nil {
		t.Fatalf("tulis docker tiruan: %v", err)
	}
	lamaPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+lamaPath)
	lamaExec := pathExec
	pathExec = dir + ":" + lamaExec
	t.Cleanup(func() { pathExec = lamaExec })
	return filepath.Join(dir, "docker.log")
}

// bukaStreamLog menaruh docker tiruan BARU lalu membuka stream-nya.
// Satu pemasangan per test: memasang ulang sementara handler test sebelumnya
// masih hidup menimbulkan data race pada pathExec (terdeteksi `go test -race`).
func bukaStreamLog(t *testing.T, skripTemplate string, args helperproto.DockerLogsArgs) *ujiStreamLog {
	t.Helper()
	return bukaStreamLogPakai(t, pasangDockerTiruan(t, skripTemplate), args)
}

// bukaStreamLogPakai membuka stream memakai docker tiruan yang SUDAH terpasang
// — untuk test yang membuka beberapa stream dalam satu fungsi.
func bukaStreamLogPakai(t *testing.T, logDocker string, args helperproto.DockerLogsArgs) *ujiStreamLog {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	srv, cli := net.Pipe()
	selesai := make(chan struct{})
	go func() {
		defer close(selesai)
		var s Server
		s.handleDockerLogs(srv, helperproto.Request{Cmd: helperproto.CmdDockerLogs, Args: raw})
	}()

	br := bufio.NewReader(cli)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("baca response awal: %v", err)
	}
	var resp helperproto.Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("response awal tidak valid: %v (%q)", err, line)
	}
	return &ujiStreamLog{resp: resp, br: br, cli: cli, selesai: selesai, logDocker: logDocker}
}

// bacaStream menunggu byte stream mengandung teks yang dicari.
func (u *ujiStreamLog) bacaStream(t *testing.T, cari string, batas time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(batas)
	var akum strings.Builder
	buf := make([]byte, 256)
	for !strings.Contains(akum.String(), cari) {
		if time.Now().After(deadline) {
			t.Fatalf("stream tidak berisi %q dalam %s (akumulasi: %q)", cari, batas, akum.String())
		}
		_ = u.cli.SetReadDeadline(time.Now().Add(time.Second))
		n, err := u.br.Read(buf)
		if n > 0 {
			akum.Write(buf[:n])
		}
		if err != nil && time.Now().After(deadline) {
			t.Fatalf("baca stream: %v (akumulasi: %q)", err, akum.String())
		}
	}
	return akum.String()
}

// tutupMenutupKlien menutup sisi klien lalu menunggu handler selesai —
// bukti `docker logs -f` ikut dimatikan, bukan ditinggal sebagai yatim.
func (u *ujiStreamLog) tutupMenutupKlien(t *testing.T) {
	t.Helper()
	_ = u.cli.Close()
	select {
	case <-u.selesai:
	case <-time.After(5 * time.Second):
		t.Fatal("handleDockerLogs tidak selesai setelah klien menutup — proses docker tertinggal")
	}
}

// tungguCatatan menunggu berkas catatan argumen tiruan memuat teks tertentu.
func (u *ujiStreamLog) tungguCatatan(t *testing.T, cari string, batas time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(batas)
	for {
		b, _ := os.ReadFile(u.logDocker)
		if strings.Contains(string(b), cari) {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("catatan argumen tidak berisi %q (isi: %q)", cari, string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Log harus benar-benar mengalir (bukan menunggu proses selesai), argumen
// yang dikirim persis seperti yang divalidasi, dan menutup koneksi klien
// harus mematikan `docker logs -f` yang sedang berjalan.
func TestHandleDockerLogsAlur(t *testing.T) {
	u := bukaStreamLog(t, skripDockerTiruan, helperproto.DockerLogsArgs{ID: "stirling-pdf", Tail: 5})
	if !u.resp.OK {
		t.Fatalf("response awal = %+v, harap OK", u.resp)
	}
	isiTeks := u.bacaStream(t, "baris-2", 5*time.Second)
	if !strings.Contains(isiTeks, "baris-1") {
		t.Fatalf("stream tidak memuat baris-1: %q", isiTeks)
	}
	u.tutupMenutupKlien(t)

	catatan := u.tungguCatatan(t, "logs -f --tail 5 stirling-pdf", 2*time.Second)
	if !strings.Contains(catatan, "inspect -f {{.State.Status}} stirling-pdf") {
		t.Fatalf("inspect tidak dipanggil dengan id yang benar: %q", catatan)
	}
}

// Dua perintah baru mem-buka proses root (`docker logs -f`, `docker exec`)
// — keduanya WAJIB ada di sudoRequired. Kalau tidak, sesi tanpa sudo yang
// masih sah (helper menegakkan sudo per permintaan, bukan hanya web layer)
// bisa menjalankan keduanya lewat panggilan helper langsung.
func TestDockerLogDankTermWajibSudo(t *testing.T) {
	for _, cmd := range []string{helperproto.CmdDockerLogs, helperproto.CmdDockerTerm} {
		if !sudoRequired[cmd] {
			t.Fatalf("%s tidak ada di sudoRequired — jalur root terbuka untuk sesi tanpa sudo", cmd)
		}
	}
}

// Kuota `docker logs -f` di helper: stream pertama memegang slot sampai
// ditutup, stream berikutnya DITOLAK (bukan diam-diam menumpuk proses root),
// dan slot kembali setelah stream pertama selesai.
func TestHandleDockerLogsKuota(t *testing.T) {
	lama := logHelperMaks
	logHelperMaks = 1
	t.Cleanup(func() { logHelperMaks = lama })

	// Sekali pasang untuk seluruh test: pemasangan ulang saat handler stream
	// pertama masih membaca pathExec adalah data race (go test -race).
	logDocker := pasangDockerTiruan(t, skripDockerTiruan)

	pertama := bukaStreamLogPakai(t, logDocker, helperproto.DockerLogsArgs{ID: "stirling-pdf", Tail: 5})
	if !pertama.resp.OK {
		t.Fatalf("stream pertama = %+v, harap OK", pertama.resp)
	}
	kedua := bukaStreamLogPakai(t, logDocker, helperproto.DockerLogsArgs{ID: "stirling-pdf", Tail: 5})
	if kedua.resp.OK {
		t.Fatal("stream kedua diterima walau kuota sudah penuh")
	}
	if kedua.resp.Code != helperproto.ErrAksiBerjalan {
		t.Fatalf("kode penolakan = %q, harap %q", kedua.resp.Code, helperproto.ErrAksiBerjalan)
	}
	kedua.tutupMenutupKlien(t)

	pertama.tutupMenutupKlien(t)
	ketiga := bukaStreamLogPakai(t, logDocker, helperproto.DockerLogsArgs{ID: "stirling-pdf", Tail: 5})
	if !ketiga.resp.OK {
		t.Fatalf("stream setelah slot dilepas = %+v, harap OK", ketiga.resp)
	}
	ketiga.tutupMenutupKlien(t)
}

// Tail dijepit helper: klien yang meminta angka di luar rentang wajar tetap
// menerima batas aman, dan tail 0 berarti minimal 1 baris.
func TestHandleDockerLogsJepitTail(t *testing.T) {
	for _, tc := range []struct {
		nama    string
		tail    int
		harapAt string
	}{
		{"terlalu besar", 999999, "--tail 2000"},
		{"nol", 0, "--tail 1"},
	} {
		t.Run(tc.nama, func(t *testing.T) {
			u := bukaStreamLog(t, skripDockerTiruan, helperproto.DockerLogsArgs{ID: "stirling-pdf", Tail: tc.tail})
			if !u.resp.OK {
				t.Fatalf("response awal = %+v, harap OK", u.resp)
			}
			// Tunggu argumen `logs` terekam SEBELUM menutup klien: menutup
			// terlalu cepat membatalkan konteks sebelum docker sempat jalan,
			// dan yang diuji justru tail yang dikirim.
			u.tungguCatatan(t, tc.harapAt, 2*time.Second)
			u.tutupMenutupKlien(t)
		})
	}
}

// ID kosong maupun diawali "-" ditolak sebelum proses docker dijalankan —
// docker membaca yang kedua sebagai flag.
func TestHandleDockerLogsTolakIDBuruk(t *testing.T) {
	for _, id := range []string{"", "  ", "-rf", "--help"} {
		u := bukaStreamLog(t, skripDockerTiruan, helperproto.DockerLogsArgs{ID: id, Tail: 5})
		if u.resp.OK {
			t.Fatalf("id %q diterima, harap ditolak", id)
		}
		if u.resp.Code != helperproto.ErrInvalid {
			t.Fatalf("kode untuk id %q = %q, harap %q", id, u.resp.Code, helperproto.ErrInvalid)
		}
		u.tutupMenutupKlien(t)
		if isi, _ := os.ReadFile(u.logDocker); len(isi) > 0 {
			t.Fatalf("docker tetap dipanggil untuk id %q: %q", id, isi)
		}
	}
}

// Container yang tidak dikenal ditolak dengan pesan yang bisa ditampilkan:
// response OK tidak boleh terkirim untuk sesuatu yang tidak ada, dan
// `docker logs` tidak boleh dijalankan sama sekali.
func TestHandleDockerLogsContainerTidakDikenal(t *testing.T) {
	skrip := `#!/bin/sh
printf '%s\n' "$*" >> "$DIR/docker.log"
case "$1" in
  inspect) echo "Error response from daemon: No such container: hilang" >&2; exit 1 ;;
esac
`
	u := bukaStreamLog(t, skrip, helperproto.DockerLogsArgs{ID: "hilang", Tail: 5})
	if u.resp.OK {
		t.Fatal("response awal OK untuk container yang tidak dikenal")
	}
	if !strings.Contains(u.resp.Error, "tidak dikenal") {
		t.Fatalf("pesan error = %q, harap menyebut tidak dikenal", u.resp.Error)
	}
	u.tutupMenutupKlien(t)
	if isi, _ := os.ReadFile(u.logDocker); strings.Contains(string(isi), "logs ") {
		t.Fatalf("docker logs tetap dijalankan: %q", isi)
	}
}
