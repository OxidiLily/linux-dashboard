package helper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Pengelolaan crontab per akun.
//
// Yang dikelola di sini adalah crontab MILIK AKUN YANG LOGIN (`crontab -l` /
// `crontab -`), bukan /etc/crontab dan bukan crontab root. Perintahnya
// dijalankan sebagai identitas akun itu lewat SysProcAttr.Credential, jadi
// kernel yang menentukan crontab siapa yang tersentuh — panel tidak perlu
// (dan tidak boleh) tahu path spool-nya, yang berbeda antar-distribusi
// (/var/spool/cron/crontabs di Debian/Ubuntu, /var/spool/cron di RedHat).
// crontab juga menolak dijalankan sebagai root di sebagian sistem, dan
// berjalan sebagai user adalah bentuk yang memang diuji paketnya sendiri.

// batasJalanCron membatasi satu pemanggilan crontab. crontab hanya membaca
// atau menulis berkas kecil, jadi ini jauh lebih longgar dari kebutuhan
// normalnya; yang dicegah adalah permintaan yang menggantung tanpa ujung.
const batasJalanCron = 5 * time.Second

// kunciCron menyerialkan urutan baca-banding-tulis.
//
// Penjaga `previous` saja tidak cukup: dua permintaan Simpan yang datang
// hampir bersamaan sama-sama membaca isi lama, sama-sama cocok, lalu
// sama-sama menulis — dan salah satunya hilang tanpa ada yang tahu. Kunci ini
// berlaku untuk seluruh proses helper, dan itu memang cukup: crontab ditulis
// jauh lebih jarang daripada dibaca, dan penulisan dari akun berbeda pun tidak
// perlu saling menunggu lebih dari beberapa milidetik.
var kunciCron sync.Mutex

// jalurCron dipisah jadi variabel supaya test bisa menunjuk crontab tiruan.
// Produksi selalu memakai biner crontab yang benar-benar ada di mesin — tidak
// ada satu pun jalur dari input user menuju variabel ini.
var jalurCron = func() (string, bool) { return lookBinary("crontab") }

// layananCron mengembalikan nama unit penjadwal yang berjalan di mesin ini.
//
// Ini bukan pelengkap: crontab yang tidak pernah dijalankan siapa pun adalah
// kegagalan yang paling sulit dilacak dari UI — jadwalnya tampak rapi dan
// waktunya sudah lewat, tapi tidak ada apa pun yang terjadi. Nama unitnya
// berbeda antar-distribusi (cron di Debian/Ubuntu, crond di keluarga RedHat),
// jadi keduanya diperiksa.
func layananCron() (string, bool) {
	for _, unit := range []string{"cron", "crond"} {
		ctx, batal := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit)
		err := cmd.Run()
		batal()
		if err == nil {
			return unit, true
		}
	}
	return "", false
}

// penampungTerbatas menampung keluaran subprocess sampai plafon, lalu membuang
// sisanya sambil menandai bahwa ada yang dibuang.
//
// Mengembalikan error dari Write bukan pilihan: `os/exec` akan menghentikan
// penyalinan dan menutup pipe, sehingga crontab mati dengan SIGPIPE — dan yang
// dilaporkan jadi "signal: broken pipe", bukan "keluaran terlalu panjang".
type penampungTerbatas struct {
	buf   bytes.Buffer
	maks  int
	lebih bool
}

func (p *penampungTerbatas) Write(b []byte) (int, error) {
	sisa := p.maks - p.buf.Len()
	if sisa <= 0 {
		p.lebih = true
		return len(b), nil
	}
	if len(b) > sisa {
		p.buf.Write(b[:sisa])
		p.lebih = true
		return len(b), nil
	}
	p.buf.Write(b)
	return len(b), nil
}

func (p *penampungTerbatas) String() string { return p.buf.String() }

// envCron adalah lingkungan minimum yang dibutuhkan crontab. HOME/USER/LOGNAME
// wajib sama seperti pada jalur per-user lain di helper ini: tanpa itu crontab
// mencari spool relatif HOME proses pemanggil (root), dan hasilnya bisa
// terbaca sebagai "tidak ada crontab" untuk akun yang jelas punya.
func envCron(u *userInfo) []string {
	shell := u.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + u.Home,
		"USER=" + u.Name,
		"LOGNAME=" + u.Name,
		"SHELL=" + shell,
		"LC_ALL=C",
	}
}

// jalanCron menjalankan crontab sebagai user target. stdin hanya diisi untuk
// penulisan; keluaran dibatasi plafon yang sama dengan batas isi crontab,
// supaya berkas spool raksasa tidak bisa membanjiri memori helper.
//
// Exit code non-nol TIDAK diterjemahkan jadi error di sini: crontab memakai
// status 1 baik untuk "akun ini belum punya crontab" (keadaan normal) maupun
// untuk penolakan sintaks (kegagalan nyata). Yang bisa membedakannya hanya
// pemanggil, lewat `stderr` yang dikembalikan apa adanya.
func jalanCron(u *userInfo, stdin string, args ...string) (helperproto.ExecResult, error) {
	jalur, ada := jalurCron()
	if !ada {
		return helperproto.ExecResult{}, errKode(helperproto.ErrNilaiTidakValid,
			"crontab tidak ada di mesin ini — pasang paket cron dulu (apt install cron)")
	}

	ctx, batal := context.WithTimeout(context.Background(), batasJalanCron)
	defer batal()

	cmd := exec.CommandContext(ctx, jalur, args...)
	cmd.Dir = "/"
	cmd.Env = envCron(u)
	// Turunkan privilege ke akun itu — kecuali saat proses ini SUDAH berjalan
	// sebagai user tersebut. setuid ke uid sendiri ditolak kernel (EPERM), dan
	// tanpa cabang ini seluruh jalur cronjob tidak bisa diuji tanpa root.
	// Cabangnya aman secara default-deny: ia hanya menyala kalau identitas
	// proses memang identik dengan identitas yang dituju, jadi tidak ada
	// penurunan privilege yang dilewati.
	if !identitasSama(u) {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential()}
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout := &penampungTerbatas{maks: helperproto.CronMaxBytes}
	stderr := &penampungTerbatas{maks: 4 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	res := helperproto.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		return res, errKode(helperproto.ErrNilaiTidakValid,
			"crontab tidak selesai dalam %s dan dihentikan", batasJalanCron)
	}
	if stdout.lebih {
		return res, errKode(helperproto.ErrNilaiTidakValid,
			"crontab akun ini lebih dari %d KiB — rapikan dulu di luar panel", helperproto.CronMaxBytes>>10)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, errDenied("gagal menjalankan crontab: %v", err)
	}
	return res, nil
}

// identitasSama melaporkan apakah proses yang berjalan sekarang sudah memiliki
// identitas user target: uid ketemu uid, dan seluruh gid-nya sudah termuat di
// daftar grup proses.
func identitasSama(u *userInfo) bool {
	if os.Geteuid() != u.UID {
		return false
	}
	punya, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, g := range u.Groups {
		if !slices.Contains(punya, int(g)) {
			return false
		}
	}
	return true
}

// tanpaCrontab menandai keadaan normal "akun ini belum punya crontab".
// Kalimatnya berasal dari crontab sendiri dan berbeda antar-versi, jadi
// dicocokkan longgar pada kata yang selalu ada.
func tanpaCrontab(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "no crontab")
}

// cronBaca mengambil crontab akun ini apa adanya.
func cronBaca(u *userInfo) (string, error) {
	res, err := jalanCron(u, "", "-l")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		if tanpaCrontab(res.Stderr) {
			return "", nil
		}
		return "", errDenied("gagal membaca crontab: %s",
			firstNonEmpty(strings.TrimSpace(res.Stderr), "exit "+strconv.Itoa(res.ExitCode)))
	}
	return res.Stdout, nil
}

// cronHasil menyusun jawaban lengkap: isi terbaru plus keadaan penjadwalnya.
// Isinya selalu dibaca ULANG dari crontab, bukan dikutip dari yang dikirim
// klien — penulisan yang "berhasil" tapi tidak mendarat jadi terlihat di layar.
func cronHasil(u *userInfo, isi string) helperproto.CronHasil {
	nama, aktif := layananCron()
	return helperproto.CronHasil{
		Isi:          isi,
		Batas:        helperproto.CronMaxBytes,
		Layanan:      nama,
		LayananAktif: aktif,
	}
}

func cronGet(u *userInfo) (helperproto.CronHasil, error) {
	if u.UID == 0 {
		return helperproto.CronHasil{}, errDenied("crontab root tidak dapat dikelola dari panel")
	}
	isi, err := cronBaca(u)
	if err != nil {
		return helperproto.CronHasil{}, err
	}
	return cronHasil(u, isi), nil
}

// cronPut memasang crontab baru setelah memastikan isinya memang yang
// diharapkan.
func cronPut(u *userInfo, args helperproto.CronArgs) (helperproto.CronHasil, error) {
	if u.UID == 0 {
		return helperproto.CronHasil{}, errDenied("crontab root tidak dapat dikelola dari panel")
	}
	// `Previous` adalah penunjuk, bukan string: "belum pernah memuat" harus
	// bisa dibedakan dari "memuat crontab yang memang kosong". Tanpa
	// pembedaan itu, klien yang melewatkan field ini lolos sebagai penimpa
	// bebas — persis yang penjaga ini ada untuk mencegahnya.
	if args.Previous == nil {
		return helperproto.CronHasil{}, errKode(helperproto.ErrNilaiTidakValid,
			"permintaan simpan crontab tidak menyertakan isi terakhir yang dilihat")
	}
	isi := args.Isi
	if isi != "" && !strings.HasSuffix(isi, "\n") {
		isi += "\n"
	}
	if len(isi) > helperproto.CronMaxBytes {
		return helperproto.CronHasil{}, errKode(helperproto.ErrNilaiTidakValid,
			"isi crontab %d byte melebihi batas %d KiB", len(isi), helperproto.CronMaxBytes>>10)
	}

	kunciCron.Lock()
	defer kunciCron.Unlock()

	sekarang, err := cronBaca(u)
	if err != nil {
		return helperproto.CronHasil{}, err
	}
	if sekarang != *args.Previous {
		return helperproto.CronHasil{}, &helperErr{
			code:   helperproto.ErrCronConflict,
			msg:    "crontab berubah sejak terakhir dimuat — muat ulang sebelum menyimpan",
			kodeUI: helperproto.ErrCronConflict,
		}
	}

	res, err := jalanCron(u, isi, "-")
	if err != nil {
		return helperproto.CronHasil{}, err
	}
	if res.ExitCode != 0 {
		// crontab menolak sintaksnya; kalimatnya dikembalikan apa adanya
		// karena di situlah nomor baris dan alasan penolakannya disebut.
		return helperproto.CronHasil{}, errKode(helperproto.ErrNilaiTidakValid,
			"crontab menolak isinya — %s",
			firstNonEmpty(strings.TrimSpace(res.Stderr), "exit "+strconv.Itoa(res.ExitCode)))
	}

	baru, err := cronBaca(u)
	if err != nil {
		return helperproto.CronHasil{}, err
	}
	return cronHasil(u, baru), nil
}
