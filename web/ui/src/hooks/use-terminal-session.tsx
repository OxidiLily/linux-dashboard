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
//
// ---- Sesi bertahan saat pindah halaman (2026-09-05) ----------------------
//
// Dulu seluruh sesi hidup di dalam useEffect: pindah halaman meng-unmount
// komponennya, cleanup memanggil sock.close(), dan di sisi server penutupan
// WebSocket itu menjatuhkan `defer stream.Close()` yang MEMBUNUH PTY-nya.
// Akibatnya `apt install`, build, atau sesi AI Agent yang sedang berjalan
// mati begitu user membuka menu lain — dan kembali ke halaman itu memberi
// shell baru yang kosong, seolah pekerjaannya tidak pernah ada.
//
// Sekarang sesi (xterm + WebSocket + elemen host-nya) disimpan di peta
// tingkat modul, di luar daur hidup komponen React mana pun. Halaman hanya
// menyediakan SLOT; elemen host-nya dipindahkan masuk saat halaman dibuka dan
// diparkir ke penampung tersembunyi saat halaman ditinggalkan. Karena elemen
// dan WebSocket-nya sama persis, PTY tidak pernah tahu ada yang berpindah
// halaman — isi layar, riwayat gulir, dan proses yang berjalan utuh.
//
// ponytail: bertahan untuk PERPINDAHAN HALAMAN, bukan reload penuh (F5) —
// reload membuang seluruh JS beserta WebSocket-nya. Jalan naiknya kalau itu
// diminta: PTY dipegang server dengan ring buffer keluaran, lalu klien
// menyambung ulang lewat id sesi. Itu menuntut protokol attach/detach di
// internal/api/ws.go dan penyimpanan riwayat per sesi di server.

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

/** Satu sesi terminal yang hidup lebih lama daripada komponen penampilnya. */
type SesiHidup = {
  kunci: string
  /** "shell" atau "agent" — lihat batasSatuPerKelompok. */
  kelompok: string
  /** Elemen tempat xterm dipasang; dipindah antara slot halaman dan penampung parkir. */
  node: HTMLDivElement
  term: Terminal
  fit: FitAddon
  ws: WebSocket
  ukuran: { cols: number; rows: number }
  /** Pelepas auto-fit; hanya terpasang selama sesi sedang ditampilkan. */
  lepasAutoFit: (() => void) | null
  err: TermErr | null
  /** Sudah ditutup server (shell keluar, kuota, sesi dihapus) — tidak bisa dipakai lagi. */
  selesai: boolean
  /** Dipasang komponen yang sedang menampilkan sesi ini. */
  onErr?: (e: TermErr | null) => void
  onSesi?: () => void
}

const sesiHidup = new Map<string, SesiHidup>()

/**
 * Penampung tersembunyi tempat elemen terminal diparkir selama tidak ada
 * halaman yang menampilkannya. Harus tetap di dalam document: elemen yang
 * dilepas sepenuhnya membuat xterm kehilangan pengukuran dan ResizeObserver
 * tidak pernah menyala lagi saat dipasang kembali.
 */
function penampungParkir(): HTMLElement {
  let el = document.getElementById("terminal-parkir")
  if (!el) {
    el = document.createElement("div")
    el.id = "terminal-parkir"
    el.style.display = "none"
    document.body.appendChild(el)
  }
  return el
}

function buangSesi(kunci: string) {
  const s = sesiHidup.get(kunci)
  if (!s) return
  sesiHidup.delete(kunci)
  // Ditandai selesai supaya cleanup efek yang menyusul TIDAK memarkir ulang
  // node yang sudah dibuang. Urutannya nyata: tombol "Coba lagi" memanggil
  // buangSesi langsung dari onClick, dan cleanup efek render lama baru jalan
  // sesudahnya — tanpa penanda ini, satu elemen mati tertinggal selamanya di
  // penampung parkir setiap kali tombol itu ditekan.
  s.selesai = true
  s.lepasAutoFit?.()
  // Handler dilepas SEBELUM close — alasannya sama dengan versi lama:
  // close() asinkron, onclose menyala sesudah ini dan akan menandai sesi yang
  // sudah tidak ada.
  s.ws.onclose = null
  s.ws.onmessage = null
  s.ws.onerror = null
  s.ws.close()
  s.term.dispose()
  s.node.remove()
}

/**
 * batasSatuPerKelompok menutup sesi lain dalam kelompok yang sama.
 *
 * Kuota sesi terminal server dihitung dari jumlah core (2 core → 2 sesi),
 * jadi sesi yang bertahan lintas halaman TIDAK boleh menumpuk. Berpindah
 * agent di halaman AI Agent adalah pilihan sadar user untuk memakai yang
 * lain — sesi agent sebelumnya ditutup. Yang tidak ditutup hanyalah sesi
 * kelompok berbeda (shell vs agent), karena keduanya memang dua tempat kerja
 * yang berbeda.
 */
function batasSatuPerKelompok(kelompok: string, kunciAktif: string) {
  for (const [kunci, s] of sesiHidup) {
    if (s.kelompok === kelompok && kunci !== kunciAktif) buangSesi(kunci)
  }
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

  // tr dipegang di ref supaya sesi yang dibuat sekali tetap memakai fungsi
  // terjemahan terbaru saat menulis pesan penutupan, tanpa membuat efek di
  // bawah bergantung padanya.
  const trRef = useRef(tr)
  trRef.current = tr

  const kunci = cmd || "shell"

  useEffect(() => {
    const slot = containerRef.current
    // aktif=false berarti halaman AI Agent memutuskan agent ini belum
    // terpasang. Sesi yang mungkin sempat dibuat untuk kunci ini tidak boleh
    // ikut diparkir: ia akan memegang slot kuota server selamanya tanpa satu
    // pun halaman yang menampilkannya. Slot kosong (`!slot`) beda perkara —
    // itu render pertama sebelum kotaknya ada, dan sesinya menyusul.
    if (!aktif) {
      buangSesi(kunci)
      return
    }
    if (!slot) return

    const kelompok = cmd ? "agent" : "shell"
    batasSatuPerKelompok(kelompok, kunci)

    let s = sesiHidup.get(kunci)
    if (!s) s = buatSesi(kunci, kelompok, cmd, labelTutup, trRef)

    // Ambil alih tampilan: elemen host dipindah dari penampung parkir (atau
    // dari slot halaman sebelumnya) ke slot halaman ini.
    slot.appendChild(s.node)
    s.onErr = setErr
    s.onSesi = () => onSesiRef.current?.()
    setErr(s.err)

    // Auto-fit hanya hidup selama sesi ditampilkan. Selama diparkir,
    // elemennya berada di dalam penampung display:none — getBoundingClientRect
    // di sana mengembalikan nol dan tinggi hasil hitungannya jadi ngawur.
    // container = SLOT halaman, bukan node xterm. pasangAutoFit menulis tinggi
    // ke elemen yang diamatinya; kalau yang ditulis node xterm, slot induknya
    // tetap setinggi nol dan `overflow-hidden` miliknya menyembunyikan seluruh
    // terminal. Node xterm mengisi slot lewat width/height 100%.
    s.lepasAutoFit = pasangAutoFit({
      container: slot,
      term: s.term,
      fit: s.fit,
      onResize: (cols, rows) => {
        s.ukuran = { cols, rows }
        kirimUkuran(s)
      },
    })
    s.term.focus()

    const sesi = s
    return () => {
      sesi.lepasAutoFit?.()
      sesi.lepasAutoFit = null
      sesi.onErr = undefined
      sesi.onSesi = undefined
      // Sesi yang sudah ditutup server tidak ada gunanya diparkir: kunjungan
      // berikutnya harus mendapat shell baru, bukan layar mati berisi pesan
      // penutupan dari kunjungan sebelumnya.
      if (sesi.selesai) {
        buangSesi(sesi.kunci)
        return
      }
      penampungParkir().appendChild(sesi.node)
    }
    // labelTutup sengaja di luar daftar: ia hanya berubah saat bahasa panel
    // diganti, dan itu tidak layak memutus sesi yang berjalan.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [retryKey, cmd, aktif, kunci])

  return {
    containerRef,
    err,
    bukaUlang: () => {
      buangSesi(kunci)
      setErr(null)
      setRetryKey((k) => k + 1)
    },
  }
}

function kirim(s: SesiHidup, msg: object) {
  if (s.ws.readyState === WebSocket.OPEN) s.ws.send(JSON.stringify(msg))
}

function kirimUkuran(s: SesiHidup) {
  kirim(s, { type: "resize", cols: s.ukuran.cols, rows: s.ukuran.rows })
}

function buatSesi(
  kunci: string,
  kelompok: string,
  cmd: string | undefined,
  labelTutup: string,
  trRef: { current: (s: string) => string },
): SesiHidup {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: "var(--font-mono, monospace)",
    fontSize: 13,
    // xterm butuh nilai warna literal — ambil dari token tema (TDD §4.1a).
    theme: { background: readToken("--bg"), foreground: readToken("--fg") },
  })
  const fit = new FitAddon()
  term.loadAddon(fit)

  const node = document.createElement("div")
  node.style.width = "100%"
  node.style.height = "100%"
  // Dipasang ke penampung parkir lebih dulu supaya term.open() punya elemen
  // yang benar-benar ada di document; pemanggil langsung memindahkannya ke
  // slot halaman sesudah ini.
  penampungParkir().appendChild(node)
  term.open(node)

  // Ukuran awal PTY. Fit yang sebenarnya menyusul begitu elemennya masuk slot
  // halaman dan punya ukuran — tapi cols/rows di query URL adalah SATU-SATUNYA
  // ukuran yang dipakai saat PTY dibuat, jadi angka di sini tetap dikirim.
  const ukuran = { cols: term.cols, rows: term.rows }

  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  const ws = new WebSocket(
    `${proto}//${window.location.host}/ws/terminal?cols=${ukuran.cols}&rows=${ukuran.rows}` +
      (cmd ? `&cmd=${cmd}` : ""),
  )
  ws.binaryType = "arraybuffer"

  const s: SesiHidup = {
    kunci,
    kelompok,
    node,
    term,
    fit,
    ws,
    ukuran,
    lepasAutoFit: null,
    err: null,
    selesai: false,
  }
  sesiHidup.set(kunci, s)

  const setErr = (e: TermErr | null) => {
    s.err = e
    s.onErr?.(e)
  }

  ws.onopen = () => {
    // Kirim ulang ukuran terakhir. `document.fonts.ready` menyusul dengan
    // lebar sel yang sebenarnya — Geist Mono baru selesai dimuat setelah cat
    // pertama, dan fit sebelum itu memakai metrik font fallback. Fit susulan
    // itu hampir selalu lebih cepat daripada handshake WebSocket, jadi frame
    // resize-nya jatuh ke socket yang masih CONNECTING dan hilang tanpa jejak.
    kirimUkuran(s)
    s.onSesi?.()
  }

  ws.onmessage = (ev) => {
    term.write(typeof ev.data === "string" ? ev.data : new Uint8Array(ev.data))
  }

  // Browser WS tidak memaparkan kode HTTP close pada `error`; semuanya
  // ditangani lewat `close` di bawah supaya pesannya akurat.
  ws.onerror = () => {}

  ws.onclose = (ev) => {
    // Penutupan datang dari SERVER (shell keluar, kuota, sesi dihapus). Yang
    // datang dari sisi kita selalu melepas handler ini lebih dulu di
    // buangSesi, jadi tidak ada kode 1005 palsu yang sampai ke sini.
    s.selesai = true
    // Dinamai `tr`, bukan `t`: pemeriksa terjemahan repo ini mengenali
    // pemanggilan lewat nama itu. Diberi nama lain, kelima kalimat di bawah
    // jatuh jadi temuan INDIREK meski terjemahannya jelas ada di kamus.
    const tr = trRef.current
    s.onSesi?.()
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
    // Sesi yang tidak sedang ditampilkan siapa pun langsung dibuang: tidak ada
    // yang akan membaca pesan penutupannya, dan membiarkannya di peta membuat
    // kunjungan berikutnya mendapat layar mati.
    if (!s.onErr) buangSesi(kunci)
  }

  term.onData((data) => kirim(s, { type: "input", data }))
  return s
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
