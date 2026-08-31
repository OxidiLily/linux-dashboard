import { useEffect, useRef, useState } from "react"
import { tr } from "@/stores/i18n"
import { create } from "zustand"
import { Button } from "@/components/ui/button"
import { AlertTriangle } from "lucide-react"

// Satu dialog konfirmasi untuk seluruh aplikasi, dipanggil sebagai promise:
//   if (!(await confirmDialog({ title: "Hapus file?" }))) return
// Menggantikan window.confirm() yang tampilannya di luar kendali tema dan
// diblokir sebagian browser saat dipanggil dari handler async.

type Req = {
  title: string
  message?: string
  detail?: string
  confirmLabel?: string
  danger?: boolean
  // Pilihan tambahan yang dijawab bersamaan dengan konfirmasinya — mis.
  // "hapus data juga" saat mencopot komponen. Nilainya dikirim lewat
  // onChange, bukan lewat nilai balik promise, supaya seluruh pemanggil lain
  // tetap memakai `await confirmDialog(...)` sebagai boolean.
  checkbox?: { label: string; onChange: (dicentang: boolean) => void }
}

type State = {
  req: (Req & { resolve: (ok: boolean) => void }) | null
  open: (r: Req) => Promise<boolean>
  close: (ok: boolean) => void
}

// ponytail: satu antrean dangkal — dialog kedua yang dibuka sebelum yang
// pertama dijawab akan membatalkan yang pertama. Cukup untuk UI ini yang
// selalu memulai dialog dari satu klik user; ganti jadi array kalau nanti
// ada dialog yang dipicu proses latar.
const useConfirmStore = create<State>((set, get) => ({
  req: null,
  open: (r) =>
    new Promise<boolean>((resolve) => {
      get().req?.resolve(false)
      set({ req: { ...r, resolve } })
    }),
  close: (ok) => {
    get().req?.resolve(ok)
    set({ req: null })
  },
}))

export function confirmDialog(r: Req): Promise<boolean> {
  return useConfirmStore.getState().open(r)
}

export function ConfirmHost() {
  const req = useConfirmStore((s) => s.req)
  const close = useConfirmStore((s) => s.close)
  const confirmRef = useRef<HTMLButtonElement>(null)
  const [dicentang, setDicentang] = useState(false)

  // Centang selalu mulai dari mati tiap dialog baru dibuka: pilihan
  // destruktif tidak boleh diwarisi dari dialog sebelumnya.
  useEffect(() => {
    setDicentang(false)
  }, [req])

  useEffect(() => {
    if (!req) return
    confirmRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close(false)
      if (e.key === "Enter") close(true)
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [req, close])

  if (!req) return null

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
      onClick={() => close(false)}
    >
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start gap-3">
          {req.danger && <AlertTriangle className="mt-0.5 size-5 shrink-0 text-crit" />}
          <div className="min-w-0">
            <p id="confirm-title" className="text-sm font-semibold">
              {req.title}
            </p>
            {req.message && <p className="mt-1 text-xs text-muted-foreground">{req.message}</p>}
            {req.detail && (
              <p className="num mt-1 truncate text-xs text-muted-foreground" title={req.detail}>
                {req.detail}
              </p>
            )}
          </div>
        </div>
        {req.checkbox && (
          <label className="mt-3 flex cursor-pointer items-start gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              className="mt-0.5 size-3.5 accent-crit"
              checked={dicentang}
              onChange={(e) => {
                setDicentang(e.target.checked)
                req.checkbox?.onChange(e.target.checked)
              }}
            />
            <span>{req.checkbox.label}</span>
          </label>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={() => close(false)}>
            {tr("Batal")}
          </Button>
          <Button
            ref={confirmRef}
            size="sm"
            variant={req.danger ? "destructive" : "default"}
            onClick={() => close(true)}
          >
            {req.confirmLabel ?? tr("Lanjutkan")}
          </Button>
        </div>
      </div>
    </div>
  )
}
