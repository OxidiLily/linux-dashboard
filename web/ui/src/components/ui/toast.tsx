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

function tampil(tone: Tone, message: string, detail?: string) {
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
  pesan: (message: string, detail?: string) =>
    sonner(message, { duration: TTL.info, description: detailNode(detail) }),

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
      success: (hasil: T) => ({
        message: typeof pesan.sukses === "function" ? pesan.sukses(hasil) : pesan.sukses,
        description: detailNode(pesan.detail?.(hasil)),
        duration: TTL.ok,
      }),
      error: (e: unknown) => ({
        message: typeof pesan.gagal === "function" ? pesan.gagal(e) : pesan.gagal,
        duration: TTL.err,
      }),
    })
    return kerja
  },
}
