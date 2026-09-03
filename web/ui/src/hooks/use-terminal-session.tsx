import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import "@xterm/xterm/css/xterm.css"
import { Button } from "@/components/ui/button"
import { pasangAutoFit } from "./use-terminal-fit"
import { trf, useTr } from "@/stores/i18n"

// Sesi terminal lewat WebSocket, dipakai halaman Terminal DAN halaman AI
// Agent. Sebelumnya keduanya menyimpan salinan sendiri: 102 baris identik —
// pembuatan xterm, pemetaan kode penutupan, pelepasan handler sebelum close,
// pemasangan auto-fit. Perbaikan pada salah satunya (dan sudah terjadi dua
// kali: kode 1005 yang salah dilaporkan, kotak hampa saat kuota penuh) harus
// diingat untuk disalin ke satunya, dan sekali saja lupa berarti satu halaman
// diam-diam tertinggal.
//
// Pesan per kode penutupan ikut disatukan. Dua halaman yang menuliskan
// kalimat berbeda untuk keadaan yang sama persis bukan fitur — itu dua
// terjemahan yang harus dijaga sinkron tanpa alasan.

export type TermErr = { kind: "auth" | "full" | "connect" | "unknown"; message: string }

function readToken(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || "#0A0A0A"
}

interface OpsiSesi {
  /** Perintah yang dieksekusi di PTY. Kosong = shell login biasa. */
  cmd?: string
  /** Sesi hanya dibuka kalau true — dipakai halaman AI Agent saat agent belum terpasang. */
  aktif?: boolean
  /** Teks yang ditulis ke terminal saat sesi ditutup normal. */
  labelTutup: string
  /** Dipanggil saat sesi terbuka dan saat tertutup — halaman Terminal memuat ulang kapasitas. */
  onSesi?: () => void
}

export function useTerminalSession({ cmd, aktif = true, labelTutup, onSesi }: OpsiSesi) {
  const tr = useTr()
  const containerRef = useRef<HTMLDivElement>(null)
  const [err, setErr] = useState<TermErr | null>(null)
  const [retryKey, setRetryKey] = useState(0)

  // onSesi disimpan di ref, bukan masuk daftar dependensi: pemanggil yang
  // mengirim fungsi inline akan membuat efek ini menutup dan membuka ulang
  // sesi tiap kali komponennya render.
  const onSesiRef = useRef(onSesi)
  onSesiRef.current = onSesi

  useEffect(() => {
    if (!containerRef.current || !aktif) return
    setErr(null)

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "var(--font-mono, monospace)",
      fontSize: 13,
      // xterm butuh nilai warna literal — ambil dari token tema (TDD §4.1a).
      theme: { background: readToken("--bg"), foreground: readToken("--fg") },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(containerRef.current)

    let ws: WebSocket | null = null
    // Ukuran terakhir hasil fit, dipegang di luar `ws` karena fit pertama
    // terjadi sebelum socket ada.
    let ukuran = { cols: term.cols, rows: term.rows }

    const kirim = (msg: object) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
    }
    const kirimUkuran = () => kirim({ type: "resize", cols: ukuran.cols, rows: ukuran.rows })

    // Auto-fit dipasang SEBELUM WebSocket dibuat. Ia yang memberi kotak
    // terminal tingginya (dihitung dari posisi elemen ke tepi bawah viewport);
    // sebelum itu kotaknya setinggi nol, jadi fit menghitung rows ≈ 1. Karena
    // cols/rows di query URL adalah SATU-SATUNYA ukuran yang dipakai saat PTY
    // dibuat, urutan lama membuat PTY lahir dengan ukuran itu.
    //
    // Auto-fit juga mengurus lebar: sidebar dibuka/ditutup, banner ganti
    // password muncul, font mono selesai dimuat — semuanya mengubah ruang
    // yang tersedia tanpa satu pun event `window.resize`.
    const lepasAutoFit = pasangAutoFit({
      container: containerRef.current,
      term,
      fit: fitAddon,
      onResize: (cols, rows) => {
        ukuran = { cols, rows }
        kirimUkuran()
      },
    })

    const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
    // `sock` dipakai untuk memasang handler dan menutup koneksi; `ws` adalah
    // pegangan yang boleh null supaya `kirim` aman dipanggil dari auto-fit
    // yang sudah berjalan sebelum socket ini dibuat.
    const sock = new WebSocket(
      `${proto}//${window.location.host}/ws/terminal?cols=${ukuran.cols}&rows=${ukuran.rows}` +
        (cmd ? `&cmd=${cmd}` : ""),
    )
    ws = sock
    sock.binaryType = "arraybuffer"

    sock.onopen = () => {
      term.focus()
      // Kirim ulang ukuran terakhir. `document.fonts.ready` menyusul dengan
      // lebar sel yang sebenarnya — Geist Mono baru selesai dimuat setelah
      // cat pertama, dan fit sebelum itu memakai metrik font fallback. Fit
      // susulan itu hampir selalu lebih cepat daripada handshake WebSocket,
      // jadi frame resize-nya jatuh ke socket yang masih CONNECTING dan
      // hilang tanpa jejak. Sisanya ditanggung PTY yang sudah lahir dengan
      // ukuran benar dari query URL.
      kirimUkuran()
      onSesiRef.current?.()
    }

    sock.onmessage = (ev) => {
      term.write(typeof ev.data === "string" ? ev.data : new Uint8Array(ev.data))
    }

    // Browser WS tidak memaparkan kode HTTP close pada `error`; semuanya
    // ditangani lewat `close` di bawah supaya pesannya akurat.
    sock.onerror = () => {}

    sock.onclose = (ev) => {
      onSesiRef.current?.()
      switch (ev.code) {
        case 1000:
        case 1001:
        case 1005:
          term.write(`\r\n\x1b[90m[${labelTutup}]\x1b[0m\r\n`)
          break
        // 4409 = sesi dihapus lewat tombol "Hapus sesi" di halaman Terminal.
        // Bukan error: penutupannya diminta user sendiri, jadi ditulis sebagai
        // baris keterangan di terminal, bukan banner merah.
        case 4409:
          term.write(`\r\n\x1b[90m[${tr("Sesi terminal dihapus dari panel")}]\x1b[0m\r\n`)
          break
        case 4401:
          setErr({ kind: "auth", message: tr("Sesi login tidak valid. Muat ulang halaman untuk login ulang.") })
          break
        case 4403:
          setErr({ kind: "auth", message: tr("Akses terminal butuh sudo.") })
          break
        case 4408:
          setErr({
            kind: "full",
            message: tr(
              "Kuota sesi terminal penuh. Tutup salah satu sesi aktif, atau naikkan kapasitas di menu Settings.",
            ),
          })
          break
        case 4500:
          setErr({ kind: "connect", message: tr("Helper daemon tidak bisa memulai PTY. Periksa log helper.") })
          break
        case 4503:
          setErr({ kind: "full", message: tr("Kuota sesi terminal penuh (503). Tutup salah satu sesi aktif.") })
          break
        default:
          setErr({ kind: "unknown", message: trf("Koneksi terminal terputus (kode {0}).", ev.code) })
      }
    }

    const onData = term.onData((data) => kirim({ type: "input", data }))

    return () => {
      lepasAutoFit()
      onData.dispose()
      // Lepas handler SEBELUM close: `close()` bersifat asinkron, jadi
      // onclose-nya menyala setelah efek ini dibersihkan — dengan kode 1005
      // (tanpa status) karena penutupan datang dari sisi kita. Kalau
      // handler-nya masih terpasang, ia menulis ke Terminal yang sudah
      // di-dispose dan memunculkan "Koneksi terminal terputus (kode 1005)"
      // di sesi baru yang sebenarnya sehat — persis yang terlihat user saat
      // menekan tombol refresh.
      sock.onclose = null
      sock.onmessage = null
      sock.onerror = null
      sock.close()
      term.dispose()
    }
    // labelTutup & tr sengaja di luar daftar: keduanya hanya berubah saat
    // bahasa panel diganti, dan itu tidak layak memutus sesi yang berjalan.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [retryKey, cmd, aktif])

  return { containerRef, err, bukaUlang: () => setRetryKey((k) => k + 1) }
}

/** Banner error sesi terminal, sama untuk halaman Terminal dan AI Agent. */
export function TerminalError({ err, onRetry }: { err: TermErr; onRetry: () => void }) {
  const tr = useTr()
  return (
    <div className="mx-2 mb-3 space-y-2 rounded border border-crit/30 bg-crit/10 px-3 py-2 text-xs text-crit">
      <p className="font-semibold">
        {err.kind === "full"
          ? tr("Kuota sesi terminal penuh")
          : err.kind === "auth"
          ? tr("Sesi tidak valid")
          : err.kind === "connect"
          ? tr("Gagal memulai sesi")
          : tr("Koneksi terminal gagal")}
      </p>
      <p>{err.message}</p>
      <Button size="sm" variant="outline" onClick={onRetry}>
        {tr("Coba lagi")}
      </Button>
    </div>
  )
}
