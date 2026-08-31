import * as React from "react"
import { cn } from "@/lib/utils"

export type InputProps = React.InputHTMLAttributes<HTMLInputElement>

export const Input = React.forwardRef<HTMLInputElement, InputProps>(({ className, type, ...props }, ref) => (
  <input
    type={type}
    ref={ref}
    className={cn(
      // text-base di HP bukan gaya, tapi syarat: Safari iOS otomatis men-zoom
      // halaman setiap input di bawah 16px difokus, dan zoom itu tidak pernah
      // dikembalikan — seluruh panel tertinggal dalam keadaan melar.
      "flex h-9 w-full rounded-md border border-border bg-input px-3 py-1 text-base sm:text-sm text-foreground transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  />
))
Input.displayName = "Input"