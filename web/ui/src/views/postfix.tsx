import { useCallback, useEffect, useState } from "react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"

type Route = { domain: string; host: string; port: number }
type Config = { routes: Route[]; values: Record<string, string>; revision: string; transport_exists: boolean }
const defaults: Record<string, string> = {
  myhostname: "mail.oxidilily.com", mydestination: "localhost", inet_interfaces: "all", inet_protocols: "ipv4",
  relay_domains: "oxidilily.com", transport_maps: "hash:/etc/postfix/transport", mynetworks: "127.0.0.0/8, [::1]/128, 100.80.10.2/32",
  smtpd_relay_restrictions: "permit_mynetworks, reject_unauth_destination",
}
export function PostfixTab() {
  const tr = useTr()
  const [config, setConfig] = useState<Config | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => {
    setBusy(true)
    try { setConfig(await apiGet<Config>("/api/proxy/postfix")) } catch (e) { notify.err(pesanError(e)) } finally { setBusy(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  const save = async () => {
    if (!config || !await confirmDialog({ title: tr("Simpan dan restart Postfix?"), message: tr("Transport map akan diganti dan diindeks, Postfix diperiksa lalu direstart. SMTP terputus sementara; pastikan port 25 tersedia."), confirmLabel: tr("Simpan dan restart") })) return
    setBusy(true)
    try {
      const values = { ...config.values, transport_maps: defaults.transport_maps, smtpd_relay_restrictions: defaults.smtpd_relay_restrictions }
      setConfig(await apiSend<Config>("/api/proxy/postfix", "PUT", { values, routes: config.routes, revision: config.revision, restart: true }))
      notify.ok(tr("Konfigurasi Postfix tersimpan; restart berhasil."))
    } catch (e) { notify.err(pesanError(e)) } finally { setBusy(false) }
  }
  return <Panel title="Proxy Mail" hint={tr("Proxy Mail memakai Postfix sebagai SMTP relay untuk meneruskan email ke server tujuan berdasarkan domain dan transport map.")}>
    <p className="mb-3"><Badge tone="warn" className="whitespace-normal">{tr("Port SMTP 25 tidak boleh dipakai layanan lain. Batasi mynetworks ke jaringan tepercaya karena dapat mengirim email tanpa autentikasi.")}</Badge></p>
    {config && !config.transport_exists && <p className="mb-3"><Badge tone="warn" className="whitespace-normal">{tr("Transport map belum lengkap. Isi domain dan endpoint SMTP tujuan lalu simpan.")}</Badge></p>}
    <p className="mb-3 text-xs text-muted-foreground">{tr("Endpoint SMTP tujuan harus reachable dari Postfix. Tailscale: host/IP tujuan. Cloudflare TCP: endpoint konektor lokal atau private route yang sudah tersedia, bukan hostname HTTP publik. mynetworks adalah klien tepercaya, bukan tujuan forwarding.")}</p>
    {config && <div className="mb-4 space-y-3">
      {(config.routes ?? []).map((route, index) => <div key={index} className="grid gap-2 md:grid-cols-4">
        {(["domain", "host", "port"] as const).map((field) => <label key={field} className="text-xs">{tr(field === "domain" ? "Domain email" : field === "host" ? "Host/IP tujuan" : "Port SMTP")}<Input className="mt-1" type={field === "port" ? "number" : "text"} min={field === "port" ? 1 : undefined} max={field === "port" ? 65535 : undefined} value={route[field]} disabled={busy} onChange={(e) => setConfig({ ...config, routes: config.routes.map((r, i) => i === index ? { ...r, [field]: field === "port" ? Number(e.target.value) : e.target.value } : r) })} /></label>)}
        <Button variant="outline" disabled={busy} onClick={() => setConfig({ ...config, routes: config.routes.filter((_, i) => i !== index) })}>{tr("Hapus")}</Button>
      </div>)}
      <Button variant="outline" disabled={busy || (config.routes?.length ?? 0) >= 100} onClick={() => setConfig({ ...config, routes: [...(config.routes ?? []), { domain: "", host: "", port: 25 }] })}>{tr("Tambah tujuan SMTP")}</Button>
    </div>}
    {config && <div className="grid gap-3 md:grid-cols-2">{Object.keys(defaults).filter((key) => key !== "relay_domains").map((key) => <label key={key} className="block text-xs">{key}<Input className="mt-1" value={key === "transport_maps" || key === "smtpd_relay_restrictions" ? defaults[key] : config.values[key] ?? ""} readOnly={key === "transport_maps" || key === "smtpd_relay_restrictions"} disabled={busy} onChange={(e) => setConfig({ ...config, values: { ...config.values, [key]: e.target.value } })} /></label>)}</div>}
    <div className="mt-4 flex flex-wrap gap-2">
      <Button variant="outline" disabled={busy} onClick={() => void load()}>{tr("Muat ulang")}</Button>
      <Button variant="outline" disabled={busy || !config} onClick={() => config && setConfig({ ...config, values: { ...defaults } })}>{tr("Isi nilai yang disetujui")}</Button>
      <Button disabled={busy || !config || !config.routes?.length} onClick={() => void save()}>{tr("Simpan dan restart")}</Button>
    </div>
  </Panel>
}
