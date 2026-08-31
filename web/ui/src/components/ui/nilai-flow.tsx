import NumberFlow from "@number-flow/react"
import type { AngkaSkala } from "@/lib/format"

// Satu tempat mengatur NumberFlow untuk seluruh panel.
//
// Konfigurasinya (locale, preferensi gerak) mudah menyimpang kalau ditulis
// ulang di tiap pemakaian — dan menyimpang di sini berarti angka yang sama
// tampil beda format di dua kartu bersebelahan.

interface NilaiFlowProps {
  nilai: number
  /** Satuan yang menempel di belakang angka, mis. "%" atau " Mb/s". */
  satuan?: string
  desimal?: number
  className?: string
}

export function NilaiFlow({ nilai, satuan, desimal = 1, className }: NilaiFlowProps) {
  return (
    <NumberFlow
      value={nilai}
      suffix={satuan}
      className={className}
      // locales dikunci ke en-US supaya pemisah desimalnya tetap titik, sama
      // dengan formatPercent()/formatRate() yang memakai toFixed(). Tanpa ini
      // angka berubah jadi koma saat bahasa panel diganti dan tampak seperti
      // nilai yang berbeda padahal sama.
      locales="en-US"
      format={{ minimumFractionDigits: desimal, maximumFractionDigits: desimal }}
      // Angka yang berputar tiap detik persis jenis gerak yang dihindari
      // prefers-reduced-motion.
      respectMotionPreference
    />
  )
}

/** Bentuk siap-pakai untuk hasil pecahBytes()/pecahRate(). */
export function NilaiSkalaFlow({ angka, className }: { angka: AngkaSkala; className?: string }) {
  return (
    <NilaiFlow nilai={angka.nilai} satuan={" " + angka.satuan} desimal={angka.desimal} className={className} />
  )
}
