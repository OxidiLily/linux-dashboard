import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Folder, Trash2, ExternalLink, Pencil, X } from "lucide-react"
import { useNavigate } from "react-router-dom"
import { useAuth } from "@/stores/auth"

type Bookmark = {
  id: number
  name: string
  path: string
}

export function BookmarksView() {
  const tr = useTr()
  // Contoh path mengikuti home akun yang login: folder data panel ini ada di
  // ~/DATA/*, dan tiap akun punya miliknya sendiri.
  const home = useAuth((s) => s.user?.home) || "/home/user"
  const navigate = useNavigate()
  const [list, setList] = useState<Bookmark[]>([])
  const [name, setName] = useState("")
  const [path, setPath] = useState("")
  const [loading, setLoading] = useState(false)
  // Form kiri dipakai dua arah: null = tambah, id = ubah bookmark itu.
  const [editing, setEditing] = useState<number | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<Bookmark[]>("/api/bookmarks")
      setList(data || [])
    } catch {}
    finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const resetForm = () => {
    setEditing(null)
    setName("")
    setPath("")
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name || !path) return
    try {
      if (editing === null) {
        await apiSend("/api/bookmarks", "POST", { name, path })
      } else {
        await apiSend(`/api/bookmarks/${editing}`, "PUT", { name, path })
        notify.ok(tr("Bookmark diperbarui."))
      }
      resetForm()
      load()
    } catch (err: any) {
      notify.err(trf("Gagal menyimpan bookmark: {0}", pesanError(err)))
    }
  }

  const handleEdit = (b: Bookmark) => {
    setEditing(b.id)
    setName(b.name)
    setPath(b.path)
  }

  const handleDelete = async (id: number) => {
    const ok = await confirmDialog({
      title: tr("Hapus bookmark ini?"),
      message: tr("Hanya pintasannya yang hilang — folder aslinya tidak disentuh."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/bookmarks/${id}`, "DELETE")
      load()
    } catch (err: any) {
      notify.err(trf("Gagal menghapus bookmark: {0}", pesanError(err)))
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Panel
        title={editing === null ? tr("Tambah Bookmark") : tr("Ubah Bookmark")}
        className="lg:col-span-1"
        actions={
          editing !== null ? (
            <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={resetForm}>
              <X className="mr-1 size-3.5" /> {tr("Batal")}
            </Button>
          ) : undefined
        }
      >
        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="text-xs font-medium text-muted-foreground">{tr("Nama Bookmark")}</label>
            <Input
              className="mt-1"
              placeholder={tr("mis. Media Downloads")}
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>
          <div>
            <label className="text-xs font-medium text-muted-foreground">{tr("Path Folder Absolut")}</label>
            <Input
              className="mt-1"
              placeholder={`${home}/DATA/Downloads`}
              value={path}
              onChange={(e) => setPath(e.target.value)}
              required
            />
          </div>
          <Button type="submit" size="sm" className="w-full">
            {editing === null ? tr("Simpan Bookmark") : tr("Simpan Perubahan")}
          </Button>
        </form>
      </Panel>

      <Panel title={tr("Daftar Bookmarks")} hint={trf("{0} pintasan folder", list.length)} className="lg:col-span-2">
        <div className="space-y-2">
          {list.map((b) => (
            <div
              key={b.id}
              className="flex items-center justify-between rounded-md border border-border p-3 hover:bg-secondary/40"
            >
              <div className="flex items-center gap-3">
                <Folder className="size-5 text-amber-500 fill-amber-500/20" />
                <div>
                  <p className="font-semibold text-sm">{b.name}</p>
                  <p className="num text-xs text-muted-foreground">{b.path}</p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2"
                  title={tr("Buka di File Manager")}
                  onClick={() => navigate(`/files?path=${encodeURIComponent(b.path)}`)}
                >
                  <ExternalLink className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-foreground"
                  onClick={() => handleEdit(b)}
                  title={tr("Edit Bookmark")}
                  aria-label={trf("Edit bookmark {0}", b.name)}
                >
                  <Pencil className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-crit"
                  onClick={() => handleDelete(b.id)}
                  title={tr("Hapus Bookmark")}
                  aria-label={trf("Hapus bookmark {0}", b.name)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </div>
          ))}
          {list.length === 0 && !loading && (
            <p className="py-6 text-center text-xs text-muted-foreground">{tr("Belum ada bookmark folder.")}</p>
          )}
        </div>
      </Panel>
    </div>
  )
}
