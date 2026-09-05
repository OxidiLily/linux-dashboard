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
import { Network, ShieldCheck, RefreshCw, Power, Package, Trash2 } from "lucide-react"
import { useNavigate } from "react-router-dom"
import { WireGuardServer } from "@/components/ui/wireguard-server"

type Iface = {
  name: string
  mac: string
  ips: string[]
  mtu: number
  up: boolean
  flags: string
}

type VPNStatus = {
  name: string
  installed: boolean
  connected: boolean
  state?: string
  detail?: string
  /** Token tunnel yang sudah terpasang di unit systemd cloudflared. */
  token?: string
  /**
   * Tailscale: node sudah terdaftar di tailnet tapi admin belum menyetujuinya
   * (Device approval). Tidak ada langkah tersisa di server ini.
   */
  needs_approval?: boolean
}

// Field per-VPN yang dikirim ke backend VPNArgs:
//   tailscale  → { action: "up"|"down", auth_key, hostname? }
//   cloudflared→ { action: "up"|"down", token }
//   wireguard  → { action: "up"|"down", config }
// Backend helper VPNArgs di internal/helperproto/proto.go. Aksi HARUS up|down
// — frontend lama pakai "connect"/"disconnect" sehingga helper menolak dengan
// tr("aksi tidak dikenal").
type VPNConfigForm = {
  authKey: string
  hostname: string
  token: string
  config: string
}

export function NetworkView() {
  const tr = useTr()
  const user = useAuth((s) => s.user)
  const [ifaces, setIfaces] = useState<Iface[]>([])
  const [dnsInput, setDnsInput] = useState("")
  const [vpns, setVpns] = useState<VPNStatus[]>([])
  const [loading, setLoading] = useState(false)
  const [vpnModal, setVpnModal] = useState<string | null>(null)
  // Penanda muat-ulang: panel WireGuard punya endpoint sendiri, jadi ia perlu
  // diberi tahu kalau daftar VPN berubah (mis. config baru saja dihapus) —
  // tanpa ini isinya tetap menampilkan server yang sudah tidak ada.
  const [versiVPN, setVersiVPN] = useState(0)
  const navigate = useNavigate()
  // Token Cloudflare Tunnel diedit langsung di panel, bukan lewat modal:
  // nilainya perlu terlihat untuk tahu tunnel mana yang sedang terpasang.
  // Yang datang dari server sudah tersamar (eyJhIjoiZ...xxxxxxxxxxxxxxxx), jadi
  // nilai itu TIDAK boleh dikirim balik — hanya token yang user ketik sendiri.
  const [cfToken, setCfToken] = useState("")
  const [cfDiketik, setCfDiketik] = useState(false)
  // Auth key Tailscale diperlakukan sama: nilai dari server sudah tersamar,
  // jadi hanya kunci yang benar-benar diketik user yang boleh dikirim balik.
  const [tsKey, setTsKey] = useState("")
  const [tsDiketik, setTsDiketik] = useState(false)
  const [tsHost, setTsHost] = useState("")
  const [vpnForm, setVpnForm] = useState<VPNConfigForm>({
    authKey: "",
    hostname: "",
    token: "",
    config: "",
  })

  // paksaTokenServer dipakai setelah aksi sambung/putus: nilai cfDiketik yang
  // tertangkap closure ini masih nilai lama, jadi flag-nya tidak bisa dipercaya
  // untuk memutuskan apakah tampilan boleh ditimpa bentuk tersamar dari server.
  const load = async (paksaTokenServer = false) => {
    setLoading(true)
    try {
      const [ifData, dnsData] = await Promise.all([
        apiGet<Iface[]>("/api/settings/network/interfaces"),
        apiGet<{ nameservers: string[] }>("/api/settings/network/dns"),
      ])
      setIfaces(ifData || [])
      setDnsInput(dnsData?.nameservers?.join(", ") || "")

      if (user?.sudo) {
        const vpnData = await apiGet<VPNStatus[]>("/api/settings/network/vpn")
        setVpns(vpnData || [])
        const cf = (vpnData || []).find((v) => v.name === "cloudflared")
        if (cf?.token && (paksaTokenServer || !cfDiketik)) setCfToken(cf.token)
        const ts = (vpnData || []).find((v) => v.name === "tailscale")
        if (paksaTokenServer || !tsDiketik) setTsKey(ts?.token ?? "")
      }
    } catch (e: any) {
      notify.err(trf("Gagal memuat network: {0}", pesanError(e)))
    } finally {
      setLoading(false)
      setVersiVPN((v) => v + 1)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleSaveDNS = async (e: React.FormEvent) => {
    e.preventDefault()
    const ns = dnsInput.split(",").map((s) => s.trim()).filter(Boolean)
    const ok = await confirmDialog({
      title: tr("Ubah DNS nameserver sistem?"),
      message: tr(
        "Resolusi nama seluruh server memakai daftar ini. Salah isi bisa memutus akses internet server.",
      ),
      detail: ns.join(", "),
      confirmLabel: tr("Simpan"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend("/api/settings/network/dns", "PUT", { nameservers: ns }), {
        jalan: tr("Menyimpan DNS nameserver…"),
        sukses: tr("DNS nameserver berhasil diperbarui."),
        gagal: (e) => trf("Gagal memperbarui DNS: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const buildVPNBody = (name: string, action: "up" | "down"): Record<string, unknown> => {
    const body: Record<string, unknown> = { name, action }
    if (action === "up") {
      if (name === "tailscale" && tsDiketik && tsKey.trim()) body.auth_key = tsKey.trim()
      if (name === "tailscale" && tsHost.trim()) body.hostname = tsHost.trim()
      if (name === "cloudflared" && cfDiketik && cfToken.trim()) body.token = cfToken.trim()
      if (name === "wireguard" && vpnForm.config) body.config = vpnForm.config
    }
    return body
  }

  const hapusConfigWG = async () => {
    const ok = await confirmDialog({
      title: tr("Hapus config WireGuard?"),
      message:
        tr("Interface diturunkan dan /etc/wireguard/<iface>.conf dihapus. Private key di dalamnya ikut hilang — salinannya disimpan sebagai .bak di folder yang sama."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(
        apiSend("/api/settings/network/vpn/wireguard", "PUT", { name: "wireguard", action: "remove" }),
        {
          jalan: tr("Menghapus config WireGuard…"),
          sukses: tr("Config WireGuard dihapus."),
          gagal: (e) => trf("Gagal menghapus config: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleVPN = async (name: string, action: "up" | "down") => {
    const ok = await confirmDialog({
      title:
        action === "up"
          ? trf("Hubungkan {0}?", vpnLabel(name))
          : trf("Putuskan {0}?", vpnLabel(name)),
      message:
        action === "up"
          ? tr("Rute jaringan server bisa berubah setelah tunnel aktif.")
          : tr("Akses jarak jauh yang lewat tunnel ini akan terputus — termasuk sesi Anda kalau membuka dashboard dari alamat tunnel."),
      confirmLabel: action === "up" ? tr("Hubungkan") : tr("Putuskan"),
      danger: action === "down",
    })
    if (!ok) return
    // `tailscale up` menunggu sampai 30 detik (batas yang dipasang helper).
    // Selama itu pola lama tidak menampilkan apa pun, jadi satu-satunya tanda
    // bahwa panel sedang bekerja baru muncul di akhir — dan hilang sama sekali
    // kalau user pindah halaman lebih dulu.
    //
    // Kalimat "menunggu persetujuan admin" dibawa oleh sukses, bukan toast
    // warn terpisah: aksinya memang berhasil (node terdaftar), yang belum
    // selesai ada di pihak lain. Sinyal "butuh perhatian" tetap dipegang badge
    // amber di kartunya, yang tidak ikut hilang bersama toast.
    try {
      await notify.tugas(
        apiSend<VPNStatus>(`/api/settings/network/vpn/${name}`, "PUT", buildVPNBody(name, action)),
        {
          jalan:
            action === "up"
              ? trf("Menghubungkan {0}…", vpnLabel(name))
              : trf("Memutus {0}…", vpnLabel(name)),
          sukses: (hasil) =>
            hasil?.needs_approval
              ? trf("{0}: menunggu persetujuan admin tailnet.", vpnLabel(name))
              : action === "up"
                ? trf("{0} tersambung.", vpnLabel(name))
                : trf("{0} diputus.", vpnLabel(name)),
          detail: (hasil) =>
            hasil?.needs_approval
              ? tr("Node sudah terdaftar. Buka https://login.tailscale.com/admin/machines lalu setujui mesin ini — tidak ada yang perlu diubah di server.")
              : undefined,
          gagal: (e) => trf("Gagal mengatur VPN {0}: {1}", name, pesanError(e)),
        },
      )
      setVpnModal(null)
      setVpnForm({ authKey: "", hostname: "", token: "", config: "" })
      // Token yang barusan diketik sudah tersimpan di sistem; tampilkan lagi
      // bentuk tersamar dari server, bukan teks mentah yang masih di layar.
      if (name === "cloudflared") setCfDiketik(false)
      if (name === "tailscale") setTsDiketik(false)
      load(name === "cloudflared" || name === "tailscale")
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const vpnLabel = (n: string) => {
    if (n === "tailscale") return "Tailscale"
    if (n === "cloudflared") return "Cloudflare Tunnel"
    if (n === "wireguard") return "WireGuard"
    return n
  }

  return (
    <div className="space-y-4">
      {/* Network Interfaces */}
      <Panel
        title={tr("Network Interfaces")}
        hint={tr("Interface aktif dengan IP & MAC")}
        actions={
          <Button variant="outline" size="sm" onClick={() => load()} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2 md:grid-cols-3">
          {ifaces.map((i) => (
            <div key={i.name} className="rounded-md border border-border p-3 space-y-1.5">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-1.5 font-semibold text-sm">
                  <Network className="size-4 text-signal" />
                  <span>{i.name}</span>
                </div>
                <Badge tone={i.up ? "ok" : "muted"}>{i.up ? "UP" : "DOWN"}</Badge>
              </div>
              <p className="num text-[11px] text-muted-foreground">MAC: {i.mac}</p>
              <div className="space-y-0.5 pt-1">
                {i.ips.map((ip) => (
                  <p key={ip} className="num text-xs font-mono font-medium text-foreground">
                    {ip}
                  </p>
                ))}
              </div>
            </div>
          ))}
        </div>
      </Panel>

      <div className="grid gap-4 md:grid-cols-2">
        {/* DNS */}
        <Panel title={tr("DNS Nameservers")} hint={tr("Sistem DNS resolver host")}>
          <form onSubmit={handleSaveDNS} className="space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                {tr("Nameservers (pisahkan dengan koma)")}
              </label>
              <Input
                className="mt-1"
                disabled={!user?.sudo}
                value={dnsInput}
                onChange={(e) => setDnsInput(e.target.value)}
                placeholder="1.1.1.1, 8.8.8.8"
              />
            </div>
            {user?.sudo && (
              <Button type="submit" size="sm">
                {tr("Simpan DNS")}
              </Button>
            )}
          </form>
        </Panel>

        {/* VPN / Tunnel Grouping */}
        {user?.sudo && (
          <Panel title={tr("VPN & Tunnels")} hint={tr("Tailscale, Cloudflare Tunnel, WireGuard")}>
            <div className="space-y-3">
              {vpns.map((v) => (
                <div key={v.name} className="rounded-md border border-border p-3">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-semibold">{vpnLabel(v.name)}</p>
                        <Badge
                          tone={
                            !v.installed ? "muted" : v.connected ? "ok" : v.needs_approval ? "warn" : "crit"
                          }
                        >
                          {!v.installed
                            ? tr("Belum terpasang")
                            : v.connected
                              ? tr("Terkoneksi")
                              : v.needs_approval
                                ? tr("Menunggu persetujuan")
                                : tr("Terputus")}
                        </Badge>
                      </div>
                      {v.state && <p className="num mt-0.5 text-xs text-muted-foreground">{v.state}</p>}
                      {v.detail && (
                        <p
                          className={
                            v.needs_approval
                              ? "max-w-md text-xs text-warn"
                              : "max-w-xs truncate text-xs text-muted-foreground"
                          }
                        >
                          {tr(v.detail)}
                        </p>
                      )}
                    </div>
                    <div className="flex gap-1.5">
                      {/* Modul yang belum terpasang tidak punya tombol sambung —
                          satu-satunya langkah berikutnya adalah memasangnya. */}
                      {!v.installed ? (
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => navigate("/settings/components")}
                        >
                          <Package className="mr-1 size-3" /> {tr("Pasang di Components")}
                        </Button>
                      ) : v.connected ? (
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => handleVPN(v.name, "down")}
                        >
                          <Power className="mr-1 size-3 text-crit" /> {tr("Putus")}
                        </Button>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => {
                            if (v.name === "cloudflared" || v.name === "tailscale") {
                              handleVPN(v.name, "up")
                              return
                            }
                            setVpnModal(v.name)
                            setVpnForm({ authKey: "", hostname: "", token: "", config: "" })
                          }}
                        >
                          <ShieldCheck className="mr-1 size-3 text-ok" /> {tr("Sambung")}
                        </Button>
                      )}
                      {v.name === "wireguard" && v.installed && !v.connected && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-muted-foreground hover:text-crit"
                          aria-label={tr("Hapus config WireGuard")}
                          title={tr("Hapus config wg")}
                          onClick={hapusConfigWG}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>

                  {v.name === "tailscale" && v.installed && (
                    <div className="mt-3 space-y-2">
                      <div>
                        <label className="text-xs font-medium text-muted-foreground">{tr("Auth key")}</label>
                        <Input
                          className="mt-1 font-mono text-[11px]"
                          value={tsKey}
                          disabled={v.connected}
                          onChange={(e) => {
                            setTsDiketik(true)
                            setTsKey(e.target.value)
                          }}
                          placeholder={tr("tempel auth key, atau perintah `sudo tailscale up --auth-key=…`")}
                        />
                      </div>
                      {!v.connected && (
                        <div>
                          <label className="text-xs font-medium text-muted-foreground">
                            {tr("Hostname di tailnet (opsional)")}
                          </label>
                          <Input
                            className="mt-1"
                            value={tsHost}
                            onChange={(e) => setTsHost(e.target.value)}
                            placeholder="lindash"
                          />
                        </div>
                      )}
                      <p className="text-[10px] text-muted-foreground">
                        {v.connected
                          ? tr("Sudah tersambung — tekan Putus dulu untuk memakai auth key lain.")
                          : tsKey && !tsDiketik
                            ? tr("Auth key terakhir yang dipakai (disamarkan).")
                            : tr("Boleh tempel kuncinya saja, atau perintah lengkap dari dashboard Tailscale — yang diambil hanya auth key-nya.")}
                      </p>
                    </div>
                  )}

                  {/* Mode server: config, kunci, dan daftar klien dibuat panel.
                      Mode klien tetap lewat tombol Sambung + tempel config. */}
                  {v.name === "wireguard" && v.installed && (
                    <WireGuardServer versi={versiVPN} onBerubah={() => load()} />
                  )}

                  {/* Token tunnel tampil apa adanya: itu satu-satunya cara tahu
                      tunnel mana yang terpasang. Dikunci selama tunnel jalan —
                      mengganti token harus lewat Putus dulu. */}
                  {v.name === "cloudflared" && v.installed && (
                    <div className="mt-3">
                      <label className="text-xs font-medium text-muted-foreground">{tr("Token tunnel")}</label>
                      <Input
                        className="mt-1 font-mono text-[11px]"
                        value={cfToken}
                        disabled={v.connected}
                        onChange={(e) => {
                          setCfDiketik(true)
                          setCfToken(e.target.value)
                        }}
                        placeholder={tr("tempel token, atau perintah `cloudflared service install <token>`")}
                      />
                      <p className="mt-1 text-[10px] text-muted-foreground">
                        {v.connected
                          ? tr("Tunnel sedang jalan — tekan Putus dulu untuk memakai token lain.")
                          : v.token && !cfDiketik
                            ? tr("Token yang sudah terpasang (disamarkan). Tekan Sambung untuk memakainya lagi, atau timpa dengan token lain.")
                            : tr("Boleh tempel token telanjang, `sudo cloudflared service install <token>`, atau `cloudflared tunnel run --token <token>` — yang diambil hanya tokennya.")}
                      </p>
                    </div>
                  )}
                </div>
              ))}
              {vpns.length === 0 && (
                <p className="py-2 text-xs text-muted-foreground">
                  {tr("Status VPN tidak terbaca. Pastikan helper daemon aktif.")}
                </p>
              )}
            </div>
          </Panel>
        )}
      </div>

      {/* Modal khusus WireGuard: isinya berkas config, bukan satu kunci —
          terlalu besar untuk ditaruh inline di daftar. */}
      {vpnModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">{trf("Hubungkan {0}", vpnLabel(vpnModal))}</p>
            <div className="mt-3 space-y-3">
              {vpnModal === "wireguard" && (
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Isi wg0.conf")}</label>
                  <textarea
                    className="mt-1 w-full rounded border border-border bg-background p-2 font-mono text-[11px]"
                    rows={8}
                    value={vpnForm.config}
                    onChange={(e) => setVpnForm({ ...vpnForm, config: e.target.value })}
                    placeholder={"[Interface]\nAddress = 10.0.0.2/24\nPrivateKey = ...\n\n[Peer]\n..."}
                  />
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    {tr("Wajib ada section [Interface]. Disimpan ke /etc/wireguard/wg0.conf (mode 0600).")}
                  </p>
                </div>
              )}
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setVpnModal(null)}>
                  {tr("Batal")}
                </Button>
                <Button size="sm" onClick={() => handleVPN(vpnModal, "up")}>
                  {tr("Hubungkan")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
