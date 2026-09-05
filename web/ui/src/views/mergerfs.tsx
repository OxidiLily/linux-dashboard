import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { HardDriveDownload, Trash2, Plus, RefreshCw, Pencil, Play, Unplug } from "lucide-react"

import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { formatBytes } from "@/lib/format"

// Backend helperproto.MergerfsPool.
type Pool = {
  mountpoint: string
  branches: string[]
  options?: string
  mounted: boolean
  external?: boolean
  total?: number
  used?: number
  free?: number
}

const OPSI_DEFAULT =
  "defaults,nofail,allow_other,use_ino,category.create=pfrd,moveonenospc=true,minfreespace=10G"

const FORM_KOSONG = { mountpoint: "", branches: "", options: OPSI_DEFAULT }

export function MergerfsView() {
  const tr = useTr()
  const [pools, setPools] = useState<Pool[]>([])
  const [loading, setLoading] = useState(false)
  const [modal, setModal] = useState(false)
  // Mount point adalah kunci pool di fstab, jadi mode ubah menguncinya.
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState(FORM_KOSONG)

  const load = async () => {
    setLoading(true)
    try {
      setPools((await apiGet<Pool[]>("/api/storage/mergerfs")) || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat pool: {0}", pesanError(e)))
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

  const openEdit = (p: Pool) => {
    setEditing(p.mountpoint)
    setForm({
      mountpoint: p.mountpoint,
      branches: p.branches.join("\n"),
      options: p.options || OPSI_DEFAULT,
    })
    setModal(true)
  }

  const simpan = async (e: React.FormEvent) => {
    e.preventDefault()
    const branches = form.branches
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean)
    const ok = await confirmDialog({
      title: editing ? trf("Simpan perubahan pool {0}?", form.mountpoint) : trf("Buat pool di {0}?", form.mountpoint),
      message:
        tr("Baris /etc/fstab ditulis lalu pool di-mount ulang. Folder sumber tidak diubah isinya — file lama tetap di tempatnya dan langsung terlihat lewat mount point."),
      detail: branches.join("\n"),
      confirmLabel: tr("Simpan"),
    })
    if (!ok) return
    // Menyimpan pool berarti /etc/fstab ditulis lalu mount dijalankan —
    // pekerjaan kernel, bukan tulis berkas biasa.
    try {
      await notify.tugas(
        apiSend("/api/storage/mergerfs", editing ? "PUT" : "POST", {
          mountpoint: form.mountpoint.trim(),
          branches,
          options: form.options.trim(),
        }),
        {
          jalan: editing
            ? trf("Menyimpan pool {0}…", form.mountpoint)
            : trf("Membuat pool {0}…", form.mountpoint),
          sukses: editing ? tr("Pool diperbarui.") : trf("Pool {0} dibuat.", form.mountpoint),
          gagal: (e) => trf("Gagal menyimpan pool: {0}", pesanError(e)),
        },
      )
      setModal(false)
      setEditing(null)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  // Mount/unmount tidak menyentuh /etc/fstab, jadi pool yang dilepas akan
  // terpasang lagi saat boot. Melepas perlu konfirmasi karena isi pool langsung
  // hilang dari mount point — aplikasi yang sedang memakainya ikut terdampak.
  const ubahMount = async (p: Pool, lepas: boolean) => {
    if (lepas) {
      const ok = await confirmDialog({
        title: trf("Lepas pool {0}?", p.mountpoint),
        message:
          tr("Isi gabungan langsung hilang dari mount point sampai dipasang lagi. Berkasnya TIDAK dihapus — tetap ada di masing-masing disk sumber. Baris /etc/fstab dibiarkan, jadi pool terpasang lagi setelah server boot."),
        confirmLabel: tr("Lepas pool"),
        danger: true,
      })
      if (!ok) return
    }
    try {
      await notify.tugas(
        apiSend("/api/storage/mergerfs/mount", "POST", { mountpoint: p.mountpoint, lepas }),
        {
          jalan: lepas
            ? trf("Melepas pool {0}…", p.mountpoint)
            : trf("Memasang pool {0}…", p.mountpoint),
          sukses: lepas
            ? trf("Pool {0} dilepas.", p.mountpoint)
            : trf("Pool {0} dipasang.", p.mountpoint),
          gagal: (e) =>
            lepas
              ? trf("Gagal melepas pool: {0}", pesanError(e))
              : trf("Gagal memasang pool: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const hapus = async (p: Pool) => {
    const ok = await confirmDialog({
      title: trf("Hapus pool {0}?", p.mountpoint),
      message:
        tr("Pool dilepas dan barisnya dibuang dari /etc/fstab. Isi folder sumber TIDAK dihapus — file tetap ada di masing-masing disk, hanya tidak lagi tergabung."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(
        apiSend(`/api/storage/mergerfs?mountpoint=${encodeURIComponent(p.mountpoint)}`, "DELETE"),
        {
          jalan: trf("Menghapus pool {0}…", p.mountpoint),
          sukses: trf("Pool {0} dihapus.", p.mountpoint),
          gagal: (e) => trf("Gagal menghapus pool: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  return (
    <>
    <Panel
      title={tr("Disk Pool")}
      hint={tr("Gabungkan beberapa folder/disk jadi satu mount point (mergerfs)")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambah}>
            <Plus className="mr-1 size-3.5" /> {tr("Buat Pool")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => load()} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {pools.map((p) => (
          <div
            key={p.mountpoint}
            className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div className="flex min-w-0 items-start gap-3">
              <HardDriveDownload className="mt-0.5 size-5 text-signal" />
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="num text-sm font-semibold">{p.mountpoint}</p>
                  <Badge tone={p.mounted ? "ok" : "crit"}>{p.mounted ? tr("Ter-mount") : tr("Tidak aktif")}</Badge>
                  {p.external && <Badge tone="warn">{tr("dari fstab")}</Badge>}
                  <Badge tone="muted">{trf("{0} sumber", p.branches.length)}</Badge>
                </div>
                <p className="num mt-1 text-xs text-muted-foreground">{p.branches.join("  ·  ")}</p>
                {p.mounted && p.total ? (
                  <p className="num mt-0.5 text-xs text-muted-foreground">
                    {trf(
                      "{0} / {1} terpakai · sisa {2}",
                      formatBytes(p.used || 0),
                      formatBytes(p.total),
                      formatBytes(p.free || 0),
                    )}
                  </p>
                ) : null}
                {p.options && (
                  <p className="mt-0.5 truncate font-mono text-[10px] text-muted-2">{p.options}</p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-1">
              {p.external ? (
                <span className="px-2 text-[10px] text-muted-foreground">{tr("dikelola di /etc/fstab")}</span>
              ) : (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={
                      p.mounted ? trf("Lepas pool {0}", p.mountpoint) : trf("Pasang pool {0}", p.mountpoint)
                    }
                    title={p.mounted ? tr("Lepas pool") : tr("Pasang pool")}
                    onClick={() => ubahMount(p, p.mounted)}
                  >
                    {p.mounted ? <Unplug className="size-4" /> : <Play className="size-4" />}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={trf("Edit pool {0}", p.mountpoint)}
                    onClick={() => openEdit(p)}
                  >
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-crit"
                    aria-label={trf("Hapus pool {0}", p.mountpoint)}
                    onClick={() => hapus(p)}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </>
              )}
            </div>
          </div>
        ))}
        {pools.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada pool. Buat satu untuk menggabungkan beberapa disk jadi satu folder — file lama di tiap disk tetap utuh dan langsung terlihat menyatu.")}
          </p>
        )}
      </div>
    </Panel>

    {modal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">{editing ? trf("Edit Pool {0}", editing) : tr("Buat Disk Pool")}</p>
          <form onSubmit={simpan} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Mount point")}</label>
              <Input
                className="mt-1"
                required
                disabled={!!editing}
                value={form.mountpoint}
                onChange={(e) => setForm({ ...form, mountpoint: e.target.value })}
                placeholder="/mnt/pool"
              />
              {editing && (
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Mount point adalah kunci pool di /etc/fstab — hapus lalu buat baru untuk memindahkannya.")}
                </p>
              )}
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                {tr("Folder sumber (satu per baris, minimal dua)")}
              </label>
              <textarea
                className="mt-1 w-full rounded border border-border bg-background p-2 font-mono text-[11px]"
                rows={4}
                required
                value={form.branches}
                onChange={(e) => setForm({ ...form, branches: e.target.value })}
                placeholder={"/mnt/disk1\n/mnt/disk2"}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Opsi mount")}</label>
              <textarea
                className="mt-1 w-full rounded border border-border bg-background p-2 font-mono text-[11px]"
                rows={3}
                value={form.options}
                onChange={(e) => setForm({ ...form, options: e.target.value })}
              />
              <p className="mt-1 text-[10px] text-muted-foreground">
                {tr("category.create=pfrd menyebar file baru ke semua disk, dipilih acak dengan bobot sisa ruang — disk yang lebih besar menerima bagian lebih banyak dan semuanya penuh pada waktu berdekatan. Ganti ke mfs kalau justru ingin satu disk terisi penuh dulu sebelum pindah ke disk berikutnya; nofail menjaga server tetap bisa boot kalau pool gagal di-mount.")}
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
                {editing ? tr("Simpan Perubahan") : tr("Buat Pool")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}
    </>
  )
}
