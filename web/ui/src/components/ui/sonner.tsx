import { Toaster as Sonner, type ToasterProps } from "sonner"
import { useTr } from "@/stores/i18n"

/**
 * Adapter Toaster untuk Sonner.
 *
 * Versi resmi shadcn membaca tema lewat `useTheme()` dari next-themes.
 * Project ini Vite, bukan Next, dan temanya dark-only di :root (lihat
 * index.css) — jadi tidak ada tema yang perlu dibaca dan tidak ada
 * dependency yang perlu ditambahkan hanya untuk membaca konstanta.
 *
 * Warnanya diikat ke token panel, bukan ke palet bawaan Sonner, supaya
 * toast tampil sebagai bagian dari panel dan bukan tempelan dari
 * library lain.
 */
export function Toaster(props: ToasterProps) {
  const tr = useTr()
  return (
    <Sonner
      theme="dark"
      className="toaster group"
      position="bottom-right"
      // richColors WAJIB: tanpanya Sonner mengabaikan --success-*/--error-*/
      // --warning-* dan memakai --normal-* untuk semua type, sehingga
      // notifikasi gagal tampil identik dengan notifikasi berhasil.
      richColors
      // 4 = batas tumpukan ToastHost lama. Tanpa batas, sederet request yang
      // gagal berbarengan menutup layar.
      visibleToasts={4}
      // ToastHost lama punya tombol tutup; error yang panjang perlu bisa
      // dibuang lebih cepat daripada TTL-nya.
      closeButton
      // aria-label bawaan Sonner selalu bahasa Inggris. Panel ini dwibahasa,
      // dan tombol tutup di ToastHost lama ikut berganti bahasa — jadi
      // labelnya diteruskan dari kamus, bukan dibiarkan default. Propertinya
      // milik toastOptions (per-toast), bukan Toaster.
      toastOptions={{ closeButtonAriaLabel: tr("Tutup notifikasi") }}
      style={
        {
          // Latar netral untuk semua type — panel ini memakai warna hanya
          // untuk status (PRD §4.3), jadi status dibedakan lewat warna teks,
          // ikon, dan border, bukan lewat latar berwarna penuh.
          "--normal-bg": "var(--surface)",
          "--normal-text": "var(--fg)",
          "--normal-border": "var(--border)",
          "--success-bg": "var(--surface)",
          "--success-text": "var(--ok)",
          "--success-border": "color-mix(in oklab, var(--ok) 40%, var(--border))",
          "--error-bg": "var(--surface)",
          "--error-text": "var(--crit)",
          "--error-border": "color-mix(in oklab, var(--crit) 50%, var(--border))",
          "--warning-bg": "var(--surface)",
          "--warning-text": "var(--warn)",
          "--warning-border": "color-mix(in oklab, var(--warn) 40%, var(--border))",
          "--info-bg": "var(--surface)",
          "--info-text": "var(--fg)",
          "--info-border": "var(--border)",
          "--border-radius": "var(--radius-sm)",
        } as React.CSSProperties
      }
      {...props}
    />
  )
}
