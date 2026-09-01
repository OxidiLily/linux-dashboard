import { useEffect, useState } from "react"
import { apiGet } from "@/lib/api"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { formatWaktu } from "@/lib/format"
import { RefreshCw } from "lucide-react"
import { trf, useTr } from "@/stores/i18n"

// Backend store.Notification: id, username, tone, message, detail, page,
// created_at. Isinya alert yang tampil sebagai toast di panel — halaman ini
// menampilkannya kembali setelah toast-nya hilang dari layar.
type Notifikasi = {
  id: number
  username: string
  tone: string
  message: string
  detail?: string
  page?: string
  created_at: string
}

const NADA = ["ok", "err", "warn", "info"] as const

// Label nada ditulis Indonesia dan diterjemahkan saat render — tabel ini
// dievaluasi sekali saat modul dimuat, sebelum bahasa user diketahui.
const labelNada: Record<string, string> = {
  ok: "Berhasil",
  err: "Gagal",
  warn: "Peringatan",
  info: "Info",
}

const toneBadge: Record<string, "ok" | "crit" | "warn" | "muted"> = {
  ok: "ok",
  err: "crit",
  warn: "warn",
  info: "muted",
}

export function LogsView() {
  const tr = useTr()
  const [logs, setLogs] = useState<Notifikasi[]>([])
  const [loading, setLoading] = useState(false)
  const [nada, setNada] = useState("")

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<Notifikasi[]>(
        `/api/logs/notifications?limit=300${nada ? `&tone=${nada}` : ""}`,
      )
      setLogs(data || [])
    } catch {
      // Sengaja diam: memunculkan toast "gagal memuat log" akan mencatat
      // dirinya sendiri ke tabel yang sedang gagal dibaca.
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [nada])

  return (
    <Panel
      title={tr("Logs")}
      hint={tr("Semua alert panel — berhasil, gagal, peringatan, info. Disimpan 1 bulan, setelah itu dihapus otomatis.")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex flex-wrap gap-1">
            <Button variant={nada === "" ? "default" : "outline"} size="sm" onClick={() => setNada("")}>
              {tr("Semua")}
            </Button>
            {NADA.map((n) => (
              <Button
                key={n}
                variant={nada === n ? "default" : "outline"}
                size="sm"
                onClick={() => setNada(n)}
              >
                {tr(labelNada[n])}
              </Button>
            ))}
          </div>
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
              <th className="pb-2 font-medium">{tr("Status")}</th>
              <th className="pb-2 font-medium">{tr("Pesan")}</th>
              <th className="pb-2 font-medium">{tr("Halaman")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {logs.map((l) => (
              <tr key={l.id} className="hover:bg-secondary/40">
                <td data-label={tr("Waktu")} className="num py-2 whitespace-nowrap text-muted-foreground">
                  {formatWaktu(l.created_at)}
                </td>
                <td data-label={tr("User")} className="py-2 font-medium">{l.username}</td>
                <td data-label={tr("Status")} className="py-2">
                  <div className="flex flex-col items-start">
                    <Badge tone={toneBadge[l.tone] ?? "muted"}>{tr(labelNada[l.tone] ?? "") || l.tone}</Badge>
                  </div>
                </td>
                <td data-label={tr("Pesan")} className="max-w-lg break-words py-2">
                  {l.message}
                  {/* Detail = keluaran mentah (stderr apt, journal). Dipendam
                      di <details> supaya satu baris log panjang tidak
                      mendorong seluruh tabel. */}
                  {l.detail && (
                    <details className="mt-1">
                      <summary className="cursor-pointer text-[10px] text-muted-foreground">
                        {tr("Detail")}
                      </summary>
                      <pre className="num mt-1 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-surface-2 p-2 text-[10px] text-muted-foreground">
                        {l.detail}
                      </pre>
                    </details>
                  )}
                </td>
                <td data-label={tr("Halaman")} className="num py-2 text-muted-foreground">{l.page || "—"}</td>
              </tr>
            ))}
            {logs.length === 0 && !loading && (
              <tr>
                <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                  {nada
                    ? trf("Belum ada alert dengan status {0}.", tr(labelNada[nada] ?? nada))
                    : tr("Belum ada alert yang tercatat.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}
