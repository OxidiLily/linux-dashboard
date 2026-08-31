import { useEffect, useState } from "react"
import { trf, useTr } from "@/stores/i18n"
import { Link } from "react-router-dom"
import { PackageX } from "lucide-react"

import { apiGet } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Panel } from "@/components/ui/panel"

type ComponentStatus = {
  name: string
  installed: boolean
  description?: string
  required_for?: string
}

/**
 * Menahan isi halaman yang butuh software tertentu di sistem.
 *
 * Tanpa penjaga ini halaman seperti Samba tampil kosong (seolah belum ada
 * share) atau melempar error mentah "command not found" — dua-duanya membuat
 * user mengira panelnya rusak, padahal software-nya memang belum dipasang.
 *
 * Dipasang di level ROUTE, bukan di dalam berkas view-nya. Sebelumnya tiap
 * view membungkus JSX-nya sendiri dengan penjaga ini, dan itu tidak pernah
 * bekerja untuk kasus yang paling penting: view-nya tetap ter-mount, jadi
 * useEffect-nya tetap memanggil API dan toast "Gagal memuat status Docker:
 * exec: docker: executable file not found in $PATH" tetap muncul — persis
 * error mentah yang penjaga ini ada untuk mencegahnya. Penjaga hanya bisa
 * menahan efek kalau ia berada di ATAS komponen yang punya efek itu.
 *
 * `label` diterjemahkan di sini, bukan oleh pemanggil, supaya definisi route
 * bisa mengoper string biasa tanpa perlu memanggil hook i18n.
 */
export function ComponentGuard({
  name,
  label,
  children,
}: {
  name: string
  label: string
  children: React.ReactNode
}) {
  const tr = useTr()
  const [status, setStatus] = useState<ComponentStatus | null>(null)
  const [siap, setSiap] = useState(false)

  useEffect(() => {
    apiGet<ComponentStatus[]>("/api/components")
      .then((list) => setStatus((list || []).find((c) => c.name === name) ?? null))
      // Gagal membaca daftar komponen bukan alasan menyembunyikan halaman:
      // biarkan halamannya jalan dan tunjukkan errornya sendiri.
      .catch(() => setStatus(null))
      .finally(() => setSiap(true))
  }, [name])

  if (!siap) return null
  if (status && !status.installed) {
    const nama = tr(label)
    return (
      <Panel title={nama} hint={tr("Belum Terpasang")}>
        <div className="flex flex-col items-center gap-3 py-10 text-center">
          <span className="flex size-12 items-center justify-center rounded-lg bg-surface-2">
            <PackageX className="size-6 text-warn" />
          </span>
          <div>
            <p className="text-sm font-semibold">{trf("{0} belum terpasang", nama)}</p>
            <p className="mx-auto mt-1 max-w-sm text-xs text-muted-foreground">
              {status.description ?? trf("Halaman ini butuh {0} terpasang di sistem.", name)}
            </p>
          </div>
          <Button asChild size="sm">
            <Link to="/settings/components">{tr("Pasang di Components")}</Link>
          </Button>
        </div>
      </Panel>
    )
  }
  return <>{children}</>
}
