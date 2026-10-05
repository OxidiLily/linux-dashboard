import { useCallback, useEffect, useState } from "react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Select } from "@/components/ui/select"
import { confirmDialog } from "@/components/ui/confirm"
import { notify } from "@/components/ui/toast"
import { useTr } from "@/stores/i18n"
import {
  Cloud,
  CloudOff,
  Eye,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react"
import {
  recordFields,
  recordPayload,
  recordName,
  recordTypes,
  priorityTypes,
  proxiedTypes,
} from "./cloudflare-record-fields"

type Zone = { id: string; name: string }
type RecordDNS = {
  id: string
  type: string
  name: string
  content?: string
  data?: unknown
  ttl?: number
  proxied?: boolean
  proxiable?: boolean
  priority?: number
  comment?: string
  tags?: string[]
  settings?: unknown
}
type Page = {
  records: RecordDNS[]
  page: number
  total_pages: number
  total_count: number
}
const blank = { type: "A", name: "", content: "", ttl: 1 }

function formatContent(r: RecordDNS): string {
  if (r.content) return r.content
  if (r.data && typeof r.data === "object") {
    const d = r.data as Record<string, unknown>
    if (r.type === "SRV" && d.target) return `${d.priority ?? 0} ${d.weight ?? 0} ${d.port ?? 0} ${d.target}`
    if (r.type === "CAA" && d.tag) return `${d.flags ?? 0} ${d.tag} "${d.value ?? ""}"`
    if (r.type === "URI" && d.target) return `${d.weight ?? 0} ${d.target}`
    if (r.type === "LOC") {
      return `${d.lat_degrees ?? 0}° ${d.lat_direction ?? ""} ${d.long_degrees ?? 0}° ${d.long_direction ?? ""}`
    }
    return Object.entries(d)
      .map(([k, v]) => `${k}:${v}`)
      .join(" ")
  }
  return "—"
}

function formatTTL(ttl?: number, tr?: (s: string) => string): string {
  if (!ttl || ttl === 1) return tr ? tr("Auto") : "Auto"
  if (ttl % 3600 === 0) return `${ttl / 3600}h`
  if (ttl % 60 === 0) return `${ttl / 60}m`
  return `${ttl}s`
}

export function CloudflareManager({ enabled }: { enabled: boolean }) {
  const tr = useTr()
  const [zones, setZones] = useState<Zone[]>([])
  const [zone, setZone] = useState("")
  const zoneName = zones.find((z) => z.id === zone)?.name || ""
  const [page, setPage] = useState(1)
  const [result, setResult] = useState<Page | null>(null)
  const [busy, setBusy] = useState(false)
  const [edit, setEdit] = useState<RecordDNS | null>(null)
  const [draft, setDraft] = useState<RecordDNS>(blank as RecordDNS)
  const [draftTags, setDraftTags] = useState("")
  const [editing, setEditing] = useState(false)
  const [dataFields, setDataFields] = useState<Record<string, string>>({})
  const [detailModal, setDetailModal] = useState<RecordDNS | null>(null)

  const refreshZones = useCallback(async () => {
    setBusy(true)
    try {
      const items = await apiGet<Zone[]>("/api/proxy/cloudflare/zones")
      setZones(items)
      setZone((current) => (items.some((z) => z.id === current) ? current : items[0]?.id || ""))
    } catch (e) {
      notify.err(`${tr("Gagal membaca zone Cloudflare")}: ${pesanError(e)}`)
    } finally {
      setBusy(false)
    }
  }, [tr])

  useEffect(() => {
    if (enabled) void refreshZones()
    else {
      setZones([])
      setZone("")
      setResult(null)
    }
  }, [enabled, refreshZones])

  const refresh = useCallback(async () => {
    if (!zone) {
      setResult(null)
      return
    }
    setResult(null)
    setBusy(true)
    try {
      setResult(await apiSend<Page>("/api/proxy/cloudflare/records", "POST", { zone_id: zone, page }))
    } catch (e) {
      setResult(null)
      notify.err(`${tr("Gagal membaca DNS Cloudflare")}: ${pesanError(e)}`)
    } finally {
      setBusy(false)
    }
  }, [zone, page, tr])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    if (!detailModal) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDetailModal(null)
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [detailModal])

  const start = (record?: RecordDNS) => {
    setEditing(true)
    setEdit(record || null)
    setDraft(record ? { ...record } : ({ ...blank, name: "@" } as RecordDNS))
    setDraftTags(record?.tags ? record.tags.join(", ") : "")
    const data = record?.data && typeof record.data === "object" ? (record.data as Record<string, unknown>) : {}
    setDataFields(Object.fromEntries(Object.entries(data).map(([key, value]) => [key, String(value)])))
  }

  const save = async () => {
    if (!zoneName || !draft.name.trim() || !draft.type.trim()) {
      notify.err(tr("Jenis dan nama record wajib diisi."))
      return
    }
    let record: Record<string, unknown>
    const tags = draftTags.trim()
      ? draftTags.split(",").map((t) => t.trim()).filter(Boolean)
      : edit?.tags && edit.tags.length > 0
        ? []
        : undefined
    const draftToSend = { ...draft, tags }
    try {
      record = recordPayload(draftToSend, dataFields, edit?.proxiable !== false, zoneName)
    } catch (e) {
      notify.err(e instanceof Error ? e.message : tr("Record DNS tidak valid."))
      return
    }
    setBusy(true)
    try {
      await apiSend("/api/proxy/cloudflare/records", "PUT", {
        zone_id: zone,
        record_id: edit?.id || "",
        record,
      })
      setEdit(null)
      setEditing(false)
      setDraft(blank as RecordDNS)
      setDraftTags("")
      await refresh()
      notify.ok(tr("Record DNS tersimpan."))
    } catch (e) {
      notify.err(`${tr("Gagal menyimpan DNS Cloudflare")}: ${pesanError(e)}`)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (record: RecordDNS) => {
    if (
      !(await confirmDialog({
        title: tr("Hapus record DNS Cloudflare?"),
        message: `${record.type} ${record.name} — ${record.content || JSON.stringify(record.data)}`,
        confirmLabel: tr("Hapus"),
      }))
    )
      return
    setBusy(true)
    try {
      await apiSend("/api/proxy/cloudflare/records/delete", "POST", { zone_id: zone, record_id: record.id })
      await refresh()
      notify.ok(tr("Record DNS dihapus."))
    } catch (e) {
      notify.err(`${tr("Gagal menghapus record DNS")}: ${pesanError(e)}`)
    } finally {
      setBusy(false)
    }
  }

  if (!enabled)
    return (
      <p className="text-sm text-muted-foreground">
        {tr("Simpan token Cloudflare untuk memuat zone dan semua jenis record DNS.")}
      </p>
    )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-64">
          <label className="mb-1 block text-xs text-muted-foreground">{tr("Zone Cloudflare")}</label>
          <Select
            value={zone}
            onChange={(val) => {
              setZone(val)
              setPage(1)
              setEdit(null)
              setEditing(false)
            }}
            options={zones.map((z) => ({ value: z.id, label: z.name }))}
            placeholder={zones.length === 0 ? tr("Tidak ada zone") : tr("Pilih zone…")}
            disabled={busy || zones.length === 0}
            searchPlaceholder={tr("Cari zone…")}
          />
        </div>
        <Button size="sm" variant="outline" onClick={() => void refreshZones()} disabled={busy}>
          <RefreshCw className={cn("mr-1 size-3.5", busy && "animate-spin")} />
          {tr("Muat zone")}
        </Button>
        <Button size="sm" variant="outline" onClick={() => void refresh()} disabled={busy || !zone}>
          <RefreshCw className="mr-1 size-3.5" />
          {tr("Muat ulang")}
        </Button>
        <Button size="sm" onClick={() => start()} disabled={!zone || busy}>
          <Plus className="mr-1 size-3.5" />
          {tr("Tambah record")}
        </Button>
      </div>

      {zones.length === 0 && !busy && (
        <p className="text-sm text-muted-foreground">{tr("Tidak ada zone yang dapat diakses token ini.")}</p>
      )}

      {editing && (
        <div className="space-y-3 rounded-lg border border-border bg-surface-2/40 p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold">{edit ? tr("Edit record") : tr("Tambah record")}</h3>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              onClick={() => {
                setDraft(blank as RecordDNS)
                setDraftTags("")
                setEdit(null)
                setEditing(false)
              }}
            >
              <X className="size-3.5" />
            </Button>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs">{tr("Jenis DNS")}</label>
              <Select
                value={draft.type}
                disabled={!!edit}
                onChange={(val) => {
                  setDraft({
                    ...draft,
                    type: val,
                    content: "",
                    priority: undefined,
                    proxied: undefined,
                    data: undefined,
                  })
                  setDataFields({})
                }}
                options={!recordTypes.includes(draft.type) ? [draft.type, ...recordTypes] : recordTypes}
                searchPlaceholder={tr("Cari jenis DNS…")}
              />
            </div>
            <label className="text-xs">
              {tr("Nama")}
              <Input
                className="mt-1"
                value={draft.name}
                placeholder={`@ / mail / mail.${zoneName}`}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
              <span className="mt-1 block break-all text-muted-foreground">
                {tr("Nama lengkap")}: {recordName(draft.name, zoneName) || "—"}
              </span>
            </label>
            {(!recordFields[draft.type] || (edit && !edit.data && edit.content)) && (
              <label className="text-xs">
                {draft.type === "A"
                  ? "IPv4"
                  : draft.type === "AAAA"
                    ? "IPv6"
                    : draft.type === "MX"
                      ? tr("Server email")
                      : "Content"}
                <Input
                  className="mt-1 font-mono"
                  placeholder={
                    draft.type === "A"
                      ? "192.0.2.1"
                      : draft.type === "AAAA"
                        ? "2001:db8::1"
                        : draft.type === "CNAME"
                          ? "target.example.com"
                          : ""
                  }
                  value={draft.content || ""}
                  onChange={(e) => setDraft({ ...draft, content: e.target.value })}
                />
              </label>
            )}
            <label className="text-xs">
              TTL (1 = auto)
              <Input
                className="mt-1"
                type="number"
                min={1}
                value={draft.ttl ?? 1}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    ttl: e.target.value === "" ? undefined : Number(e.target.value),
                  })
                }
              />
            </label>
            {priorityTypes.has(draft.type) && (
              <label className="text-xs">
                Priority
                <Input
                  className="mt-1"
                  type="number"
                  min={0}
                  max={65535}
                  value={draft.priority ?? ""}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      priority: e.target.value === "" ? undefined : Number(e.target.value),
                    })
                  }
                />
              </label>
            )}
            {!(edit && !edit.data && edit.content) &&
              recordFields[draft.type]?.map((field) => (
                <div key={field.key}>
                  <label className="mb-1 block text-xs">
                    {field.label}
                    {field.required ? " *" : ""}
                  </label>
                  {field.options ? (
                    <Select
                      value={dataFields[field.key] || ""}
                      onChange={(val) => setDataFields({ ...dataFields, [field.key]: val })}
                      options={["", ...field.options]}
                      placeholder="—"
                    />
                  ) : (
                    <Input
                      type={field.number ? "number" : "text"}
                      min={field.number && field.key !== "altitude" ? 0 : undefined}
                      step={field.key === "altitude" || field.key.endsWith("seconds") ? "any" : undefined}
                      value={dataFields[field.key] ?? ""}
                      onChange={(e) => setDataFields({ ...dataFields, [field.key]: e.target.value })}
                    />
                  )}
                </div>
              ))}
            <label className="text-xs">
              {tr("Catatan (opsional)")}
              <Input
                className="mt-1"
                placeholder={tr("Catatan untuk record ini")}
                value={draft.comment || ""}
                onChange={(e) => setDraft({ ...draft, comment: e.target.value })}
              />
            </label>
            <label className="text-xs">
              {tr("Tags (opsional)")}
              <Input
                className="mt-1"
                placeholder={tr("pisahkan dengan koma: tag1, tag2")}
                value={draftTags}
                onChange={(e) => setDraftTags(e.target.value)}
              />
            </label>
          </div>
          {proxiedTypes.has(draft.type) && edit?.proxiable !== false && (
            <label className="flex items-center gap-2 text-xs cursor-pointer">
              <input
                type="checkbox"
                className="rounded border-border"
                checked={draft.proxied ?? false}
                onChange={(e) => setDraft({ ...draft, proxied: e.target.checked })}
              />
              <span>{tr("Cloudflare proxy (orange cloud)")}</span>
            </label>
          )}
          <div className="flex gap-2 pt-1">
            <Button size="sm" disabled={busy} onClick={() => void save()}>
              {tr("Simpan")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setDraft(blank as RecordDNS)
                setDraftTags("")
                setEdit(null)
                setEditing(false)
              }}
            >
              {tr("Batal")}
            </Button>
          </div>
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border bg-secondary/30 text-muted-foreground">
              <th className="p-2.5 font-medium">{tr("Name")}</th>
              <th className="p-2.5 font-medium">{tr("Type")}</th>
              <th className="p-2.5 font-medium">{tr("Content")}</th>
              <th className="p-2.5 font-medium">{tr("Proxy Status")}</th>
              <th className="p-2.5 font-medium">{tr("TTL")}</th>
              <th className="p-2.5 font-medium">{tr("Tags")}</th>
              <th className="p-2.5 font-medium">{tr("Comments")}</th>
              <th className="p-2.5 text-right font-medium">{tr("Details")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {!result?.records || result.records.length === 0 ? (
              <tr>
                <td colSpan={8} data-label="" className="p-6 text-center text-muted-foreground">
                  {busy ? tr("Memuat record DNS…") : tr("Tidak ada record DNS pada zone ini.")}
                </td>
              </tr>
            ) : (
              result.records.map((r) => (
                <tr key={r.id} className="hover:bg-secondary/40 transition-colors">
                  <td data-label={tr("Name")} className="p-2.5 font-medium text-foreground break-all">
                    {r.name}
                  </td>
                  <td data-label={tr("Type")} className="p-2.5">
                    <Badge tone="muted" className="font-mono text-[11px] font-semibold tracking-wider">
                      {r.type}
                    </Badge>
                  </td>
                  <td data-label={tr("Content")} className="p-2.5 font-mono text-xs text-muted-foreground break-all max-w-xs">
                    {formatContent(r)}
                  </td>
                  <td data-label={tr("Proxy Status")} className="p-2.5">
                    {r.proxied ? (
                      <Badge tone="warn" className="gap-1 border-orange-500/40 bg-orange-500/10 text-orange-400 font-normal">
                        <Cloud className="size-3" />
                        {tr("Proxied")}
                      </Badge>
                    ) : proxiedTypes.has(r.type) || r.proxiable ? (
                      <Badge tone="muted" className="gap-1 font-normal text-muted-foreground">
                        <CloudOff className="size-3" />
                        {tr("DNS only")}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground text-xs">{tr("DNS only")}</span>
                    )}
                  </td>
                  <td data-label={tr("TTL")} className="p-2.5 num text-xs text-muted-foreground">
                    {formatTTL(r.ttl, tr)}
                  </td>
                  <td data-label={tr("Tags")} className="p-2.5">
                    {r.tags && r.tags.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {r.tags.map((t) => (
                          <span
                            key={t}
                            className="inline-block rounded border border-border bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground"
                          >
                            {t}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td data-label={tr("Comments")} className="p-2.5 text-xs text-muted-foreground max-w-xs">
                    {r.comment ? (
                      <span className="line-clamp-2" title={r.comment}>
                        {r.comment}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td data-label={tr("Details")} className="p-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
                        onClick={() => setDetailModal(r)}
                        title={tr("Lihat detail")}
                      >
                        <Eye className="mr-1 size-3" />
                        {tr("Detail")}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 px-2 text-xs"
                        onClick={() => start(r)}
                        aria-label={`${tr("Edit record")} ${r.name}`}
                      >
                        <Pencil className="mr-1 size-3" />
                        {tr("Edit")}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 px-2 text-xs text-crit hover:bg-crit/10"
                        onClick={() => void remove(r)}
                        aria-label={`${tr("Hapus record")} ${r.name}`}
                      >
                        <Trash2 className="mr-1 size-3" />
                        {tr("Hapus")}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {detailModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-lg overflow-y-auto rounded-lg border border-border bg-surface p-5 shadow-2xl">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <Badge tone="muted" className="font-mono font-semibold">
                  {detailModal.type}
                </Badge>
                <h3 className="font-semibold text-sm break-all">{detailModal.name}</h3>
              </div>
              <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => setDetailModal(null)}>
                <X className="size-4" />
              </Button>
            </div>

            <div className="mt-4 space-y-3 text-xs">
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">ID Record</span>
                <span className="col-span-2 font-mono text-muted-foreground select-all break-all">{detailModal.id}</span>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Type")}</span>
                <span className="col-span-2 font-medium">{detailModal.type}</span>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Name")}</span>
                <span className="col-span-2 font-medium break-all">{detailModal.name}</span>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Content")}</span>
                <span className="col-span-2 font-mono text-foreground break-all">{formatContent(detailModal)}</span>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Proxy Status")}</span>
                <span className="col-span-2">
                  {detailModal.proxied ? (
                    <Badge tone="warn" className="gap-1 border-orange-500/40 bg-orange-500/10 text-orange-400 font-normal">
                      <Cloud className="size-3" />
                      {tr("Proxied")}
                    </Badge>
                  ) : proxiedTypes.has(detailModal.type) || detailModal.proxiable ? (
                    <Badge tone="muted" className="gap-1 font-normal text-muted-foreground">
                      <CloudOff className="size-3" />
                      {tr("DNS only")}
                    </Badge>
                  ) : (
                    <span className="text-muted-foreground">{tr("DNS only")}</span>
                  )}
                </span>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("TTL")}</span>
                <span className="col-span-2 font-mono">{formatTTL(detailModal.ttl, tr)}</span>
              </div>
              {detailModal.priority !== undefined && (
                <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                  <span className="text-muted-foreground">Priority</span>
                  <span className="col-span-2 font-mono">{detailModal.priority}</span>
                </div>
              )}
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Tags")}</span>
                <div className="col-span-2">
                  {detailModal.tags && detailModal.tags.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {detailModal.tags.map((t) => (
                        <span
                          key={t}
                          className="inline-block rounded border border-border bg-secondary/60 px-1.5 py-0.5 text-[10px] text-muted-foreground"
                        >
                          {t}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </div>
              </div>
              <div className="grid grid-cols-3 gap-2 border-b border-border/60 pb-2">
                <span className="text-muted-foreground">{tr("Comments")}</span>
                <span className="col-span-2 text-muted-foreground">{detailModal.comment || "—"}</span>
              </div>

              {!!detailModal.data && typeof detailModal.data === "object" && (
                <div className="space-y-1 pt-1">
                  <span className="text-muted-foreground font-medium">Data (Structured):</span>
                  <pre className="max-h-32 overflow-auto rounded bg-secondary/50 p-2 font-mono text-[11px] text-muted-foreground">
                    {JSON.stringify(detailModal.data, null, 2)}
                  </pre>
                </div>
              )}
            </div>

            <div className="mt-5 flex items-center justify-end gap-2 border-t border-border pt-3">
              <Button size="sm" variant="outline" onClick={() => setDetailModal(null)}>
                {tr("Tutup")}
              </Button>
              <Button
                size="sm"
                onClick={() => {
                  const target = detailModal
                  setDetailModal(null)
                  start(target)
                }}
              >
                <Pencil className="mr-1 size-3.5" />
                {tr("Edit record ini")}
              </Button>
            </div>
          </div>
        </div>
      )}

      {result && (
        <div className="flex items-center gap-2 text-xs">
          <span>
            {result.total_count} record · {result.page}/{result.total_pages || 1}
          </span>
          <Button size="sm" variant="outline" disabled={busy || page <= 1} onClick={() => setPage(page - 1)}>
            {tr("Sebelumnya")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={busy || page >= result.total_pages}
            onClick={() => setPage(page + 1)}
          >
            {tr("Berikutnya")}
          </Button>
        </div>
      )}
    </div>
  )
}
