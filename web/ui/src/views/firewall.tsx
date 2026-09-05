import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { useAuth } from "@/stores/auth"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Trash2, Plus, RefreshCw, Power, Pencil } from "lucide-react"

type UfwRule = {
  num?: string
  raw?: string
  action: string
  port: string
  proto: string
  from?: string
}

type UfwStatus = {
  installed: boolean
  enabled: boolean
  rules: UfwRule[]
}

export function FirewallView() {
  const tr = useTr()
  const user = useAuth((s) => s.user)
  const [rules, setRules] = useState<UfwRule[]>([])
  const [enabled, setEnabled] = useState(false)
  const [installed, setInstalled] = useState(true)
  const [loading, setLoading] = useState(false)
  const [showAdd, setShowAdd] = useState(false)
  // Rule yang sedang diubah. ufw tidak punya "edit": simpan = hapus rule lama
  // lalu tambahkan yang baru, dikerjakan sekaligus oleh helper.
  const [editing, setEditing] = useState<UfwRule | null>(null)
  const [form, setForm] = useState({ port: "", proto: "tcp", action: "allow", from: "" })

  const load = async () => {
    setLoading(true)
    try {
      const res = await apiGet<UfwStatus>("/api/firewall/rules")
      setEnabled(res.enabled)
      setInstalled(res.installed !== false)
      setRules(res.rules || [])
    } catch (e: any) {
      // Beda dengan "tidak ada rule": ufw belum terinstall → tampilkan
      // pesan installable, bukan alert merah yang membingungkan.
      const msg = String(e?.message || "")
      // Mencocokkan pesan mentah dari sistem, bukan teks UI.
      if (msg.includes("not found") || msg.includes("tidak ditemukan") || msg.includes("command not found")) { // i18n-abaikan
        setInstalled(false)
      } else {
        notify.err(trf("Gagal memuat rules firewall: {0}", pesanError(e)))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const openTambah = () => {
    setEditing(null)
    setForm({ port: "", proto: "tcp", action: "allow", from: "" })
    setShowAdd(true)
  }

  const openEdit = (r: UfwRule) => {
    setEditing(r)
    setForm({
      port: r.port,
      proto: r.proto || "any",
      action: (r.action || "allow").toLowerCase(),
      from: r.from && r.from !== "Anywhere" && r.from !== "any" ? r.from : "",
    })
    setShowAdd(true)
  }

  const tutupForm = () => {
    setShowAdd(false)
    setEditing(null)
  }

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    const body = {
      port: form.port,
      proto: form.proto,
      action: form.action,
      // kosong = Anywhere; ufw butuh literal "any" untuk itu.
      from: form.from.trim() || "any",
    }
    // Menyimpan rule berarti ufw menulis ulang aturannya dan memuat ulang
    // filter di kernel — bukan tulis berkas biasa. Rule baru juga tidak pernah
    // punya pesan berhasil sebelum ini: yang mengonfirmasi hanya baris yang
    // muncul di tabel, dan itu tidak terlihat kalau daftarnya panjang.
    const spec = editing?.num ? "" : editing?.raw
    const url = editing
      ? editing.num
        ? `/api/firewall/rules/${editing.num}`
        : `/api/firewall/rules/-?spec=${encodeURIComponent(spec!)}`
      : "/api/firewall/rules"
    try {
      await notify.tugas(apiSend(url, editing ? "PUT" : "POST", body), {
        jalan: editing ? tr("Menyimpan rule firewall…") : tr("Menambah rule firewall…"),
        sukses: editing ? tr("Rule firewall diperbarui.") : tr("Rule firewall ditambahkan."),
        gagal: (e) => trf("Gagal menyimpan rule: {0}", pesanError(e)),
      })
      tutupForm()
      setForm({ port: "", proto: "tcp", action: "allow", from: "" })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleDelete = async (rule: UfwRule) => {
    // ufw nonaktif -> rule tidak bernomor, dihapus lewat spec ("allow 22/tcp").
    const spec = rule.num ? "" : rule.raw
    if (!rule.num && !spec) return
    const ok = await confirmDialog({
      title: rule.num
        ? trf("Hapus rule firewall nomor {0}?", rule.num)
        : trf("Hapus rule firewall {0}?", spec ?? ""),
      message: tr("Port yang tadinya difilter rule ini akan mengikuti kebijakan default ufw."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(
        apiSend(
          rule.num
            ? `/api/firewall/rules/${rule.num}`
            : `/api/firewall/rules/-?spec=${encodeURIComponent(spec!)}`,
          "DELETE",
        ),
        {
          jalan: tr("Menghapus rule firewall…"),
          sukses: tr("Rule firewall dihapus."),
          gagal: (e) => trf("Gagal menghapus rule: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleToggle = async () => {
    const next = !enabled
    const ok = await confirmDialog({
      title: next ? tr("Nyalakan ufw?") : tr("Matikan ufw?"),
      message: next
        ? tr("Rule aktif langsung berlaku di kernel. Pastikan port SSH sudah diizinkan agar akses jarak jauh tidak terputus.")
        : tr("Semua filter berhenti — server menerima koneksi apa pun yang sampai ke port terbuka."),
      confirmLabel: next ? tr("Nyalakan") : tr("Matikan"),
      danger: !next,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend("/api/firewall/toggle", "POST", { enable: next }), {
        jalan: next ? tr("Menyalakan ufw…") : tr("Mematikan ufw…"),
        sukses: next ? tr("ufw dinyalakan.") : tr("ufw dimatikan."),
        gagal: (e) => trf("Gagal mengubah status ufw: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  return (
    <div className="space-y-4">
      <Panel
        title={tr("Firewall (ufw)")}
        hint={tr("Konfigurasi port filter host")}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={enabled ? "ok" : "crit"}>{enabled ? tr("UFW Aktif") : tr("UFW Nonaktif")}</Badge>
            {user?.sudo && installed && (
              <Button
                variant={enabled ? "outline" : "default"}
                size="sm"
                onClick={handleToggle}
                disabled={loading}
                title={enabled ? tr("Matikan ufw") : tr("Nyalakan ufw")}
              >
                <Power className="mr-1 size-3.5" />
                {enabled ? tr("Matikan") : tr("Nyalakan")}
              </Button>
            )}
            {user?.sudo && (
              <Button size="sm" onClick={openTambah}>
                <Plus className="mr-1 size-3.5" /> {tr("Tambah Rule")}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={load} disabled={loading}>
              <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </div>
        }
      >
        {!installed ? (
          <div className="space-y-2 py-6 text-center text-sm text-muted-foreground">
            <p>{tr("ufw belum terpasang di host ini.")}</p>
            <p className="text-[11px]">
              {tr("Pasang dengan apt install ufw lewat SSH, atau lewat menu Components (jika diotomasi di sana).")}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            {!enabled && rules.length > 0 && (
              <p className="pb-2 text-[11px] text-muted-foreground">
                {tr("Rule di bawah sudah tersimpan tapi belum berlaku — ufw masih nonaktif.")}
              </p>
            )}
            <table className="tabel-kartu w-full text-left text-xs">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="pb-2 font-medium">#</th>
                  <th className="pb-2 font-medium">{tr("Aksi")}</th>
                  <th className="pb-2 font-medium">{tr("Port / Protokol")}</th>
                  <th className="pb-2 font-medium">{tr("Dari")}</th>
                  <th className="pb-2 text-right font-medium">{tr("Aksi")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {rules.map((r, i) => (
                  <tr key={r.num || i} className="hover:bg-secondary/40">
                    <td data-label="#" className="num py-2 text-muted-foreground">{r.num || i + 1}</td>
                    <td data-label={tr("Aksi")} className="py-2">
                      <Badge tone={r.action?.toLowerCase().includes("allow") ? "ok" : "crit"}>
                        {(r.action || "").toUpperCase()}
                      </Badge>
                    </td>
                    <td data-label={tr("Port / Protokol")} className="num py-2 font-semibold">
                      {r.port}{r.proto && r.proto !== "any" ? `/${r.proto}` : ""}
                    </td>
                    <td data-label={tr("Dari")} className="num py-2 text-muted-foreground">{r.from || tr("Anywhere")}</td>
                    <td data-label="" className="py-2 text-right">
                      {user?.sudo && (r.num || r.raw) && (
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-1.5 text-muted-foreground hover:text-foreground"
                            aria-label={trf("Edit rule {0}", r.port)}
                            onClick={() => openEdit(r)}
                          >
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-1.5 text-muted-foreground hover:text-crit"
                            aria-label={trf("Hapus rule {0}", r.port)}
                            onClick={() => handleDelete(r)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
                {rules.length === 0 && !loading && (
                  <tr>
                    <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                      {enabled ? tr("Tidak ada rule aktif.") : tr("Belum ada rule tersimpan. ufw juga nonaktif — nyalakan untuk memfilter traffic.")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      {/* Modal Tambah Rule */}
      {showAdd && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">
              {editing
                ? editing.num
                  ? trf("Edit Rule #{0}", editing.num)
                  : tr("Edit Rule")
                : tr("Tambah Firewall Rule")}
            </p>
            {editing && (
              <p className="mt-1 text-[11px] text-muted-foreground">
                {tr("ufw tidak bisa mengubah rule di tempat — rule lama dihapus lalu yang baru ditambahkan, jadi posisinya pindah ke urutan terakhir.")}
              </p>
            )}
            <form onSubmit={handleAdd} className="mt-3 space-y-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Port / Service")}</label>
                <Input
                  className="mt-1"
                  required
                  placeholder={tr("mis. 80, 443, 22")}
                  value={form.port}
                  onChange={(e) => setForm({ ...form, port: e.target.value })}
                />
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Protokol")}</label>
                  <select
                    className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                    value={form.proto}
                    onChange={(e) => setForm({ ...form, proto: e.target.value })}
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                    <option value="any">Any</option>
                  </select>
                </div>
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Aksi")}</label>
                  <select
                    className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                    value={form.action}
                    onChange={(e) => setForm({ ...form, action: e.target.value })}
                  >
                    <option value="allow">ALLOW</option>
                    <option value="deny">DENY</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Dari (sumber)")}</label>
                <Input
                  className="mt-1"
                  placeholder={tr("kosongkan jika ingin Anywhere — atau 192.168.1.10 / 10.0.0.0/8")}
                  value={form.from}
                  onChange={(e) => setForm({ ...form, from: e.target.value })}
                />
              </div>
              <div className="flex justify-end gap-2 pt-3">
                <Button type="button" variant="outline" size="sm" onClick={tutupForm}>
                  {tr("Batal")}
                </Button>
                <Button type="submit" size="sm">
                  {tr("Simpan Rule")}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
