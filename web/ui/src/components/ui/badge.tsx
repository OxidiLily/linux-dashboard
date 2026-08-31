import * as React from "react"
import { cn } from "@/lib/utils"

type Tone = "ok" | "warn" | "crit" | "signal" | "muted"

const toneClass: Record<Tone, string> = {
  ok: "border-ok/40 text-ok bg-ok/10",
  warn: "border-warn/40 text-warn bg-warn/10",
  crit: "border-crit/40 text-crit bg-crit/10",
  signal: "border-signal/40 text-signal bg-signal/10",
  muted: "border-border text-muted-foreground bg-secondary",
}

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone
}

export const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, tone = "muted", ...props }, ref) => (
    <span
      ref={ref}
      className={cn(
        "inline-flex items-center rounded border px-1.5 py-0.5 text-[0.6875rem] font-medium",
        toneClass[tone],
        className,
      )}
      {...props}
    />
  ),
)
Badge.displayName = "Badge"