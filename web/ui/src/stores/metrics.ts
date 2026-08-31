import { create } from "zustand"
import type { Snapshot, SystemInfo, Threshold, AlertLevel } from "@/lib/types"

const HISTORY = 60

/**
 * Satu titik riwayat metrik.
 *
 * Sebelumnya riwayat disimpan sebagai number[] polos — cukup untuk Sparkline
 * yang hanya menggambar bentuk, tapi live-line-chart menggeser sumbu X
 * berdasarkan waktu nyata dan butuh stempel waktunya. Menurunkan waktu dari
 * indeks array tidak bisa dipakai: interval polling bisa diubah user (250 ms
 * – 60 dtk) dan frame bisa terlewat saat koneksi tersendat, sehingga jarak
 * antar titik tidak pernah dijamin seragam.
 *
 * `time` dalam DETIK unix (pecahan), sesuai kontrak LiveLinePoint.
 */
export type TitikMetrik = { time: number; value: number }

interface MetricsStore {
  snapshot: Snapshot | null
  system: SystemInfo | null
  thresholds: Threshold[]
  connected: boolean
  cpuHistory: TitikMetrik[]
  ramHistory: TitikMetrik[]
  rxHistory: TitikMetrik[]
  txHistory: TitikMetrik[]
  loadStatic: () => Promise<void>
  apply: (snap: Snapshot) => void
  setConnected: (b: boolean) => void
  levelFor: (metric: string, pct: number) => AlertLevel
}

export const useMetrics = create<MetricsStore>((set, get) => ({
  snapshot: null,
  system: null,
  thresholds: [],
  connected: false,
  cpuHistory: [],
  ramHistory: [],
  rxHistory: [],
  txHistory: [],
  async loadStatic() {
    const [system, thresholds] = await Promise.all([
      fetch("/api/system/info", { credentials: "include" }).then((r) => r.json()),
      fetch("/api/settings/alert-thresholds", { credentials: "include" }).then((r) => r.json()),
    ])
    set({ system, thresholds })
  },
  apply(snap) {
    const now = Date.now() / 1000
    const push = (arr: TitikMetrik[], v: number) => {
      const next = [...arr, { time: now, value: v }]
      if (next.length > HISTORY) next.shift()
      return next
    }
    let rxRate = 0,
      txRate = 0
    for (const n of snap.network ?? []) {
      rxRate += n.rx_rate
      txRate += n.tx_rate
    }
    set({
      snapshot: snap,
      cpuHistory: push(get().cpuHistory, snap.cpu?.total_pct ?? 0),
      ramHistory: push(get().ramHistory, snap.memory?.used_pct ?? 0),
      rxHistory: push(get().rxHistory, rxRate),
      txHistory: push(get().txHistory, txRate),
    })
  },
  setConnected(b) {
    set({ connected: b })
  },
  levelFor(metric, pct) {
    const t = get().thresholds.find((x) => x.metric === metric)
    if (!t) return "idle"
    if (pct >= t.crit_pct) return "crit"
    if (pct >= t.warn_pct) return "warn"
    return "idle"
  },
}))