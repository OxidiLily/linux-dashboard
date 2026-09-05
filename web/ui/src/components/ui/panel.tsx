import type { ReactNode } from "react"
import { cn } from "@/lib/utils"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import type { AlertLevel } from "@/lib/types"

interface PanelProps {
  title?: string
  // ReactNode, bukan string: File Manager menempelkan tombol salin path
  // tepat di kanan teks path-nya, di dalam baris hint yang sama.
  hint?: ReactNode
  level?: AlertLevel
  className?: string
  /**
   * Override padding isi panel. Dipakai halaman terminal: padding 16 px di
   * kiri-kanan memakan ~4 kolom teks yang tidak dibayar apa pun, sementara
   * panel lain memang butuh jarak itu agar teks tidak menempel border.
   */
  contentClassName?: string
  actions?: ReactNode
  children?: ReactNode
}

const railClass: Record<AlertLevel, string> = {
  idle: "bg-transparent",
  warn: "bg-warn",
  crit: "bg-crit",
}

// Panel adapter di atas shadcn Card + tone alert domain.
// Tile metrik dashboard mengikuti Alert Thresholds (idle/warn/crit) lewat rel
// status di kiri — lihat PRD §5.2 + §5.5.4.

export function Panel({
  title,
  hint,
  level = "idle",
  className,
  contentClassName,
  actions,
  children,
}: PanelProps) {
  return (
    <Card className={cn("relative", className)}>
      {(title || hint) && (
        <CardHeader>
          <div>
            {title && <CardTitle>{title}</CardTitle>}
            {hint && <CardDescription>{hint}</CardDescription>}
          </div>
          {/* flex-wrap: deretan aksi panel (badge status + beberapa tombol)
              melebihi lebar HP di beberapa halaman — Firewall paling parah —
              dan tanpa ini tombol terakhirnya terdorong keluar kartu. */}
          {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
        </CardHeader>
      )}
      <CardContent className={contentClassName}>{children}</CardContent>
      <span className={cn("rail", railClass[level])} aria-hidden />
    </Card>
  )
}