import { useEffect, useState } from "react"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { trf, useTr } from "@/stores/i18n"

// Bentuknya sama persis dengan helperproto.IfaceConfig.
export type IfaceConfig = {
  iface: string
  managed: boolean
  ipv4: "dhcp" | "static" | "off"
  addrs4: string[]
  gw4?: string
  ipv6: "auto" | "static" | "off"
  addrs6: string[]
  gw6?: string
}

const MODE4 = [
  ["dhcp", "Otomatis (DHCP)"],
  ["static", "Manual"],
  ["off", "Dimatikan"],
] as const
const MODE6 = [
  ["auto", "Otomatis (SLAAC/DHCPv6)"],
  ["static", "Manual"],
  ["off", "Dimatikan"],
] as const

const pecah = (s: string) => s.split(",").map((x) => x.trim()).filter(Boolean)

/**
 * Editor IP satu interface — setara panel "IPv4/IPv6" di pengaturan jaringan
 * Ubuntu. Konfigurasi dibaca saat modal dibuka (butuh helper, root), dan
 * disimpan lewat `netplan apply`.
 */
export function IfaceEditor({
  iface,
  ipsSekarang,
  onClose,
  onSaved,
}: {
  iface: string
  /** IP yang sedang terpasang — untuk tahu apakah panel sendiri ikut pindah. */
  ipsSekarang: string[]
  onClose: () => void
  onSaved: () => void
}) {
  const tr = useTr()
  const [cfg, setCfg] = useState<IfaceConfig | null>(null)
  // Alamat diketik sebagai satu baris dipisah koma, seperti field DNS.
  const [a4, setA4] = useState("")
  const [a6, setA6] = useState("")

  useEffect(() => {
    apiGet<IfaceConfig>(`/api/settings/network/interfaces/${iface}/config`)
      .then((c) => {
        setCfg(c)
        setA4(c.addrs4.join(", "))
        setA6(c.addrs6.join(", "))
      })
      .catch((e) => {
        notify.err(trf("Gagal membaca konfigurasi {0}: {1}", iface, pesanError(e)))
        onClose()
      })
  }, [iface])

  const simpan = async () => {
    if (!cfg) return
    const body: IfaceConfig = { ...cfg, addrs4: pecah(a4), addrs6: pecah(a6) }
    // Panel dibuka lewat IP interface ini dan alamatnya berubah → sesi ini
    // putus begitu netplan apply jalan; browser diarahkan ke alamat baru.
    const host = location.hostname
    const ipBaru = body.ipv4 === "static" ? body.addrs4[0]?.split("/")[0] : ""
    const pindah = ipsSekarang.includes(host) && ipBaru !== host
    const ringkas = [
      `IPv4: ${body.ipv4 === "static" ? body.addrs4.join(", ") + (body.gw4 ? ` via ${body.gw4}` : "") : body.ipv4}`,
      `IPv6: ${body.ipv6 === "static" ? body.addrs6.join(", ") + (body.gw6 ? ` via ${body.gw6}` : "") : body.ipv6}`,
    ].join(" · ")
    const ok = await confirmDialog({
      title: trf("Terapkan konfigurasi {0}?", iface),
      message: pindah
        ? ipBaru
          ? trf(
              "Panel ini sedang dibuka lewat {0}. Setelah diterapkan, sesi ini terputus dan browser diarahkan ke {1} — login ulang diperlukan. Alamat yang salah berarti server hanya bisa dijangkau lewat konsol.",
              host,
              ipBaru,
            )
          : trf(
              "Panel ini sedang dibuka lewat {0}. Setelah diterapkan alamat itu hilang dan sesi ini terputus — cari alamat baru server di router/DHCP atau konsol VM.",
              host,
            )
        : tr("Konfigurasi ditulis ke /etc/netplan lalu diterapkan dengan netplan apply. Koneksi yang lewat interface ini bisa terputus sesaat."),
      detail: ringkas,
      confirmLabel: tr("Terapkan"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend(`/api/settings/network/interfaces/${iface}/config`, "PUT", body), {
        jalan: trf("Menerapkan konfigurasi {0}…", iface),
        sukses: trf("Konfigurasi {0} diterapkan.", iface),
        // Saat pindah alamat, gagalnya fetch adalah putusnya koneksi lama —
        // bukan kegagalan netplan; kalimatnya menyebut itu.
        gagal: (e) =>
          pindah
            ? trf("Koneksi ke {0} terputus — kalau alamat baru tidak bisa dibuka, periksa lewat konsol. ({1})", host, pesanError(e))
            : trf("Gagal menerapkan konfigurasi {0}: {1}", iface, pesanError(e)),
      })
    } catch {
      if (!pindah) return
    }
    onSaved()
    if (pindah && ipBaru) {
      // Beri networkd waktu memasang alamat baru sebelum browser mencobanya.
      setTimeout(() => {
        location.replace(`${location.protocol}//${ipBaru}${location.port ? ":" + location.port : ""}${location.pathname}`)
      }, 3000)
    }
  }

  const label = "text-xs font-medium text-muted-foreground"
  const select = "mt-1 w-full rounded border border-border bg-background p-2 text-xs"

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
        <p className="font-semibold text-sm">{trf("Konfigurasi {0}", iface)}</p>
        {!cfg ? (
          <p className="mt-3 text-xs text-muted-foreground">{tr("Membaca konfigurasi netplan…")}</p>
        ) : !cfg.managed ? (
          <p className="mt-3 text-xs text-warn">
            {tr("Interface ini tidak ada di /etc/netplan — biasanya interface virtual (Docker, VPN) yang alamatnya diatur programnya sendiri.")}
          </p>
        ) : (
          <div className="mt-3 space-y-4">
            <div className="space-y-2">
              <div>
                <label className={label}>IPv4</label>
                <select className={select} value={cfg.ipv4} onChange={(e) => setCfg({ ...cfg, ipv4: e.target.value as IfaceConfig["ipv4"] })}>
                  {MODE4.map(([v, l]) => (
                    <option key={v} value={v}>{tr(l)}</option>
                  ))}
                </select>
              </div>
              {cfg.ipv4 === "static" && (
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className={label}>{tr("Alamat/prefix")}</label>
                    <Input className="mt-1 font-mono text-xs" value={a4} onChange={(e) => setA4(e.target.value)} placeholder="192.168.2.33/24" />
                  </div>
                  <div>
                    <label className={label}>{tr("Gateway")}</label>
                    <Input className="mt-1 font-mono text-xs" value={cfg.gw4 ?? ""} onChange={(e) => setCfg({ ...cfg, gw4: e.target.value.trim() })} placeholder="192.168.2.1" />
                  </div>
                </div>
              )}
            </div>
            <div className="space-y-2">
              <div>
                <label className={label}>IPv6</label>
                <select className={select} value={cfg.ipv6} onChange={(e) => setCfg({ ...cfg, ipv6: e.target.value as IfaceConfig["ipv6"] })}>
                  {MODE6.map(([v, l]) => (
                    <option key={v} value={v}>{tr(l)}</option>
                  ))}
                </select>
              </div>
              {cfg.ipv6 === "static" && (
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className={label}>{tr("Alamat/prefix")}</label>
                    <Input className="mt-1 font-mono text-xs" value={a6} onChange={(e) => setA6(e.target.value)} placeholder="fd00::5/64" />
                  </div>
                  <div>
                    <label className={label}>{tr("Gateway")}</label>
                    <Input className="mt-1 font-mono text-xs" value={cfg.gw6 ?? ""} onChange={(e) => setCfg({ ...cfg, gw6: e.target.value.trim() })} placeholder="fd00::1" />
                  </div>
                </div>
              )}
            </div>
            <p className="text-[10px] text-muted-foreground">
              {tr("Beberapa alamat dipisah koma. Nonaktif pada IPv6 juga mematikan alamat link-local (fe80::).")}
            </p>
          </div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose}>{tr("Batal")}</Button>
          {cfg?.managed && (
            <Button size="sm" onClick={simpan}>{tr("Terapkan")}</Button>
          )}
        </div>
      </div>
    </div>
  )
}
