package api

import (
	"strconv"
	"testing"
	"time"
)

// Kegagalan prompt password ulang (reset sesi terminal) tidak boleh memblokir
// LOGIN username yang sama: keduanya memakai kredensial yang sama, tapi
// penghitungnya dipisah supaya salah-ketik di prompt sudo — dari sesi yang
// memang sudah login — tidak mengunci pintu masuk.
func TestKegagalanPromptSudoTidakMemblokirLogin(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	kunci := reauthThrottleKey("budi")
	if kunci == "budi" {
		t.Fatal("kunci throttle prompt sudo harus berbeda dari kunci login")
	}
	for i := 0; i < throttleUserMax; i++ {
		th.recordAt(kunci, "192.0.2.9", now)
	}
	if ok, _ := th.allowedAt(kunci, "192.0.2.9", now); ok {
		t.Error("prompt sudo seharusnya sudah dibatasi setelah " + strconv.Itoa(throttleUserMax) + " kegagalan")
	}
	if ok, _ := th.allowedAt("budi", "192.0.2.9", now); !ok {
		t.Error("login username yang sama ikut terblokir oleh kegagalan prompt sudo")
	}
}

// Tanpa batas per alamat, penyerang bisa mengirim ribuan percobaan gagal dengan
// username acak: setiap percobaan membuat key baru, dan karena jumlah key
// dibatasi, yang terbuang justru catatan atas akun korban (percobaan terakhirnya
// paling lama). Batas per alamat menutup jalur itu.
func TestThrottleMembatasiSatuAlamatWalauUsernameAcak(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	for i := 0; i < throttleIPMax; i++ {
		th.recordAt("acak"+strconv.Itoa(i), "192.0.2.9", now)
	}
	if ok, _ := th.allowedAt("korban", "192.0.2.9", now); ok {
		t.Errorf("alamat dengan %d kegagalan masih boleh mencoba", throttleIPMax)
	}
}

func TestThrottlePerAlamatTidakMemblokirAlamatLain(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	for i := 0; i < throttleIPMax; i++ {
		th.recordAt("acak"+strconv.Itoa(i), "192.0.2.9", now)
	}
	if ok, _ := th.allowedAt("korban", "198.51.100.4", now); !ok {
		t.Error("alamat lain ikut terblokir oleh kegagalan dari alamat pertama")
	}
}

// Login yang berhasil mengosongkan catatan username, tetapi TIDAK catatan
// alamat: kalau dikosongkan, pemegang satu kredensial sah bisa berulang kali
// menghapus jejak percobaan gagal dari alamatnya sendiri.
func TestThrottleResetTidakMenghapusCatatanAlamat(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	th.recordAt("budi", "192.0.2.9", now)
	th.reset("budi", "192.0.2.9")
	if ok, _ := th.allowedAt("budi", "192.0.2.9", now); !ok {
		t.Fatal("login yang berhasil tidak boleh menyisakan blokir")
	}
	for i := 0; i < throttleIPMax-1; i++ {
		th.recordAt("acak"+strconv.Itoa(i), "192.0.2.9", now)
	}
	if ok, _ := th.allowedAt("budi", "192.0.2.9", now); ok {
		t.Error("catatan alamat terhapus oleh login yang berhasil")
	}
}

// Catatan per alamat juga dibersihkan oleh GC, sama seperti dua map lainnya.
func TestThrottleGCMembersihkanCatatanAlamat(t *testing.T) {
	th := newThrottle()
	lama := time.Now().Add(-2 * throttleWindow)
	th.recordAt("budi", "192.0.2.9", lama)
	th.gc(time.Now())
	th.mu.Lock()
	defer th.mu.Unlock()
	if len(th.perIP) != 0 {
		t.Errorf("perIP masih menyimpan %d entri setelah GC", len(th.perIP))
	}
}
