import { useEffect, useRef, useState } from "react"
import { formatWaktu, zonaTampilan } from "@/lib/format"
import { usePrefs } from "@/stores/prefs"
import { daftarkanEscape } from "@/lib/lapisan-escape"
import { pesanError } from "@/lib/pesan-error"
import { Link } from "react-router-dom"
import { GEOIP_HINT, kunciGeoIP, lokasiGeoIP, pollGeoIP, statusGeoIP, pollGeoIPFull, poleGeoIP, labelGeoIP, nilaiGeoIP, benderaGeoIP, gambarBenderaGeoIP, riwayatIP, type GeoIPFull, type GeoIP } from "@/lib/fail2ban-geoip"
import { ShieldBan, Trash2, Plus, RefreshCw, Pencil, Unlock, Download, Power, Flag, Globe, Network, Clock, Languages, Coins, ShieldCheck, Gauge, Info, Check, X, History } from "lucide-react"

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

export { muatDetailIP } from "@/lib/fail2ban-detail"

export function GeoIPFields({ data }: { data: Record<string, unknown> | null }) {
  const tr = useTr()
  const [failedImage, setFailedImage] = useState<string | null>(null)
  const image = gambarBenderaGeoIP(data?.flag && typeof data.flag === "object" ? (data.flag as Record<string, unknown>).img : null)
  const groups: Record<string, [string, unknown][]> = Object.create(null)
  const icons: Record<string, typeof Globe> = { connection: Network, timezone: Clock, flag: Flag, native: Languages, currency: Coins, security: ShieldCheck, rate: Gauge }
  const flag = data ? benderaGeoIP(data) : null
  try {
    for (const [key, value] of Object.entries(data || {})) {
      const group = value !== null && typeof value === "object" ? key : tr("Identitas dan lokasi")
      ;(groups[group] ||= []).push(...poleGeoIP(value, key, 1))
    }
  } catch {
    return <p role="alert">{tr("GeoIP ditolak: kedalaman JSON melebihi 32; payload tidak ditampilkan.")}</p>
  }
  return <>
    {data === null && <p>{tr("GeoIP tidak tersedia")}</p>}
    {Object.entries(groups).map(([group, rows]) => {
      const Icon = Object.hasOwn(icons, group) ? icons[group] : (group === tr("Identitas dan lokasi") ? Globe : Info)
      return <section key={group}>
      <h3 className="mb-2 flex items-center gap-2 font-semibold"><Icon aria-hidden={true} className="size-4 text-muted-foreground" />{group === tr("Identitas dan lokasi") ? group : labelGeoIP(group)}</h3>
      <div className="overflow-x-auto rounded-lg border border-border"><table className="tabel-kartu w-full text-left text-xs">
        <thead><tr className="border-b border-border bg-secondary/30 text-muted-foreground"><th scope="col" className="p-2.5 font-medium">{tr("Kolom")}</th><th scope="col" className="p-2.5 font-medium">{tr("Nilai")}</th></tr></thead>
        <tbody>{rows.map(([key, value]) => <tr key={key} data-field={key} className="transition-colors hover:bg-secondary/40"><td data-label={tr("Kolom")} className="p-2.5 break-words font-medium">{key === "flag.img" ? labelGeoIP("flag") : labelGeoIP(key.startsWith(`${group}.`) ? key.slice(group.length + 1) : key)}</td><td data-label={tr("Nilai")} className="p-2.5 whitespace-pre-wrap break-all">
          <span className="inline-flex items-center gap-1.5">
            {key === "flag.img" ? image && failedImage !== image ? <img src={image} alt={labelGeoIP("flag")} loading="lazy" referrerPolicy="no-referrer" width={24} height={16} className="h-4 w-6 object-contain" onError={() => setFailedImage(image)} /> : <span role="img" aria-label={labelGeoIP("flag")}>{flag || <Flag aria-hidden={true} className="size-4" />}</span> : <>
              {typeof value === "boolean" && (value ? <Check aria-hidden={true} className="size-3.5" /> : <X aria-hidden={true} className="size-3.5" />)}
              {nilaiGeoIP(key, value, data!)}
            </>}
          </span>
        </td></tr>)}</tbody>
      </table></div>
    </section>})}
  </>
}

export function DetailIPModal({ jail, ip, tutup, lepas }: {
  jail: string; ip: string; tutup: () => void; lepas: () => Promise<boolean | void>
}) {
  const tr = useTr()
  const timezone = usePrefs((s) => s.timezone)
  const [data, setData] = useState<GeoIPFull>()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const closeRef = useRef<HTMLButtonElement>(null)
  const active = useRef(false)
  useEffect(() => {
    active.current = true
    const previous = document.activeElement as HTMLElement | null
    closeRef.current?.focus()
    return () => { active.current = false; previous?.isConnected && previous.focus() }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    setData(undefined)
    setError("")
    pollGeoIPFull(jail, ip, controller.signal, setData).then((exhausted) => {
      if (!controller.signal.aborted && exhausted) setError(trf("Polling GeoIP berhenti; muat ulang untuk mencoba lagi."))
    }).catch((e) => {
      if (!controller.signal.aborted) setError(pesanError(e))
    })
    return () => controller.abort()
  }, [jail, ip])
  useEffect(() => daftarkanEscape(tutup), [tutup])
  const unban = async () => {
    if (busy) return
    setBusy(true)
    try {
      const success = await lepas()
      if (active.current && success) tutup()
    } finally {
      if (active.current) { setBusy(false); closeRef.current?.focus() }
    }
  }
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div role="dialog" aria-modal="true" aria-labelledby="fail2ban-detail-title"
        className="max-h-[85dvh] w-full max-w-2xl overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl"
        onKeyDown={(e) => {
          if (e.key !== "Tab") return
          const buttons = Array.from(e.currentTarget.querySelectorAll<HTMLElement>("button:not(:disabled), a[href], summary"))
          const first = buttons[0], last = buttons[buttons.length - 1]
          if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
          if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
        }}>
        <h2 id="fail2ban-detail-title" className="text-sm font-semibold">{tr("Detail IP diblokir")}</h2>
        <p className="num mt-2 break-all text-sm">{jail} · {ip}</p>
        <p className="mt-1 text-xs text-muted-foreground">{tr(GEOIP_HINT)}</p>
        <p className="mt-1 text-xs">{tr("Cache GeoIP")}: {statusGeoIP(data?.status)} · {data?.source || "—"}</p>
        <p className="mt-1 text-xs">{tr("Waktu pengambilan cache")}: {data?.fetched_at ? formatWaktu(data.fetched_at) : "—"} · {tr("Zona waktu")}: {timezone || zonaTampilan() || Intl.DateTimeFormat().resolvedOptions().timeZone}</p>
        <p className="mt-1 text-xs text-muted-foreground">{tr("Waktu snapshot menunjukkan waktu saat data diambil, bukan jam saat ini.")}</p>
        <div className="mt-3 space-y-2 text-xs" aria-live="polite" aria-busy={!data && !error}>
          {!data && !error && <p role="status">{tr("Memuat GeoIP…")}</p>}
          {error && <p role="alert" className="text-crit">{trf("Gagal memuat detail IP: {0}", error)}</p>}
          {data && <GeoIPFields data={data.data} />}
        </div>
        <Button asChild variant="outline" size="sm" className="mt-3"><Link to={riwayatIP(jail, ip)}><History aria-hidden={true} className="size-3.5" />{tr("Riwayat")}</Link></Button>
        <div className="mt-4 flex justify-end gap-2">
          <Button ref={closeRef} variant="outline" size="sm" onClick={tutup}>{tr("Tutup")}</Button>
          <Button size="sm" disabled={busy} onClick={unban}><Unlock className="mr-1 size-3" />{tr("Lepas blokir")}</Button>
        </div>
      </div>
    </div>
  )
}

export function GeoIPTable({ jail, ips, items, buka }: { jail: string; ips: string[]; items: Record<string, GeoIP>; buka: (ip: string) => void }) {
  const tr = useTr()
  usePrefs(s => s.timezone)
  const labels = ["IP", tr("Lokasi"), "ISP / ASN", tr("Cache GeoIP")]
  const seen = new Set<string>()
  const unique = ips.filter(ip => { const key = kunciGeoIP(ip); if (seen.has(key)) return false; seen.add(key); return true })
  return <div className="mt-3 overflow-x-auto rounded-lg border border-border">
    <table className="tabel-kartu w-full text-left text-xs" aria-label={`${jail} · ${tr("Detail IP diblokir")}`}>
      <thead><tr className="border-b border-border bg-secondary/30 text-muted-foreground">{labels.map(label => <th key={label} scope="col" className="p-2.5 font-medium">{label}</th>)}</tr></thead>
      <tbody>{unique.map(ip => {
        const item = items[kunciGeoIP(ip)]
        return <tr key={ip} className="transition-colors hover:bg-secondary/40">
          <td data-label={labels[0]} className="p-2.5"><button type="button" className="num break-all text-signal underline underline-offset-2" onClick={() => buka(ip)}>{ip}</button></td>
          <td data-label={labels[1]} className="p-2.5 break-words">{lokasiGeoIP(item)}</td>
          <td data-label={labels[2]} className="p-2.5 break-words">{item?.isp || item?.org || "—"}{item?.asn ? ` · AS${item.asn}` : ""}</td>
          <td data-label={labels[3]} className="p-2.5"><p>{statusGeoIP(item?.status)}{item && item.source !== "none" ? ` · ${item.source}` : ""}</p>{item?.fetched_at && formatWaktu(item.fetched_at) !== "—" && <p className="num text-muted-foreground" title={tr("Terakhir diperbarui")}>{formatWaktu(item.fetched_at)}</p>}</td>
        </tr>
      })}{ips.length === 0 && <tr><td colSpan={4} data-label="" className="p-6 text-center text-muted-foreground">{tr("Belum ada IP diblokir.")}</td></tr>}</tbody>
    </table>
  </div>
}

const FORM_KOSONG = { name: "", enabled: true, maxretry: 5, bantime: "1h", findtime: "10m", port: "", adopt: false }

export function Fail2banView() {
  const tr = useTr()
  const [jails, setJails] = useState<Jail[]>([])
  const [loading, setLoading] = useState(false)
  const [modal, setModal] = useState(false)
  const [detail, setDetail] = useState<{ jail: string; ip: string } | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState(FORM_KOSONG)

  const [geo, setGeo] = useState<Record<string, GeoIP>>({})
  const [geoError, setGeoError] = useState("")
  const loadSeq = useRef(0)
  const request = useRef<AbortController>()

  const load = async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    const seq = ++loadSeq.current
    const current = () => seq === loadSeq.current && !controller.signal.aborted
    setLoading(true)
    setGeoError("")
    try {
      const list = (await apiGet<Jail[]>("/api/security/fail2ban", controller.signal)) || []
      if (!current()) return
      setJails(list)
      setLoading(false)
      const ips = new Set(list.flatMap(j => j.banned_ips || []).map(kunciGeoIP))
      setGeo(previous => Object.fromEntries(Object.entries(previous).filter(([ip]) => ips.has(ip))))
      if (ips.size) void pollGeoIP(controller.signal, current, items => {
        setGeo(previous => ({ ...previous, ...Object.fromEntries(Object.entries(items).filter(([ip]) => ips.has(ip))) }))
      }).then(exhausted => {
        if (current() && exhausted) setGeoError("Polling GeoIP berhenti; muat ulang untuk mencoba lagi.")
      }).catch(e => {
        if (current()) {
          setGeoError(pesanError(e))
          setGeo(previous => Object.fromEntries([...ips].map(ip => [ip, previous[ip] || { ip, country: "", country_code: "", region: "", city: "", isp: "", org: "", asn: 0, timezone: "", status: "error", source: "none", fetched_at: "" }])))
        }
      })
    } catch (e: any) {
      if (current()) notify.err(trf("Gagal memuat jail: {0}", pesanError(e)))
    } finally {
      if (current()) setLoading(false)
    }
  }

  useEffect(() => {
    load()
    return () => { ++loadSeq.current; request.current?.abort() }
  }, [])

  const openTambah = () => {
    setEditing(null)
    setForm(FORM_KOSONG)
    setModal(true)
  }

  const openEdit = (j: Jail, adopt = false) => {
    setEditing(j.name)
    setForm({
      name: j.name,
      enabled: j.enabled,
      maxretry: j.maxretry || 5,
      bantime: j.bantime || "1h",
      findtime: j.findtime || "10m",
      port: j.port || "",
      adopt,
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
    // Menyimpan jail selalu diikuti fail2ban dimuat ulang — beberapa detik di
    // mesin kecil, dan sepanjang itu layar lama tidak berubah sama sekali.
    try {
      await notify.tugas(
        apiSend(
          editing ? `/api/security/fail2ban/${encodeURIComponent(editing)}` : "/api/security/fail2ban",
          editing ? "PUT" : "POST",
          { ...form, maxretry: Number(form.maxretry) },
        ),
        {
          jalan: editing ? trf("Menyimpan jail {0}…", form.name) : trf("Membuat jail {0}…", form.name),
          sukses: editing ? tr("Jail diperbarui.") : trf("Jail {0} dibuat.", form.name),
          gagal: (e) => trf("Gagal menyimpan jail: {0}", pesanError(e)),
        },
      )
      setModal(false)
      setEditing(null)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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
      await notify.tugas(
        apiSend(`/api/security/fail2ban/${encodeURIComponent(j.name)}`, "PUT", {
          name: j.name,
          enabled: aktif,
          maxretry: j.maxretry || 5,
          bantime: j.bantime || "1h",
          findtime: j.findtime || "10m",
          port: j.port || "",
        }),
        {
          jalan: aktif ? trf("Mengaktifkan jail {0}…", j.name) : trf("Mematikan jail {0}…", j.name),
          sukses: aktif ? trf("Jail {0} diaktifkan.", j.name) : trf("Jail {0} dimatikan.", j.name),
          gagal: (e) => trf("Gagal mengubah status jail: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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
      await notify.tugas(apiSend(`/api/security/fail2ban/${encodeURIComponent(j.name)}`, "DELETE"), {
        jalan: trf("Menghapus jail {0}…", j.name),
        sukses: trf("Jail {0} dihapus.", j.name),
        gagal: (e) => trf("Gagal menghapus jail: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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
      await notify.tugas(
        apiSend(
          `/api/security/fail2ban/${encodeURIComponent(jail)}/unban?ip=${encodeURIComponent(ip)}`,
          "POST",
        ),
        {
          jalan: trf("Melepas blokir {0}…", ip),
          sukses: trf("{0} dilepas dari {1}.", ip, jail),
          gagal: (e) => trf("Gagal melepas blokir: {0}", pesanError(e)),
        },
      )
      load()
      return true
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }


  // Escape menutup modal ini — lewat tumpukan lapisan bersama supaya hanya
  // lapisan teratas yang tertutup (lihat lib/lapisan-escape.ts).
  useEffect(() => {
    if (!modal) return
    return daftarkanEscape(() => setModal(false))
  }, [modal])
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
      <p className="mb-3 text-xs text-muted-foreground">{tr(GEOIP_HINT)}</p>
      {geoError && <p role="status" className="mb-3 text-xs text-warn">{tr("Gagal memuat GeoIP")}: {tr(geoError)}</p>}
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
                    onClick={() => openEdit(j, true)}
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

            <GeoIPTable jail={j.name} ips={j.banned_ips || []} items={geo} buka={ip => setDetail({ jail: j.name, ip })} />
          </div>
        ))}
        {jails.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada jail. Tambahkan sshd untuk mulai memblokir percobaan login SSH yang gagal berulang.")}
          </p>
        )}
      </div>
    </Panel>

    {detail && <DetailIPModal key={`${detail.jail}/${detail.ip}`} jail={detail.jail} ip={detail.ip}
      tutup={() => setDetail(null)} lepas={() => lepasBlokir(detail.jail, detail.ip)} />}

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
