import { useEffect, useState } from "react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { trf, useTr } from "@/stores/i18n"

export type CertificatesStatus = {
  cert_path: string
  key_path: string
  active: boolean
  valid: boolean
  subject?: string
  issuer?: string
  not_before?: string
  not_after?: string
  dns_names?: string[]
  error?: string
}

export function isianCertificatesValid(certPath: string, keyPath: string): boolean {
  const cert = certPath.trim()
  const key = keyPath.trim()
  if (!cert && !key) return true
  return cert.startsWith("/") && key.startsWith("/")
}

function formatTanggal(value?: string): string {
  if (!value) return "—"
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}

export function CertificatesView() {
  const tr = useTr()
  const [status, setStatus] = useState<CertificatesStatus | null>(null)
  const [certPath, setCertPath] = useState("")
  const [keyPath, setKeyPath] = useState("")
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<CertificatesStatus>("/api/settings/certificates")
      setStatus(data)
      setCertPath(data.cert_path || "")
      setKeyPath(data.key_path || "")
    } catch (e) {
      notify.err(trf("Gagal memuat certificates: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [])

  const simpan = async () => {
    if (!isianCertificatesValid(certPath, keyPath)) {
      notify.err(tr("Isi kedua path absolut, atau kosongkan keduanya untuk menonaktifkan TLS."))
      return
    }
    const mematikan = !certPath.trim() && !keyPath.trim()
    const ok = await confirmDialog({
      title: mematikan ? tr("Nonaktifkan TLS panel?") : tr("Terapkan sertifikat TLS?"),
      message: mematikan
        ? tr("Panel akan kembali memakai HTTP. Password dan cookie tidak terenkripsi bila port dapat dijangkau langsung dari jaringan.")
        : tr("Sertifikat dan private key divalidasi, setelan disimpan, lalu web app restart. Sambungan halaman ini akan terputus sesaat."),
      confirmLabel: mematikan ? tr("Nonaktifkan TLS") : tr("Terapkan"),
      danger: mematikan,
    })
    if (!ok) return
    setLoading(true)
    try {
      const data = await apiSend<CertificatesStatus>("/api/settings/certificates", "PUT", {
        cert_path: certPath.trim(),
        key_path: keyPath.trim(),
      })
      setStatus(data)
      notify.ok(mematikan
        ? tr("TLS dinonaktifkan. Web app sedang restart.")
        : tr("TLS diterapkan. Buka ulang panel memakai HTTPS setelah restart selesai."))
    } catch (e) {
      notify.err(trf("Gagal menyimpan certificates: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-4">
      <Panel
        title={tr("Certificates")}
        hint={tr("Kelola DASHBOARD_TLS_CERT dan DASHBOARD_TLS_KEY untuk HTTPS langsung pada panel.")}
        actions={<Button size="sm" onClick={simpan} disabled={loading || !isianCertificatesValid(certPath, keyPath)}>{tr("Simpan dan restart")}</Button>}
      >
        <div className="max-w-2xl space-y-4">
          <div className="flex items-center gap-2">
            <Badge tone={status?.active && status?.valid ? "ok" : status?.error ? "crit" : "muted"}>
              {status?.active && status?.valid ? tr("TLS aktif") : status?.error ? tr("Konfigurasi tidak valid") : tr("TLS nonaktif")}
            </Badge>
            {loading && <span className="text-xs text-muted-foreground">{tr("Memuat…")}</span>}
          </div>

          {status?.error && <p role="alert" className="rounded-md border border-crit/40 bg-crit/10 p-3 text-sm text-crit">{status.error}</p>}

          <div>
            <label htmlFor="tls-cert" className="text-xs font-medium text-muted-foreground">DASHBOARD_TLS_CERT</label>
            <Input id="tls-cert" className="mt-1 font-mono" value={certPath} onChange={(e) => setCertPath(e.target.value)} placeholder="/etc/ssl/certs/linux-dashboard.crt" spellCheck={false} />
          </div>
          <div>
            <label htmlFor="tls-key" className="text-xs font-medium text-muted-foreground">DASHBOARD_TLS_KEY</label>
            <Input id="tls-key" className="mt-1 font-mono" value={keyPath} onChange={(e) => setKeyPath(e.target.value)} placeholder="/etc/ssl/private/linux-dashboard.key" spellCheck={false} />
            <p className="mt-1 text-xs text-muted-foreground">{tr("Private key tidak ditampilkan; halaman hanya menyimpan path berkas. Berkas harus dapat dibaca user linux-dashboard.")}</p>
          </div>
        </div>
      </Panel>

      {status?.valid && (
        <Panel title={tr("Detail sertifikat")} hint={tr("Metadata certificate leaf yang sedang dikonfigurasi.")}>
          <dl className="grid max-w-2xl grid-cols-1 gap-3 text-sm sm:grid-cols-2">
            <div><dt className="text-xs text-muted-foreground">{tr("Subject")}</dt><dd className="mt-1 break-all">{status.subject || "—"}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{tr("Issuer")}</dt><dd className="mt-1 break-all">{status.issuer || "—"}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{tr("Berlaku sejak")}</dt><dd className="mt-1">{formatTanggal(status.not_before)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{tr("Kedaluwarsa")}</dt><dd className="mt-1">{formatTanggal(status.not_after)}</dd></div>
            <div className="sm:col-span-2"><dt className="text-xs text-muted-foreground">DNS SAN</dt><dd className="mt-1 break-all">{status.dns_names?.join(", ") || "—"}</dd></div>
          </dl>
        </Panel>
      )}
    </div>
  )
}
