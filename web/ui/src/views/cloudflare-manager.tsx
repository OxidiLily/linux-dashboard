import { useCallback, useEffect, useState } from "react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { confirmDialog } from "@/components/ui/confirm"
import { notify } from "@/components/ui/toast"
import { useTr } from "@/stores/i18n"
import { recordFields, recordPayload, recordTypes, priorityTypes, proxiedTypes } from "./cloudflare-record-fields"

type Zone = { id: string; name: string }
type RecordDNS = { id: string; type: string; name: string; content?: string; data?: unknown; ttl?: number; proxied?: boolean; proxiable?: boolean; priority?: number; comment?: string; tags?: string[]; settings?: unknown }
type Page = { records: RecordDNS[]; page: number; total_pages: number; total_count: number }
const blank = { type: "A", name: "", content: "", ttl: 1 }

export function CloudflareManager({ enabled }: { enabled: boolean }) {
  const tr = useTr()
  const [zones, setZones] = useState<Zone[]>([])
  const [zone, setZone] = useState("")
  const [page, setPage] = useState(1)
  const [result, setResult] = useState<Page | null>(null)
  const [busy, setBusy] = useState(false)
  const [edit, setEdit] = useState<RecordDNS | null>(null)
  const [draft, setDraft] = useState<RecordDNS>(blank as RecordDNS)
  const [editing, setEditing] = useState(false)
  const [dataFields, setDataFields] = useState<Record<string, string>>({})

  const refreshZones = useCallback(async () => {
    setBusy(true)
    try {
      const items = await apiGet<Zone[]>("/api/proxy/cloudflare/zones")
      setZones(items)
      setZone((current) => items.some((z) => z.id === current) ? current : items[0]?.id || "")
    } catch (e) { notify.err(`${tr("Gagal membaca zone Cloudflare")}: ${pesanError(e)}`) }
    finally { setBusy(false) }
  }, [tr])
  useEffect(() => { if (enabled) void refreshZones(); else { setZones([]); setZone(""); setResult(null) } }, [enabled, refreshZones])

  const refresh = useCallback(async () => {
    if (!zone) { setResult(null); return }
    setResult(null)
    setBusy(true)
    try { setResult(await apiSend<Page>("/api/proxy/cloudflare/records", "POST", { zone_id: zone, page })) }
    catch (e) { setResult(null); notify.err(`${tr("Gagal membaca DNS Cloudflare")}: ${pesanError(e)}`) }
    finally { setBusy(false) }
  }, [zone, page, tr])
  useEffect(() => { void refresh() }, [refresh])

  const start = (record?: RecordDNS) => {
    setEditing(true)
    setEdit(record || null)
    setDraft(record ? { ...record } : { ...blank, name: zones.find((z) => z.id === zone)?.name || "" } as RecordDNS)
    const data = record?.data && typeof record.data === "object" ? record.data as Record<string, unknown> : {}
    setDataFields(Object.fromEntries(Object.entries(data).map(([key, value]) => [key, String(value)])))
  }
  const save = async () => {
    if (!zone || !draft.name.trim() || !draft.type.trim()) { notify.err(tr("Jenis dan nama record wajib diisi.")); return }
    let record: Record<string, unknown>
    try { record = recordPayload(draft, dataFields, edit?.proxiable !== false) }
    catch (e) { notify.err(e instanceof Error ? e.message : tr("Record DNS tidak valid.")); return }
    setBusy(true)
    try {
      await apiSend("/api/proxy/cloudflare/records", "PUT", { zone_id: zone, record_id: edit?.id || "", record })
      setEdit(null); setEditing(false); setDraft(blank as RecordDNS); await refresh()
      notify.ok(tr("Record DNS tersimpan."))
    } catch (e) { notify.err(`${tr("Gagal menyimpan DNS Cloudflare")}: ${pesanError(e)}`) }
    finally { setBusy(false) }
  }
  const remove = async (record: RecordDNS) => {
    if (!await confirmDialog({ title: tr("Hapus record DNS Cloudflare?"), message: `${record.type} ${record.name} — ${record.content || JSON.stringify(record.data)}`, confirmLabel: tr("Hapus") })) return
    setBusy(true)
    try { await apiSend("/api/proxy/cloudflare/records/delete", "POST", { zone_id: zone, record_id: record.id }); await refresh(); notify.ok(tr("Record DNS dihapus.")) }
    catch (e) { notify.err(`${tr("Gagal menghapus record DNS")}: ${pesanError(e)}`) }
    finally { setBusy(false) }
  }

  if (!enabled) return <p className="text-sm text-muted-foreground">{tr("Simpan token Cloudflare untuk memuat zone dan semua jenis record DNS.")}</p>
  return <div className="space-y-4">
    <div className="flex flex-wrap items-end gap-2">
      <label className="text-xs text-muted-foreground">{tr("Zone Cloudflare")}<select className="mt-1 block rounded border border-border bg-surface-2 p-2" value={zone} onChange={(e) => { setZone(e.target.value); setPage(1); setEdit(null) }}>{zones.map((z) => <option key={z.id} value={z.id}>{z.name}</option>)}</select></label>
      <Button size="sm" variant="outline" onClick={() => void refreshZones()} disabled={busy}>{tr("Muat zone")}</Button>
      <Button size="sm" variant="outline" onClick={() => void refresh()} disabled={busy || !zone}>{tr("Muat ulang")}</Button>
      <Button size="sm" onClick={() => start()} disabled={!zone || busy}>{tr("Tambah record")}</Button>
    </div>
    {zones.length === 0 && !busy && <p className="text-sm text-muted-foreground">{tr("Tidak ada zone yang dapat diakses token ini.")}</p>}
    {editing && <div className="space-y-2 rounded-lg border border-border p-3">
      <h3 className="text-sm font-semibold">{edit ? tr("Edit record") : tr("Tambah record")}</h3>
      <div className="grid gap-2 sm:grid-cols-2">
        <label className="text-xs">{tr("Jenis DNS")}<select className="mt-1 block w-full rounded border border-border bg-surface-2 p-2" value={draft.type} disabled={!!edit} onChange={(e) => { setDraft({ ...draft, type: e.target.value, content: "", priority: undefined, proxied: undefined, data: undefined }); setDataFields({}) }}>
          {!recordTypes.includes(draft.type) && <option value={draft.type}>{draft.type}</option>}
          {recordTypes.map((type) => <option key={type} value={type}>{type}</option>)}
        </select></label>
        <label className="text-xs">{tr("Nama")}<Input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
        {(!recordFields[draft.type] || (edit && !edit.data && edit.content)) && <label className="text-xs">{draft.type === "A" ? "IPv4" : draft.type === "AAAA" ? "IPv6" : draft.type === "MX" ? tr("Server email") : "Content"}<Input value={draft.content || ""} onChange={(e) => setDraft({ ...draft, content: e.target.value })} /></label>}
        <label className="text-xs">TTL (1 = auto)<Input type="number" min={1} value={draft.ttl ?? 1} onChange={(e) => setDraft({ ...draft, ttl: e.target.value === "" ? undefined : Number(e.target.value) })} /></label>
        {priorityTypes.has(draft.type) && <label className="text-xs">Priority<Input type="number" min={0} max={65535} value={draft.priority ?? ""} onChange={(e) => setDraft({ ...draft, priority: e.target.value === "" ? undefined : Number(e.target.value) })} /></label>}
        {!(edit && !edit.data && edit.content) && recordFields[draft.type]?.map((field) => <label key={field.key} className="text-xs">{field.label}{field.required ? " *" : ""}{field.options ? <select className="mt-1 block w-full rounded border border-border bg-surface-2 p-2" value={dataFields[field.key] || ""} onChange={(e) => setDataFields({ ...dataFields, [field.key]: e.target.value })}><option value="">—</option>{field.options.map((value) => <option key={value} value={value}>{value}</option>)}</select> : <Input type={field.number ? "number" : "text"} min={field.number && field.key !== "altitude" ? 0 : undefined} step={field.key === "altitude" || field.key.endsWith("seconds") ? "any" : undefined} value={dataFields[field.key] ?? ""} onChange={(e) => setDataFields({ ...dataFields, [field.key]: e.target.value })} />}</label>)}
        <label className="text-xs">{tr("Catatan")}<Input value={draft.comment || ""} onChange={(e) => setDraft({ ...draft, comment: e.target.value })} /></label>
      </div>
      {proxiedTypes.has(draft.type) && edit?.proxiable !== false && <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.proxied ?? false} onChange={(e) => setDraft({ ...draft, proxied: e.target.checked })} />Cloudflare proxy</label>}
      <div className="flex gap-2"><Button size="sm" disabled={busy} onClick={() => void save()}>{tr("Simpan")}</Button><Button size="sm" variant="outline" onClick={() => { setDraft(blank as RecordDNS); setEdit(null); setEditing(false) }}>{tr("Batal")}</Button></div>
    </div>}
    <div className="space-y-2">{result?.records.map((r) => <div key={r.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border p-3"><div className="min-w-0 break-all text-sm"><strong>{r.type}</strong> {r.name}<div className="text-xs text-muted-foreground">{r.content || JSON.stringify(r.data)} · TTL {r.ttl} {r.proxied ? "· Proxied" : ""}</div></div><div className="flex gap-2"><Button size="sm" variant="outline" onClick={() => start(r)}>{tr("Edit")}</Button><Button size="sm" variant="outline" className="text-crit" onClick={() => void remove(r)}>{tr("Hapus")}</Button></div></div>)}</div>
    {result && <div className="flex items-center gap-2 text-xs"><span>{result.total_count} record · {result.page}/{result.total_pages || 1}</span><Button size="sm" variant="outline" disabled={busy || page <= 1} onClick={() => setPage(page - 1)}>{tr("Sebelumnya")}</Button><Button size="sm" variant="outline" disabled={busy || page >= result.total_pages} onClick={() => setPage(page + 1)}>{tr("Berikutnya")}</Button></div>}
  </div>
}
