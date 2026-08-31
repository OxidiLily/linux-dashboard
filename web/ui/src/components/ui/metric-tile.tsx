import type { ReactNode } from "react"
import { NilaiFlow } from "@/components/ui/nilai-flow"
import { cn } from "@/lib/utils"
import { StatusDot } from "@/components/ui/status-dot"
import type { AlertLevel } from "@/lib/types"

// Tile metrik dashboard: label eyebrow, angka besar, subteks muted
// (PRD §4.3.1). Alert Threshold mengubah warna angka + dot, BUKAN
// background card (TDD §4.1b).

const valueClass: Record<AlertLevel, string> = {
  idle: "text-foreground",
  warn: "text-warn",
  crit: "text-crit",
}

interface MetricTileProps {
  label: string
  /**
   * Angka mentah metrik. Kalau diisi, angkanya dianimasikan NumberFlow
   * alih-alih diganti begitu saja tiap tick.
   *
   * Dipisah dari `value` karena tidak semua tile punya angka: GPU yang tidak
   * terdeteksi menampilkan "—", dan itu bukan nilai yang bisa dianimasikan.
   * Saat `angka` undefined, `value` dirender apa adanya seperti sebelumnya.
   */
  angka?: number
  /** Satuan yang menempel di belakang angka, mis. "%". */
  satuan?: string
  /** Jumlah desimal — samakan dengan format teks yang digantikan. */
  desimal?: number
  value: ReactNode
  sub?: ReactNode
  /** Kosongkan kalau metrik ini tidak ikut sistem warna (mis. Network). */
  state?: AlertLevel
  className?: string
  children?: ReactNode
}

export function MetricTile({
  label,
  angka,
  satuan,
  desimal = 1,
  value,
  sub,
  state,
  className,
  children,
}: MetricTileProps) {
  return (
    <div className={cn("panel p-4", className)}>
      <div className="flex items-center gap-2">
        <p className="eyebrow">{label}</p>
        {state && <StatusDot state={state === "idle" ? "ok" : state} className="ml-auto" />}
      </div>
      <p className={cn("display mt-2 text-3xl", state ? valueClass[state] : "text-foreground")}>
        {typeof angka === "number" && isFinite(angka) ? (
          <NilaiFlow nilai={angka} satuan={satuan} desimal={desimal} />
        ) : (
          value
        )}
      </p>
      {sub && <p className="mt-1 text-xs text-muted">{sub}</p>}
      {children && <div className="mt-3">{children}</div>}
    </div>
  )
}
