import { cn } from "@/lib/utils"

// Satu tempat untuk kredit + URL-nya: dipakai kaki sidebar dan halaman login,
// dan URL yang ditulis dua kali cepat atau lambat jadi dua URL berbeda.
export function Byline({ className }: { className?: string }) {
  return (
    <a
      className={cn(
        "block text-center text-xs text-muted-2 transition-colors hover:text-foreground",
        className,
      )}
      href="https://oxidilily.com"
      target="_blank"
      rel="noreferrer noopener"
    >
      by OxidiLily
    </a>
  )
}
