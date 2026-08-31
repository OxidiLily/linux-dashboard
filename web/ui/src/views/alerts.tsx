import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { useAuth } from "@/stores/auth"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

type Threshold = {
  metric: string
  warn_pct: number
  crit_pct: number
}

export function AlertThresholdsView() {
  const tr = useTr()
  const user = useAuth((s) => s.user)
  const [list, setList] = useState<Threshold[]>([])
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const data = await apiGet<Threshold[]>("/api/settings/alert-thresholds")
      setList(data || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat thresholds: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleUpdate = (metric: string, field: "warn_pct" | "crit_pct", val: number) => {
    setList(list.map((t) => (t.metric === metric ? { ...t, [field]: val } : t)))
  }

  const handleSave = async () => {
    const ok = await confirmDialog({
      title: tr("Simpan ambang peringatan?"),
      message: tr("Ambang ini berlaku global — sama untuk semua akun yang membuka dashboard."),
      confirmLabel: tr("Simpan"),
    })
    if (!ok) return
    try {
      await apiSend("/api/settings/alert-thresholds", "PUT", list)
      notify.ok(tr("Ambang batas peringatan berhasil disimpan."))
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan: {0}", pesanError(e)))
    }
  }

  return (
    <Panel
      title={tr("Alert Thresholds")}
      hint={tr("Ambang persentase warna Amber (Warning) & Merah (Critical) pada tile Dashboard (berlaku global)")}
      actions={
        user?.sudo && (
          <Button size="sm" onClick={handleSave} disabled={loading}>
            {tr("Simpan Perubahan")}
          </Button>
        )
      }
    >
      <div className="space-y-4 max-w-xl">
        {list.map((t) => (
          <div key={t.metric} className="rounded-md border border-border p-3 space-y-2">
            <p className="font-semibold uppercase tracking-wider text-xs">{t.metric}</p>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label className="text-xs text-amber-500 font-medium">{tr("Warning (Amber %)")}</label>
                <Input
                  type="number"
                  min="1"
                  max="100"
                  className="mt-1"
                  disabled={!user?.sudo}
                  value={t.warn_pct}
                  onChange={(e) => handleUpdate(t.metric, "warn_pct", Number(e.target.value))}
                />
              </div>
              <div>
                <label className="text-xs text-crit font-medium">{tr("Critical (Merah %)")}</label>
                <Input
                  type="number"
                  min="1"
                  max="100"
                  className="mt-1"
                  disabled={!user?.sudo}
                  value={t.crit_pct}
                  onChange={(e) => handleUpdate(t.metric, "crit_pct", Number(e.target.value))}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </Panel>
  )
}
