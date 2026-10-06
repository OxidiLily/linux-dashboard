import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { Panel } from "@/components/ui/panel"
import { Button } from "@/components/ui/button"
import { muatDetailIP, type DetailIP } from "@/lib/fail2ban-detail"
import { aksiFail2ban, peringatanFail2ban } from "@/lib/fail2ban-detail"
import { GEOIP_HINT } from "@/lib/fail2ban-geoip"
import { formatWaktu, zonaTampilan } from "@/lib/format"
import { pesanError } from "@/lib/pesan-error"
import { usePrefs } from "@/stores/prefs"
import { useTr, trf } from "@/stores/i18n"

export function HistoryTable({ data }: { data: DetailIP }) {
  const tr = useTr()
  usePrefs(s => s.timezone)
  const labels = [tr("Tanggal dan waktu"), tr("Sumber"), tr("Aksi"), tr("Pesan")]
  return <>
    {data.warnings.map((warning, i) => <p key={i} className="mb-2 break-words text-xs text-warn">{tr("Peringatan")}: {warning === "Location unavailable: no local Geo-IP facility configured; IP was not shared externally." ? tr(GEOIP_HINT) : peringatanFail2ban(warning)}</p>)}
    <div className="overflow-x-auto rounded-lg border border-border"><table className="tabel-kartu w-full text-left text-xs">
      <thead><tr className="border-b border-border bg-secondary/30 text-muted-foreground">{labels.map(label => <th key={label} scope="col" className="p-2.5 font-medium">{label}</th>)}</tr></thead>
      <tbody>{data.events.map((event, i) => <tr key={i} className="transition-colors hover:bg-secondary/40">
        <td data-label={labels[0]} className="num p-2.5">{formatWaktu(event.time) === "—" ? event.time || tr("Waktu tidak tersedia") : formatWaktu(event.time)}</td>
        <td data-label={labels[1]} className="p-2.5 break-all">{event.source || "—"}</td>
        <td data-label={labels[2]} className="p-2.5">{aksiFail2ban(event.action)}</td>
        <td data-label={labels[3]} className="p-2.5 whitespace-pre-wrap break-words">{event.message}</td>
      </tr>)}{data.events.length === 0 && <tr><td colSpan={4} data-label="" className="p-6 text-center text-muted-foreground">{tr("Tidak ada riwayat percobaan dalam log yang tersedia.")}</td></tr>}</tbody>
    </table></div>
  </>
}

export function Fail2banHistoryView() {
  const tr = useTr()
  const [params] = useSearchParams()
  const jail = params.get("jail") || "", ip = params.get("ip") || ""
  const timezone = usePrefs(s => s.timezone)
  const [result, setResult] = useState<{ jail: string; ip: string; data?: DetailIP; error?: string }>()
  useEffect(() => {
    const controller = new AbortController()
    setResult(undefined)
    if (!jail || !ip) setResult({ jail, ip, error: "Jail dan IP wajib diisi." })
    else void muatDetailIP(jail, ip, controller.signal).then(data => {
      if (!controller.signal.aborted) setResult({ jail, ip, data })
    }).catch(e => { if (!controller.signal.aborted) setResult({ jail, ip, error: pesanError(e) }) })
    return () => controller.abort()
  }, [jail, ip])
  const current = result?.jail === jail && result.ip === ip ? result : undefined
  return <Panel title={tr("Riwayat Fail2ban")} hint={`${jail} · ${ip}`} actions={<Button asChild variant="outline" size="sm"><Link to="/settings/fail2ban">{tr("Kembali ke Fail2ban")}</Link></Button>}>
    <p className="mb-3 text-xs text-muted-foreground">{tr("Zona waktu")}: {timezone || zonaTampilan() || Intl.DateTimeFormat().resolvedOptions().timeZone}</p>
    <div aria-live="polite" aria-busy={!current}>
      {!current && <p role="status" className="text-xs">{tr("Memuat riwayat percobaan…")}</p>}
      {current?.error && <p role="alert" className="text-xs text-crit">{trf("Gagal memuat detail IP: {0}", tr(current.error))}</p>}
      {current?.data && <HistoryTable data={current.data} />}
    </div>
  </Panel>
}
