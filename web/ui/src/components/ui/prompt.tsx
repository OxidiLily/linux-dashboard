import { useEffect, useRef, useState } from "react"
import { tr } from "@/stores/i18n"
import { create } from "zustand"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

// Satu dialog isian untuk seluruh aplikasi, dipanggil sebagai promise:
//   const nama = await promptDialog({ title: "Folder baru", label: "Nama folder" })
//   if (!nama) return
// Menggantikan window.prompt() yang tampilannya di luar kendali tema, tidak
// bisa menyembunyikan ketikan password, dan diblokir sebagian browser saat
// dipanggil dari handler async. Pasangan dari confirmDialog() di confirm.tsx.

type Req = {
  title: string
  // label: keterangan di atas kolom isian.
  label?: string
  // detail: baris kecil di bawah judul, mis. path folder yang sedang dibuka.
  detail?: string
  defaultValue?: string
  placeholder?: string
  confirmLabel?: string
  // password menyembunyikan ketikan dan mematikan autocomplete.
  password?: boolean
}

// id: penanda unik tiap permintaan, dipakai sebagai key React.
type Aktif = Req & { id: number; resolve: (v: string | null) => void }

type State = {
  req: Aktif | null
  open: (r: Req) => Promise<string | null>
  close: (v: string | null) => void
}

// ponytail: satu antrean dangkal, sama seperti confirmDialog — dialog kedua
// yang dibuka sebelum yang pertama dijawab membatalkan yang pertama. Cukup
// untuk UI ini yang selalu memulai dialog dari satu klik user.
let urutan = 0

const usePromptStore = create<State>((set, get) => ({
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

// Isian kosong (atau spasi saja) tidak pernah dikirim: nama file/folder kosong
// hanya akan ditolak backend, dan password kosong akan menghapus proteksi akun
// tanpa disadari. Diekspor supaya bisa diperiksa cek-runtime.ts.
export const isiValid = (v: string) => v.trim() !== ""

export function promptDialog(r: Req): Promise<string | null> {
  return usePromptStore.getState().open(r)
}

export function PromptHost() {
  const req = usePromptStore((s) => s.req)
  const close = usePromptStore((s) => s.close)
  if (!req) return null
  // key=id: isi dialog di-mount ulang tiap permintaan supaya state-nya mulai
  // dari defaultValue permintaan itu. Tanpa remount, nilai awal harus diisi
  // lewat useEffect — dan select() di effect yang sama berjalan sebelum nilai
  // itu sampai ke DOM, sehingga teks default tidak pernah tersorot. Memakai id,
  // bukan judul, supaya dua permintaan berturut-turut yang judulnya sama tetap
  // dimulai dari nol (tanpa jeda null di antaranya).
  return <DialogIsian key={req.id} req={req} close={close} />
}

// Diekspor supaya cek-runtime.ts bisa merender dialognya langsung tanpa
// menyentuh store — komponen inilah yang memuat seluruh markup dialog.
export function DialogIsian({
  req,
  close,
}: {
  req: Req
  close: (v: string | null) => void
}) {
  const [nilai, setNilai] = useState(req.defaultValue ?? "")
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    // Isi awal langsung terseleksi supaya bisa ditimpa dengan mengetik, tanpa
    // harus menghapusnya dulu.
    inputRef.current?.focus()
    inputRef.current?.select()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close(null)
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [close])

  const boleh = isiValid(nilai)

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
      onClick={() => close(null)}
    >
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby="prompt-title"
        className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault()
          if (boleh) close(nilai)
        }}
      >
        <p id="prompt-title" className="text-sm font-semibold">
          {req.title}
        </p>
        {req.detail && (
          <p className="num mt-1 truncate text-xs text-muted-foreground" title={req.detail}>
            {req.detail}
          </p>
        )}
        <div className="mt-4 space-y-3">
          <div>
            {req.label && <label className="text-xs font-medium">{req.label}</label>}
            <Input
              ref={inputRef}
              className="mt-1"
              type={req.password ? "password" : "text"}
              autoComplete={req.password ? "new-password" : "off"}
              placeholder={req.placeholder}
              value={nilai}
              onChange={(e) => setNilai(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="outline" size="sm" onClick={() => close(null)}>
              {tr("Batal")}
            </Button>
            <Button type="submit" size="sm" disabled={!boleh}>
              {req.confirmLabel ?? tr("Simpan")}
            </Button>
          </div>
        </div>
      </form>
    </div>
  )
}
