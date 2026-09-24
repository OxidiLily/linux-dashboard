package helper

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Umur token helper mengikuti setelan umur sesi panel (config.SessionTTLHours,
// env DASHBOARD_SESSION_TTL_HOURS), bukan angka tetap 12 jam.
//
// Sebelum perbaikan ini, operator yang menaikkan TTL sesi di atas 12 jam
// mendapat sesi panel yang masih hidup tetapi tokennya sudah mati: setiap
// permintaan berikutnya dijawab 401 dan user dipaksa login ulang tanpa sebab
// yang terlihat di panel.
//
// Nilai TTL dikirim web app di LoginArgs.TTLHours dan diuji DI SINI lewat
// parameter — bukan dengan menunggu 12 jam. Jalur yang ditempuh tetap jalur
// sungguhan: auth.login lewat Unix socket, jungkat framing/HMAC/handleLogin
// yang sama dengan produksi.

// gantiAutentikasi memasang stub verifikasi kredensial. PAM hanya bisa
// dijalankan sebagai root dengan akun nyata, jadi tanpa seam ini jalur login
// tidak bisa diuji sama sekali.
func gantiAutentikasi(t *testing.T, fn func(string, string) error) {
	t.Helper()
	simpan := autentikasi
	t.Cleanup(func() { autentikasi = simpan })
	autentikasi = fn
}

// pasangLoginUji menyiapkan akun uji "dewi" (sudo) dan verifikasi kredensial
// yang selalu berhasil.
func pasangLoginUji(t *testing.T) {
	t.Helper()
	gantiPembacaIdentitas(t, func(nama string) (*userInfo, error) {
		return userUji(nama, true), nil
	})
	gantiAutentikasi(t, func(string, string) error { return nil })
}

// loginUji menjalankan auth.login lewat socket dan mengembalikan catatan token
// yang diterbitkan beserta tokennya. ttlHours dikirim APA ADANYA di dalam
// `args` sebagai JSON (nil = field tidak dikirim sama sekali), supaya yang
// diuji adalah kontrak di kabel — nama field "ttl_hours" di dalam argumen
// login — bukan susunan struct di sisi Go.
func loginUji(t *testing.T, h *helperSocketUji, ttlHours any) (*sesiToken, string) {
	t.Helper()
	args := map[string]any{
		"username": "dewi",
		"password": "rahasia",
	}
	if ttlHours != nil {
		args["ttl_hours"] = ttlHours
	}
	resp := h.kirim(t, map[string]any{
		"cmd":  helperproto.CmdAuthLogin,
		"args": args,
	})
	if !resp.OK {
		t.Fatalf("login gagal: kode %q (%s)", resp.Code, resp.Error)
	}
	var res helperproto.LoginResult
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		t.Fatalf("LoginResult tidak terbaca: %v (%s)", err, resp.Data)
	}
	if res.Token == "" {
		t.Fatal("login tidak menerbitkan token")
	}
	h.srv.tokenMu.Lock()
	rec := h.srv.tokens[res.Token]
	h.srv.tokenMu.Unlock()
	if rec == nil {
		t.Fatal("token hasil login tidak ada di peta token helper")
	}
	return rec, res.Token
}

// toleransiUmur menyerap waktu yang berjalan selama test (beberapa milidetik),
// tapi tetap jauh lebih kecil daripada selisih antar-kasus, jadi tidak bisa
// menutupi TTL yang salah.
const toleransiUmur = 2 * time.Minute

func umurToken(t *testing.T, rec *sesiToken) time.Duration {
	t.Helper()
	return time.Until(rec.Expires)
}

// Inti: setelan operator benar-benar menentukan umur token. Token ber-TTL
// panjang harus hidup jauh melewati jendela bawaan 12 jam, dan token ber-TTL
// pendek harus ikut pendek — dua arah, supaya tidak lolos hanya dengan
// "sekurang-kurangnya 12 jam".
func TestLoginTokenMengikutiSetelanSesi(t *testing.T) {
	pasangLoginUji(t)
	h := jalankanHelperSocket(t)

	bawaan, tokenBawaan := loginUji(t, h, nil)
	panjang, tokenPanjang := loginUji(t, h, 48)
	pendek, _ := loginUji(t, h, 1)

	if d := umurToken(t, bawaan); d < 11*time.Hour+toleransiUmur || d > 13*time.Hour {
		t.Fatalf("umur token tanpa ttl_hours = %v, harap ~12 jam", d)
	}
	if d := umurToken(t, panjang); d < 48*time.Hour-toleransiUmur || d > 48*time.Hour+toleransiUmur {
		t.Fatalf("umur token dengan ttl_hours=48 = %v, harap ~48 jam", d)
	}
	// Uji "hidup lebih lama daripada bawaan" lewat parameter, tanpa menunggu:
	// token 48 jam harus kedaluwarsa SETIDAKNYA 30 jam setelah token bawaan.
	if !panjang.Expires.After(bawaan.Expires.Add(30 * time.Hour)) {
		t.Fatalf("token ttl_hours=48 kedaluwarsa %v setelah token bawaan, harap setidaknya 30 jam",
			panjang.Expires.Sub(bawaan.Expires))
	}
	if d := umurToken(t, pendek); d < time.Hour-toleransiUmur || d > time.Hour+toleransiUmur {
		t.Fatalf("umur token dengan ttl_hours=1 = %v, harap ~1 jam", d)
	}

	// Dan token itu memang masih berlaku: umur panjang tidak boleh tercapai
	// dengan token yang justru sudah tidak dikenal peta token.
	if _, ok := h.srv.tokenUser(tokenPanjang); !ok {
		t.Fatal("token ber-TTL 48 jam tidak sah menurut helper")
	}
	if _, ok := h.srv.tokenUser(tokenBawaan); !ok {
		t.Fatal("token bawaan tidak sah menurut helper")
	}
}

// Nilai aneh dari klien tidak boleh menghasilkan token abadi, token mati, atau
// durasi negatif: semuanya dijepit ke batas yang jelas.
func TestLoginTokenTTLDijepit(t *testing.T) {
	pasangLoginUji(t)
	h := jalankanHelperSocket(t)

	kasus := []struct {
		nama  string
		jam   any
		harap time.Duration
	}{
		{"nol pakai bawaan", 0, sesiTTL},
		{"negatif pakai bawaan", -3, sesiTTL},
		{"satu jam", 1, time.Hour},
		{"pas di batas atas", 720, 720 * time.Hour},
		{"jauh di atas batas", 100000, 720 * time.Hour},
		{"luapan bilangan", 1 << 40, 720 * time.Hour},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			rec, _ := loginUji(t, h, k.jam)
			d := umurToken(t, rec)
			if d < k.harap-toleransiUmur || d > k.harap+toleransiUmur {
				t.Fatalf("umur token untuk ttl_hours=%v = %v, harap %v", k.jam, d, k.harap)
			}
		})
	}
}

// Perintah lain tidak boleh ikut berubah hanya karena login sekarang menerima
// parameter TTL: nilai yang tidak dikenal tetap diabaikan, dan login tanpa
// parameter tetap menghasilkan token yang bisa dipakai.
func TestLoginTokenTanpaParameterTTLTetapBerfungsi(t *testing.T) {
	pasangLoginUji(t)
	h := jalankanHelperSocket(t)

	rec, token := loginUji(t, h, nil)
	if rec.Username != "dewi" {
		t.Fatalf("identitas token = %q, harap dewi", rec.Username)
	}
	if _, ok := h.srv.tokenUser(token); !ok {
		t.Fatal("token login tanpa parameter TTL tidak sah")
	}
}

// Angka batasnya dikunci di sini: bawaan 12 jam, lantai 1 menit, plafon 30
// hari. Batas ini kontrak antara helper dan web app — mengubahnya diam-diam
// berarti mengubah berapa lama sebuah kunci akses hidup.
func TestBatasUmurTokenTerpaku(t *testing.T) {
	if sesiTTL != 12*time.Hour {
		t.Fatalf("sesiTTL = %v, harap 12 jam", sesiTTL)
	}
	if sesiTTLMin != time.Minute {
		t.Fatalf("sesiTTLMin = %v, harap 1 menit", sesiTTLMin)
	}
	if sesiTTLMaks != 720*time.Hour {
		t.Fatalf("sesiTTLMaks = %v, harap 720 jam (30 hari)", sesiTTLMaks)
	}
}

// Aturan penjepitan diuji langsung pada fungsinya, termasuk lantai 1 menit yang
// belum bisa dicapai lewat setelan yang ada (jamnya bilangan bulat). Kalau
// setelan sub-jam ditambahkan di kemudian hari, token yang mati saat dibuat
// sudah tertutup dari sekarang.
func TestTTLDijepitLangsung(t *testing.T) {
	kasus := []struct {
		nama  string
		ttl   time.Duration
		harap time.Duration
	}{
		{"nol jadi lantai", 0, time.Minute},
		{"sub-menit jadi lantai", 30 * time.Second, time.Minute},
		{"negatif jadi lantai", -time.Hour, time.Minute},
		{"di dalam batas dibiarkan", 6 * time.Hour, 6 * time.Hour},
		{"di atas plafon dijepit", 1000 * time.Hour, 720 * time.Hour},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			if d := jepitTTL(k.ttl); d != k.harap {
				t.Fatalf("jepitTTL(%v) = %v, harap %v", k.ttl, d, k.harap)
			}
		})
	}
}

// Pemetaan jam → durasi, tanpa melewati socket: kasus luapan bilangan diuji di
// sini karena nilainya tidak bisa dikirim lewat setelan mana pun.
func TestTTLTokenDariJam(t *testing.T) {
	kasus := []struct {
		jam   int
		harap time.Duration
	}{
		{0, sesiTTL},
		{-1, sesiTTL},
		{-100000, sesiTTL},
		{1, time.Hour},
		{13, 13 * time.Hour},
		{720, sesiTTLMaks},
		{721, sesiTTLMaks},
		{math.MaxInt, sesiTTLMaks},
		{math.MinInt, sesiTTL},
	}
	for _, k := range kasus {
		if d := ttlToken(k.jam); d != k.harap {
			t.Errorf("ttlToken(%d) = %v, harap %v", k.jam, d, k.harap)
		}
	}
}
