// Lapisan Escape — tumpukan penutup modal multi-lapis.
//
// daftarkanEscape(cb) mendaftarkan callback penutup ke tumpukan. Saat user
// menekan Escape, hanya lapisan teratas yang ditutup. Callback mengembalikan
// `false` untuk menolak (mis. proses sedang jalan) — event tetap ditelan
// supaya drawer/menu di bawahnya tidak ikut menutup.
//
// Pemakaian di useEffect:
//   useEffect(() => {
//     if (!buka) return
//     return daftarkanEscape(() => setBuka(false))
//   }, [buka])

type Penutup = () => boolean | void

const tumpukan: Penutup[] = []

/** Daftarkan penutup. Kembalian adalah fungsi lepas — pakai sebagai
 *  return value useEffect supaya otomatis dilepas saat unmount. */
function daftarkanEscape(penutup: Penutup): () => void {
  tumpukan.push(penutup)
  return () => {
    const idx = tumpukan.indexOf(penutup)
    if (idx >= 0) tumpukan.splice(idx, 1)
  }
}

/** Apakah ada lapisan escape yang terdaftar? Dipakai app-shell untuk
 *  mencegah drawer/menu profil menutup saat modal sedang terbuka. */
function adaLapisanEscape(): boolean {
  return tumpukan.length > 0
}

function tekan(e: KeyboardEvent) {
  if (e.key !== "Escape" || tumpukan.length === 0) return
  const atas = tumpukan[tumpukan.length - 1]
  const hasil = atas()

  // Baik tertutup maupun menolak: telan event supaya listener lain
  // (drawer, menu profil) tidak ikut bereaksi.
  e.stopPropagation()
  e.preventDefault()

  // Kalau callback mengembalikan false eksplisit, lapisan menolak ditutup
  // (mis. proses sedang jalan). Jangan lepas dari tumpukan.
  if (hasil === false) return

  // Tutup berhasil — lepas dari tumpukan.
  const idx = tumpukan.indexOf(atas)
  if (idx >= 0) tumpukan.splice(idx, 1)
}

if (typeof document !== "undefined") document.addEventListener("keydown", tekan)

export { daftarkanEscape, adaLapisanEscape }
