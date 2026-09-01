import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { trf, useTr } from "@/stores/i18n"
import { apiGet } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { formatWaktu } from "@/lib/format"
import { RefreshCw } from "lucide-react"

// Backend ActivityEntry (internal/store): id, username, event_type, action,
// detail (JSON string), ip_address, created_at. Activity log "belum jelas"
// sebelumnya karena frontend cuma menampilkan `action` (sub-detail kosong
// untuk event login) — sekarang tampilkan event_type sebagai badge utama.
type Activity = {
  id: number
  username: string
  event_type: string
  action: string
  detail: string
  ip_address: string
  created_at: string
}

// Label disimpan dalam bahasa Indonesia dan diterjemahkan saat dirender:
// tabel ini dievaluasi sekali saat modul dimuat, sebelum preferensi bahasa
// user diketahui.
const eventLabel: Record<string, string> = {
  login_success: "Login Berhasil",
  login_failed: "Login Gagal",
  logout: "Logout",
  terminal_open: "Buka Terminal",
  hostname_change: "Ubah Hostname",
  password_change: "Ubah Password",
  user_create: "Buat User",
  user_modify: "Ubah User",
  user_delete: "Hapus User",
  samba_share_save: "Simpan Share",
  samba_share_delete: "Hapus Share",
  vpn_configure: "Konfigurasi VPN",
  firewall_rule_add: "Tambah Rule FW",
  firewall_rule_delete: "Hapus Rule FW",
  component_install: "Pasang Komponen",
  component_uninstall: "Hapus Komponen",
}

function formatDetail(raw: string): string {
  if (!raw) return "—"
  try {
    const obj = JSON.parse(raw)
    if (typeof obj === "string") return obj
    if (obj && typeof obj === "object") {
      // Tampilkan key:value ringkas, skip key panjang/verbose.
      return Object.entries(obj)
        .filter(([k]) => !["ip", "ip_address", "username"].includes(k))
        .map(([k, v]) => `${k}: ${typeof v === "string" ? v : JSON.stringify(v)}`)
        .join(" · ")
    }
  } catch {
    // raw adalah string biasa
  }
  return raw
}

export function ActivityLogsView() {
  const tr = useTr()
  const [logs, setLogs] = useState<Activity[]>([])
  const [loading, setLoading] = useState(false)
  const [scopeAll, setScopeAll] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<Activity[]>(`/api/logs/activity?limit=200${scopeAll ? "&scope=all" : ""}`)
      setLogs(data || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat activity log: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [scopeAll])

  const tone = (et: string): "ok" | "crit" | "warn" | "muted" => {
    if (et === "login_success" || et === "logout") return "ok"
    if (et === "login_failed") return "crit"
    if (et.includes("delete") || et.includes("uninstall")) return "crit"
    if (et.includes("create") || et.includes("install") || et.includes("save") || et.includes("add")) return "ok"
    return "muted"
  }

  return (
    <Panel
      title={tr("Activity Logs")}
      hint={
        scopeAll
          ? tr("Semua aksi sistem & login. Disimpan 2 tahun, setelah itu dihapus otomatis.")
          : tr("Riwayat event login / logout. Disimpan 2 tahun, setelah itu dihapus otomatis.")
      }
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-1.5 text-xs cursor-pointer text-muted-foreground">
            <input type="checkbox" checked={scopeAll} onChange={(e) => setScopeAll(e.target.checked)} />
            <span>{tr("Tampilkan semua aksi")}</span>
          </label>
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="overflow-x-auto">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="pb-2 font-medium">{tr("Waktu")}</th>
              <th className="pb-2 font-medium">{tr("User")}</th>
              <th className="pb-2 font-medium">{tr("Event")}</th>
              <th className="pb-2 font-medium">{tr("IP Address")}</th>
              <th className="pb-2 font-medium">{tr("Detail")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {logs.map((l) => (
              <tr key={l.id} className="hover:bg-secondary/40">
                <td data-label={tr("Waktu")} className="num py-2 text-muted-foreground whitespace-nowrap">
                  {formatWaktu(l.created_at)}
                </td>
                <td data-label={tr("User")} className="py-2 font-medium">{l.username}</td>
                <td data-label={tr("Event")} className="py-2">
                  {/* self-start: Badge adalah inline-flex, tapi di dalam
                      flex-col parent ia akan stretch ke lebar cell. Tanpa
                      ini "Login Berhasil" jadi sepanjang kolom Event. */}
                  <div className="flex flex-col items-start gap-0.5">
                    <Badge tone={tone(l.event_type)}>
                      {tr(eventLabel[l.event_type] ?? "") || l.event_type}
                    </Badge>
                    {l.action && l.action !== l.event_type && (
                      <span className="text-[10px] text-muted-foreground">{tr(l.action)}</span>
                    )}
                  </div>
                </td>
                <td data-label={tr("IP Address")} className="num py-2 text-muted-foreground">{l.ip_address || "—"}</td>
                <td data-label={tr("Detail")} className="py-2 text-muted-foreground max-w-md break-words">
                  {formatDetail(l.detail)}
                </td>
              </tr>
            ))}
            {logs.length === 0 && !loading && (
              <tr>
                <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                  {tr("Belum ada log aktivitas.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}
