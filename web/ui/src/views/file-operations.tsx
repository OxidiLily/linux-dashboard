import { useEffect, useState } from "react"
import { apiGet } from "@/lib/api"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { formatWaktu } from "@/lib/format"
import { RefreshCw } from "lucide-react"
import { useTr } from "@/stores/i18n"

type FileOp = {
  id: number
  username: string
  operation: string
  source_path: string
  dest_path?: string
  created_at: string
}

export function FileOperationsView() {
  const tr = useTr()
  const [logs, setLogs] = useState<FileOp[]>([])
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<FileOp[]>("/api/logs/file-operations?limit=100")
      setLogs(data || [])
    } catch {}
    finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Panel
      title={tr("File Operations Log")}
      hint={tr("Riwayat operasi file (upload, copy, move, delete, rename). Disimpan 1 bulan, setelah itu dihapus otomatis.")}
      actions={
        <Button variant="outline" size="sm" onClick={load} disabled={loading}>
          <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
        </Button>
      }
    >
      <div className="overflow-x-auto">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="pb-2 font-medium">{tr("Waktu")}</th>
              <th className="pb-2 font-medium">{tr("User")}</th>
              <th className="pb-2 font-medium">{tr("Operasi")}</th>
              <th className="pb-2 font-medium">{tr("Sumber")}</th>
              <th className="pb-2 font-medium">{tr("Tujuan")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {logs.map((l) => (
              <tr key={l.id} className="hover:bg-secondary/40">
                <td data-label={tr("Waktu")} className="num py-2 text-muted-foreground whitespace-nowrap">
                  {formatWaktu(l.created_at)}
                </td>
                <td data-label={tr("User")} className="py-2 font-medium">{l.username}</td>
                <td data-label={tr("Operasi")} className="py-2">
                  {/* w-fit: Badge adalah inline-flex, tapi beberapa style
                      global membuat ia stretch ke lebar cell. Bungkus
                      dengan width-fit (lebar = konten + padding) supaya
                      border mengikuti panjang label "upload"/"copy"/dst. */}
                  <div className="w-fit">
                    <Badge tone={l.operation === "delete" ? "crit" : l.operation === "upload" ? "ok" : "signal"}>
                      {l.operation}
                    </Badge>
                  </div>
                </td>
                <td data-label={tr("Sumber")} className="num py-2 text-muted-foreground max-w-xs truncate" title={l.source_path}>
                  {l.source_path || "—"}
                </td>
                <td data-label={tr("Tujuan")} className="num py-2 text-muted-foreground max-w-xs truncate" title={l.dest_path}>
                  {l.dest_path || "—"}
                </td>
              </tr>
            ))}
            {logs.length === 0 && !loading && (
              <tr>
                <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                  {tr("Belum ada log operasi file.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}
