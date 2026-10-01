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
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border bg-secondary/30 text-muted-foreground">
              <th className="p-2.5 font-medium">{tr("Waktu")}</th>
              <th className="p-2.5 font-medium">{tr("User")}</th>
              <th className="p-2.5 font-medium">{tr("Status")}</th>
              <th className="p-2.5 font-medium">{tr("Pesan")}</th>
              <th className="p-2.5 font-medium">{tr("Detail")}</th>
              <th className="p-2.5 font-medium">{tr("Halaman")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {logs.map((l) => (
              <tr key={l.id} className="hover:bg-secondary/40 transition-colors">
                <td data-label={tr("Waktu")} className="num p-2.5 whitespace-nowrap text-muted-foreground">
                  {formatWaktu(l.created_at)}
                </td>
                <td data-label={tr("User")} className="p-2.5 font-medium">{l.username}</td>
                <td data-label={tr("Status")} className="p-2.5">
                  <div className="flex flex-col items-start">
                    <Badge tone={toneBadge[l.tone] ?? "muted"}>{tr(labelNada[l.tone] ?? "") || l.tone}</Badge>
                  </div>
                </td>
                <td data-label={tr("Pesan")} className="max-w-lg break-words p-2.5">{l.message}</td>
                <td data-label={tr("Detail")} className="max-w-xl p-2.5">
                  {l.detail ? (
                    <pre className="num max-h-40 overflow-auto whitespace-pre-wrap rounded bg-surface-2 p-2 text-[10px] text-muted-foreground">
                      {l.detail}
                    </pre>
                  ) : "—"}
                </td>
                <td data-label={tr("Halaman")} className="num p-2.5 text-muted-foreground">{l.page || "—"}</td>
              </tr>
            ))}
            {logs.length === 0 && !loading && (
              <tr>
                <td data-label="" colSpan={6} className="p-6 text-center text-muted-foreground">
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
