import { toast as sonner } from "sonner"

// Notifikasi in-app menggantikan window.alert(). Selain tampilannya di luar
// kendali tema, alert() memblokir seluruh tab sampai user menekan OK — jadi
// pesan "berhasil" pun menghentikan pekerjaan. Dipanggil dari mana saja:
//   notify.ok("Komponen wireguard berhasil dihapus.")
//   notify.err(`Gagal memasang docker: ${pesanError(e)}`)
//
// Sejak Sonner dipasang, berkas ini tinggal adapter: renderer, tumpukan, dan
// timer-nya milik Sonner (lihat components/ui/sonner.tsx untuk temanya).
// Bentuk `notify` sengaja TIDAK diubah — ada 103 pemanggil di seluruh view,
// dan mengganti nama/argumennya berarti menyentuh semuanya tanpa satu pun
// perbaikan perilaku sebagai gantinya.

type Tone = "ok" | "err" | "info" | "warn"

// Error tampil lebih lama: pesan gagal perlu dibaca, pesan berhasil cukup
// dilirik. Angkanya dibawa apa adanya dari ToastHost lama supaya perilaku
// yang sudah dikenal user tidak berubah diam-diam.
const TTL: Record<Tone, number> = { ok: 4000, info: 5000, warn: 6000, err: 9000 }

/**
 * Detail dirender sebagai blok monospace yang bisa di-scroll, bukan teks
 * biasa.
 *
 * Isinya sering keluaran mentah dari sistem — potongan journal dari
 * systemctlDiagnose, stderr apt, pesan smbd — yang punya baris sendiri dan
 * tidak boleh dilipat sembarangan. Tanpa <pre>, keluaran multi-baris menyatu
 * jadi satu paragraf dan justru menyembunyikan barisnya yang penting.
 */
function detailNode(detail?: string) {
  if (!detail) return undefined
  return (
    <pre className="num mt-1.5 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-background p-2 text-[10px] text-muted-foreground">
      {detail}
    </pre>
  )
}

/**
 * Simpan alert ke halaman Logs.
 *
 * Yang dicatat adalah pesan yang benar-benar dilihat user — termasuk kegagalan
 * yang tidak pernah sampai ke server (validasi di browser, koneksi putus),
 * yang justru tidak akan pernah muncul di activity log. Server menyimpannya
 * satu bulan lalu menghapusnya sendiri.
 *
 * Sengaja fire-and-forget dan sengaja TIDAK memakai apiSend: kegagalan
 * pencatatan tidak boleh memunculkan toast baru — satu toast gagal akan
 * mencatat, gagal lagi, lalu memunculkan toast berikutnya tanpa henti. Toast
 * di halaman login juga ditolak server (belum ada sesi) dan itu memang
 * dibiarkan diam.
 */
function rekam(tone: Tone, message: string, detail?: string) {
  void fetch("/api/logs/notifications", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ tone, message, detail: detail ?? "", page: window.location.pathname }),
  }).catch(() => undefined)
}

function tampil(tone: Tone, message: string, detail?: string) {
  rekam(tone, message, detail)
  const opsi = { duration: TTL[tone], description: detailNode(detail) }
  switch (tone) {
    case "ok":
      return sonner.success(message, opsi)
    case "err":
      return sonner.error(message, opsi)
    case "warn":
      return sonner.warning(message, opsi)
    default:
      return sonner.info(message, opsi)
  }
}

export const notify = {
  ok: (message: string, detail?: string) => tampil("ok", message, detail),
  err: (message: string, detail?: string) => tampil("err", message, detail),
  info: (message: string, detail?: string) => tampil("info", message, detail),
  warn: (message: string, detail?: string) => tampil("warn", message, detail),

  /** Pesan netral tanpa ikon status — untuk kabar yang bukan berhasil/gagal. */
  pesan: (message: string, detail?: string) => {
    rekam("info", message, detail)
    return sonner(message, { duration: TTL.info, description: detailNode(detail) })
  },

  /**
   * Satu toast untuk seluruh umur sebuah operasi: berputar selama berjalan,
   * lalu berubah sendiri jadi berhasil atau gagal.
   *
   * Menggantikan pola yang ditulis ulang di belasan tempat — `await` lalu
   * notify.ok di jalur sukses dan notify.err di catch. Selama menunggu, pola
   * itu tidak menampilkan apa pun, sehingga operasi yang makan beberapa detik
   * terlihat seperti tombol yang tidak menanggapi.
   *
   * Mengembalikan promise aslinya supaya pemanggil tetap bisa menunggu
   * hasilnya dan menjalankan langkah lanjutan (mis. memuat ulang daftar).
   */
  tugas: <T,>(
    kerja: Promise<T>,
    pesan: {
      jalan: string
      sukses: string | ((hasil: T) => string)
      gagal: string | ((e: unknown) => string)
      /** Keluaran mentah untuk blok monospace di toast sukses. */
      detail?: (hasil: T) => string | undefined
    },
  ): Promise<T> => {
    sonner.promise(kerja, {
      loading: pesan.jalan,
      // Bentuk objek, bukan string: hanya lewat sini description bisa diisi
      // ReactNode, dan durasinya bisa dibedakan antara sukses dan gagal
      // seperti nada lain di berkas ini.
      // Pencatatan ikut di dalam callback ini, bukan di pemanggil: toast
      // berumur-panjang tidak lewat tampil(), jadi tanpa ini hasil operasi
      // yang paling penting (install, hapus, deploy) justru absen dari Logs.
      success: (hasil: T) => {
        const teks = typeof pesan.sukses === "function" ? pesan.sukses(hasil) : pesan.sukses
        const rinci = pesan.detail?.(hasil)
        rekam("ok", teks, rinci)
        return { message: teks, description: detailNode(rinci), duration: TTL.ok }
      },
      error: (e: unknown) => {
        const teks = typeof pesan.gagal === "function" ? pesan.gagal(e) : pesan.gagal
        rekam("err", teks)
        return { message: teks, duration: TTL.err }
      },
    })
    return kerja
  },
}
