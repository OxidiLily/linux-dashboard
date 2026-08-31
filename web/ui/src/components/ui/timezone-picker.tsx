import { useEffect, useMemo, useRef, useState } from "react"
import { Check, Globe, Search } from "lucide-react"

import { cn } from "@/lib/utils"
import { daftarTimezone, offsetUTC } from "@/stores/prefs"

/**
 * Pemilih zona waktu.
 *
 * `<select>` bawaan tidak dipakai karena daftarnya 400+ entri: tanpa pencarian,
 * menemukan "America/Argentina/Buenos_Aires" berarti menggulir ratusan baris,
 * dan tampilannya mengikuti gaya OS sehingga menabrak tema panel.
 */
export function TimezonePicker({
  value,
  onChange,
  labelIkutServer,
  labelCari,
  ariaLabel,
}: {
  value: string
  onChange: (tz: string) => void
  labelIkutServer: string
  labelCari: string
  ariaLabel: string
}) {
  const [buka, setBuka] = useState(false)
  const [cari, setCari] = useState("")
  const wadah = useRef<HTMLDivElement>(null)
  const inputCari = useRef<HTMLInputElement>(null)
  const semua = useMemo(() => daftarTimezone(), [])

  // Klik di luar & Escape menutup — dropdown yang hanya bisa ditutup dengan
  // memilih sesuatu memaksa user mengubah setelan yang tidak ingin diubah.
  useEffect(() => {
    if (!buka) return
    const klik = (e: MouseEvent) => {
      if (wadah.current && !wadah.current.contains(e.target as Node)) setBuka(false)
    }
    const tombol = (e: KeyboardEvent) => {
      if (e.key === "Escape") setBuka(false)
    }
    document.addEventListener("mousedown", klik)
    document.addEventListener("keydown", tombol)
    inputCari.current?.focus()
    return () => {
      document.removeEventListener("mousedown", klik)
      document.removeEventListener("keydown", tombol)
    }
  }, [buka])

  const hasil = useMemo(() => {
    const q = cari.trim().toLowerCase().replace(/\s+/g, "_")
    if (!q) return semua.slice(0, 300)
    return semua.filter((z) => z.toLowerCase().includes(q)).slice(0, 300)
  }, [cari, semua])

  const offset = value ? offsetUTC(value) : ""
  const label = value ? value.replace(/_/g, " ") : labelIkutServer

  return (
    <div className="relative" ref={wadah}>
      <button
        type="button"
        aria-label={ariaLabel}
        aria-expanded={buka}
        onClick={() => {
          setBuka((v) => !v)
          setCari("")
        }}
        className="flex max-w-[15rem] items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs text-muted transition-colors hover:text-foreground"
      >
        <Globe className="size-3.5 shrink-0" />
        {offset && <span className="num shrink-0">{offset}</span>}
        <span className="truncate">{label}</span>
      </button>

      {buka && (
        <div className="absolute right-0 z-50 mt-1 w-72 overflow-hidden rounded-md border border-border bg-surface shadow-xl">
          <div className="flex items-center gap-2 border-b border-border px-2.5 py-2">
            <Search className="size-3.5 shrink-0 text-muted-foreground" />
            <input
              ref={inputCari}
              value={cari}
              onChange={(e) => setCari(e.target.value)}
              placeholder={labelCari}
              className="w-full bg-transparent text-xs outline-none placeholder:text-muted-2"
            />
          </div>
          <ul className="max-h-72 overflow-y-auto py-1" role="listbox">
            {/* Opsi "ikut server" disembunyikan saat mencari: ia selalu berada
                di paling atas, sehingga hasil pencarian pertama justru bukan
                zona yang diketik user. */}
            {cari.trim() === "" && (
            <li>
              <button
                type="button"
                role="option"
                aria-selected={value === ""}
                onClick={() => {
                  onChange("")
                  setBuka(false)
                }}
                className="flex w-full items-center justify-between gap-2 px-2.5 py-1.5 text-left text-xs hover:bg-secondary"
              >
                <span>{labelIkutServer}</span>
                {value === "" && <Check className="size-3.5 text-ok" />}
              </button>
            </li>
            )}
            {hasil.map((z) => (
              <li key={z}>
                <button
                  type="button"
                  role="option"
                  aria-selected={value === z}
                  onClick={() => {
                    onChange(z)
                    setBuka(false)
                  }}
                  className={cn(
                    "flex w-full items-center justify-between gap-2 px-2.5 py-1.5 text-left text-xs hover:bg-secondary",
                    value === z && "text-foreground",
                  )}
                >
                  <span className="truncate">{z.replace(/_/g, " ")}</span>
                  <span className="num shrink-0 text-[10px] text-muted-foreground">{offsetUTC(z)}</span>
                </button>
              </li>
            ))}
            {hasil.length === 0 && (
              <li className="px-2.5 py-3 text-center text-xs text-muted-foreground">—</li>
            )}
          </ul>
        </div>
      )}
    </div>
  )
}
