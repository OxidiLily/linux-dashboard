package helper

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// skripDockerTermTiruan: `inspect` menjawab container berjalan, probe shell
// (`exec <id> sh -c …`) menjawab /bin/bash, dan sesi nyata (`exec -i -t …`)
// mencetak satu baris lalu keluar — meniru proses di dalam container yang
// langsung selesai, sehingga sesi PTY-nya juga harus berakhir sendiri.
const skripDockerTermTiruan = `#!/bin/sh
printf '%s\n' "$*" >> "$DIR/docker.log"
case "$1" in
  inspect) echo true ;;
  exec)
    if [ "$2" = "-i" ]; then printf 'masuk-exec\n'; else printf '/bin/bash\n'; fi ;;
esac
`

// bukaStreamTerm menjalankan handleDockerTerm pada pipe dengan docker tiruan.
func bukaStreamTerm(t *testing.T, skripTemplate string, args helperproto.DockerTermArgs) *ujiStreamLog {
	t.Helper()
	logDocker := pasangDockerTiruan(t, skripTemplate)

	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	srv, cli := net.Pipe()
	selesai := make(chan struct{})
	go func() {
		defer close(selesai)
		var s Server
		s.handleDockerTerm(srv, bufio.NewReader(srv), helperproto.Request{Cmd: helperproto.CmdDockerTerm, Args: raw})
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

// Sesi harus benar-benar membuka PTY berisi `docker exec`, meneruskan
// keluaran proses di dalam container, lalu BERAKHIR SENDIRI ketika proses
// itu keluar — tanpa menunggu klien menutup koneksi.
func TestHandleDockerTermAlur(t *testing.T) {
	u := bukaStreamTerm(t, skripDockerTermTiruan, helperproto.DockerTermArgs{ID: "stirling-pdf", Cols: 100, Rows: 30})
	if !u.resp.OK {
		t.Fatalf("response awal = %+v, harap OK", u.resp)
	}
	// PTY mengubah \n menjadi \r\n; yang dicari hanya teks barisnya.
	if isi := u.bacaStream(t, "masuk-exec", 5*time.Second); !strings.Contains(isi, "masuk-exec") {
		t.Fatalf("keluaran exec tidak sampai: %q", isi)
	}
	// Proses di container keluar → handler harus selesai dengan sendirinya.
	select {
	case <-u.selesai:
	case <-time.After(5 * time.Second):
		t.Fatal("handleDockerTerm tidak selesai setelah proses di container keluar")
	}
	// Shell terpilih harus yang paling lengkap di container (probe menjawab
	// /bin/bash), bukan `sh` yang tanpa Tab-completion.
	if catatan := u.tungguCatatan(t, "exec -i -t stirling-pdf /bin/bash", 2*time.Second); !strings.Contains(catatan, "inspect") {
		t.Fatalf("inspect tidak dipanggil: %q", catatan)
	}
}

// ID buruk dan container yang tidak berjalan ditolak SEBELUM PTY dibuka:
// `docker exec` tidak boleh sama sekali dipanggil.
func TestHandleDockerTermTolak(t *testing.T) {
	for _, tc := range []struct {
		nama    string
		args    helperproto.DockerTermArgs
		harapAt string
	}{
		{"id diawali strip", helperproto.DockerTermArgs{ID: "-rf"}, "tidak valid"},
		{"id kosong", helperproto.DockerTermArgs{ID: "  "}, "tidak valid"},
	} {
		t.Run(tc.nama, func(t *testing.T) {
			u := bukaStreamTerm(t, skripDockerTermTiruan, tc.args)
			if u.resp.OK {
				t.Fatalf("args %+v diterima, harap ditolak", tc.args)
			}
			if !strings.Contains(u.resp.Error, tc.harapAt) {
				t.Fatalf("pesan error = %q, harap memuat %q", u.resp.Error, tc.harapAt)
			}
			if isi, _ := os.ReadFile(u.logDocker); strings.Contains(string(isi), "exec") {
				t.Fatalf("docker exec tetap dijalankan: %q", isi)
			}
		})
	}
}

func TestHandleDockerTermContainerBerhenti(t *testing.T) {
	skrip := `#!/bin/sh
printf '%s\n' "$*" >> "$DIR/docker.log"
case "$1" in
  inspect) echo false ;;
esac
`
	u := bukaStreamTerm(t, skrip, helperproto.DockerTermArgs{ID: "stirling-pdf"})
	if u.resp.OK {
		t.Fatal("response awal OK untuk container yang tidak berjalan")
	}
	if !strings.Contains(u.resp.Error, "tidak berjalan") {
		t.Fatalf("pesan error = %q, harap menyebut tidak berjalan", u.resp.Error)
	}
	if isi, _ := os.ReadFile(u.logDocker); strings.Contains(string(isi), "exec") {
		t.Fatalf("docker exec tetap dijalankan: %q", isi)
	}
}

// Probe shell yang gagal atau menjawab aneh TIDAK boleh menjadi argumen
// exec: sesi tetap dibuka dengan `sh` (cadangan), bukan argumen sewenang-
// wenang dari dalam container.
func TestHandleDockerTermShellCadangan(t *testing.T) {
	for _, tc := range []struct {
		nama   string
		probe  string
		harapK string
	}{
		{"jawaban tidak berbentuk shell", `else printf '/bin/bash jelek\n'; fi ;;`, "exec -i -t stirling-pdf sh"},
		{"probe gagal", `else exit 1; fi ;;`, "exec -i -t stirling-pdf sh"},
	} {
		t.Run(tc.nama, func(t *testing.T) {
			skrip := `#!/bin/sh
printf '%s\n' "$*" >> "$DIR/docker.log"
case "$1" in
  inspect) echo true ;;
  exec)
    if [ "$2" = "-i" ]; then printf 'masuk-exec\n'; ` + tc.probe + `
esac
`
			u := bukaStreamTerm(t, skrip, helperproto.DockerTermArgs{ID: "stirling-pdf"})
			if !u.resp.OK {
				t.Fatalf("response awal = %+v, harap OK", u.resp)
			}
			u.tungguCatatan(t, tc.harapK, 2*time.Second)
			// Handler harus benar-benar selesai sebelum subtest berikutnya
			// memasang docker tiruan baru — menulis pathExec saat handler lama
			// masih membacanya adalah data race (go test -race).
			u.tutupMenutupKlien(t)
		})
	}
}
