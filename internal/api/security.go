package api

import (
	"net/http"
	"strings"
)

// Lapisan keamanan HTTP yang berlaku untuk SELURUH response, bukan hanya
// endpoint tertentu. Semua kontrol di sini murni defensif: tidak mengubah
// perilaku aplikasi, hanya menutup kelas serangan yang tidak butuh bug di
// kode kita untuk berhasil.

// Panel ini menyajikan seluruh asetnya sendiri dari binary (go:embed) dan
// tidak pernah memuat script/gambar/font dari domain lain. Karena itu CSP-nya
// bisa dikunci ke 'self' — kalau suatu saat ada XSS yang lolos, script
// injeksi tetap tidak bisa dieksekusi maupun mengirim data keluar.
//
// 'unsafe-inline' pada style-src disengaja: React/xterm menulis style inline
// untuk ukuran terminal dan lebar meter. Script tidak pernah inline.
const cspApp = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; " +
	"media-src 'self' blob:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		// Panel administrasi tidak pernah pantas di-embed: tanpa ini, halaman
		// lain bisa membingkainya dan menipu admin agar menekan tombol
		// destruktif (clickjacking).
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		// Endpoint file punya CSP sandbox sendiri (lihat files.go) — jangan
		// ditimpa, karena isi file milik user tidak boleh dianggap bagian
		// dari aplikasi.
		if h.Get("Content-Security-Policy") == "" {
			h.Set("Content-Security-Policy", cspApp)
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginOnly menolak permintaan yang mengubah keadaan bila datang dari
// origin lain.
//
// Cookie session sudah SameSite=Lax, yang memblokir POST lintas situs dari
// form. Ini lapisan kedua untuk jalur yang tidak tercakup Lax (mis. request
// dari halaman yang sama-sama http di jaringan lokal, atau browser lama), dan
// biayanya hanya perbandingan string.
func sameOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// Browser modern mengirim Sec-Fetch-Site; itu sinyal paling akurat.
		switch r.Header.Get("Sec-Fetch-Site") {
		case "same-origin", "none":
			next.ServeHTTP(w, r)
			return
		case "cross-site", "same-site":
			writeErr(w, http.StatusForbidden, "Permintaan lintas situs ditolak")
			return
		}
		// Fallback untuk klien tanpa Sec-Fetch-Site (curl, skrip): Origin harus
		// kosong atau sama dengan host yang diminta.
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if hostDariOrigin(origin) != r.Host {
			writeErr(w, http.StatusForbidden, "Permintaan lintas situs ditolak")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostDariOrigin(origin string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	if i := strings.IndexAny(s, "/"); i >= 0 {
		s = s[:i]
	}
	return s
}
