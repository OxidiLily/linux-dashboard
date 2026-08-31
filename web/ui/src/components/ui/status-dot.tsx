import { cn } from "@/lib/utils"

// Dot status solid 8px — warna murni dari token status (TDD §4.1b).
// Dipakai di list, tabel, Components, dan header tile metrik.

export type StatusState = "ok" | "warn" | "crit" | "idle"

const stateClass: Record<StatusState, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  crit: "bg-crit",
  idle: "bg-idle",
}

interface StatusDotProps {
  state?: StatusState
  /** Isi untuk memberi makna ke pembaca layar; kosong = dekoratif. */
  label?: string
  pulse?: boolean
  className?: string
}

export function StatusDot({ state = "idle", label, pulse = false, className }: StatusDotProps) {
  return (
    <span
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        stateClass[state],
        pulse && "animate-pulse",
        className,
      )}
    />
  )
}
