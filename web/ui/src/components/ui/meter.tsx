import { useMemo } from "react"
import { cn } from "@/lib/utils"
import type { AlertLevel } from "@/lib/types"

// Meter horizontal bertakik — VU meter language, lebih mudah dibandingkan
// antar-metrik daripada donut gauge. PRD §4.2 salah satu dari tiga custom
// yang diizinkan (selain MetricTile dan Sparkline).

interface MeterProps {
  value: number
  level?: AlertLevel
  label?: string
  ticks?: boolean
  className?: string
}

// Fill netral putih di bawah threshold; amber/merah hanya saat terlampaui
// (PRD §4.3, TDD §4.1a) — dilarang gradient warna.
//
// "idle" adalah STATE (di bawah ambang warning), bukan gaya per-widget: semua
// meter di dashboard hampir selalu idle, jadi mengubah warnanya mengubah
// tampilan CPU, per-core, RAM, dan GPU sekaligus — bukan cuma satu bar.
const fillClass: Record<AlertLevel, string> = {
  idle: "bg-foreground",
  warn: "bg-warn",
  crit: "bg-crit",
}

export function Meter({ value, level = "idle", label, ticks = true, className }: MeterProps) {
  const pct = Math.min(100, Math.max(0, isNaN(value) ? 0 : value))
  const ticksArray = useMemo(() => Array.from({ length: 7 }, (_, i) => i), [])
  return (
    <div
      role="meter"
      aria-valuenow={Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={label}
      className={cn("relative h-2 w-full overflow-hidden rounded-sm bg-surface-2", className)}
    >
      <div
        className={cn("h-full transition-[width] duration-300 ease-out", fillClass[level])}
        style={{ width: `${pct}%` }}
      />
      {ticks && (
        <div className="pointer-events-none absolute inset-0 flex justify-between px-[12.5%]" aria-hidden>
          {ticksArray.map((i) => (
            <span key={i} className="w-px bg-background/50" />
          ))}
        </div>
      )}
    </div>
  )
}