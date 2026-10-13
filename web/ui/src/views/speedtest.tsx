import { useTr } from "@/stores/i18n"
import { Panel } from "@/components/ui/panel"

export function SpeedtestView() {
  const tr = useTr()
  return (
    <Panel title="Speedtest" hint={tr("Uji kecepatan jaringan dari panel.")}>
      <p className="text-sm text-muted-foreground">
        {tr("Fitur speedtest sedang dalam pengembangan.")}
      </p>
    </Panel>
  )
}
