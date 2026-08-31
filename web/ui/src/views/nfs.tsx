import { useEffect, useState } from "react"
import { useAuth } from "@/stores/auth"
import { pesanError } from "@/lib/pesan-error"
import { Share, Trash2, Plus, RefreshCw, Pencil } from "lucide-react"

import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"

type NFSClient = { host: string; options?: string }
type NFSExport = {
  path: string
  clients: NFSClient[]
  active: boolean
  external?: boolean
}

const OPSI_DEFAULT = "rw,sync,no_subtree_check"
const FORM_KOSONG = { path: "", clients: "" }

// Satu klien per baris: "192.168.2.0/24 rw,sync" atau "192.168.2.0/24".
function parseKlien(teks: string): NFSClient[] {
  return teks
    .split("\n")
    .map((b) => b.trim())
    .filter(Boolean)
    .map((b) => {
      const [host, ...sisa] = b.split(/\s+/)
      return { host, options: sisa.join(" ") || OPSI_DEFAULT }
    })
}

export function NFSView() {
  const tr = useTr()
  // Contoh path mengikuti home akun yang login — folder data panel ada di
  // ~/DATA/*. NFS mengekspor satu path tetap (tidak ada makro per user seperti
  // %U di Samba), jadi yang diekspor selalu folder milik satu akun tertentu.
  const home = useAuth((s) => s.user?.home) || "/home/user"
  const [list, setList] = useState<NFSExport[]>([])
  const [loading, setLoading] = useState(false)
  const [modal, setModal] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState(FORM_KOSONG)

  const load = async () => {
    setLoading(true)
    try {
      setList((await apiGet<NFSExport[]>("/api/storage/nfs")) || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat export: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const openTambah = () => {
    setEditing(null)
    setForm(FORM_KOSONG)
    setModal(true)
  }

  const openEdit = (e: NFSExport) => {
    setEditing(e.path)
    setForm({
      path: e.path,
      clients: e.clients.map((c) => `${c.host} ${c.options ?? ""}`.trim()).join("\n"),
    })
    setModal(true)
  }

  const simpan = async (ev: React.FormEvent) => {
    ev.preventDefault()
    const clients = parseKlien(form.clients)
    const terbuka = clients.some((c) => c.host === "*")
    const ok = await confirmDialog({
      title: editing
        ? trf("Simpan perubahan export {0}?", form.path)
        : trf("Export {0} ke jaringan?", form.path),
      message: terbuka
        ? tr("Salah satu klien adalah * — folder ini bisa dimount oleh SIAPA PUN yang bisa menjangkau server.")
        : tr("Folder dibagikan ke klien yang terdaftar, lalu tabel export kernel dimuat ulang (exportfs -ra)."),
      detail: clients.map((c) => `${c.host}(${c.options})`).join("\n"),
      confirmLabel: tr("Simpan"),
      danger: terbuka,
    })
    if (!ok) return
    try {
      await apiSend("/api/storage/nfs", editing ? "PUT" : "POST", { path: form.path.trim(), clients })
      notify.ok(editing ? tr("Export diperbarui.") : trf("Export {0} dibuat.", form.path))
      setModal(false)
      setEditing(null)
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan export: {0}", pesanError(e)))
    }
  }

  const hapus = async (e: NFSExport) => {
    const ok = await confirmDialog({
      title: trf("Hapus export {0}?", e.path),
      message: tr("Folder berhenti dibagikan lewat NFS. Isi foldernya tidak dihapus."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/storage/nfs?path=${encodeURIComponent(e.path)}`, "DELETE")
      load()
    } catch (err: any) {
      notify.err(trf("Gagal menghapus export: {0}", pesanError(err)))
    }
  }

  return (
    <>
    <Panel
      title={tr("NFS Exports")}
      hint={tr("Bagikan folder ke klien Linux/Unix lewat NFS (/etc/exports)")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambah}>
            <Plus className="mr-1 size-3.5" /> {tr("Tambah Export")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => load()} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {list.map((e) => (
          <div
            key={e.path}
            className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div className="flex min-w-0 items-start gap-3">
              <Share className="mt-0.5 size-5 text-signal" />
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="num text-sm font-semibold">{e.path}</p>
                  {/* "Aktif" dibaca dari exportfs -s, bukan dari file: dua-duanya
                      sering beda kalau file diubah tanpa exportfs -ra. */}
                  <Badge tone={e.active ? "ok" : "crit"}>{e.active ? tr("Aktif") : tr("Belum aktif")}</Badge>
                  {e.external && <Badge tone="warn">{tr("dari /etc/exports")}</Badge>}
                  {e.clients.some((c) => c.host === "*") && <Badge tone="warn">{tr("terbuka untuk semua")}</Badge>}
                </div>
                <div className="mt-1 space-y-0.5">
                  {e.clients.map((c) => (
                    <p key={c.host} className="num text-xs text-muted-foreground">
                      {c.host} <span className="text-muted-2">({c.options})</span>
                    </p>
                  ))}
                </div>
              </div>
            </div>
            <div className="flex items-center gap-1">
              {e.external ? (
                <span className="px-2 text-[10px] text-muted-foreground">{tr("dikelola di /etc/exports")}</span>
              ) : (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={trf("Edit export {0}", e.path)}
                    onClick={() => openEdit(e)}
                  >
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-crit"
                    aria-label={trf("Hapus export {0}", e.path)}
                    onClick={() => hapus(e)}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </>
              )}
            </div>
          </div>
        ))}
        {list.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada folder yang dibagikan lewat NFS.")}
          </p>
        )}
      </div>
    </Panel>

    {modal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">{editing ? trf("Edit Export {0}", editing) : tr("Tambah NFS Export")}</p>
          <form onSubmit={simpan} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Folder yang dibagikan")}</label>
              <Input
                className="mt-1"
                required
                disabled={!!editing}
                value={form.path}
                onChange={(e) => setForm({ ...form, path: e.target.value })}
                placeholder={`${home}/DATA/Media`}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                {tr("Klien (satu per baris: alamat lalu opsi)")}
              </label>
              <textarea
                className="mt-1 w-full rounded border border-border bg-background p-2 font-mono text-[11px]"
                rows={4}
                required
                value={form.clients}
                onChange={(e) => setForm({ ...form, clients: e.target.value })}
                placeholder={"192.168.2.0/24 rw,sync,no_subtree_check\n192.168.2.50 ro,sync"}
              />
              <p className="mt-1 text-[10px] text-muted-foreground">
                {trf(
                  "Opsi kosong memakai {0}. Gunakan CIDR agar hanya jaringan lokal yang bisa mount — * membuka folder untuk siapa pun yang bisa menjangkau server.",
                  OPSI_DEFAULT,
                )}
              </p>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setModal(false)
                  setEditing(null)
                }}
              >
                {tr("Batal")}
              </Button>
              <Button type="submit" size="sm">
                {editing ? tr("Simpan Perubahan") : tr("Simpan Export")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}
    </>
  )
}
