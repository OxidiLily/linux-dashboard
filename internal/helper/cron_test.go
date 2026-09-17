package helper

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Test jalur cronjob lewat crontab TIRUAN di direktori sementara.
//
// crontab sungguhan milik akun yang menjalankan test ini TIDAK PERNAH
// disentuh: yang diuji adalah seluruh jalur panel — penjaga `previous`, batas
// ukuran, penambahan newline di ujung, pembacaan ulang setelah menulis, dan
// kalimat yang diterima user.
//
// Skrip tiruannya meniru perilaku crontab yang sudah diverifikasi langsung di
// mesin ini (cron 3.0pl1-200ubuntu1):
//   - `crontab -l` tanpa crontab → exit 1, stderr "no crontab for <user>"
//   - isi tanpa newline di ujung   → exit 1, "missing newline before EOF"
//   - baris berawalan "bad"        → exit 1, '"-":1: bad minute'
//
// argv skrip tiruan sama persis dengan crontab asli (hanya "-l" atau "-"),
// sehingga bentuk pemanggilan yang diuji identik dengan produksi.
//
// Path spool DITANAM ke dalam skrip, bukan dibaca dari environment: helper
// menyetel `cmd.Env` secara eksplisit (tanpa mewarisi env induk), jadi variabel
// apa pun yang diset test tidak akan pernah sampai ke subprocess. Menyandarkan
// diri pada env di sini akan membuat test gagal karena alasan yang tidak ada
// hubungannya dengan yang diuji.
const skripCrontabTiruan = `#!/bin/sh
spool="__SPOOL__"
case "$1" in
  -l)
    if [ ! -f "$spool" ]; then
      echo "no crontab for tiruan" >&2
      exit 1
    fi
    cat "$spool"
    exit 0
    ;;
  -)
    # Isi stdin ditulis apa adanya ke berkas sementara lebih dulu. Memakai
    # $(cat) di sini salah: command substitution membuang newline di ujung,
    # jadi pemeriksaan byte terakhirnya tidak pernah bisa benar.
    tmp="$spool.tmp"
    cat > "$tmp"
    if [ -s "$tmp" ]; then
      akhir=$(tail -c 1 "$tmp" | od -An -c | tr -d ' ')
      if [ "$akhir" != '\n' ]; then
        echo "new crontab file is missing newline before EOF, can't install." >&2
        rm -f "$tmp"
        exit 1
      fi
      if grep -q '^bad' "$tmp"; then
        echo '"-":1: bad minute' >&2
        echo "errors in crontab file, can't install." >&2
        rm -f "$tmp"
        exit 1
      fi
    fi
    mv "$tmp" "$spool"
    exit 0
    ;;
esac
echo "usage error" >&2
exit 2
`

// pasangCrontabTiruan menukar jalur biner crontab ke skrip tiruan dan
// mengembalikan penunjuk berkas spool-nya. Fungsi pemulih dikembalikan lewat
// t.Cleanup supaya jalur produksi selalu kembali, walau test gagal di tengah.
func pasangCrontabTiruan(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skrip := filepath.Join(dir, "crontab")
	spool := filepath.Join(dir, "spool")
	isi := strings.ReplaceAll(skripCrontabTiruan, "__SPOOL__", spool)
	if err := os.WriteFile(skrip, []byte(isi), 0o755); err != nil {
		t.Fatalf("tulis crontab tiruan: %v", err)
	}
	asli := jalurCron
	jalurCron = func() (string, bool) { return skrip, true }
	t.Cleanup(func() { jalurCron = asli })
	return spool
}

// ujiUser memakai identitas akun yang menjalankan test ini supaya subprocess
// bisa benar-benar dieksekusi tanpa root: setuid ke uid sendiri ditolak
// kernel, dan helper memang sudah punya cabang untuk keadaan itu.
func ujiUser(t *testing.T) *userInfo {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Skipf("identitas user tidak terbaca: %v", err)
	}
	uid, _ := strconv.Atoi(me.Uid)
	gid, _ := strconv.Atoi(me.Gid)
	u := &userInfo{Name: me.Username, UID: uid, GID: gid, Home: me.HomeDir, Shell: "/bin/sh"}
	if gids, err := me.GroupIds(); err == nil {
		for _, g := range gids {
			if n, err := strconv.Atoi(g); err == nil {
				u.Groups = append(u.Groups, uint32(n))
			}
		}
	}
	return u
}

func TestCronTanpaCrontabBukanError(t *testing.T) {
	pasangCrontabTiruan(t)
	u := ujiUser(t)

	isi, err := cronBaca(u)
	if err != nil {
		t.Fatalf("akun tanpa crontab harus terbaca sebagai kosong, bukan gagal: %v", err)
	}
	if isi != "" {
		t.Fatalf("isi harus kosong, dapat %q", isi)
	}
}

func TestCronSimpanDanBacaUlang(t *testing.T) {
	spool := pasangCrontabTiruan(t)
	u := ujiUser(t)

	kosong := ""
	jadwal := "*/5 * * * * /usr/bin/echo halo\n"
	hasil, err := cronPut(u, helperproto.CronArgs{Isi: jadwal, Previous: &kosong})
	if err != nil {
		t.Fatalf("simpan crontab: %v", err)
	}
	// Yang dikembalikan adalah isi yang DIBACA ULANG dari crontab, bukan
	// kutipan dari yang dikirim — penulisan yang gagal mendarat harus terlihat.
	if hasil.Isi != jadwal {
		t.Fatalf("isi hasil = %q, harap %q", hasil.Isi, jadwal)
	}
	if hasil.Batas != helperproto.CronMaxBytes {
		t.Fatalf("batas yang dilaporkan = %d, harap %d", hasil.Batas, helperproto.CronMaxBytes)
	}
	diDisk, err := os.ReadFile(spool)
	if err != nil {
		t.Fatalf("baca spool: %v", err)
	}
	if string(diDisk) != jadwal {
		t.Fatalf("spool berisi %q, harap %q", diDisk, jadwal)
	}
}

func TestCronNewlineUjungDitambahkan(t *testing.T) {
	spool := pasangCrontabTiruan(t)
	u := ujiUser(t)

	// Tanpa penambahan newline, crontab menolak isinya dengan "missing newline
	// before EOF" — pesan yang tidak menyebut apa yang harus dilakukan.
	kosong := ""
	hasil, err := cronPut(u, helperproto.CronArgs{Isi: "0 3 * * * /usr/bin/true", Previous: &kosong})
	if err != nil {
		t.Fatalf("isi tanpa newline di ujung harus tetap bisa disimpan: %v", err)
	}
	if !strings.HasSuffix(hasil.Isi, "\n") {
		t.Fatalf("isi tersimpan harus diakhiri newline, dapat %q", hasil.Isi)
	}
	diDisk, _ := os.ReadFile(spool)
	if !strings.HasSuffix(string(diDisk), "\n") {
		t.Fatalf("spool harus diakhiri newline, dapat %q", diDisk)
	}
}

func TestCronKonflikDitolak(t *testing.T) {
	pasangCrontabTiruan(t)
	u := ujiUser(t)

	kosong := ""
	if _, err := cronPut(u, helperproto.CronArgs{Isi: "*/5 * * * * /usr/bin/first\n", Previous: &kosong}); err != nil {
		t.Fatalf("simpan pertama: %v", err)
	}
	// Simpan kedua masih menyangka crontab-nya kosong (tab yang basi). Tanpa
	// penjaga ini, jadwal pertama hilang tanpa jejak.
	_, err := cronPut(u, helperproto.CronArgs{Isi: "*/10 * * * * /usr/bin/kedua\n", Previous: &kosong})
	if err == nil {
		t.Fatalf("simpan dengan previous basi harus ditolak")
	}
	if code := kodeErr(err); code != helperproto.ErrCronConflict {
		t.Fatalf("kode error = %q, harap %q", code, helperproto.ErrCronConflict)
	}
	// Isi di spool tidak boleh berubah.
	isi, _ := cronBaca(u)
	if isi != "*/5 * * * * /usr/bin/first\n" {
		t.Fatalf("crontab berubah walau ditolak: %q", isi)
	}
}

func TestCronPreviousWajib(t *testing.T) {
	pasangCrontabTiruan(t)
	u := ujiUser(t)

	// `previous` yang tidak dikirim harus DITOLAK, bukan diperlakukan sebagai
	// "crontab kosong" — kalau tidak, klien mana pun bisa menimpa jadwal
	// dengan sekali PUT tanpa pernah membacanya.
	if _, err := cronPut(u, helperproto.CronArgs{Isi: "*/5 * * * * /usr/bin/x\n"}); err == nil {
		t.Fatalf("permintaan tanpa previous harus ditolak")
	}
}

func TestCronIsiTerlaluBesarditolak(t *testing.T) {
	spool := pasangCrontabTiruan(t)
	u := ujiUser(t)

	besar := strings.Repeat("# padding\n", helperproto.CronMaxBytes/10+10)
	kosong := ""
	_, err := cronPut(u, helperproto.CronArgs{Isi: besar, Previous: &kosong})
	if err == nil {
		t.Fatalf("isi di atas %d byte harus ditolak", helperproto.CronMaxBytes)
	}
	if _, statErr := os.Stat(spool); statErr == nil {
		t.Fatalf("spool tidak boleh dibuat untuk isi yang ditolak")
	}
}

func TestCronBatasDiperiksaSetelahNewlineDitambahkan(t *testing.T) {
	spool := pasangCrontabTiruan(t)
	u := ujiUser(t)
	kosong := ""

	isi := strings.Repeat("#", helperproto.CronMaxBytes)
	_, err := cronPut(u, helperproto.CronArgs{Isi: isi, Previous: &kosong})
	if err == nil {
		t.Fatalf("isi yang melampaui batas setelah normalisasi harus ditolak")
	}
	if _, statErr := os.Stat(spool); !os.IsNotExist(statErr) {
		t.Fatalf("spool tidak boleh berubah, stat error = %v", statErr)
	}
}

func TestCronMenolakCrontabRoot(t *testing.T) {
	pasangCrontabTiruan(t)
	u := ujiUser(t)
	u.UID = 0
	if _, err := cronGet(u); err == nil || kodeErr(err) != helperproto.ErrDenied {
		t.Fatalf("cronGet root harus ditolak, dapat %v", err)
	}
	kosong := ""
	if _, err := cronPut(u, helperproto.CronArgs{Previous: &kosong}); err == nil || kodeErr(err) != helperproto.ErrDenied {
		t.Fatalf("cronPut root harus ditolak, dapat %v", err)
	}
}

func TestCronSintaksDitolakCrontab(t *testing.T) {
	pasangCrontabTiruan(t)
	u := ujiUser(t)

	kosong := ""
	_, err := cronPut(u, helperproto.CronArgs{Isi: "bad minute di sini\n", Previous: &kosong})
	if err == nil {
		t.Fatalf("isi yang ditolak crontab harus jadi error, bukan sukses diam")
	}
	// Kalimat crontab-nya ikut dibawa: di sanalah nomor baris dan alasannya.
	if !strings.Contains(err.Error(), "bad minute") {
		t.Fatalf("alasan crontab harus ikut terbawa, dapat %q", err.Error())
	}
}

func TestCronHapusSemuaJadwal(t *testing.T) {
	spool := pasangCrontabTiruan(t)
	u := ujiUser(t)

	kosong := ""
	ada, err := cronPut(u, helperproto.CronArgs{Isi: "*/5 * * * * /usr/bin/x\n", Previous: &kosong})
	if err != nil {
		t.Fatalf("simpan: %v", err)
	}
	// Menyimpan isi kosong = hapus semua jadwal, dan itu harus benar-benar
	// terjadi — bukan diam-diam tidak menulis apa pun.
	hasil, err := cronPut(u, helperproto.CronArgs{Isi: "", Previous: &ada.Isi})
	if err != nil {
		t.Fatalf("hapus semua jadwal: %v", err)
	}
	if hasil.Isi != "" {
		t.Fatalf("isi harus kosong, dapat %q", hasil.Isi)
	}
	diDisk, _ := os.ReadFile(spool)
	if len(diDisk) != 0 {
		t.Fatalf("spool harus kosong, dapat %q", diDisk)
	}
}

func TestCronTanpaBinerCrontab(t *testing.T) {
	asli := jalurCron
	jalurCron = func() (string, bool) { return "", false }
	t.Cleanup(func() { jalurCron = asli })

	u := ujiUser(t)
	_, err := cronBaca(u)
	if err == nil {
		t.Fatalf("mesin tanpa crontab harus melapor, bukan mengembalikan kosong")
	}
	if !strings.Contains(err.Error(), "crontab tidak ada") {
		t.Fatalf("kalimatnya harus menyebut penyebabnya, dapat %q", err.Error())
	}
}

func TestTanpaCrontabCocokLonggar(t *testing.T) {
	// Kalimatnya berbeda antar-versi/terjemahan; yang dicocokkan hanya kata
	// yang selalu ada.
	for _, s := range []string{
		"no crontab for pc",
		"No crontab for root",
		"no crontab for user\n",
	} {
		if !tanpaCrontab(s) {
			t.Fatalf("%q harus dikenali sebagai 'tidak ada crontab'", s)
		}
	}
	for _, s := range []string{
		"errors in crontab file, can't install.",
		"bad minute",
		"missing newline before EOF",
		"",
	} {
		if tanpaCrontab(s) {
			t.Fatalf("%q TIDAK boleh dianggap 'tidak ada crontab'", s)
		}
	}
}

// kodeErr mengambil kode error helper tanpa mengimpor helperclient (paket test
// ini ada di dalam paket helper, dan helperclient mengimpor helperproto saja).
func kodeErr(err error) string {
	var he *helperErr
	if errors.As(err, &he) {
		return he.code
	}
	return ""
}
