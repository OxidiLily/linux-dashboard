import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { formatBytes } from "@/lib/format"
import { Search, RefreshCw, XCircle } from "lucide-react"

type Proc = {
  pid: number
  ppid: number
  user: string
  cpu_pct: number
  mem_pct: number
  mem_rss: number
  command: string
  name: string
  status: string
  own: boolean
}

export function ProcessesView() {
  const tr = useTr()
  const [procs, setProcs] = useState<Proc[]>([])
  const [loading, setLoading] = useState(false)
  const [query, setQuery] = useState("")
  const [err, setErr] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    setErr(null)
    try {
      const data = await apiGet<Proc[]>("/api/processes")
      setProcs(data)
    } catch (e: any) {
      setErr(pesanError(e) || tr("Gagal memuat daftar proses"))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [])

  const kill = async (pid: number) => {
    const ok = await confirmDialog({
      title: trf("Hentikan proses PID {0}?", pid),
      message: tr("Sinyal SIGTERM dikirim. Data yang belum disimpan proses ini bisa hilang."),
      confirmLabel: tr("Hentikan"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/processes/${pid}/kill`, "POST", { signal: 15 })
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menghentikan proses: {0}", pesanError(e)))
    }
  }

  const filtered = procs.filter(
    (p) =>
      p.name.toLowerCase().includes(query.toLowerCase()) ||
      p.command.toLowerCase().includes(query.toLowerCase()) ||
      p.user.toLowerCase().includes(query.toLowerCase()) ||
      String(p.pid).includes(query),
  )

  return (
    <Panel
      title={tr("Processes")}
      hint={trf("{0} total proses", procs.length)}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-48 sm:w-64">
            <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <Input
              placeholder={tr("Cari proses, PID, user...")}
              className="h-8 pl-8 text-xs"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      {err && (
        <div className="mb-3 rounded border border-crit/30 bg-crit/10 px-3 py-2 text-xs text-crit">
          {err}
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="pb-2 font-medium">PID</th>
              <th className="pb-2 font-medium">User</th>
              <th className="pb-2 font-medium text-right">CPU%</th>
              <th className="pb-2 font-medium text-right">MEM%</th>
              <th className="pb-2 font-medium text-right">RSS</th>
              <th className="pb-2 pl-4 font-medium">Command</th>
              <th className="pb-2 text-right font-medium">{tr("Aksi")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {filtered.slice(0, 100).map((p) => (
              <tr key={p.pid} className="hover:bg-secondary/40">
                <td data-label="PID" className="num py-2">{p.pid}</td>
                <td data-label="User" className="py-2">
                  <span className={p.own ? "text-signal font-medium" : "text-muted-foreground"}>
                    {p.user}
                  </span>
                </td>
                <td data-label="CPU%" className="num py-2 text-right font-semibold">
                  <span className={p.cpu_pct > 50 ? "text-crit" : p.cpu_pct > 20 ? "text-amber-500" : ""}>
                    {p.cpu_pct.toFixed(1)}%
                  </span>
                </td>
                <td data-label="MEM%" className="num py-2 text-right">{p.mem_pct.toFixed(1)}%</td>
                <td data-label="RSS" className="num py-2 text-right text-muted-foreground">{formatBytes(p.mem_rss)}</td>
                <td data-label="Command" className="max-w-xs truncate py-2 pl-4 num text-muted-foreground sm:max-w-md" title={p.command}>
                  {p.command || p.name}
                </td>
                <td data-label="" className="py-2 text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-1.5 text-muted-foreground hover:text-crit"
                    onClick={() => kill(p.pid)}
                    title={p.own ? tr("Kill proses milik sendiri") : tr("Kill (perlu sudo)")}
                  >
                    <XCircle className="size-3.5" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}
