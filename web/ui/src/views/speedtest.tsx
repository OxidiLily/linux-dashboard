import { useEffect, useState, useRef, useCallback } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { Panel } from "@/components/ui/panel"
import { useTr, trf, daftarkanTerjemahan } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Play, Square, ArrowDown, ArrowUp, Activity, Server } from "lucide-react"

daftarkanTerjemahan({
  "Uji kecepatan jaringan dari panel.": "Test network speed from the panel.",
  "Mulai tes": "Start test",
  "Hentikan": "Stop",
  "Mengukur ping…": "Measuring ping…",
  "Mengunduh…": "Downloading…",
  "Mengunggah…": "Uploading…",
  "Selesai": "Done",
  "Download": "Download",
  "Upload": "Upload",
  "Ping": "Ping",
  "Jitter": "Jitter",
  "Server uji": "Test server",
  "Speed test gagal: {0}": "Speed test failed: {0}",
  "Belum ada hasil.": "No results yet.",
  "Riwayat": "History",
  "Gagal memuat status speed test: {0}": "Failed to load speed test status: {0}",
})

type SpeedtestResult = {
  id: string
  download_mbps: number
  upload_mbps: number
  ping_ms: number
  jitter_ms: number
  server: string
  server_url: string
  phase: "idle" | "ping" | "download" | "upload" | "done" | "error"
  progress: number
  error?: string
  started_at: number
  finished_at?: number
}

export function SpeedtestView() {
  const tr = useTr()
  const [result, setResult] = useState<SpeedtestResult | null>(null)
  const [loading, setLoading] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const isRunning = result && !["idle", "done", "error"].includes(result.phase)

  // Poll status saat test berjalan.
  const startPolling = useCallback(() => {
    if (pollRef.current) return
    pollRef.current = setInterval(async () => {
      try {
        const st = await apiGet<SpeedtestResult>("/api/speedtest/status")
        setResult(st)
        if (st.phase === "done" || st.phase === "error" || st.phase === "idle") {
          if (pollRef.current) {
            clearInterval(pollRef.current)
            pollRef.current = null
          }
          if (st.phase === "error" && st.error) {
            notify.err(trf("Speed test gagal: {0}", st.error))
          }
        }
      } catch (e: unknown) {
        notify.err(trf("Gagal memuat status speed test: {0}", pesanError(e)))
      }
    }, 300)
  }, [])

  // Cek status awal (mungkin test sudah jalan dari tab lain).
  useEffect(() => {
    apiGet<SpeedtestResult>("/api/speedtest/status").then((st) => {
      setResult(st)
      if (st && !["idle", "done", "error"].includes(st.phase)) {
        startPolling()
      }
    }).catch(() => {})
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [startPolling])

  const handleStart = async () => {
    setLoading(true)
    try {
      const st = await apiSend<SpeedtestResult>("/api/speedtest/start", "POST")
      setResult(st)
      startPolling()
    } catch (e: unknown) {
      notify.err(trf("Speed test gagal: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  const handleStop = async () => {
    try {
      await apiSend("/api/speedtest/stop", "POST")
    } catch {}
  }

  const phaseLabel = (phase: string) => {
    switch (phase) {
      case "ping": return tr("Mengukur ping…")
      case "download": return tr("Mengunduh…")
      case "upload": return tr("Mengunggah…")
      case "done": return tr("Selesai")
      case "error": return result?.error ?? "Error"
      default: return ""
    }
  }

  return (
    <Panel
      title="Speedtest"
      hint={tr("Uji kecepatan jaringan dari panel.")}
      actions={
        isRunning ? (
          <Button variant="outline" size="sm" onClick={handleStop}>
            <Square className="mr-1 size-3.5" /> {tr("Hentikan")}
          </Button>
        ) : (
          <Button size="sm" onClick={handleStart} disabled={loading}>
            <Play className="mr-1 size-3.5" /> {tr("Mulai tes")}
          </Button>
        )
      }
    >
      <div className="space-y-6">
        {/* Status */}
        {isRunning && (
          <div className="text-center space-y-3">
            <p className="text-sm font-medium text-signal animate-pulse">
              {phaseLabel(result.phase)}
            </p>
            <ProgressBar progress={result.progress} />
          </div>
        )}

        {/* Gauge / Hasil */}
        <div className="grid gap-4 sm:grid-cols-2">
          <GaugeCard
            label={tr("Download")}
            value={result?.download_mbps ?? 0}
            unit="Mbps"
            icon={<ArrowDown className="size-5 text-signal" />}
            active={result?.phase === "download"}
            max={1000}
          />
          <GaugeCard
            label={tr("Upload")}
            value={result?.upload_mbps ?? 0}
            unit="Mbps"
            icon={<ArrowUp className="size-5 text-emerald-400" />}
            active={result?.phase === "upload"}
            max={1000}
          />
        </div>

        {/* Ping / Jitter / Server */}
        <div className="grid gap-3 sm:grid-cols-3">
          <MetricCard
            label={tr("Ping")}
            value={result?.ping_ms ?? 0}
            unit="ms"
            icon={<Activity className="size-4 text-amber-400" />}
          />
          <MetricCard
            label={tr("Jitter")}
            value={result?.jitter_ms ?? 0}
            unit="ms"
            icon={<Activity className="size-4 text-amber-400" />}
          />
          <MetricCard
            label={tr("Server uji")}
            value={0}
            unit=""
            icon={<Server className="size-4 text-muted-foreground" />}
            text={result?.server || "—"}
          />
        </div>

        {/* Catatan test selesai */}
        {result?.phase === "done" && result.finished_at && (
          <p className="text-center text-[11px] text-muted-foreground">
            {new Date(result.finished_at * 1000).toLocaleString()}
          </p>
        )}
        {result?.phase === "error" && result.error && (
          <p className="text-center text-sm text-crit">{result.error}</p>
        )}
        {(!result || result.phase === "idle") && (
          <p className="text-center text-sm text-muted-foreground">
            {tr("Belum ada hasil.")}
          </p>
        )}
      </div>
    </Panel>
  )
}

// ---- Subkomponen ----

function ProgressBar({ progress }: { progress: number }) {
  const pct = Math.min(Math.max(progress * 100, 0), 100)
  return (
    <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
      <div
        className="h-full rounded-full bg-signal transition-all duration-300"
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

function GaugeCard({
  label, value, unit, icon, active, max,
}: {
  label: string; value: number; unit: string; icon: React.ReactNode
  active?: boolean; max: number
}) {
  // SVG arc gauge.
  const radius = 70
  const stroke = 8
  const circumference = Math.PI * radius // setengah lingkaran
  const pct = Math.min(value / max, 1)
  const offset = circumference * (1 - pct)

  return (
    <div className={`rounded-lg border p-4 text-center ${active ? "border-signal/50 bg-signal/5" : "border-border"}`}>
      <div className="flex items-center justify-center gap-1.5 mb-2">
        {icon}
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
      </div>
      <div className="relative mx-auto" style={{ width: 160, height: 90 }}>
        <svg viewBox="0 0 160 90" className="w-full h-full">
          {/* Track */}
          <path
            d="M 10 80 A 70 70 0 0 1 150 80"
            fill="none"
            stroke="currentColor"
            className="text-muted/30"
            strokeWidth={stroke}
            strokeLinecap="round"
          />
          {/* Value arc */}
          <path
            d="M 10 80 A 70 70 0 0 1 150 80"
            fill="none"
            stroke="currentColor"
            className={active ? "text-signal animate-pulse" : "text-signal"}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            style={{ transition: "stroke-dashoffset 0.5s ease" }}
          />
        </svg>
        <div className="absolute inset-0 flex items-end justify-center pb-1">
          <span className="num text-2xl font-bold tabular-nums">{value.toFixed(2)}</span>
          <span className="ml-1 text-xs text-muted-foreground mb-0.5">{unit}</span>
        </div>
      </div>
    </div>
  )
}

function MetricCard({
  label, value, unit, icon, text,
}: {
  label: string; value: number; unit: string
  icon: React.ReactNode; text?: string
}) {
  return (
    <div className="rounded-lg border border-border p-3 text-center">
      <div className="flex items-center justify-center gap-1.5 mb-1">
        {icon}
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
      </div>
      {text ? (
        <p className="text-sm font-semibold truncate">{text}</p>
      ) : (
        <p className="num text-lg font-bold tabular-nums">
          {value.toFixed(2)} <span className="text-xs text-muted-foreground">{unit}</span>
        </p>
      )}
    </div>
  )
}
