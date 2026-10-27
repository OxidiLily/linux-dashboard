import { useEffect } from "react"
import { create } from "zustand"
import { Button } from "@/components/ui/button"
import { daftarkanEscape } from "@/lib/lapisan-escape"
import { tr } from "@/stores/i18n"

// Satu dialog pilihan untuk seluruh aplikasi: beberapa tombol aksi dalam satu
// kotak, dipanggil sebagai promise —
//   const aksi = await pilihDialog({ title: "Nama sama", opsi: [...] })
// Pasangan promptDialog (satu isian) dan confirmDialog (ya/tidak) untuk kasus
// dengan lebih dari dua jawaban, mis. konflik nama saat upload. Batal/Escape
// menghasilkan null, sama seperti keduanya.

export type OpsiPilihan = {
  nilai: string
  label: string
  variant?: "default" | "outline" | "destructive"
}

type Req = {
  title: string
  detail?: string
  opsi: OpsiPilihan[]
}

// id: penanda unik tiap permintaan, dipakai sebagai key React.
type Aktif = Req & { id: number; resolve: (v: string | null) => void }

type State = {
  req: Aktif | null
  open: (r: Req) => Promise<string | null>
  close: (v: string | null) => void
}

// ponytail: satu antrean dangkal, sama seperti confirmDialog/promptDialog —
// dialog kedua yang dibuka sebelum yang pertama dijawab membatalkan yang
// pertama. Cukup untuk UI ini yang selalu memulai dialog dari satu klik user.
let urutan = 0

const usePilihStore = create<State>((set, get) => ({
  req: null,
  open: (r) =>
    new Promise<string | null>((resolve) => {
      get().req?.resolve(null)
      set({ req: { ...r, id: ++urutan, resolve } })
    }),
  close: (v) => {
    get().req?.resolve(v)
    set({ req: null })
  },
}))

export function pilihDialog(r: Req): Promise<string | null> {
  return usePilihStore.getState().open(r)
}

export function PilihHost() {
  const req = usePilihStore((s) => s.req)
  const close = usePilihStore((s) => s.close)
  if (!req) return null
  return <DialogPilihan key={req.id} req={req} close={close} />
}

export function DialogPilihan({ req, close }: { req: Aktif; close: (v: string | null) => void }) {
  // Escape lewat tumpukan lapisan bersama (lihat lib/lapisan-escape.ts).
  useEffect(() => daftarkanEscape(() => close(null)), [close])
  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
      onClick={() => close(null)}
    >
      <div
        role="dialog"
        aria-modal="true"
        className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <p className="text-sm font-semibold">{req.title}</p>
        {req.detail && (
          <p className="num mt-1 truncate text-xs text-muted-foreground" title={req.detail}>
            {req.detail}
          </p>
        )}
        <div className="mt-4 flex flex-col gap-2">
          {req.opsi.map((o) => (
            <Button key={o.nilai} size="sm" variant={o.variant ?? "outline"} onClick={() => close(o.nilai)}>
              {o.label}
            </Button>
          ))}
          <Button size="sm" variant="destructive" onClick={() => close(null)}>
            {tr("Batal")}
          </Button>
        </div>
      </div>
    </div>
  )
}
