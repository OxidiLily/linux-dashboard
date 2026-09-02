import { useEffect, useRef, useState } from "react"
import { Loader2, RefreshCw, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { confirmDialog } from "@/components/ui/confirm"
import { tr, trf } from "@/stores/i18n"
import { useUpdateStore } from "@/stores/update"

type UpdateStatus = {
  running: boolean
  log: string
  result?: string
  exit: number
  lokal?: string
  remote?: string
  tertinggal: boolean
  /** Judul commit yang belum terpasang, terbaru dulu. */
  perubahan?: string[]
  /** Commit terpasang ketemu di riwayat remote, jadi daftarnya persis selisihnya. */
  perubahan_pasti?: boolean
}

// Selang polling saat pembaruan jalan. Keluarannya berupa langkah build yang
// panjang, jadi tidak ada gunanya menarik lebih rapat dari ini.
const SELANG_MS = 1500

export function UpdateModal({ onClose }: { onClose: () => void }) {
  const [st, setSt] = useState<UpdateStatus | null>(null)
  const [memuat, setMemuat] = useState(true)
  const [terputus, setTerputus] = useState(false)
  const [galat, setGalat] = useState("")
  const logRef = useRef<HTMLPreElement>(null)
  // Dipakai supaya polling langsung rapat setelah tombol ditekan, tanpa
  // menunggu status pertama datang dari server.
  const barusanMulai = useRef(false)
  // Status "sedang jalan" versi ref: loop polling hidup di luar siklus render,
  // jadi nilai state di dalamnya sudah basi begitu request pertama selesai.
  const berjalan = useRef(false)
  const setBerjalanStore = useUpdateStore((s) => s.mulai)
  const setSelesaiStore = useUpdateStore((s) => s.selesai)

  useEffect(() => {
    let hidup = true
    let timer: number | undefined

    const tarik = async (cek: boolean) => {
      try {
        // rinci=1 hanya di permintaan pertama (saat modal dibuka): daftar
        // perubahan butuh git fetch di server, dan polling log tiap 1,5 detik
        // tidak boleh ikut menariknya.
        const data = await apiGet<UpdateStatus>(
          `/api/settings/update${cek ? "?cek=1&rinci=1" : ""}`,
        )
        if (!hidup) return
        // Polling log TIDAK memakai cek=1, jadi jawabannya tidak memuat versi
        // remote maupun daftar perubahan. Ditimpa mentah-mentah, keterangan
        // "Ada versi baru" dan daftar commit-nya lenyap 1,5 detik setelah modal
        // dibuka — sisanya cuma "Terpasang: …" tanpa penjelasan apa pun.
        setSt((prev) =>
          cek || !prev
            ? data
            : {
                ...data,
                remote: prev.remote,
                tertinggal: prev.tertinggal,
                perubahan: prev.perubahan,
                perubahan_pasti: prev.perubahan_pasti,
              },
        )
        setTerputus(false)
        berjalan.current = data.running
        // Sinkronkan ke store bersama — dipakai AppShell untuk memutar
        // icon Update di sidebar selama pembaruan jalan. Ditaruh SETELAH
        // setSt supaya render berikutnya konsisten dengan status bar.
        if (data.running) setBerjalanStore()
        else if (berjalan.current === false && !barusanMulai.current) setSelesaiStore()
        if (!data.running) barusanMulai.current = false
      } catch (e: any) {
        if (!hidup) return
        // Installer me-restart web app di langkah terakhir: request gagal
        // beberapa detik adalah bagian normal dari pembaruan, bukan kegagalan.
        if (barusanMulai.current || berjalan.current) setTerputus(true)
        else setGalat(pesanError(e))
      } finally {
        if (hidup) setMemuat(false)
      }
      if (hidup) timer = window.setTimeout(() => tarik(false), SELANG_MS)
    }

    tarik(true)
    return () => {
      hidup = false
      if (timer) window.clearTimeout(timer)
      // Modal ditutup sebelum pembaruan selesai — biarkan store tetap
      // "berjalan" supaya sidebar tetap berputar sampai server bilang
      // selesai (dilakukan oleh siklus polling komponen berikutnya yang
      // mount, atau saat user buka modal lagi). Tidak dipanggil di sini.
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Log mengalir ke bawah seperti terminal.
  useEffect(() => {
    const el = logRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [st?.log])

  const jalan = st?.running || terputus || barusanMulai.current

  const mulai = async () => {
    const ok = await confirmDialog({
      title: tr("Jalankan pembaruan panel?"),
      message: tr(
        "Sumber ditarik ulang dari GitHub, dibangun ulang, lalu kedua service di-restart. Panel akan terputus sebentar di akhir proses, dan build bisa memakan beberapa menit di mesin kecil.",
      ),
      confirmLabel: tr("Perbarui"),
      danger: true,
    })
    if (!ok) return
    setGalat("")
    barusanMulai.current = true
    setBerjalanStore()
    try {
      const data = await apiSend<UpdateStatus>("/api/settings/update", "POST")
      berjalan.current = true
      setSt(data)
    } catch (e: any) {
      barusanMulai.current = false
      setSelesaiStore()
      setGalat(pesanError(e))
    }
  }

  const selesaiGagal = !!st && !st.running && !!st.result && st.result !== "success"

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="update-title"
    >
      <div className="flex max-h-[85dvh] w-full max-w-2xl flex-col rounded-lg border border-border bg-surface shadow-xl">
        <div className="flex items-start gap-3 border-b border-border p-4">
          <div className="min-w-0 flex-1">
            <p id="update-title" className="text-sm font-semibold">
              {tr("Pembaruan panel")}
            </p>
            <p className="mt-1 truncate text-xs text-muted-foreground">
              {memuat
                ? tr("Memeriksa versi…")
                : st?.lokal
                  ? trf("Terpasang: {0}", st.lokal)
                  : tr("Versi terpasang tidak terbaca — sumber belum ada di mesin ini.")}
            </p>
            {!memuat && !!st && (
              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                {/* Remote kosong = `git ls-remote` gagal. Dibiarkan tanpa baris
                    sama sekali, modal jadi diam soal satu-satunya hal yang
                    dicarinya: apakah ada versi baru. */}
                {!st.remote
                  ? tr("Versi di GitHub tidak terbaca — cek koneksi mesin ini ke github.com.")
                  : st.tertinggal
                    ? trf("Ada versi baru di GitHub: {0}", st.remote)
                    : tr("Sudah versi terbaru.")}
              </p>
            )}
          </div>
          <button
            className="rounded-md p-1.5 text-muted hover:bg-accent hover:text-foreground"
            aria-label={tr("Tutup")}
            onClick={onClose}
          >
            <X className="size-4" />
          </button>
        </div>

        {!!st?.perubahan?.length && (
          <div className="max-h-56 overflow-auto border-b border-border px-4 py-3">
            <p className="text-xs font-medium">
              {st.perubahan_pasti
                ? trf("Yang akan ikut terpasang ({0} commit)", st.perubahan.length)
                : trf("Commit terbaru di GitHub ({0})", st.perubahan.length)}
            </p>
            {!st.perubahan_pasti && (
              /* Riwayat lokal tidak menyambung ke remote — daftarnya tetap
                 ditampilkan, tapi jangan mengaku sebagai selisih yang persis. */
              <p className="mt-0.5 text-[11px] text-muted-2">
                {tr("Versi terpasang tidak ditemukan di riwayat itu, jadi sebagian mungkin sudah ada di mesin ini.")}
              </p>
            )}
            <ul className="mt-2 space-y-1">
              {st.perubahan.map((baris) => {
                const spasi = baris.indexOf(" ")
                const sha = spasi > 0 ? baris.slice(0, spasi) : baris
                const judul = spasi > 0 ? baris.slice(spasi + 1) : ""
                return (
                  <li key={baris} className="flex gap-2 text-xs">
                    <span className="num shrink-0 text-muted-2">{sha}</span>
                    <span className="min-w-0 break-words text-muted-foreground">{judul}</span>
                  </li>
                )
              })}
            </ul>
          </div>
        )}

        <pre
          ref={logRef}
          className="num min-h-24 flex-1 overflow-auto whitespace-pre-wrap break-words bg-surface-2 p-3 text-[11px] leading-relaxed text-muted-foreground"
        >
          {st?.log || tr("Belum ada pembaruan yang dijalankan di mesin ini.")}
        </pre>

        <div className="flex items-center gap-2 border-t border-border p-3">
          <p className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
            {galat
              ? galat
              : terputus
                ? tr("Service sedang di-restart — menunggu panel hidup lagi…")
                : jalan
                  ? tr("Pembaruan berjalan. Modal boleh ditutup, prosesnya jalan terus di server.")
                  : selesaiGagal
                    ? trf("Pembaruan gagal (exit {0}). Baca ekor log di atas.", st!.exit)
                    : st?.result === "success"
                      ? tr("Pembaruan selesai. Muat ulang halaman untuk memakai versi baru.")
                      : ""}
          </p>
          <Button variant="ghost" onClick={onClose}>
            {tr("Tutup")}
          </Button>
          <Button onClick={mulai} disabled={jalan || memuat}>
            {jalan ? (
              <Loader2 className="mr-1 size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="mr-1 size-3.5" />
            )}
            {jalan ? tr("Memperbarui…") : tr("Perbarui sekarang")}
          </Button>
        </div>
      </div>
    </div>
  )
}
