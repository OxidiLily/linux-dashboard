import { useCallback, useEffect, useState } from "react"
import { CheckCircle2, Pencil, Plus, RefreshCw, Route, ShieldCheck, Trash2, ExternalLink } from "lucide-react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { confirmDialog } from "@/components/ui/confirm"
import { notify } from "@/components/ui/toast"
import { useTr } from "@/stores/i18n"
import { CloudflareManager } from "./cloudflare-manager"

type ProxyHost = {
  id?: string
  domain: string
  target_host: string
  target_port: number
  scheme: "http" | "https"
  enabled: boolean
  managed?: boolean
  tls_mode?: "pending" | "certbot" | ""
  /** Issuer + kedaluwarsa dari fullchain.pem, diisi helper. */
  cert_issuer?: string
  cert_not_after?: string
}

type ProxyStatus = {
  installed: boolean
  running: boolean
  config_ok: boolean
  message?: string
}

const kosong: ProxyHost = { domain: "", target_host: "127.0.0.1", target_port: 3000, scheme: "http", enabled: true }

export function tautanProxy(h: Pick<ProxyHost, "domain" | "enabled" | "scheme" | "tls_mode">): string {
  const domain = h.domain.trim()
  if (!h.enabled || !/^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/i.test(domain) || domain.includes("..")) return ""
  return `${h.tls_mode === "certbot" ? "https" : "http"}://${domain}/`
}

type Tab = "proxy" | "ssl" | "dns"

export function ProxyManagerView() {
  const tr = useTr()
  const [tab, setTab] = useState<Tab>("proxy")
  const [hosts, setHosts] = useState<ProxyHost[]>([])
  const [status, setStatus] = useState<ProxyStatus | null>(null)
  const [form, setForm] = useState<ProxyHost>(kosong)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [certHost, setCertHost] = useState<ProxyHost | null>(null)
  const [certEmail, setCertEmail] = useState("")
  const [certStaging, setCertStaging] = useState(false)
  const [certCloudflareDNS, setCertCloudflareDNS] = useState(false)
  const [issuing, setIssuing] = useState(false)
  // Token hanya ada di input sementara; setelah disimpan respons berisi status saja.
  const [cfToken, setCfToken] = useState("")
  const [cfTokenSaved, setCfTokenSaved] = useState(false)
  const [cfTokenBusy, setCfTokenBusy] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [list, st] = await Promise.all([
        apiGet<ProxyHost[]>("/api/proxy/hosts"),
        apiGet<ProxyStatus>("/api/proxy/status"),
      ])
      setHosts(list || [])
      setStatus(st)
    } catch (e) {
      notify.err(`${tr("Gagal memuat Proxy manager")}: ${pesanError(e)}`)
    } finally {
      setLoading(false)
    }
  }, [tr])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    void apiGet<{ saved: boolean }>("/api/proxy/cloudflare/token")
      .then((result) => setCfTokenSaved(result.saved))
      .catch((e) => notify.err(`${tr("Gagal membaca status token Cloudflare")}: ${pesanError(e)}`))
  }, [tr])

  const simpanTokenCloudflare = async () => {
    if (!cfToken.trim()) return
    setCfTokenBusy(true)
    try {
      const status = await apiSend<{ saved: boolean }>("/api/proxy/cloudflare/token", "PUT", { token: cfToken.trim() })
      setCfTokenSaved(status.saved)
      setCfToken("")
      notify.ok(tr("Token Cloudflare tersimpan."))
    } catch (e) {
      notify.err(`${tr("Gagal menyimpan token Cloudflare")}: ${pesanError(e)}`)
    } finally {
      setCfTokenBusy(false)
    }
  }

  const hapusTokenCloudflare = async () => {
    if (!await confirmDialog({ title: tr("Hapus token DNS Cloudflare?"), message: tr("Akses DNS dengan token tersimpan akan berhenti; record DNS dan Cloudflare Tunnel tidak dihapus."), confirmLabel: tr("Hapus") })) return
    setCfTokenBusy(true)
    try {
      const status = await apiSend<{ saved: boolean }>("/api/proxy/cloudflare/token", "DELETE")
      setCfTokenSaved(status.saved)
      setCfToken("")
      notify.ok(tr("Token DNS Cloudflare dihapus."))
    } catch (e) {
      notify.err(`${tr("Gagal menghapus token Cloudflare")}: ${pesanError(e)}`)
    } finally {
      setCfTokenBusy(false)
    }
  }

  const save = async () => {
    if (!form.domain.trim() || !form.target_host.trim() || form.target_port < 1 || form.target_port > 65535 || !["http", "https"].includes(form.scheme.toLowerCase())) {
      notify.err(tr("Domain, target, port, dan scheme (http/https) valid wajib diisi."))
      return
    }
    setSaving(true)
    try {
      const path = form.id ? `/api/proxy/hosts/${form.id}` : "/api/proxy/hosts"
      await apiSend<ProxyHost>(path, form.id ? "PUT" : "POST", { ...form, scheme: form.scheme.toLowerCase().trim() })
      notify.ok(tr("Proxy host tersimpan dan nginx dimuat ulang."))
      setForm(kosong)
      await load()
    } catch (e) {
      notify.err(`${tr("Gagal menyimpan proxy host")}: ${pesanError(e, true)}`)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (h: ProxyHost) => {
    if (!h.id) return
    if (!await confirmDialog({ title: tr("Hapus proxy host?"), message: h.domain, confirmLabel: tr("Hapus") })) return
    try {
      await apiSend(`/api/proxy/hosts/${h.id}`, "DELETE")
      notify.ok(tr("Proxy host dihapus."))
      if (form.id === h.id) setForm(kosong)
      await load()
    } catch (e) {
      notify.err(`${tr("Gagal menghapus proxy host")}: ${pesanError(e)}`)
    }
  }

  const testConfig = async () => {
    try {
      await apiSend("/api/proxy/test", "POST")
      notify.ok(tr("Konfigurasi nginx valid."))
      await load()
    } catch (e) {
      notify.err(`${tr("Konfigurasi nginx tidak valid")}: ${pesanError(e)}`)
    }
  }

  const issueCert = async () => {
    if (!certHost?.id || !certEmail.includes("@")) {
      notify.err(tr("Email ACME valid wajib diisi."))
      return
    }
    setIssuing(true)
    try {
      await apiSend(`/api/proxy/hosts/${certHost.id}/cert`, "POST", { email: certEmail, staging: certStaging, cloudflare_dns: certCloudflareDNS })
      notify.ok(tr("Sertifikat diterbitkan dan HTTPS diaktifkan."))
      setCertHost(null)
      await load()
    } catch (e) {
      notify.err(`${tr("Gagal menerbitkan sertifikat")}: ${pesanError(e)}`)
    } finally {
      setIssuing(false)
    }
  }

  const bukaTLS = (h: ProxyHost) => {
    setCertHost(h)
    // Challenge HTTP-01 sudah terbukti jalan: default production supaya browser
    // tidak memperingati. Staging tetap tersedia sebagai opsi pengujian.
    setCertStaging(false)
    setCertCloudflareDNS(cfTokenSaved)
  }

  // Matikan TLS = kembali murni HTTP (config 443 ditarik oleh helper).
  const matikanTLS = async (h: ProxyHost) => {
    if (!h.id) return
    if (!await confirmDialog({ title: tr("Matikan TLS untuk host ini?"), message: `${h.domain} — ${tr("Peringatan: akses berikutnya memakai HTTP; password, OTP, dan sesi bisa disadap. Jangan matikan TLS pada domain untuk login panel.")}`, confirmLabel: tr("Matikan TLS") })) return
    try {
      await apiSend<ProxyHost>(`/api/proxy/hosts/${h.id}/disable-tls`, "POST")
      notify.ok(tr("TLS dimatikan; host kembali HTTP."))
      await load()
    } catch (e) {
      notify.err(`${tr("Gagal mematikan TLS")}: ${pesanError(e)}`)
    }
  }

  const tabs: { id: Tab; label: string }[] = [
    { id: "proxy", label: tr("Proxy Manager") },
    { id: "ssl", label: tr("SSL/TLS") },
    { id: "dns", label: tr("DNS Cloudflare") },
  ]

  return (
    <Panel
      title={tr("Proxy manager")}
      hint={tr("Kelola nama domain dan teruskan trafiknya ke alamat IP serta port aplikasi.")}
      actions={<div className="flex gap-2"><Button variant="outline" size="sm" onClick={testConfig}><CheckCircle2 className="mr-1 size-3.5" />{tr("Uji config")}</Button><Button variant="outline" size="sm" onClick={() => load()} disabled={loading}><RefreshCw className="mr-1 size-3.5" />{tr("Muat ulang")}</Button></div>}
    >
      <p className="mb-3 text-xs text-crit">{tr("HTTP langsung di port 1122 tidak mengenkripsi password, OTP, maupun sesi. Gunakan HTTPS sebelum membuka akses publik.")}</p>
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={status?.running ? "signal" : "crit"}>{status?.running ? tr("Nginx aktif") : tr("Nginx nonaktif")}</Badge>
        <Badge tone={status?.config_ok ? "signal" : "warn"}>{status?.config_ok ? tr("Config valid") : tr("Config bermasalah")}</Badge>
        {status?.message && <span className="text-xs text-crit">{status.message}</span>}
      </div>

      <div className="mb-4 flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            aria-selected={tab === t.id}
            className={`-mb-px border-b-2 px-3 py-1.5 text-sm transition-colors ${tab === t.id ? "border-signal font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"}`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "proxy" && (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="overflow-x-auto rounded-lg border border-border self-start">
            <table className="tabel-kartu w-full text-left text-xs">
              <thead>
                <tr className="border-b border-border bg-secondary/30 text-muted-foreground">
                  <th className="p-2.5 font-medium">{tr("Domain")}</th>
                  <th className="p-2.5 font-medium">{tr("Target Upstream")}</th>
                  <th className="p-2.5 font-medium">{tr("Status")}</th>
                  <th className="p-2.5 font-medium">{tr("TLS")}</th>
                  <th className="p-2.5 text-right font-medium">{tr("Aksi")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {!loading && hosts.length === 0 ? (
                  <tr>
                    <td colSpan={5} data-label="" className="p-6 text-center text-muted-foreground">
                      <Route className="mx-auto mb-2 size-5 text-muted-foreground" />
                      {tr("Belum ada proxy host")}
                    </td>
                  </tr>
                ) : (
                  hosts.map((h) => (
                    <tr key={h.id} className="hover:bg-secondary/40 transition-colors">
                      <td data-label={tr("Domain")} className="p-2.5 font-medium text-foreground break-all">
                        <div className="flex items-center gap-1.5 flex-wrap">
                          <span>{h.domain || tr("Panel bawaan — domain belum diatur")}</span>
                          {h.managed && <Badge tone="muted">{tr("Panel bawaan")}</Badge>}
                        </div>
                      </td>
                      <td data-label={tr("Target Upstream")} className="p-2.5 num font-mono text-xs text-muted-foreground">
                        {h.scheme}://{h.target_host}:{h.target_port}
                      </td>
                      <td data-label={tr("Status")} className="p-2.5">
                        <Badge tone={h.enabled ? "signal" : "muted"}>
                          {h.enabled ? tr("Aktif") : tr("Nonaktif")}
                        </Badge>
                      </td>
                      <td data-label={tr("TLS")} className="p-2.5">
                        <Badge tone={h.tls_mode === "certbot" ? "ok" : "muted"}>
                          {h.tls_mode === "certbot" ? "HTTPS" : "HTTP"}
                        </Badge>
                      </td>
                      <td data-label={tr("Aksi")} className="p-2.5 text-right">
                        <div className="flex items-center justify-end gap-1">
                          {tautanProxy(h) && (
                            <Button asChild variant="outline" size="sm" className="h-7 px-2 text-xs">
                              <a
                                href={tautanProxy(h)}
                                target="_blank"
                                rel="noopener noreferrer"
                                title={`${tr("Buka")} ${h.domain} ${tr("di tab baru")}`}
                              >
                                <ExternalLink className="mr-1 size-3" />
                                {tr("Buka")}
                              </a>
                            </Button>
                          )}
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 px-2 text-xs"
                            onClick={() => setForm(h)}
                            aria-label={`${tr("Edit proxy host")}: ${h.domain || tr("Panel bawaan")}`}
                            title={tr("Edit proxy host")}
                          >
                            <Pencil className="mr-1 size-3" />
                            {tr("Edit")}
                          </Button>
                          {!h.managed && (
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 px-2 text-xs text-crit hover:bg-crit/10"
                              onClick={() => remove(h)}
                              title={tr("Hapus proxy host")}
                            >
                              <Trash2 className="mr-1 size-3" />
                              {tr("Hapus")}
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          <div className="space-y-3 rounded-lg border border-border bg-surface-2/40 p-4">
            <h2 className="text-sm font-semibold">{form.id ? tr("Edit proxy host") : tr("Tambah proxy host")}</h2>
            <label className="block text-xs text-muted-foreground">{tr("Nama domain atau IPv4")}<Input className="mt-1" value={form.domain} placeholder="app.example.com / 192.168.2.11" onChange={(e) => setForm({ ...form, domain: e.target.value })} /></label>
            <label className="block text-xs text-muted-foreground">{tr("Target IP / hostname")}<Input className="mt-1" value={form.target_host} placeholder="127.0.0.1" onChange={(e) => setForm({ ...form, target_host: e.target.value })} /></label>
            <div className="grid grid-cols-2 gap-2">
              <label className="block text-xs text-muted-foreground">{tr("Scheme")}<Input className="mt-1" list="proxy-scheme-options" value={form.scheme} placeholder="http / https" onChange={(e) => setForm({ ...form, scheme: e.target.value as "http" | "https" })} /><datalist id="proxy-scheme-options"><option value="http" /><option value="https" /></datalist></label>
              <label className="block text-xs text-muted-foreground">{tr("Port")}<Input className="mt-1" type="number" min={1} max={65535} value={form.target_port} onChange={(e) => setForm({ ...form, target_port: Number(e.target.value) })} /></label>
            </div>
            <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />{tr("Aktifkan host")}</label>
            {form.managed && <p className="text-xs text-muted-foreground">{tr("Rule bawaan tidak dapat dihapus. Target, port, dan protokol upstream bisa diubah; HTTPS publik di tab SSL/TLS.")}</p>}
            <div className="flex gap-2"><Button size="sm" onClick={save} disabled={saving}><Plus className="mr-1 size-3.5" />{form.id ? tr("Simpan Perubahan") : tr("Tambah")}</Button>{form.id && <Button variant="outline" size="sm" onClick={() => setForm(kosong)}>{tr("Batal")}</Button>}</div>
          </div>
        </div>
      )}

      {tab === "ssl" && (
        <div className="space-y-4">
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
            <div className="overflow-x-auto rounded-lg border border-border self-start">
              <table className="tabel-kartu w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-border bg-secondary/30 text-muted-foreground">
                    <th className="p-2.5 font-medium">{tr("Domain")}</th>
                    <th className="p-2.5 font-medium">{tr("Status TLS")}</th>
                    <th className="p-2.5 font-medium">{tr("Sertifikat / Masa Berlaku")}</th>
                    <th className="p-2.5 text-right font-medium">{tr("Aksi")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {!loading && hosts.length === 0 ? (
                    <tr>
                      <td colSpan={4} data-label="" className="p-6 text-center text-muted-foreground">
                        <ShieldCheck className="mx-auto mb-2 size-5 text-muted-foreground" />
                        {tr("Belum ada proxy host — buat host dulu di tab Proxy Manager.")}
                      </td>
                    </tr>
                  ) : (
                    hosts.map((h) => (
                      <tr key={h.id} className="hover:bg-secondary/40 transition-colors">
                        <td data-label={tr("Domain")} className="p-2.5 font-medium text-foreground break-all">
                          <div className="flex items-center gap-1.5 flex-wrap">
                            <span>{h.domain || tr("Panel bawaan — domain belum diatur")}</span>
                            {h.managed && <Badge tone="muted">{tr("Panel bawaan")}</Badge>}
                          </div>
                        </td>
                        <td data-label={tr("Status TLS")} className="p-2.5">
                          <div className="flex items-center gap-1 flex-wrap">
                            <Badge tone={h.tls_mode === "certbot" ? "ok" : "muted"}>
                              {h.tls_mode === "certbot" ? "HTTPS" : "HTTP"}
                            </Badge>
                            {h.tls_mode === "certbot" && h.cert_issuer?.toUpperCase().includes("STAGING") && (
                              <Badge tone="warn">{tr("Sertifikat staging — tidak dipercaya browser")}</Badge>
                            )}
                          </div>
                        </td>
                        <td data-label={tr("Sertifikat / Masa Berlaku")} className="p-2.5 num text-xs text-muted-foreground">
                          {h.tls_mode === "certbot" ? (
                            <div>
                              <span className="font-medium text-foreground">{h.cert_issuer || "certbot"}</span>
                              {h.cert_not_after && (
                                <span className="block text-[11px] text-muted-foreground">
                                  {tr("Kedaluwarsa")} {h.cert_not_after.slice(0, 10)}
                                </span>
                              )}
                            </div>
                          ) : (
                            <span className="text-muted-foreground">{tr("Belum ada sertifikat — host dilayani HTTP.")}</span>
                          )}
                        </td>
                        <td data-label={tr("Aksi")} className="p-2.5 text-right">
                          <div className="flex items-center justify-end gap-1.5 flex-wrap">
                            {!h.domain && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-xs"
                                onClick={() => {
                                  setForm(h)
                                  setTab("proxy")
                                }}
                              >
                                {tr("Atur domain")}
                              </Button>
                            )}
                            {h.domain && !h.domain.endsWith(".local") && !/^\d+\.\d+\.\d+\.\d+$/.test(h.domain) && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-xs"
                                onClick={() => bukaTLS(h)}
                              >
                                {h.tls_mode === "certbot" ? tr("Terbitkan ulang") : tr("Aktifkan TLS")}
                              </Button>
                            )}
                            {/^\d+\.\d+\.\d+\.\d+$/.test(h.domain) && (
                              <span className="text-[11px] text-muted-foreground">
                                {tr("IP privat hanya HTTP; Certbot tidak menerbitkan sertifikat untuk IP ini.")}
                              </span>
                            )}
                            {h.domain.endsWith(".local") && (
                              <span className="text-[11px] text-muted-foreground">
                                {tr("Domain .local memerlukan sertifikat privat; Let's Encrypt tidak menerbitkannya.")}
                              </span>
                            )}
                            {h.managed && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-xs"
                                onClick={() => {
                                  setForm(h)
                                  setTab("proxy")
                                }}
                              >
                                {tr("Edit")}
                              </Button>
                            )}
                            {h.tls_mode === "certbot" && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-xs text-crit hover:bg-crit/10"
                                onClick={() => matikanTLS(h)}
                              >
                                {tr("Matikan TLS")}
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            <div className="space-y-3 rounded-lg border border-border bg-surface-2/40 p-4">
              <h2 className="text-sm font-semibold">{tr("HTTPS melalui nginx")}</h2>
              <p className="text-xs text-muted-foreground">{tr("Port panel 1122 tetap HTTP; sertifikat domain dikelola Certbot dan dipakai nginx pada port 443. Jangan ubah Scheme upstream panel menjadi https.")}</p>
            </div>
          </div>

          {certHost && (
            <div className="max-w-xl space-y-3 rounded-lg border border-border bg-surface-2/40 p-4">
              <h2 className="text-sm font-semibold">{tr("TLS/SSL untuk")} {certHost.domain}</h2>
              <p className="text-xs text-muted-foreground">{certCloudflareDNS ? tr("DNS-01 memakai token Cloudflare tersimpan. Domain harus berada dalam zone token; sertifikat hanya dipercaya browser jika production berhasil.") : tr("Certbot memakai HTTP-01. DNS domain harus mengarah ke server ini dan port 80 harus dapat diakses dari internet.")}</p>
              <label className="block text-xs text-muted-foreground">{tr("Email Let's Encrypt")}<Input className="mt-1" type="email" value={certEmail} placeholder="admin@example.com" onChange={(e) => setCertEmail(e.target.value)} /></label>
              <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={certCloudflareDNS} disabled={!cfTokenSaved} onChange={(e) => setCertCloudflareDNS(e.target.checked)} />{tr("Gunakan DNS-01 Cloudflare (token tersimpan)")}</label>
              <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={certStaging} onChange={(e) => setCertStaging(e.target.checked)} />{tr("Gunakan Let's Encrypt staging untuk pengujian")}</label>
              <div className="flex gap-2"><Button size="sm" onClick={issueCert} disabled={issuing}>{tr("Terbitkan sertifikat")}</Button><Button variant="outline" size="sm" onClick={() => setCertHost(null)}>{tr("Batal")}</Button></div>
            </div>
          )}
        </div>
      )}

      {tab === "dns" && (
        <div className="space-y-4">
          <div className="space-y-3 rounded-lg border border-border bg-surface-2/40 p-4">
            <h2 className="text-sm font-semibold">{tr("Cloudflare DNS")}</h2>
            <p className="text-xs text-muted-foreground">{tr("Token DNS Cloudflare memerlukan Zone Read dan DNS Write. Disimpan hanya di server; nilai tidak ditampilkan kembali. Token ini dipakai untuk tantangan DNS-01 saat penerbitan sertifikat di tab SSL/TLS.")}</p>
            <Badge tone={cfTokenSaved ? "ok" : "muted"}>{cfTokenSaved ? tr("Token tersimpan") : tr("Token belum disimpan")}</Badge>
            <label className="block text-xs text-muted-foreground">{tr("API token")}<Input className="mt-1" type="password" autoComplete="off" value={cfToken} placeholder={cfTokenSaved ? tr("Token tersimpan (tersembunyi)") : tr("Masukkan token API")} onChange={(e) => setCfToken(e.target.value)} /></label>
            <div className="flex gap-2">
              <Button size="sm" onClick={simpanTokenCloudflare} disabled={cfTokenBusy || !cfToken.trim()}>{tr("Simpan token")}</Button>
              {cfTokenSaved && <Button variant="outline" size="sm" className="text-crit" onClick={hapusTokenCloudflare} disabled={cfTokenBusy}>{tr("Hapus token")}</Button>}
            </div>

          </div>

          <CloudflareManager enabled={cfTokenSaved} />
        </div>
      )}
    </Panel>
  )
}
