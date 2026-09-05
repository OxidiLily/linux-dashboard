import { useEffect, useState } from "react"
import { useAuth } from "@/stores/auth"
import { pesanError } from "@/lib/pesan-error"
import { Share, Trash2, Plus, RefreshCw, Pencil, HardDriveDownload, Play, Unplug, Search } from "lucide-react"

import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { formatBytes } from "@/lib/format"

type NFSClient = { host: string; options?: string }
type NFSExport = {
  path: string
  clients: NFSClient[]
  active: boolean
  external?: boolean
}

// Backend helperproto.NFSMount — sisi klien: export server lain yang dipasang
// di mesin ini.
type NFSMount = {
  server: string
  remote: string
  mountpoint: string
  options?: string
  mounted: boolean
  in_fstab: boolean
  external?: boolean
  total?: number
  used?: number
  free?: number
}

type RemoteExport = { path: string; clients?: string }

const OPSI_DEFAULT = "rw,sync,no_subtree_check"
const OPSI_MOUNT_DEFAULT = "_netdev,nofail,rw,hard,retry=0,timeo=600,retrans=2"
const FORM_KOSONG = { path: "", clients: "" }
const FORM_MOUNT_KOSONG = { server: "", remote: "", mountpoint: "", options: OPSI_MOUNT_DEFAULT }

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

  // Sisi klien punya daftar, modal, dan form sendiri: dua panel di satu halaman
  // karena keduanya sisi berlawanan dari protokol yang sama — yang dibagikan
  // mesin ini, dan yang dipasang mesin ini dari server lain.
  const [mounts, setMounts] = useState<NFSMount[]>([])
  const [loadingMount, setLoadingMount] = useState(false)
  const [modalMount, setModalMount] = useState(false)
  const [editingMount, setEditingMount] = useState<string | null>(null)
  const [formMount, setFormMount] = useState(FORM_MOUNT_KOSONG)
  const [exportsRemote, setExportsRemote] = useState<RemoteExport[] | null>(null)
  const [mencari, setMencari] = useState(false)

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
    loadMounts()
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
    // /etc/exports ditulis lalu `exportfs -ra` dijalankan; di mesin dengan
    // banyak export itu beberapa detik tanpa satu pun perubahan di layar.
    try {
      await notify.tugas(
        apiSend("/api/storage/nfs", editing ? "PUT" : "POST", { path: form.path.trim(), clients }),
        {
          jalan: editing ? trf("Menyimpan export {0}…", form.path) : trf("Membuat export {0}…", form.path),
          sukses: editing ? tr("Export diperbarui.") : trf("Export {0} dibuat.", form.path),
          gagal: (e) => trf("Gagal menyimpan export: {0}", pesanError(e)),
        },
      )
      setModal(false)
      setEditing(null)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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
      await notify.tugas(apiSend(`/api/storage/nfs?path=${encodeURIComponent(e.path)}`, "DELETE"), {
        jalan: trf("Menghapus export {0}…", e.path),
        sukses: trf("Export {0} dihapus.", e.path),
        gagal: (err) => trf("Gagal menghapus export: {0}", pesanError(err)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const loadMounts = async () => {
    setLoadingMount(true)
    try {
      setMounts((await apiGet<NFSMount[]>("/api/storage/nfs/mounts")) || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat mount: {0}", pesanError(e)))
    } finally {
      setLoadingMount(false)
    }
  }

  const openTambahMount = () => {
    setEditingMount(null)
    setFormMount(FORM_MOUNT_KOSONG)
    setExportsRemote(null)
    setModalMount(true)
  }

  const openEditMount = (m: NFSMount) => {
    setEditingMount(m.mountpoint)
    setFormMount({
      server: m.server,
      remote: m.remote,
      mountpoint: m.mountpoint,
      options: m.options || OPSI_MOUNT_DEFAULT,
    })
    setExportsRemote(null)
    setModalMount(true)
  }

  // showmount ke server yang tidak menjawab menggantung sampai batas waktu
  // helper, jadi tombolnya dikunci selama pencarian berjalan.
  const cariExport = async () => {
    const server = formMount.server.trim()
    if (!server) return
    setMencari(true)
    try {
      const hasil = await apiSend<RemoteExport[]>("/api/storage/nfs/mounts/discover", "POST", { server })
      setExportsRemote(hasil || [])
      if (!hasil || hasil.length === 0) {
        notify.warn(trf("{0} tidak membagikan folder apa pun lewat NFS.", server))
      }
    } catch (e: any) {
      setExportsRemote(null)
      notify.err(trf("Gagal membaca export {0}: {1}", server, pesanError(e)))
    } finally {
      setMencari(false)
    }
  }

  const simpanMount = async (ev: React.FormEvent) => {
    ev.preventDefault()
    const body = {
      server: formMount.server.trim(),
      remote: formMount.remote.trim(),
      mountpoint: formMount.mountpoint.trim(),
      options: formMount.options.trim(),
    }
    const ok = await confirmDialog({
      title: editingMount
        ? trf("Simpan perubahan mount {0}?", body.mountpoint)
        : trf("Pasang {0}:{1} di {2}?", body.server, body.remote, body.mountpoint),
      message: tr(
        "Baris /etc/fstab ditulis lalu folder langsung di-mount. Isi folder mount point yang lama tidak terhapus — hanya tertutup selama mount hidup.",
      ),
      detail: `${body.server}:${body.remote}  →  ${body.mountpoint}\n${body.options}`,
      confirmLabel: tr("Simpan"),
    })
    if (!ok) return
    // Menulis fstab lalu mount ke server lain: bisa beberapa detik, dan lebih
    // lama lagi kalau servernya lambat menjawab.
    try {
      await notify.tugas(
        apiSend("/api/storage/nfs/mounts", editingMount ? "PUT" : "POST", body),
        {
          jalan: trf("Memasang {0}…", body.mountpoint),
          sukses: editingMount
            ? tr("Mount diperbarui.")
            : trf("{0} terpasang di {1}.", body.remote, body.mountpoint),
          gagal: (e) => trf("Gagal memasang mount: {0}", pesanError(e)),
        },
      )
      setModalMount(false)
      setEditingMount(null)
      loadMounts()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  // Pasang/lepas tidak menyentuh /etc/fstab, jadi mount yang dilepas terpasang
  // lagi setelah server boot.
  const ubahMount = async (m: NFSMount, lepas: boolean) => {
    if (lepas) {
      const ok = await confirmDialog({
        title: trf("Lepas mount {0}?", m.mountpoint),
        message: tr(
          "Isi folder dari server langsung hilang dari mount point sampai dipasang lagi. Tidak ada berkas yang dihapus — semuanya tetap di server. Baris /etc/fstab dibiarkan, jadi mount kembali terpasang setelah boot.",
        ),
        confirmLabel: tr("Lepas mount"),
        danger: true,
      })
      if (!ok) return
    }
    try {
      await notify.tugas(
        apiSend("/api/storage/nfs/mounts/mount", "POST", { mountpoint: m.mountpoint, lepas }),
        {
          jalan: lepas ? trf("Melepas {0}…", m.mountpoint) : trf("Memasang {0}…", m.mountpoint),
          sukses: lepas ? trf("{0} dilepas.", m.mountpoint) : trf("{0} dipasang.", m.mountpoint),
          gagal: (e) =>
            lepas
              ? trf("Gagal melepas mount: {0}", pesanError(e))
              : trf("Gagal memasang mount: {0}", pesanError(e)),
        },
      )
      loadMounts()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const hapusMount = async (m: NFSMount) => {
    const ok = await confirmDialog({
      title: trf("Hapus mount {0}?", m.mountpoint),
      message: tr(
        "Folder dilepas dan barisnya dibuang dari /etc/fstab. Isi di server TIDAK dihapus — mesin ini hanya berhenti memasangnya.",
      ),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(
        apiSend(`/api/storage/nfs/mounts?mountpoint=${encodeURIComponent(m.mountpoint)}`, "DELETE"),
        {
          jalan: trf("Menghapus mount {0}…", m.mountpoint),
          sukses: trf("Mount {0} dihapus.", m.mountpoint),
          gagal: (e) => trf("Gagal menghapus mount: {0}", pesanError(e)),
        },
      )
      loadMounts()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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

    <Panel
      title={tr("Klien NFS")}
      hint={tr("Pasang export dari server lain ke folder di mesin ini (/etc/fstab)")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambahMount}>
            <Plus className="mr-1 size-3.5" /> {tr("Tambah Mount")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => loadMounts()} disabled={loadingMount}>
            <RefreshCw className={`size-3.5 ${loadingMount ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {mounts.map((m) => (
          <div
            key={m.mountpoint}
            className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div className="flex min-w-0 items-start gap-3">
              <HardDriveDownload className="mt-0.5 size-5 text-signal" />
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="num text-sm font-semibold">{m.mountpoint}</p>
                  <Badge tone={m.mounted ? "ok" : "crit"}>
                    {m.mounted ? tr("Ter-mount") : tr("Tidak aktif")}
                  </Badge>
                  {/* Mount tanpa baris fstab hilang setelah reboot — itu beda
                      yang tidak terlihat di mana pun kalau tidak disebut. */}
                  {!m.in_fstab && <Badge tone="warn">{tr("hilang setelah reboot")}</Badge>}
                  {m.external && m.in_fstab && <Badge tone="warn">{tr("dari fstab")}</Badge>}
                </div>
                <p className="num mt-1 text-xs text-muted-foreground">
                  {m.server}:{m.remote}
                </p>
                {m.mounted && m.total ? (
                  <p className="num mt-0.5 text-xs text-muted-foreground">
                    {trf(
                      "{0} / {1} terpakai · sisa {2}",
                      formatBytes(m.used || 0),
                      formatBytes(m.total),
                      formatBytes(m.free || 0),
                    )}
                  </p>
                ) : null}
                {m.options && (
                  <p className="mt-0.5 truncate font-mono text-[10px] text-muted-2">{m.options}</p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-1">
              {m.external ? (
                <span className="px-2 text-[10px] text-muted-foreground">
                  {m.in_fstab ? tr("dikelola di /etc/fstab") : tr("dipasang di luar panel")}
                </span>
              ) : (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={
                      m.mounted ? trf("Lepas mount {0}", m.mountpoint) : trf("Pasang mount {0}", m.mountpoint)
                    }
                    title={m.mounted ? tr("Lepas mount") : tr("Pasang mount")}
                    onClick={() => ubahMount(m, m.mounted)}
                  >
                    {m.mounted ? <Unplug className="size-4" /> : <Play className="size-4" />}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={trf("Edit mount {0}", m.mountpoint)}
                    onClick={() => openEditMount(m)}
                  >
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-crit"
                    aria-label={trf("Hapus mount {0}", m.mountpoint)}
                    onClick={() => hapusMount(m)}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </>
              )}
            </div>
          </div>
        ))}
        {mounts.length === 0 && !loadingMount && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada folder dari server lain yang dipasang di mesin ini.")}
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

    {modalMount && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">
            {editingMount ? trf("Edit Mount {0}", editingMount) : tr("Tambah Mount NFS")}
          </p>
          <form onSubmit={simpanMount} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Alamat server NFS")}</label>
              <div className="mt-1 flex gap-2">
                <Input
                  required
                  value={formMount.server}
                  onChange={(e) => setFormMount({ ...formMount, server: e.target.value })}
                  placeholder="192.168.2.11"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={cariExport}
                  disabled={mencari || !formMount.server.trim()}
                  title={tr("Tanya server folder apa saja yang dibagikannya (showmount -e)")}
                >
                  <Search className={mencari ? "mr-1 size-3.5 animate-pulse" : "mr-1 size-3.5"} />
                  {tr("Cari export")}
                </Button>
              </div>
            </div>
            {exportsRemote && exportsRemote.length > 0 && (
              <div className="rounded border border-border p-2">
                <p className="text-[10px] text-muted-foreground">
                  {tr("Folder yang dibagikan server ini — klik untuk memakainya:")}
                </p>
                <div className="mt-1 space-y-1">
                  {exportsRemote.map((x) => (
                    <button
                      key={x.path}
                      type="button"
                      className="block w-full rounded px-2 py-1 text-left hover:bg-secondary"
                      onClick={() =>
                        setFormMount((f) => ({
                          ...f,
                          remote: x.path,
                          // Mount point diisikan sekali sebagai saran, dan hanya
                          // kalau masih kosong: tebakan yang menimpa ketikan
                          // user lebih mengganggu daripada membantu.
                          mountpoint: f.mountpoint || `/mnt/${x.path.split("/").filter(Boolean).pop() || "nfs"}`,
                        }))
                      }
                    >
                      <span className="num text-xs">{x.path}</span>
                      {x.clients && <span className="ml-2 text-[10px] text-muted-2">{x.clients}</span>}
                    </button>
                  ))}
                </div>
              </div>
            )}
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Path export di server")}</label>
              <Input
                className="mt-1"
                required
                value={formMount.remote}
                onChange={(e) => setFormMount({ ...formMount, remote: e.target.value })}
                placeholder="/home/user/DATA/Documents"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                {tr("Mount point di mesin ini")}
              </label>
              <Input
                className="mt-1"
                required
                disabled={!!editingMount}
                value={formMount.mountpoint}
                onChange={(e) => setFormMount({ ...formMount, mountpoint: e.target.value })}
                placeholder="/mnt/documents"
              />
              {editingMount && (
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Mount point adalah kunci baris di /etc/fstab — hapus lalu buat baru untuk memindahkannya.")}
                </p>
              )}
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Opsi mount")}</label>
              <textarea
                className="mt-1 w-full rounded border border-border bg-background p-2 font-mono text-[11px]"
                rows={2}
                value={formMount.options}
                onChange={(e) => setFormMount({ ...formMount, options: e.target.value })}
              />
              <p className="mt-1 text-[10px] text-muted-foreground">
                {tr("nofail dan _netdev menjaga mesin ini tetap bisa boot kalau server NFS-nya mati; hard menahan tulisan sampai server menjawab lagi, bukan membuangnya diam-diam seperti soft. Hak tulis ditentukan UID: akun di mesin ini harus punya UID yang sama dengan pemilik folder di server.")}
              </p>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setModalMount(false)
                  setEditingMount(null)
                }}
              >
                {tr("Batal")}
              </Button>
              <Button type="submit" size="sm">
                {editingMount ? tr("Simpan Perubahan") : tr("Pasang Mount")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}
    </>
  )
}
