import { useCallback, useEffect, useRef, useState } from "react"
import { ArrowUpCircle, RefreshCw } from "lucide-react"
import { apiGet } from "@/lib/api"
import { Panel } from "@/components/ui/panel"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { perbaruiKomponen } from "@/lib/perbarui"
import { useTr } from "@/stores/i18n"

// Daftar update datang dari cache scan backend (lihat scanUpdateOtomatis):
// endpoint bisa menjawab "masih menghitung" dengan daftar kosong, jadi
// halaman ini poll ulang sampai datanya ada — sama seperti pola Components.
type ItemUpdate = {
  name: string
  installed: boolean
  version?: string
  latest_version?: string
  category?: string
}

export function UpdatesView() {
  const tr = useTr()
  const [items, setItems] = useState<ItemUpdate[]>([])
  const [loading, setLoading] = useState(false)
  const [sudahCek, setSudahCek] = useState(false)
  const [memperbarui, setMemperbarui] = useState<string | null>(null)
  const seq = useRef(0)

  const muat = useCallback(async () => {
    const s = ++seq.current
    setLoading(true)
    try {
      const data = await apiGet<ItemUpdate[]>("/api/components/updates")
      if (s !== seq.current) return
      setItems((data ?? []).filter((c) => c.installed && c.latest_version))
      setSudahCek(true)
    } catch {
      // Gagal memuat bukan masalah kritis — coba lagi pada poll berikutnya.
    } finally {
      if (s === seq.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    muat()
    // Backend menulis cache tiap 2 menit; poll 1 menit cukup untuk
    // menangkap rilis baru tanpa membanjiri endpoint.
    const id = setInterval(() => muat(), 60 * 1000)
    return () => clearInterval(id)
  }, [muat])

  const jalankan = async (name: string) => {
    setMemperbarui(name)
    await perbaruiKomponen(name)
    setMemperbarui(null)
    muat()
  }

  return (
    <Panel
      title={tr("Pembaruan tersedia")}
      hint={tr("Komponen terpasang yang punya versi baru. Pemeriksaan berjalan otomatis di backend tiap 2 menit — halaman ini menampilkan hasilnya.")}
      actions={
        <Button variant="outline" size="sm" onClick={() => muat()} disabled={loading}>
          <RefreshCw className={loading ? "mr-1 size-3.5 animate-spin" : "mr-1 size-3.5"} />
          {tr("Muat ulang")}
        </Button>
      }
    >
      <div className="self-start overflow-x-auto rounded-lg border border-border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/30 text-muted-foreground">
              <th className="p-2.5 font-medium">{tr("Komponen")}</th>
              <th className="p-2.5 font-medium">{tr("Versi terpasang")}</th>
              <th className="p-2.5 font-medium">{tr("Versi tersedia")}</th>
              <th className="p-2.5 text-right font-medium">{tr("Aksi")}</th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <td colSpan={4} data-label="" className="p-6 text-center text-muted-foreground">
                  {!sudahCek
                    ? tr("Memeriksa pembaruan…")
                    : tr("Tidak ada pembaruan — semua komponen sudah versi terbaru.")}
                </td>
              </tr>
            ) : (
              items.map((c) => (
                <tr key={c.name} className="transition-colors hover:bg-secondary/40">
                  <td data-label="Komponen" className="p-2.5">
                    <span className="flex items-center gap-2">
                      {c.name}
                      {c.category && (
                        <Badge tone="muted" className="px-1.5 py-0 text-[10px]">
                          {c.category}
                        </Badge>
                      )}
                    </span>
                  </td>
                  <td data-label="Versi terpasang" className="num p-2.5 text-muted-foreground">
                    {c.version || "—"}
                  </td>
                  {/* "commits" berarti build git, bukan nomor rilis — jangan
                      diberi awalan "v" supaya tidak terbaca sebagai versi semver. */}
                  <td data-label="Versi tersedia" className="num p-2.5">
                    <Badge tone="warn">
                      {c.latest_version === "commits"
                        ? tr("Commit baru tersedia")
                        : /[^0-9.]/.test(c.latest_version ?? "")
                          ? c.latest_version
                          : `v${c.latest_version}`}
                    </Badge>
                  </td>
                  <td data-label="" className="p-2.5 text-right">
                    <Button
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={memperbarui !== null}
                      onClick={() => jalankan(c.name)}
                    >
                      <ArrowUpCircle
                        className={memperbarui === c.name ? "mr-1 size-3.5 animate-spin" : "mr-1 size-3.5"}
                      />
                      {tr("Perbarui")}
                    </Button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}
