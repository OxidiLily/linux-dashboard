package helper

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// unitHeadroomPerluGanti harus mengenali SEMUA generasi unit salah tulis dan
// tidak pernah menyentuh unit kustom admin.
func TestUnitHeadroomPerluGanti(t *testing.T) {
	if unitHeadroomPerluGanti(string(unitHeadroomTertanam)) {
		t.Fatalf("unit tertanam (jembatan sudah utuh) tidak boleh dianggap perlu ganti")
	}
	lama := "[Service]\nExecStart=/opt/headroom/bin/headroom proxy --host 127.0.0.1 --port 8787\nRestart=on-failure\n"
	if !unitHeadroomPerluGanti(lama) {
		t.Fatalf("unit generasi pertama (tanpa ExecStartPost) harus dianggap perlu ganti")
	}
	kustom := "[Service]\nExecStart=/opt/headroom/bin/headroom proxy --host 127.0.0.1 --port 9999\n"
	if unitHeadroomPerluGanti(kustom) {
		t.Fatalf("unit kustom (port lain) tetap milik admin — tidak boleh ditimpa")
	}
}

// Generasi KEDUA — unit yang sudah punya ExecStartPost tapi memakai specifier
// "%s" yang disuntik systemd menjadi "/bin/bash" — wajib ikut dikenali.
// Pengecekan lama ("ada ExecStartPost atau tidak") meloloskannya, dan unit
// seperti itu tidak akan pernah memperbaiki diri: proxy.pid selalu berisi
// string "/bin/bash", bukan pid apa pun.
func TestUnitHeadroomGenerasiKeduaPerluGanti(t *testing.T) {
	// Persis baris yang benar-benar terpasang di mesin 2026-09-17, dibaca dari
	// `systemctl show -p ExecStartPost --value headroom`.
	kedua := "[Service]\n" +
		"ExecStart=/opt/headroom/bin/headroom proxy --host 127.0.0.1 --port 8787\n" +
		`ExecStartPost=/bin/sh -c 'install -d "$HOME/.9router/headroom" && printf "%s\n" "$MAINPID" > "$HOME/.9router/headroom/proxy.pid"'` + "\n"
	if !unitHeadroomPerluGanti(kedua) {
		t.Fatalf("unit dengan specifier %%s di ExecStartPost tidak boleh dianggap benar")
	}
}

// Specifier systemd TIDAK boleh muncul di ExecStartPost. Ini penjaga regresi
// paling penting untuk bug yang ditemukan: "%s" terlihat persis seperti
// placeholder printf, jadi tanpa uji ini ia akan ditulis ulang dengan yakin
// oleh siapa pun yang menyunting unit ini. "%d" sama bahayanya — ia diganti
// nilai kredensial systemd.
func TestJembatanPidBebasSpecifierSystemd(t *testing.T) {
	if strings.Contains(jembatanPidHeadroom, "%") {
		t.Fatalf("ExecStartPost memuat '%%' — systemd akan menggantinya sebelum shell melihatnya: %q",
			jembatanPidHeadroom)
	}
	baris := execStartPostUnit(string(unitHeadroomTertanam))
	if baris == "" {
		t.Fatalf("unit tertanam tidak punya ExecStartPost")
	}
	if strings.Contains(baris, "%") {
		t.Fatalf("ExecStartPost di unit tertanam memuat specifier systemd: %q", baris)
	}
}

// Baris jembatan yang dipakai Go harus sama persis dengan yang ada di unit
// tertanam, dan unit harus punya tepat satu ExecStartPost. Beda satu karakter
// → unit selalu dianggap perlu ganti dan service di-restart setiap kali
// pemasangan atau tombol Jalankan ditekan.
func TestJembatanPidSamaDenganUnitTertanam(t *testing.T) {
	if !jembatanPidHeadroomUtuh(string(unitHeadroomTertanam)) {
		t.Fatalf("unit tertanam tidak memuat jembatan pid file %q", jembatanPidHeadroom)
	}
	if n := strings.Count(string(unitHeadroomTertanam), "ExecStartPost="); n != 1 {
		t.Fatalf("unit harus punya tepat satu ExecStartPost, ada %d", n)
	}
	if got := execStartPostUnit(string(unitHeadroomTertanam)); got != jembatanPidHeadroom {
		t.Fatalf("ExecStartPost unit tertanam\n  %q\nberbeda dari konstanta Go\n  %q", got, jembatanPidHeadroom)
	}
}

// Baris ExecStartPost dari ARTEFAK YANG DIKIRIM harus meninggalkan PID proxy di
// proxy.pid saat dijalankan /bin/sh (dash) dengan $MAINPID dan $HOME seperti
// yang diberikan systemd. Ini eksekusi shell sungguhan, bukan pemeriksaan
// string: bug yang ditemukan hanya terlihat dengan benar-benar menjalankannya.
func TestJembatanPidMenulisMainpidSungguhan(t *testing.T) {
	baris := execStartPostUnit(string(unitHeadroomTertanam))
	argv := perintahDariBaris(baris)
	if len(argv) == 0 {
		t.Fatalf("baris ExecStartPost tidak bisa dibaca sebagai perintah: %q", baris)
	}
	dir := t.TempDir()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "HOME="+dir, "MAINPID=424242")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ExecStartPost gagal: %v — keluaran: %s", err, out)
	}
	isi, err := os.ReadFile(dir + "/.9router/headroom/proxy.pid")
	if err != nil {
		t.Fatalf("proxy.pid tidak tertulis: %v", err)
	}
	if got := strings.TrimSpace(string(isi)); got != "424242" {
		t.Fatalf("proxy.pid berisi %q, seharusnya \"424242\"", got)
	}
}

// Bentuk yang SUDAH rusak harus tetap terbaca rusak oleh uji yang sama. Tanpa
// ini, uji di atas tidak membedakan apa pun.
func TestJembatanLamaTidakMenulisPidProxy(t *testing.T) {
	// Yang dijalankan dash setelah systemd mengganti "%s" dengan shell manager.
	rusak := `install -d "$HOME/.9router/headroom" && printf "/bin/bash\n" "$MAINPID" > "$HOME/.9router/headroom/proxy.pid"`
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", rusak)
	cmd.Env = append(os.Environ(), "HOME="+dir, "MAINPID=424242")
	_ = cmd.Run()
	isi, err := os.ReadFile(dir + "/.9router/headroom/proxy.pid")
	if err != nil {
		t.Fatalf("berkas tidak dibuat sama sekali — bentuk rusak tidak terdeteksi: %v", err)
	}
	if got := strings.TrimSpace(string(isi)); got == "424242" {
		t.Fatalf("bentuk rusak dianggap benar: proxy.pid berisi %q", got)
	}
	if got := strings.TrimSpace(string(isi)); got != "/bin/bash" {
		t.Fatalf("bentuk rusak seharusnya menulis \"/bin/bash\", ternyata %q", got)
	}
}

// prosesHeadroom harus mengenali proses headroom yang sungguhan dan menolak
// segala hal lain. Ini penjaga bagi rebutHeadroomDari9router: pid dari
// proxy.pid yang basi bisa menunjuk proses siapa pun, dan mengirim SIGTERM ke
// sana berarti panel membunuh proses tak berdosa.
func TestProsesHeadroom(t *testing.T) {
	if !prosesHidup(os.Getpid()) {
		t.Fatalf("prosesHidup gagal untuk proses uji ini sendiri")
	}
	if prosesHeadroom(os.Getpid()) {
		t.Fatalf("proses uji (go test) tidak boleh dianggap headroom")
	}

	// Proses yang menamai dirinya "python" tapi bukan headroom: /usr/bin/python3.
	// Ini yang menutup lubang "cocokkan saja prefix python".
	pythonBiasa, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 tidak ada, uji proses dilewati")
	}
	proc := spawnTidur(t, pythonBiasa, "-c", "import time; time.sleep(30)")
	defer proc.Process.Kill()
	if prosesHeadroom(proc.Process.Pid) {
		t.Fatalf("python3 biasa (cmdline tanpa /headroom) tidak boleh dianggap headroom")
	}

	// Proses yang benar, dalam bentuk yang SAMA dengan proses nyata di mesin
	// ini: comm "python3", argv memuat path entri headroom. Argumen terakhir
	// diabaikan python, jadi ini murah dan tidak menjalankan proxy kedua.
	if _, err := os.Stat("/opt/headroom/bin/python3"); err == nil {
		hd := spawnTidur(t, "/opt/headroom/bin/python3", "-c", "import time; time.sleep(30)",
			"/opt/headroom/bin/headroom")
		defer hd.Process.Kill()
		if !prosesHeadroom(hd.Process.Pid) {
			t.Fatalf("proses dengan argv memuat entri headroom seharusnya dikenali")
		}
	}

	// Dan proses headroom yang benar-benar berjalan di mesin ini harus dikenali
	// oleh fungsi yang sama — tidak ada yang lebih meyakinkan daripada ini.
	for _, pid := range pgrepHeadroom() {
		if !prosesHeadroom(pid) {
			t.Fatalf("pid %d adalah proses headroom yang sedang jalan, seharusnya dikenali", pid)
		}
		t.Logf("pid headroom yang berjalan dikenali: %d", pid)
	}

	// Proses yang sudah mati / pid yang tidak ada → bukan headroom. Dipakai
	// angka di atas pid_max supaya hasilnya deterministik: kernel tidak pernah
	// mengalokasikan nomor itu, jadi tidak ada risiko pid-nya didaur ulang
	// proses lain saat uji berjalan.
	batas, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err != nil {
		t.Skip("pid_max tidak terbaca, uji pid mati dilewati")
	}
	pidMax, err := strconv.Atoi(strings.TrimSpace(string(batas)))
	if err != nil {
		t.Skip("pid_max tidak bisa dibaca sebagai angka")
	}
	if prosesHeadroom(pidMax + 1) {
		t.Fatalf("pid %d tidak pernah ada, tidak boleh dianggap headroom", pidMax+1)
	}
}

// pgrepHeadroom mengembalikan pid proses headroom yang sedang berjalan di mesin
// ini, kalau ada. Dipakai uji untuk memeriksa fungsi yang sama terhadap proses
// sungguhan, bukan hanya proses buatan.
func pgrepHeadroom() []int {
	out, err := exec.Command("pgrep", "-x", "headroom").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if p, err := strconv.Atoi(f); err == nil {
			pids = append(pids, p)
		}
	}
	return pids
}

// spawnTidur menjalankan proses yang hidup cukup lama untuk diperiksa, lalu
// MENUNGGU exec-nya benar-benar selesai.
//
// cmd.Start() kembali setelah fork, bukan setelah execve. Tanpa penantian ini
// /proc/<pid> masih memperlihatkan binary go test, dan uji positif gagal
// sementara uji negatif lolos karena alasan yang salah — uji yang tidak menguji
// apa pun.
func spawnTidur(t *testing.T, nama string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(nama, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("gagal menjalankan %s: %v", nama, err)
	}
	tenggat := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/cmdline")
		if err == nil && strings.HasPrefix(string(b), nama+"\x00") {
			return cmd
		}
		if time.Now().After(tenggat) {
			cmd.Process.Kill()
			t.Fatalf("proses %s (pid %d) tidak selesai exec dalam 5 detik", nama, cmd.Process.Pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// execStartPostUnit mengambil isi baris ExecStartPost dari sebuah unit.
func execStartPostUnit(isi string) string {
	for _, l := range strings.Split(isi, "\n") {
		if strings.HasPrefix(l, "ExecStartPost=") {
			return strings.TrimPrefix(l, "ExecStartPost=")
		}
	}
	return ""
}

// perintahDariBaris memecah satu baris ExecStartPost menjadi argv seperti yang
// dilakukan systemd pada bentuk "/bin/sh -c '…'": kutip tunggal pembuka dan
// penutup dihilangkan, isinya menjadi satu argumen setelah -c. Bentuk inilah
// satu-satunya yang dipakai unit headroom.
func perintahDariBaris(baris string) []string {
	const prefiks = "/bin/sh -c '"
	if !strings.HasPrefix(baris, prefiks) || !strings.HasSuffix(baris, "'") {
		return nil
	}
	return []string{"/bin/sh", "-c", strings.TrimSuffix(strings.TrimPrefix(baris, prefiks), "'")}
}
