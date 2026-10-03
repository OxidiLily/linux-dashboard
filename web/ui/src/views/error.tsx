import { isRouteErrorResponse, Link, useRouteError } from "react-router-dom"
import { useTr } from "@/stores/i18n"
import { AlertTriangle, Loader2, WifiOff } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// Halaman error dipakai dua tempat: rute catch-all (404) dan errorElement
// router (crash render / respons error dari loader). Keduanya memakai badan
// yang sama, cuma beda pembungkus — di dalam shell vs satu halaman penuh.

// Nilai di sini disimpan dalam bahasa Indonesia dan diterjemahkan saat
// dirender (lihat Badan) — tabel ini dievaluasi sekali saat modul dimuat,
// jauh sebelum preferensi bahasa user diketahui.
const PESAN: Record<number, { judul: string; detail: string }> = {
  400: { judul: "Permintaan tidak valid", detail: "Server menolak permintaan ini." },
  403: { judul: "Akses ditolak", detail: "Akun ini tidak punya izin membuka halaman tersebut. Butuh hak sudo?" },
  404: { judul: "Halaman tidak ditemukan", detail: "Alamat yang dibuka tidak ada di dashboard ini." },
  500: { judul: "Server bermasalah", detail: "Permintaan gagal diproses. Cek log service lindash di server." },
  502: { judul: "Gateway bermasalah", detail: "Reverse proxy tidak bisa menghubungi dashboard." },
  503: { judul: "Layanan tidak tersedia", detail: "Service sedang dimulai ulang atau kelebihan beban." },
}

const UMUM = { judul: "Terjadi kesalahan", detail: "Ada yang gagal saat memuat halaman ini." }

function Badan({ status, pesanError, umum }: { status: number; pesanError?: string; umum?: boolean }) {
  const tr = useTr()
  const { judul, detail } = (!umum && PESAN[status]) || UMUM
  const judulT = tr(judul)
  const detailT = tr(detail)
  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-4 text-center">
      <span className="flex size-12 items-center justify-center rounded-lg bg-surface-2">
        <AlertTriangle className={cn("size-6", status >= 500 ? "text-crit" : "text-warn")} />
      </span>
      <div>
        <p className="num text-4xl font-semibold tracking-tight">{status}</p>
        <h1 className="mt-1 text-lg font-semibold">{judulT}</h1>
        <p className="mt-1.5 text-sm text-muted">{detailT}</p>
      </div>
      {pesanError && (
        <pre className="num max-h-40 w-full overflow-auto whitespace-pre-wrap rounded-md border border-border bg-background p-3 text-left text-[11px] text-muted-foreground">
          {pesanError}
        </pre>
      )}
      <div className="flex flex-wrap items-center justify-center gap-2">
        <Button asChild size="sm">
          <Link to="/">{tr("Kembali ke dashboard")}</Link>
        </Button>
        {status >= 500 && (
          <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
            {tr("Muat ulang")}
          </Button>
        )}
      </div>
    </div>
  )
}

/** Rute tak dikenal — dirender di dalam app shell supaya sidebar tetap ada. */
export function NotFoundView() {
  return (
    <div className="flex min-h-[60dvh] items-center justify-center p-6">
      <Badan status={404} />
    </div>
  )
}

/** errorElement router: shell ikut gagal, jadi halaman penuh berdiri sendiri. */
export function RouteErrorView() {
  const error = useRouteError()
  // Exception saat render bukan respons server: statusnya tetap 500 (tidak ada
  // yang lebih tepat), tapi pesannya jangan menuduh server bermasalah.
  const dariServer = isRouteErrorResponse(error)
  const status = dariServer ? error.status : 500
  const pesanError = dariServer
    ? error.statusText || undefined
    : error instanceof Error
      ? error.message
      : undefined
  return (
    <div className="flex min-h-dvh items-center justify-center bg-bg p-6">
      <Badan status={status} pesanError={pesanError} umum={!dariServer} />
    </div>
  )
}

/**
 * Halaman disconnect: overlay penuh di atas shell selama stream realtime
 * (/ws/metrics) putus. Dua keadaan, satu tampilan:
 *   - masih menyambung → spinner, tanpa aksi (backoff berjalan otomatis);
 *   - menyerah (gagalSambung) → ikon putus + tombol muat ulang.
 * Konten shell di belakangnya sudah diblur + aria-hidden oleh app-shell.
 */
export function OverlayTerputus({ gagalSambung }: { gagalSambung: boolean }) {
  const tr = useTr()
  const judul = gagalSambung ? tr("Koneksi terputus") : tr("Menyambungkan…")
  return (
    <div
      role="alertdialog"
      aria-live="polite"
      aria-label={judul}
      className="absolute inset-x-0 bottom-0 top-11 z-20 flex items-center justify-center bg-bg/60 p-6"
    >
      <div className="w-full max-w-sm rounded-xl border border-border bg-surface p-6 text-center shadow-xl">
        <span className="mx-auto flex size-12 items-center justify-center rounded-lg bg-surface-2">
          {gagalSambung ? (
            <WifiOff className="size-6 text-crit" />
          ) : (
            <Loader2 className="size-6 animate-spin text-muted" />
          )}
        </span>
        <h1 className="mt-4 text-base font-semibold">{judul}</h1>
        <p className="mt-1.5 text-sm text-muted">
          {gagalSambung
            ? tr("Server tidak merespons — data di layar ini mungkin sudah basi. Muat ulang halaman setelah layanan pulih.")
            : tr("Mencoba menyambungkan kembali ke server…")}
        </p>
        {gagalSambung && (
          <div className="mt-4 flex justify-center">
            <Button size="sm" onClick={() => window.location.reload()}>
              {tr("Hubungkan kembali")}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
