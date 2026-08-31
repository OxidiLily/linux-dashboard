import { useEffect, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { apiGet } from "@/lib/api"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RefreshCw, Bot, Download } from "lucide-react"
import { trf, useTr } from "@/stores/i18n"
import { TerminalError, useTerminalSession } from "@/hooks/use-terminal-session"
import { cn } from "@/lib/utils"

type ComponentStatus = {
  name: string
  installed: boolean
  version?: string
  running?: boolean
  service?: string
  category?: string
  description?: string
  required_for?: string
}

type AgentOption = {
  id: string
  name: string
  componentKey: string
  desc: string
}

// ALAT_WAJIB = alat & skill yang dipakai SEMUA agent, bukan milik salah satu.
// Ditampilkan sebagai baris status supaya kelengkapannya terlihat sebelum
// sesi dimulai — agent yang jalan tanpa alat ini boros token dan kehilangan
// arahan ponytail, dan itu tidak kelihatan dari dalam terminal.
// Keterangannya diterjemahkan di titik render (bukan di sini) supaya ikut
// berubah saat bahasa panel diganti tanpa reload.
const ALAT_WAJIB = ["rtk", "graphify", "ponytail"]

const AGENTS: AgentOption[] = [
  {
    id: "hermes",
    name: "Hermes Agent",
    componentKey: "hermes",
    desc: "Autonomous AI Agent by Nous Research",
  },
  {
    id: "claude-code",
    name: "Claude Code",
    componentKey: "claude-code",
    desc: "Anthropic Agentic Coding CLI",
  },
  {
    id: "codex",
    name: "OpenAI Codex",
    componentKey: "codex",
    desc: "OpenAI Codex CLI Assistant",
  },
  {
    id: "opencode",
    name: "OpenCode",
    componentKey: "opencode",
    desc: "Open-source Autonomous Coding Agent",
  },
  {
    id: "openclaw",
    name: "OpenClaw",
    componentKey: "openclaw",
    desc: "Multi-channel Autonomous AI Agent Gateway",
  },
]

export function AIAgentView() {
  const tr = useTr()
  const navigate = useNavigate()
  const location = useLocation()

  // Ambil pilihan agent dari query param atau localStorage atau default hermes
  const queryParamAgent = new URLSearchParams(location.search).get("agent")
  const [selectedAgentId, setSelectedAgentId] = useState<string>(() => {
    if (queryParamAgent && AGENTS.some((a) => a.id === queryParamAgent)) {
      return queryParamAgent
    }
    const saved = localStorage.getItem("lindash:selected_ai_agent")
    if (saved && AGENTS.some((a) => a.id === saved)) {
      return saved
    }
    return "hermes"
  })

  const [components, setComponents] = useState<ComponentStatus[]>([])
  const [loadingComp, setLoadingComp] = useState(false)

  // fresh=1 dipakai tombol Refresh: tanpa itu helper menjawab dari cache 30
  // detik, jadi alat yang baru dipasang dari halaman Components masih
  // tertulis "belum ada" di sini.
  const loadComponents = async (fresh = false) => {
    setLoadingComp(true)
    try {
      const data = await apiGet<ComponentStatus[]>(`/api/components${fresh ? "?fresh=1" : ""}`)
      setComponents(data || [])
    } catch {
      // Abaikan error background fetch
    } finally {
      setLoadingComp(false)
    }
  }

  useEffect(() => {
    loadComponents()
  }, [])

  // components kosong = daftar belum termuat; jangan laporkan alat "kurang"
  // sebelum jawabannya datang, karena tombol pasang akan berkedip tiap load.
  const alatKurang =
    components.length === 0
      ? []
      : ALAT_WAJIB.filter((key) => !components.find((c) => c.name === key)?.installed)

  // Keterangan singkat per alat, dipakai sebagai tooltip badge.
  const ketAlat: Record<string, string> = {
    rtk: tr("Pemangkas token untuk keluaran perintah shell"),
    graphify: tr("Knowledge graph kode untuk penelusuran repo"),
    ponytail: tr("Harness lazy senior dev level ultra + skill audit/review/debt"),
  }

  const selectedAgent = AGENTS.find((a) => a.id === selectedAgentId) || AGENTS[0]
  const currentCompStatus = components.find((c) => c.name === selectedAgent.componentKey)
  const isInstalled = currentCompStatus ? !!currentCompStatus.installed : true

  const handleSelectAgent = (agentId: string) => {
    setSelectedAgentId(agentId)
    localStorage.setItem("lindash:selected_ai_agent", agentId)
  }

  const { containerRef, err, bukaUlang } = useTerminalSession({
    cmd: selectedAgent.id,
    aktif: isInstalled,
    labelTutup: tr("Sesi CLI AI Agent ditutup"),
  })

  return (
    <Panel
      title={tr("AI Agent")}
      hint={tr("Antarmuka interaktif AI Agent CLI — Ctrl+C memuat ulang agent, bukan menutup sesi")}
      contentClassName="p-1"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void loadComponents(true)
              bukaUlang()
            }}
            title={tr("Buka ulang sesi")}
          >
            <RefreshCw className={cn("size-3.5", loadingComp && "animate-spin")} />
          </Button>
        </div>
      }
    >
      {/* px-2 dikembalikan per-baris karena panel ini memakai
          contentClassName="p-1" demi lebar terminal — hanya kotak terminal
          yang benar-benar ingin menempel ke tepi, bukan barisan tombol. */}
      {/* Di HP baris pemilih agent digeser, bukan dibungkus: dibungkus ia jadi
          tiga baris tombol yang mendorong terminal ke luar layar. */}
      <div className="mb-4 flex items-center gap-2 overflow-x-auto border-b border-border px-2 pb-3 pt-1 sm:flex-wrap sm:overflow-visible">
        {AGENTS.map((ag) => {
          const comp = components.find((c) => c.name === ag.componentKey)
          const active = ag.id === selectedAgentId
          const installed = comp ? comp.installed : false
          return (
            <button
              key={ag.id}
              type="button"
              onClick={() => handleSelectAgent(ag.id)}
              className={cn(
                "flex shrink-0 items-center gap-2 rounded-md border px-3 py-1.5 text-xs transition-colors",
                active
                  ? "border-primary bg-primary/10 font-medium text-foreground"
                  : "border-border text-muted-foreground hover:bg-secondary/40",
              )}
            >
              <Bot className="size-3.5" />
              <span>{ag.name}</span>
              {comp && (
                <Badge
                  tone={installed ? "ok" : "muted"}
                  className="px-1.5 py-0 text-[10px]"
                >
                  {installed ? tr("Terpasang") : tr("Belum ada")}
                </Badge>
              )}
            </button>
          )
        })}
      </div>

      <div className="mb-4 flex items-center gap-2 overflow-x-auto px-2 text-xs sm:flex-wrap sm:overflow-visible">
        <span className="text-muted-foreground">{tr("Alat & skill wajib")}:</span>
        {ALAT_WAJIB.map((key) => {
          const ada = !!components.find((c) => c.name === key)?.installed
          return (
            <Badge key={key} tone={ada ? "ok" : "warn"} title={ketAlat[key]}>
              {key}
            </Badge>
          )
        })}
        {alatKurang.length > 0 && (
          <Button size="sm" variant="outline" onClick={() => navigate("/settings/components")}>
            <Download className="mr-1.5 size-3.5" />
            {trf("Pasang {0} alat yang kurang", alatKurang.length)}
          </Button>
        )}
      </div>

      {!isInstalled ? (
        <div className="m-2 flex flex-col items-center justify-center rounded-md border border-dashed border-border p-8 text-center">
          <Bot className="mb-3 size-10 text-muted-foreground" />
          <h3 className="mb-1 text-sm font-medium">{trf("{0} Belum Terpasang", selectedAgent.name)}</h3>
          <p className="mb-4 max-w-sm text-xs text-muted-foreground">
            {trf("Komponen {0} belum dipasang di sistem ini. Anda dapat memasangnya dari menu Components.", selectedAgent.name)}
          </p>
          <Button
            size="sm"
            onClick={() => navigate("/settings/components")}
          >
            <Download className="mr-1.5 size-3.5" />
            {tr("Buka Menu Components")}
          </Button>
        </div>
      ) : (
        <>
          {err && <TerminalError err={err} onRetry={bukaUlang} />}
          {/* Tinggi & lebar diatur pasangAutoFit dari ruang yang benar-benar
              tersisa; padding dihapus supaya tiap piksel jadi kolom teks.

              Kuota penuh = tidak ada PTY, jadi kotaknya disembunyikan
              alih-alih meninggalkan persegi hitam setinggi layar yang tidak
              bisa diketik. Pakai class, bukan melepasnya dari DOM:
              containerRef harus tetap terisi supaya "Coba lagi" punya elemen
              untuk dipasangi terminal baru. */}
          <div
            ref={containerRef}
            className={cn("w-full overflow-hidden rounded-md bg-bg", err?.kind === "full" && "hidden")}
          />
        </>
      )}
    </Panel>
  )
}
