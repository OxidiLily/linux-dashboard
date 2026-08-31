import * as React from "react"
import { Slot } from "@radix-ui/react-slot"
import { cn } from "@/lib/utils"

// Hanya varian utama (aksi primer: Simpan, Masuk, Buat, Up) yang putih dengan
// teks hitam. Varian pendukung tetap netral supaya tabel dan toolbar tidak
// penuh blok putih.
const VARIAN = {
  default: "bg-foreground text-background hover:bg-foreground/90",
  outline: "border-border bg-transparent text-foreground hover:bg-secondary",
  ghost: "border-transparent text-muted-foreground hover:text-foreground hover:bg-secondary",
  destructive: "border-destructive/40 bg-destructive/15 text-destructive hover:bg-destructive/25",
} as const

// Tinggi di HP dinaikkan: tombol 32px adalah target sentuh yang meleset, dan
// varian "sm" justru yang paling banyak dipakai — tombol ikon di toolbar dan
// di tiap baris tabel. Di sm ke atas kembali ke ukuran padat semula supaya
// kerapatan tampilan desktop tidak berubah.
const UKURAN = {
  default: "h-10 sm:h-9 px-3.5 text-sm",
  sm: "h-9 sm:h-8 px-2.5 text-xs",
} as const

const DASAR =
  "inline-flex items-center justify-center gap-2 rounded-md border border-transparent font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 whitespace-nowrap"

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: keyof typeof VARIAN
  size?: keyof typeof UKURAN
  asChild?: boolean
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = "default", size = "default", asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button"
    return <Comp ref={ref} className={cn(DASAR, VARIAN[variant], UKURAN[size], className)} {...props} />
  },
)
Button.displayName = "Button"
