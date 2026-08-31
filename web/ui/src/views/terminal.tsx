import { useCallback, useEffect, useState } from "react"
import { useTr } from "@/stores/i18n"
import { apiGet } from "@/lib/api"
import { Panel } from "@/components/ui/panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RefreshCw } from "lucide-react"
import type { TerminalCapacity } from "@/lib/types"
import { TerminalError, useTerminalSession } from "@/hooks/use-terminal-session"

export function TerminalView() {
  const tr = useTr()
  const [cap, setCap] = useState<TerminalCapacity | null>(null)

  // Kapasitas dibaca ulang tiap sesi dibuka/ditutup, bukan sekali saat mount.
  // Slot baru diambil server SETELAH permintaan WebSocket diterima, jadi
  // pembacaan saat mount selalu mendahului sesi ini sendiri — itu sebabnya
  // badge dulu terus menampilkan "Sesi: 0" padahal terminalnya jelas terbuka.
  const muatKapasitas = useCallback(() => {
    apiGet<TerminalCapacity>("/api/terminal/capacity").then(setCap).catch(() => undefined)
  }, [])

  useEffect(() => {
    muatKapasitas()
  }, [muatKapasitas])

  const { containerRef, err, bukaUlang } = useTerminalSession({
    labelTutup: tr("Sesi terminal ditutup"),
    onSesi: muatKapasitas,
  })

  return (
    <Panel
      title={tr("Terminal")}
      hint={tr("Shell web interaktif")}
      contentClassName="p-1"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {cap && (
            <Badge tone="muted" className="num">
              {tr("Sesi")}: {cap.active} / {cap.max} ({tr("Core")}: {cap.cores}
              {cap.login_users !== undefined && cap.login_users > 0 ? ` · ${tr("Login")}: ${cap.login_users}` : ""}
              )
            </Badge>
          )}
          <Button variant="outline" size="sm" onClick={bukaUlang} title={tr("Buka ulang sesi")}>
            <RefreshCw className="size-3.5" />
          </Button>
        </div>
      }
    >
      {err && <TerminalError err={err} onRetry={bukaUlang} />}
      {/* Tinggi diatur pasangAutoFit dari posisi elemen ke tepi bawah
          viewport — bukan angka tetap yang meleset begitu ada banner di
          atasnya. Padding kiri-kanan dihapus supaya tiap piksel lebar jadi
          kolom teks; jarak ke border sudah diberi panel di p-1.

          Saat kuota penuh tidak ada PTY sama sekali, jadi kotaknya
          disembunyikan: yang tersisa cuma persegi hitam setinggi layar yang
          tidak menerima ketikan dan tidak akan pernah menampilkan apa pun.
          Disembunyikan lewat class, bukan dilepas dari DOM — containerRef
          harus tetap terisi supaya percobaan ulang punya elemen untuk
          dipasangi terminal baru. */}
      <div
        ref={containerRef}
        className={`w-full overflow-hidden rounded-md bg-bg${err?.kind === "full" ? " hidden" : ""}`}
      />
    </Panel>
  )
}
