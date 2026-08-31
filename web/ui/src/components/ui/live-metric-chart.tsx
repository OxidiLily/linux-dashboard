import { LiveLineChart } from "@/components/charts/live-line-chart"
import { LiveLine } from "@/components/charts/live-line"
import { LiveYAxis } from "@/components/charts/live-y-axis"
import type { TitikMetrik } from "@/stores/metrics"

// Pembungkus tipis di atas live-line-chart bklit.
//
// Ada dua alasan komponen ini berdiri sendiri alih-alih ditulis inline di
// dashboard:
//
//  1. Konfigurasinya dipakai tiga kali (CPU, Network masuk, Network keluar).
//     Menyalinnya tiga kali berarti tiga tempat yang harus diubah bersamaan.
//  2. Ia jadi SATU titik import untuk seluruh pustaka chart — @visx, d3-array,
//     dan motion ikut lewat sini. Dashboard tidak lazy-loaded (ia sengaja ada
//     di chunk utama supaya halaman pertama setelah login tidak menunggu),
//     jadi mengimpornya langsung akan menambah ~233 kB ke unduhan pertama
//     SETIAP user. Dengan satu modul, dashboard cukup me-lazy-load berkas ini.

interface LiveMetricChartProps {
  data: TitikMetrik[]
  value: number
  /** Rentang waktu terlihat, detik. */
  window?: number
  /** Format nilai untuk badge di ujung garis dan label sumbu Y. */
  formatValue: (v: number) => string
  stroke?: string
  className?: string
}

export function LiveMetricChart({
  data,
  value,
  window: jendela = 60,
  formatValue,
  stroke = "var(--chart-line-primary)",
  className,
}: LiveMetricChartProps) {
  return (
    <LiveLineChart
      data={data}
      value={value}
      window={jendela}
      className={className}
      // Margin kiri disediakan untuk label sumbu Y; sumbu X sengaja tidak
      // dipasang — panel ini menunjukkan tren beberapa puluh detik terakhir,
      // dan stempel jam di bawahnya hanya menghabiskan tinggi tanpa menjawab
      // pertanyaan yang dibawa user ke dashboard.
      margin={{ top: 12, right: 12, bottom: 8, left: 44 }}
    >
      <LiveLine dataKey="value" stroke={stroke} formatValue={formatValue} />
      <LiveYAxis position="left" formatValue={formatValue} />
    </LiveLineChart>
  )
}

export default LiveMetricChart
