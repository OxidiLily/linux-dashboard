package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// X-Forwarded-For / X-Real-IP bisa ditulis siapa saja yang bisa menjangkau
// port panel. Kalau nilainya dijadikan identitas atau key pembatas login,
// pembatas itu cukup dilewati dengan mengganti header tiap lima percobaan.
func TestClientIPMengabaikanHeaderForwarded(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = "192.0.2.4:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("X-Real-IP", "203.0.113.10")
	r.Header.Set("True-Client-IP", "203.0.113.11")

	if got := clientIP(r); got != "192.0.2.4" {
		t.Fatalf("clientIP = %q, ingin 192.0.2.4 (alamat peer TCP)", got)
	}
}

func TestClientIPAlamatTanpaPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "2001:db8::1"
	if got := clientIP(r); got != "2001:db8::1" {
		t.Fatalf("clientIP = %q, ingin 2001:db8::1", got)
	}
}

// Bentuk yang benar-benar dikirim net/http untuk IPv6 selalu berbentuk bracket
// plus port ("[::1]:1234"), bukan alamat telanjang — jadi bentuk itulah yang
// wajib benar; tanpa kurung siku, SplitHostPort gagal dan alamatnya dianggap
// satu kesatuan, sehingga semua klien IPv6 terlihat sebagai satu alamat saja.
func TestClientIPIPv6Berkurung(t *testing.T) {
	cases := []struct{ remote, mau string }{
		{"[::1]:1234", "::1"},
		{"[2001:db8::1]:8080", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if got := clientIP(r); got != c.mau {
			t.Errorf("clientIP(%q) = %q, ingin %q", c.remote, got, c.mau)
		}
	}
}

// Penyerang yang bisa memakai banyak alamat sumber (IPv6 /64, botnet, proxy
// yang memakai header forwarded) tidak boleh bisa menebak password satu akun
// tanpa batas hanya karena tiap alamat memakai kuota sendiri.
func TestThrottleMembatasiUsernameLintasAlamat(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	for i := 0; i < throttleUserMax; i++ {
		th.recordAt("alice", "198.51.100."+strconv.Itoa(i+1), now)
	}
	if ok, _ := th.allowedAt("alice", "203.0.113.7", now); ok {
		t.Fatal("username bisa ditebak tanpa batas dengan mengganti alamat sumber")
	}
	// User lain tidak ikut terkunci.
	if ok, _ := th.allowedAt("bob", "203.0.113.7", now); !ok {
		t.Fatal("percobaan user lain ikut diblokir")
	}
}

func TestThrottleMembatasiPerAlamat(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	for i := 0; i < throttleMax; i++ {
		th.recordAt("alice", "192.0.2.1", now)
	}
	if ok, _ := th.allowedAt("alice", "192.0.2.1", now); ok {
		t.Fatal("lima percobaan gagal beruntun masih boleh dilanjutkan")
	}
	if ok, _ := th.allowedAt("alice", "192.0.2.2", now); !ok {
		t.Fatal("alamat lain masih punya kuota sendiri")
	}
}

// Percobaan login gagal TIDAK butuh autentikasi, jadi jumlah key yang disimpan
// harus terbatas dan catatan lama harus benar-benar hilang — kalau tidak,
// username acak cukup untuk menumbuhkan map sampai memori proses habis.
func TestThrottleMembatasiMemori(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	for i := 0; i < throttleMaxEntries+50; i++ {
		th.recordAt("user"+strconv.Itoa(i), "192.0.2.1", now)
	}
	if len(th.attempts) > throttleMaxEntries {
		t.Fatalf("key tersimpan = %d, batas %d", len(th.attempts), throttleMaxEntries)
	}
	if len(th.perUser) > throttleMaxEntries {
		t.Fatalf("key per user = %d, batas %d", len(th.perUser), throttleMaxEntries)
	}

	th.gc(now.Add(throttleWindow + time.Second))
	if len(th.attempts) != 0 || len(th.perUser) != 0 {
		t.Fatalf("catatan kedaluwarsa masih tersimpan: %d / %d", len(th.attempts), len(th.perUser))
	}
}

// Cookie penghapus sesi harus memakai atribut yang sama dengan cookie saat
// login, kalau tidak sebagian browser mengabaikannya dan cookie sesi tetap
// terkirim setelah "logout".
func TestCookieLogoutMemakaiAtributSama(t *testing.T) {
	c := expiredSessionCookie(true)
	if c.Name != sessionCookie || c.Path != "/" || !c.HttpOnly || !c.Secure {
		t.Fatalf("cookie logout = %#v", c)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, ingin Lax", c.SameSite)
	}
	if c.MaxAge >= 0 || !c.Expires.Before(time.Now()) {
		t.Fatalf("cookie logout tidak kedaluwarsa: MaxAge=%d Expires=%v", c.MaxAge, c.Expires)
	}
	if expiredSessionCookie(false).Secure {
		t.Fatal("cookie non-TLS tidak boleh ditandai Secure (browser akan menolaknya)")
	}
}
