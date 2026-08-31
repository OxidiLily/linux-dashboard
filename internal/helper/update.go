package helper

import (
	_ "embed"
	"os"
	"strconv"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pembaruan panel: tarik sumber terbaru dari repo, build ulang, pasang, lalu
// restart kedua service — persis langkah yang dipakai installer.
//
// Prosesnya TIDAK boleh jadi anak dari helper daemon: langkah terakhir
// installer me-restart linux-dashboard-helper, dan systemd membunuh seluruh
// cgroup unit itu — pembaruan akan mati di tengah jalan, meninggalkan binary
// separuh terpasang. Karena itu skrip dijalankan sebagai unit transient lewat
// systemd-run, di luar cgroup helper maupun web.
//
// Konsekuensinya keluaran skrip tidak bisa di-stream lewat koneksi yang sedang
// terbuka (koneksi itu ikut putus saat restart), jadi keluarannya ditulis ke
// berkas log dan UI menariknya berkala. Log berkas juga yang membuat tampilan
// shell di UI tetap utuh setelah web app hidup lagi.

//go:embed update.sh
var updateScript string

// Berkas kerja pembaruan sengaja TIDAK ditaruh di /var/lib/linux-dashboard:
// kedua unit memakai StateDirectory=linux-dashboard, dan systemd meng-chown
// seluruh isi folder itu ke user service (linux-dashboard) tiap kali web app
// start. Skrip yang dijalankan systemd-run sebagai root karena itu akan jadi
// milik user web yang tidak berprivilege — proses web yang dibobol bisa
// menimpanya dan mendapat eksekusi root di pembaruan berikutnya. Folder
// tersendiri milik root menutup jalur itu.
const (
	updateUnit   = "linux-dashboard-update"
	updateDir    = "/var/lib/linux-dashboard-update"
	updateLog    = updateDir + "/update.log"
	updateSkrip  = updateDir + "/update.sh"
	updateSrc    = "/usr/local/src/go-react-linux-dashboard"
	updateRepo   = "https://github.com/OxidiLily/linux-dashboard.git"
	updateCabang = "main"

	// Ekor log yang dikirim ke UI. Cukup untuk melihat jalannya build tanpa
	// mengirim ulang puluhan ribu baris tiap polling.
	updateLogMaks = 32 * 1024
)

func updateBerjalan() bool {
	res, _ := run("systemctl", "is-active", updateUnit)
	st := strings.TrimSpace(res.Stdout)
	return st == "active" || st == "activating" || st == "reloading"
}

func updateStatus(args helperproto.UpdateArgs) helperproto.UpdateStatus {
	st := helperproto.UpdateStatus{Running: updateBerjalan()}

	if b, err := os.ReadFile(updateLog); err == nil {
		st.Log = ekorLog(b)
	}

	if !st.Running {
		if res, err := run("systemctl", "show", "-p", "Result", "--value", updateUnit); err == nil {
			st.Result = strings.TrimSpace(res.Stdout)
		}
		if res, err := run("systemctl", "show", "-p", "ExecMainStatus", "--value", updateUnit); err == nil {
			st.Exit, _ = strconv.Atoi(strings.TrimSpace(res.Stdout))
		}
	}

	lokalSha := ""
	if res, err := run("git", "-C", updateSrc, "rev-parse", "HEAD"); err == nil {
		lokalSha = strings.TrimSpace(res.Stdout)
	}
	if res, err := run("git", "-C", updateSrc, "log", "-1", "--format=%h %s"); err == nil {
		st.Lokal = firstLine(res.Stdout)
	}

	// Pengecekan remote butuh jaringan, jadi hanya dilakukan saat diminta
	// (modal dibuka), bukan tiap polling log.
	if args.Cek {
		if res, err := run("git", "ls-remote", updateRepo, "refs/heads/"+updateCabang); err == nil {
			if f := strings.Fields(firstLine(res.Stdout)); len(f) > 0 {
				st.Remote = f[0]
				if len(st.Remote) > 7 {
					st.Remote = st.Remote[:7]
				}
				// Sumber tidak ada di mesin ini (mis. dipasang dari checkout
				// lain lewat `make install`): versi terpasang tidak bisa
				// dibandingkan, jadi dianggap tertinggal — pembaruan sekali
				// jalan justru yang membuat checkout-nya ada.
				st.Tertinggal = lokalSha == "" || !strings.HasPrefix(f[0], lokalSha[:7])
			}
		}
	}
	return st
}

// ekorLog memotong log ke bagian belakangnya saja. Baris pertama hasil potongan
// hampir pasti terbelah di tengah, jadi dibuang supaya tampilan shell di UI
// tidak dimulai dari separuh kalimat.
func ekorLog(b []byte) string {
	if len(b) <= updateLogMaks {
		return string(b)
	}
	b = b[len(b)-updateLogMaks:]
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		b = b[i+1:]
	}
	return string(b)
}

func updateStart() (helperproto.UpdateStatus, error) {
	if updateBerjalan() {
		return helperproto.UpdateStatus{}, errInvalid("pembaruan sedang berjalan")
	}
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		return helperproto.UpdateStatus{}, err
	}
	// Versi lama menaruh kedua berkas ini di dalam StateDirectory; sisa dari
	// pembaruan sebelum perbaikan itu dibuang supaya tidak ada skrip milik
	// user web yang tertinggal di sistem.
	_ = os.Remove("/var/lib/linux-dashboard/update.sh")
	_ = os.Remove("/var/lib/linux-dashboard/update.log")
	if err := os.WriteFile(updateSkrip, []byte(updateScript), 0o700); err != nil {
		return helperproto.UpdateStatus{}, err
	}
	// Log dikosongkan tiap mulai supaya UI tidak menampilkan campuran keluaran
	// pembaruan lama dan baru.
	if err := os.WriteFile(updateLog, []byte("[i] Memulai pembaruan…\n"), 0o640); err != nil {
		return helperproto.UpdateStatus{}, err
	}
	// Unit yang gagal sebelumnya masih tersimpan namanya; tanpa reset-failed,
	// systemd-run menolak memakai nama yang sama.
	_, _ = run("systemctl", "reset-failed", updateUnit)

	if _, err := run("systemd-run",
		"--unit="+updateUnit,
		"--description=linux-dashboard update",
		// Unit transient tidak mewarisi HOME sama sekali. Tanpa ini `go build`
		// mati dengan "neither GOCACHE nor HOME is defined" dan npm menulis
		// cache-nya ke tempat yang salah — build gagal sebelum apa pun terpasang.
		"--setenv=HOME=/root",
		"--property=StandardOutput=append:"+updateLog,
		"--property=StandardError=append:"+updateLog,
		"/bin/bash", updateSkrip,
	); err != nil {
		return helperproto.UpdateStatus{}, err
	}
	return updateStatus(helperproto.UpdateArgs{}), nil
}
