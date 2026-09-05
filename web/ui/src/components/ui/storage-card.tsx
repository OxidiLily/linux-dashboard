import * as React from "react"
import { ChevronRight } from "lucide-react"

import { cn } from "@/lib/utils"
import { trf, useTr } from "@/stores/i18n"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

interface StorageCategory {
  name: string
  size: number
  color: string
}

interface ApplicationItem {
  name: string
  size: number
  detail?: string
  href?: string
  /** Baris yang membuka dialog, bukan pindah halaman. */
  onClick?: () => void
  /**
   * Tombol aksi di ujung kanan baris. Dirender SEBELAHAN dengan bagian yang
   * bisa diklik, bukan di dalamnya: tombol di dalam <a>/<button> bukan HTML
   * yang sah, dan kliknya akan ikut menjalankan aksi barisnya.
   */
  actions?: React.ReactNode
  icon: React.ReactNode
}

// Semua field diisi pemanggilnya (views/dashboard.tsx). Nilai default hanya
// menyembunyikan pemanggil yang lupa mengisi — di sini tidak ada yang lupa.
export interface StorageCardProps extends React.ComponentProps<typeof Card> {
  title: string
  seeAllHref: string
  /** Kapasitas total, satuan bebas — dipakai apa adanya pada label `unit`. */
  totalStorage: number
  unit: string
  categories: StorageCategory[]
  applications: ApplicationItem[]
  applicationsTitle: string
  alertMessage?: React.ReactNode
}

const StorageCard = React.forwardRef<HTMLDivElement, StorageCardProps>(
  (
    {
      className,
      title,
      seeAllHref,
      totalStorage,
      unit,
      categories,
      applications,
      applicationsTitle,
      alertMessage,
      ...props
    },
    ref,
  ) => {
    const tr = useTr()
    const usedStorage = React.useMemo(
      () => categories.reduce((acc, category) => acc + category.size, 0),
      [categories],
    )

    // Bar tumbuh dari 0 saat mount — transisi CSS, tanpa framer-motion.
    const [grown, setGrown] = React.useState(false)
    React.useEffect(() => {
      const id = requestAnimationFrame(() => setGrown(true))
      return () => cancelAnimationFrame(id)
    }, [])

    return (
      <Card className={cn("w-full", className)} ref={ref} {...props}>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>{title}</CardTitle>
            <a
              href={seeAllHref}
              className="text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
            >
              {tr("Lihat semua")}
            </a>
          </div>
        </CardHeader>
        {/* flex-col, bukan grid: item grid punya min-width:auto, jadi kolomnya
            melebar mengikuti isi terlebar (path mount /dev/mapper/... yang tidak
            bisa dipatahkan) dan di HP kartunya jadi 502px di layar 375px. Pada
            flex arah kolom, batas otomatis itu berlaku pada tinggi, bukan lebar,
            sehingga isinya menyusut mengikuti kartu. Tampilannya sama. */}
        <CardContent className="flex flex-col gap-6">
          <div>
            <div
              className="relative flex h-3 w-full overflow-hidden rounded-full bg-surface-2"
              role="progressbar"
              aria-valuenow={usedStorage}
              aria-valuemin={0}
              aria-valuemax={totalStorage}
              aria-label={trf("Rincian pemakaian {0}", title.toLowerCase())}
            >
              {categories.map((category, index) => {
                const percentage = totalStorage > 0 ? (category.size / totalStorage) * 100 : 0
                return (
                  <div
                    key={category.name}
                    className={cn(
                      "h-full shrink-0 transition-[width] duration-500 ease-out motion-reduce:transition-none",
                      category.color,
                      index < categories.length - 1 && "border-r-2 border-card",
                    )}
                    style={{
                      width: grown ? `${percentage}%` : "0%",
                      transitionDelay: `${index * 100}ms`,
                    }}
                  />
                )
              })}
            </div>

            <div className="mt-4 flex flex-wrap items-center justify-between">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
                {categories.map((category) => (
                  <div key={category.name} className="flex items-center gap-2">
                    <span className={cn("h-2.5 w-2.5 rounded-full", category.color)} />
                    <span className="truncate">{category.name}</span>
                  </div>
                ))}
              </div>
              <p className="num mt-2 text-sm text-muted-foreground sm:mt-0">
                {trf("{0} {1} dari {2} {3} terpakai", usedStorage, unit, totalStorage, unit)}
              </p>
            </div>
          </div>

          {alertMessage && (
            <div className="rounded-lg border border-warn/40 bg-warn/10 p-4 text-sm text-warn">
              {alertMessage}
            </div>
          )}

          <div>
            <h3 className="text-base font-semibold text-card-foreground">{applicationsTitle}</h3>
            <div className="mt-2 overflow-hidden rounded-lg border">
              {applications.map((app, index) => {
                // Baris tanpa tujuan bukan tautan: href="#" membuatnya terlihat
                // bisa diklik (chevron, hover) lalu tidak melakukan apa pun.
                const bisaDiklik = Boolean(app.href || app.onClick)
                const Tag = app.href ? "a" : app.onClick ? "button" : "div"
                return (
                  <div
                    key={app.name}
                    className={cn(
                      "flex w-full items-center gap-2 pr-4",
                      index < applications.length - 1 && "border-b",
                    )}
                  >
                    <Tag
                      href={app.href}
                      type={app.onClick && !app.href ? "button" : undefined}
                      onClick={app.onClick}
                      className={cn(
                        "flex min-w-0 flex-1 items-center justify-between gap-4 p-4 text-left",
                        bisaDiklik && "transition-colors hover:bg-accent",
                      )}
                    >
                      <div className="flex min-w-0 items-center gap-4">
                        {app.icon}
                        <div className="min-w-0">
                          <span className="num block truncate font-medium">{app.name}</span>
                          {app.detail && (
                            <span className="block truncate text-xs text-muted-foreground">
                              {app.detail}
                            </span>
                          )}
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center gap-2 text-muted-foreground">
                        <span className="num text-sm">
                          {app.size} {unit}
                        </span>
                        {bisaDiklik && <ChevronRight className="h-4 w-4" />}
                      </div>
                    </Tag>
                    {app.actions && (
                      <div className="flex shrink-0 items-center gap-1">{app.actions}</div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        </CardContent>
      </Card>
    )
  },
)
StorageCard.displayName = "StorageCard"

export { StorageCard }
