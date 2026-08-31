import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { ShieldBan, Trash2, Plus, RefreshCw, Pencil, Unlock, Download, Power } from "lucide-react"

import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"

type Jail = {
  name: string
  enabled: boolean
  maxretry: number
  bantime?: string
  findtime?: string
  port?: string
  running: boolean
  currently_banned: number
  total_banned: number
  currently_failed: number
  total_failed: number
  banned_ips?: string[]
  external?: boolean
}

const FORM_KOSONG = { name: "", enabled: true, maxretry: 5, bantime: "1h", findtime: "10m", port: "" }

export function Fail2banView() {
  const tr = useTr()
  const [jails, setJails] = useState<Jail[]>([])
  const [loading, setLoading] = useState(false)
  const [modal, setModal] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState(FORM_KOSONG)

  const load = async () => {
    setLoading(true)
    try {
      setJails((await apiGet<Jail[]>("/api/security/fail2ban")) || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat jail: {0}", pesanError(e)))
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

  const openEdit = (j: Jail) => {
    setEditing(j.name)
    setForm({
      name: j.name,
      enabled: j.enabled,
      maxretry: j.maxretry || 5,
      bantime: j.bantime || "1h",
      findtime: j.findtime || "10m",
      port: j.port || "",
    })
    setModal(true)
  }

  const simpan = async (e: React.FormEvent) => {
    e.preventDefault()
    const ok = await confirmDialog({
      title: editing ? trf("Simpan perubahan jail \"{0}\"?", form.name) : trf("Buat jail \"{0}\"?", form.name),
      message:
        tr("jail.local ditulis lalu fail2ban dimuat ulang. Blokir yang sedang berjalan tetap berlaku sampai masa bannya habis."),
      detail: `maxretry ${form.maxretry} · bantime ${form.bantime} · findtime ${form.findtime}`,
      confirmLabel: tr("Simpan"),
    })
    if (!ok) return
    try {
      await apiSend(
        editing ? `/api/security/fail2ban/${encodeURIComponent(editing)}` : "/api/security/fail2ban",
        editing ? "PUT" : "POST",
        { ...form, maxretry: Number(form.maxretry) },
      )
      notify.ok(editing ? tr("Jail diperbarui.") : trf("Jail {0} dibuat.", form.name))
      setModal(false)
      setEditing(null)
      load()
    } catch (err: any) {
      notify.err(trf("Gagal menyimpan jail: {0}", pesanError(err)))
    }
  }

  // Menulis section dengan enabled=false ke jail.local adalah SATU-SATUNYA cara
  // menghentikan jail yang dinyalakan file sistem: fail2ban membaca jail.local
  // paling akhir, sedangkan menghapus section hanya mengembalikan nilai bawaan.
  const setAktif = async (j: Jail, aktif: boolean) => {
    const ok = await confirmDialog({
      title: aktif ? trf("Aktifkan jail \"{0}\"?", j.name) : trf("Matikan jail \"{0}\"?", j.name),
      message: aktif
        ? tr("Jail mulai memantau lagi setelah fail2ban dimuat ulang.")
        : tr("Panel menulis enabled = false ke jail.local, yang menimpa nilai dari jail.conf/jail.d. Layanan ini berhenti dipantau — percobaan login gagal tidak lagi diblokir otomatis."),
      confirmLabel: aktif ? tr("Aktifkan") : tr("Matikan"),
      danger: !aktif,
    })
    if (!ok) return
    try {
      await apiSend(`/api/security/fail2ban/${encodeURIComponent(j.name)}`, "PUT", {
        name: j.name,
        enabled: aktif,
        maxretry: j.maxretry || 5,
        bantime: j.bantime || "1h",
        findtime: j.findtime || "10m",
        port: j.port || "",
      })
      notify.ok(aktif ? trf("Jail {0} diaktifkan.", j.name) : trf("Jail {0} dimatikan.", j.name))
      load()
    } catch (e: any) {
      notify.err(trf("Gagal mengubah status jail: {0}", pesanError(e)))
    }
  }

  const hapus = async (j: Jail) => {
    const ok = await confirmDialog({
      title: trf("Hapus jail \"{0}\"?", j.name),
      message:
        tr("Jail dibuang dari sistem: pengaturannya di jail.local dihapus, dan stanza dengan nama sama di /etc/fail2ban/jail.d juga dibuang — termasuk definisi bawaan Debian/Ubuntu. Section [DEFAULT] dan jail lain di berkas yang sama tidak disentuh, dan berkas yang diubah dicadangkan sebagai .lindash.bak. Layanan ini berhenti dipantau dan hilang dari daftar."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/security/fail2ban/${encodeURIComponent(j.name)}`, "DELETE")
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menghapus jail: {0}", pesanError(e)))
    }
  }

  const lepasBlokir = async (jail: string, ip: string) => {
    const ok = await confirmDialog({
      title: trf("Lepas blokir {0}?", ip),
      message: trf("IP ini bisa mencoba login lagi ke {0}.", jail),
      confirmLabel: tr("Lepas"),
    })
    if (!ok) return
    try {
      await apiSend(
        `/api/security/fail2ban/${encodeURIComponent(jail)}/unban?ip=${encodeURIComponent(ip)}`,
        "POST",
      )
      notify.ok(trf("{0} dilepas dari {1}.", ip, jail))
      load()
    } catch (e: any) {
      notify.err(trf("Gagal melepas blokir: {0}", pesanError(e)))
    }
  }

  return (
    <>
    <Panel
      title={tr("Fail2ban")}
      hint={tr("Blokir otomatis IP yang berulang kali gagal login")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambah}>
            <Plus className="mr-1 size-3.5" /> {tr("Tambah Jail")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => load()} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {jails.map((j) => (
          <div key={j.name} className="rounded-md border border-border p-3">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex min-w-0 items-start gap-3">
                <ShieldBan className="mt-0.5 size-5 text-signal" />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="num text-sm font-semibold">{j.name}</p>
                    {/* "running" dibaca dari fail2ban-client, "enabled" dari file:
                        jail yang enabled tapi gagal start akan terlihat aman
                        padahal tidak memblokir apa pun. */}
                    <Badge tone={j.running ? "ok" : j.enabled ? "warn" : "muted"}>
                      {j.running ? tr("Jalan") : j.enabled ? tr("Enabled, belum jalan") : tr("Nonaktif")}
                    </Badge>
                    {j.external && <Badge tone="warn">{tr("di luar panel")}</Badge>}
                    {j.currently_banned > 0 && <Badge tone="crit">{trf("{0} IP diblokir", j.currently_banned)}</Badge>}
                  </div>
                  <p className="num mt-0.5 text-xs text-muted-foreground">
                    maxretry {j.maxretry} · bantime {j.bantime || "-"} · findtime {j.findtime || "-"}
                    {j.port ? ` · port ${j.port}` : ""}
                  </p>
                  {j.running && (
                    <p className="num text-xs text-muted-foreground">
                      {trf(
                        "gagal {0} sekarang / {1} total · diblokir {2} total",
                        j.currently_failed,
                        j.total_failed,
                        j.total_banned,
                      )}
                    </p>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1">
                {/* Matikan/Aktifkan berlaku untuk semua jail, termasuk yang
                    dikelola file sistem — inilah aksi yang benar-benar
                    menghentikan jail, bukan Hapus. */}
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs"
                  onClick={() => setAktif(j, !(j.running || j.enabled))}
                >
                  <Power className={`mr-1 size-3 ${j.running || j.enabled ? "text-crit" : "text-ok"}`} />
                  {j.running || j.enabled ? tr("Matikan") : tr("Aktifkan")}
                </Button>
                {j.external && (
                  // Jail dari jail.conf / jail.d bisa diambil alih: fail2ban
                  // memang membaca jail.local paling akhir, jadi menulis
                  // section bernama sama di sana adalah cara resmi menimpanya.
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => openEdit({ ...j, external: false })}
                  >
                    <Download className="mr-1 size-3" /> {tr("Kelola di panel")}
                  </Button>
                )}
                {!j.external && (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 px-2 text-muted-foreground hover:text-foreground"
                      aria-label={trf("Edit jail {0}", j.name)}
                      onClick={() => openEdit(j)}
                    >
                      <Pencil className="size-4" />
                    </Button>
                  </>
                )}
                {/* Hapus berlaku untuk semua jail: definisinya memang bisa
                    berada di jail.local maupun jail.d, dan keduanya dibuang. */}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-crit"
                  aria-label={trf("Hapus jail {0}", j.name)}
                  title={tr("Hapus rule dari sistem")}
                  onClick={() => hapus(j)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </div>

            {j.banned_ips && j.banned_ips.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-1.5 border-t border-border pt-2">
                {j.banned_ips.map((ip) => (
                  <Button
                    key={ip}
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => lepasBlokir(j.name, ip)}
                  >
                    <Unlock className="mr-1 size-3" />
                    <span className="num">{ip}</span>
                  </Button>
                ))}
              </div>
            )}
          </div>
        ))}
        {jails.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada jail. Tambahkan sshd untuk mulai memblokir percobaan login SSH yang gagal berulang.")}
          </p>
        )}
      </div>
    </Panel>

    {modal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">{editing ? trf("Edit Jail {0}", editing) : tr("Tambah Jail")}</p>
          <form onSubmit={simpan} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Nama jail")}</label>
              <Input
                className="mt-1"
                required
                disabled={!!editing}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="sshd"
              />
              <p className="mt-1 text-[10px] text-muted-foreground">
                {tr("Namanya harus cocok dengan filter bawaan fail2ban (mis. sshd, nginx-http-auth).")}
              </p>
            </div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">maxretry</label>
                <Input
                  className="mt-1"
                  type="number"
                  min={1}
                  max={100}
                  value={form.maxretry}
                  onChange={(e) => setForm({ ...form, maxretry: Number(e.target.value) })}
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">bantime</label>
                <Input
                  className="mt-1"
                  value={form.bantime}
                  onChange={(e) => setForm({ ...form, bantime: e.target.value })}
                  placeholder="1h"
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">findtime</label>
                <Input
                  className="mt-1"
                  value={form.findtime}
                  onChange={(e) => setForm({ ...form, findtime: e.target.value })}
                  placeholder="10m"
                />
              </div>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Port (opsional)")}</label>
              <Input
                className="mt-1"
                value={form.port}
                onChange={(e) => setForm({ ...form, port: e.target.value })}
                placeholder={tr("ssh atau 22")}
              />
            </div>
            <label className="flex cursor-pointer items-center gap-1.5 text-xs">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
              />
              <span>{tr("Aktifkan jail ini")}</span>
            </label>
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
                {editing ? tr("Simpan Perubahan") : tr("Buat Jail")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}
    </>
  )
}
