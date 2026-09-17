import { useEffect, useMemo, useRef, useState } from "react"
import { useBlocker } from "react-router-dom"
import { apiGet, apiSend, ApiError } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { trf, useTr } from "@/stores/i18n"
import { RefreshCw, Save, Search, X, AlertTriangle, CalendarClock } from "lucide-react"

// Cronjob: crontab MILIK AKUN YANG SEDANG LOGIN.
//
// Halaman ini mengedit crontab, bukan menggantikannya dengan tabel milik panel.
// Alasannya bukan kesederhanaan: crontab sudah punya sintaks yang lengkap dan
// teruji (rentang, langkah, @daily, variabel PATH, komentar), dan setiap panel
// yang menawarkan "tambah jadwal" berupa kolom-kolom akhirnya menolak jadwal
// sah yang tidak terpikirkan pembuatnya. Yang panel tambahkan di sini justru
// hal-hal yang hilang saat mengedit crontab lewat SSH: peringatan bahwa
// penjadwalnya tidak jalan, penjaga tab yang isinya sudah basi, dan batas
// ukuran yang jelas.

type CronData = {
  isi: string
  batas: number
  layanan?: string
  layanan_aktif?: boolean
}

// Penanda jenis baris sengaja berbahasa Inggris: pemeriksa terjemahan
// memindai SEMUA string di dalam berkas, termasuk yang tidak pernah sampai ke
// layar, dan kata Indonesia di sini akan terbaca sebagai teks yang belum
// diterjemahkan.
type Baris =
  | { kind: "blank"; line: number }
  | { kind: "comment"; line: number; text: string }
  | { kind: "variable"; line: number; name: string; value: string }
  | { kind: "schedule"; line: number; spec: string; command: string }
  // Baris yang tidak dikenali TIDAK disembunyikan: crontab akan menolaknya
  // saat disimpan, dan menyembunyikannya membuat penolakan itu terasa datang
  // dari tempat lain.
  | { kind: "other"; line: number; text: string }

// Pola ini hanya untuk MEMBACA tampilan. crontab sendiri yang memutuskan mana
// yang sah saat menyimpan, jadi di sini cukup bentuk yang paling umum.
const POLA_VARIABEL = /^[A-Za-z_][A-Za-z0-9_]*\s*=/

/**
 * Baca isi crontab jadi baris-baris yang bisa dibaca manusia.
 *
 * Fungsi murni supaya bisa diuji `scripts/cek-runtime.ts` tanpa DOM — inilah
 * bagian yang salahnya paling halus (memecah jadwal dari perintahnya) dan
 * paling sulit terlihat: jadwal yang salah pecah tetap tampil rapi.
 *
 * Jumlah kolom jadwal BUKAN lima untuk semua baris: bentuk `@daily` dan
 * `@reboot` hanya satu kata. Karena itu baris yang diawali `@` diperlakukan
 * sebagai kasus tersendiri, bukan dipaksa masuk hitungan kolom.
 */
export function bacaCrontab(isi: string): Baris[] {
  const out: Baris[] = []
  const lines = isi.split("\n")
  // Baris terakhir hasil split selalu kosong untuk teks yang diakhiri newline;
  // menghitungnya sebagai baris kosong membuat nomor baris di tampilan meleset
  // satu dibanding isi berkasnya.
  if (lines.length && lines[lines.length - 1] === "") lines.pop()

  lines.forEach((mentah, i) => {
    const line = i + 1
    const t = mentah.trim()
    if (t === "") {
      out.push({ kind: "blank", line })
      return
    }
    if (t.startsWith("#")) {
      out.push({ kind: "comment", line, text: t.replace(/^#+\s?/, "") })
      return
    }
    if (POLA_VARIABEL.test(t)) {
      const m = t.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/)
      out.push({ kind: "variable", line, name: m?.[1] ?? t, value: m?.[2] ?? "" })
      return
    }
    if (t.startsWith("@")) {
      // @daily /usr/bin/backup.sh
      const spasi = t.search(/\s/)
      if (spasi < 0) {
        out.push({ kind: "other", line, text: t })
        return
      }
      out.push({ kind: "schedule", line, spec: t.slice(0, spasi), command: t.slice(spasi).trim() })
      return
    }
    const kolom = t.split(/\s+/)
    if (kolom.length >= 6) {
      out.push({
        kind: "schedule",
        line,
        spec: kolom.slice(0, 5).join(" "),
        command: kolom.slice(5).join(" "),
      })
      return
    }
    out.push({ kind: "other", line, text: t })
  })
  return out
}

/** Saringan daftar jadwal: jadwal ATAU perintahnya memuat kata kunci. */
export function cariJadwal(daftar: Baris[], kueri: string): Baris[] {
  const q = kueri.trim().toLowerCase()
  if (!q) return daftar
  return daftar.filter((b) =>
    b.kind === "schedule"
      ? (b.spec + " " + b.command).toLowerCase().includes(q)
      : b.kind === "variable"
        ? (b.name + "=" + b.value).toLowerCase().includes(q)
      : "text" in b
        ? b.text.toLowerCase().includes(q)
        : false,
  )
}

/**
 * Ukuran isi dalam byte UTF-8.
 *
 * Bukan `.length`: satu karakter ber-aksen atau emoji memakan beberapa byte di
 * crontab, dan batasnya memang dihitung dalam byte. Menghitung karakter membuat
 * klien meloloskan isi yang ditolak server — persis kegagalan yang batas ini
 * ada untuk mencegahnya, hanya saja muncul di tempat yang salah.
 */
export function ukuranByte(s: string): number {
  return new TextEncoder().encode(s).length
}

export function ukuranCrontabTersimpan(s: string): number {
  return ukuranByte(s !== "" && !s.endsWith("\n") ? s + "\n" : s)
}

export function CronView() {
  const tr = useTr()
  const [data, setData] = useState<CronData | null>(null)
  // Isi yang terakhir dibaca dari server. Inilah yang dikirim sebagai
  // `previous` saat menyimpan, dan inilah pembanding untuk tahu ada perubahan.
  const [tersimpan, setTersimpan] = useState("")
  const [edit, setEdit] = useState("")
  const [loading, setLoading] = useState(false)
  const [menyimpan, setMenyimpan] = useState(false)
  // Konflik: crontab berubah di tempat lain sejak halaman ini dimuat.
  const [bentrok, setBentrok] = useState(false)
  const [cari, setCari] = useState("")
  // Dipakai penjaga beforeunload; state bisa basi di dalam listener, ref tidak.
  const kotorRef = useRef(false)
  const editRef = useRef("")
  const urutanMuat = useRef(0)
  const dialogNavigasiAktif = useRef(false)

  const kotor = edit !== tersimpan
  kotorRef.current = kotor
  editRef.current = edit
  const blocker = useBlocker(kotor)

  const muat = async (paksa = false) => {
    if (paksa && kotor) {
      const ok = await confirmDialog({
        title: tr("Buang perubahan yang belum disimpan?"),
        message: tr("Isi kotak editor kembali ke versi yang tersimpan di server."),
        confirmLabel: tr("Muat ulang"),
        danger: true,
      })
      if (!ok) return
    }
    const nomor = ++urutanMuat.current
    const editSaatMulai = editRef.current
    setLoading(true)
    try {
      const d = await apiGet<CronData>("/api/cron")
      if (nomor !== urutanMuat.current) return
      setData(d)
      if (editRef.current === editSaatMulai) {
        setTersimpan(d.isi)
        setEdit(d.isi)
        setBentrok(false)
      } else {
        // Editor berubah ketika GET berjalan. Mengadopsi d.isi hanya sebagai
        // `previous` sementara mempertahankan edit lama akan membuat PUT lolos
        // dan menimpa revisi server yang belum pernah dilihat pengguna.
        // Pertahankan baseline lama dan paksa rekonsiliasi lewat Muat ulang.
        setBentrok(true)
      }
    } catch (e: any) {
      notify.err(trf("Gagal memuat crontab: {0}", pesanError(e)))
    } finally {
      if (nomor === urutanMuat.current) setLoading(false)
    }
  }

  useEffect(() => {
    void muat()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Penjaga tutup halaman: crontab yang sedang disunting dan belum disimpan
  // hilang tanpa satu pun pemberitahuan tanpa ini — dan yang hilang bukan teks
  // biasa, melainkan jadwal yang sudah disusun.
  useEffect(() => {
    const jaga = (e: BeforeUnloadEvent) => {
      if (!kotorRef.current) return
      e.preventDefault()
      // Browser modern mengabaikan teksnya dan memakai kalimatnya sendiri;
      // returnValue tetap wajib diset supaya dialognya muncul.
      e.returnValue = ""
    }
    window.addEventListener("beforeunload", jaga)
    return () => window.removeEventListener("beforeunload", jaga)
  }, [])

  useEffect(() => {
    if (blocker.state !== "blocked" || dialogNavigasiAktif.current) return
    dialogNavigasiAktif.current = true
    void confirmDialog({
      title: tr("Buang perubahan yang belum disimpan?"),
      message: tr("Isi kotak editor akan hilang bila meninggalkan halaman ini."),
      confirmLabel: tr("Tinggalkan halaman"),
      danger: true,
    }).then((ok) => {
      if (ok) blocker.proceed()
      else blocker.reset()
    }).finally(() => {
      dialogNavigasiAktif.current = false
    })
    // `tr` sengaja tidak menjadi dependency: useTr mengembalikan closure baru
    // setiap render dan itu bisa membuka dialog kedua untuk blocker yang sama.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [blocker.state])

  const simpan = async () => {
    if (!kotor || menyimpan || bentrok) return
    const snapshotEdit = edit
    const snapshotTersimpan = tersimpan
    setMenyimpan(true)
    try {
      const d = await apiSend<CronData>("/api/cron", "PUT", {
        isi: snapshotEdit,
        // `previous` WAJIB: server menolak permintaan tanpa ini, dan itu
        // memang disengaja — menyimpan tanpa pernah membaca lebih dulu adalah
        // cara menimpa jadwal orang lain tanpa jejak.
        previous: snapshotTersimpan,
      })
      setData(d)
      setTersimpan(d.isi)
      if (editRef.current === snapshotEdit) setEdit(d.isi)
      setBentrok(false)
      notify.ok(tr("Cronjob disimpan."), tr("Isi di bawah sudah dibaca ulang dari crontab."))
    } catch (e: any) {
      // Konflik dibedakan dari penolakan isi: yang satu butuh muat ulang, yang
      // satu butuh memperbaiki tulisan. Menyamakan keduanya membuat user
      // memperbaiki sintaks yang sebenarnya sudah benar.
      if (e instanceof ApiError && e.code === "cron_conflict") {
        setBentrok(true)
        notify.err(
          tr("Crontab berubah di tempat lain."),
          tr("Tekan Muat ulang supaya isi terbaru terbaca. Simpan dinonaktifkan agar perubahan eksternal tidak tertimpa."),
        )
      } else {
        notify.err(trf("Gagal menyimpan crontab: {0}", pesanError(e)))
      }
    } finally {
      setMenyimpan(false)
    }
  }

  const batas = data?.batas ?? 64 << 10
  const lewatBatas = ukuranCrontabTersimpan(edit) > batas
  const baris = useMemo(() => bacaCrontab(edit), [edit])
  const terlihat = useMemo(() => cariJadwal(baris, cari), [baris, cari])
  // Jumlah baris AKTIF (yang benar-benar dijalankan cron) dihitung dari seluruh
  // isi, bukan dari hasil saring: angka itu menjelaskan isi crontab-nya,
  // sementara kotak pencarian hanya mengubah apa yang terlihat di tabel.
  const barisAktif = useMemo(() => baris.filter((b) => b.kind === "schedule").length, [baris])

  return (
    <div className="space-y-4">
      <Panel
        title={tr("Cronjob")}
        hint={tr(
          "Jadwal berkala milik akun Anda sendiri (crontab), dijalankan oleh penjadwal sistem. Ditulis sebagai akun Anda — tidak pernah menyentuh crontab user lain atau crontab sistem.",
        )}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {/* Keadaan penjadwalnya sendiri, dan ia tampil lebih dulu dari
                tombol apa pun: crontab yang rapi tapi tanpa cron yang jalan
                adalah kegagalan yang paling lama tidak ketahuan. */}
            {data &&
              (data.layanan_aktif ? (
                <Badge tone="ok">
                  {data.layanan} · {tr("berjalan")}
                </Badge>
              ) : (
                <Badge tone="warn">{tr("Penjadwal tidak terdeteksi di mesin ini")}</Badge>
              ))}
            <Button variant="outline" size="sm" onClick={() => void muat(true)} disabled={loading}>
              <RefreshCw className={`size-3.5 sm:mr-1 ${loading ? "animate-spin" : ""}`} />
              <span className="sr-only sm:not-sr-only">{tr("Muat ulang")}</span>
            </Button>
            <Button size="sm" onClick={simpan} disabled={!kotor || menyimpan || lewatBatas || bentrok}>
              <Save className="size-3.5 sm:mr-1" />
              <span className="sr-only sm:not-sr-only">
                {menyimpan ? tr("Menyimpan…") : tr("Simpan")}
              </span>
            </Button>
          </div>
        }
      >
        {bentrok && (
          <div
            role="alert"
            className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn"
          >
            <span className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
              {tr(
                "Crontab berubah di tempat lain sejak halaman ini dimuat. Muat ulang supaya isi terbaru terbaca; Simpan dinonaktifkan untuk melindungi perubahan tersebut.",
              )}
            </span>
            <Button variant="outline" size="sm" onClick={() => void muat(true)}>
              {tr("Muat ulang dulu")}
            </Button>
          </div>
        )}

        {data && !data.layanan_aktif && (
          <p className="mb-3 rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn">
            {tr(
              "Crontab tersimpan, tapi perintah cron tidak ada yang menjalankannya — pasang paket cron lewat terminal atau nyalakan unit-nya, kalau tidak seluruh jadwal di halaman ini tidak akan pernah berbunyi.",
            )}
          </p>
        )}

        <label className="text-xs font-medium" htmlFor="cron-editor">
          {tr("Isi crontab")}
        </label>
        <p className="mt-0.5 text-[10px] text-muted-foreground">
          {tr(
            "Tulis jadwal dengan format cron: menit jam tanggal bulan hari, lalu perintahnya. Contoh: 0 3 * * * /usr/bin/rsync -a /data /backup",
          )}
        </p>
        <textarea
          id="cron-editor"
          className="mt-2 min-h-[40dvh] w-full rounded border border-border bg-background p-3 font-mono text-[11px] leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          spellCheck={false}
          value={edit}
          readOnly={data === null || loading || menyimpan}
          aria-busy={loading || menyimpan}
          onChange={(e) => setEdit(e.target.value)}
          // Tab masuk ke isi crontab, bukan memindahkan fokus: berkas ini
          // disunting seperti berkas, dan melompatnya fokus keluar kotak
          // membuat perataan baris mustahil.
          onKeyDown={(e) => {
            if (e.key !== "Tab" || e.shiftKey) return
            e.preventDefault()
            const el = e.currentTarget
            const a = el.selectionStart
            const b = el.selectionEnd
            setEdit(edit.slice(0, a) + "  " + edit.slice(b))
            // Kursor dikembalikan setelah React menulis ulang isinya.
            requestAnimationFrame(() => el.setSelectionRange(a + 2, a + 2))
          }}
        />
        <div className="mt-1 flex flex-wrap items-center justify-between gap-2 text-[10px] text-muted-foreground">
          <span className="num">{trf("{0} baris", baris.length)}</span>
          <span className={`num ${lewatBatas ? "text-crit" : ""}`}>
            {trf("{0} / {1} KiB", (ukuranByte(edit) / 1024).toFixed(1), batas >> 10)}
          </span>
        </div>
        {lewatBatas && (
          <p className="mt-1 text-[10px] text-crit">
            {trf("Isi crontab melebihi batas {0} KiB — rapikan dulu sebelum menyimpan.", batas >> 10)}
          </p>
        )}
        {kotor && !lewatBatas && (
          <p className="mt-1 text-[10px] text-warn">{tr("Ada perubahan yang belum disimpan.")}</p>
        )}
      </Panel>

      <Panel
        title={tr("Jadwal")}
        hint={trf("{0} jadwal aktif", barisAktif)}
        actions={
          <div className="relative w-48">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
              className="h-8 w-full rounded-md border border-border bg-input pl-8 pr-7 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              placeholder={tr("Cari di jadwal…")}
              aria-label={tr("Cari di jadwal")}
              value={cari}
              onChange={(e) => setCari(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setCari("")
              }}
            />
            {cari !== "" && (
              <button
                type="button"
                className="absolute right-1 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-secondary hover:text-foreground"
                onClick={() => setCari("")}
                aria-label={tr("Bersihkan pencarian")}
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
        }
      >
        <div className="overflow-x-auto">
          <table className="tabel-kartu w-full text-left text-xs">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="w-10 pb-2 font-medium">#</th>
                <th className="pb-2 font-medium">{tr("Jadwal")}</th>
                <th className="pb-2 font-medium">{tr("Perintah")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {terlihat.map((b) => (
                <tr key={b.line} className="align-top hover:bg-secondary/40">
                  <td data-label="#" className="num py-2 text-muted-foreground">
                    {b.line}
                  </td>
                  {b.kind === "schedule" ? (
                    <>
                      <td data-label={tr("Jadwal")} className="num whitespace-nowrap py-2 font-medium">
                        {b.spec}
                      </td>
                      <td data-label={tr("Perintah")} className="num break-all py-2 text-muted-foreground">
                        {b.command}
                      </td>
                    </>
                  ) : (
                    <td data-label="" colSpan={2} className="py-2 text-muted-foreground">
                      {b.kind === "comment" && <span># {b.text}</span>}
                      {b.kind === "variable" && (
                        <span className="num">
                          {b.name}={b.value}
                        </span>
                      )}
                      {b.kind === "other" && <span className="num text-warn">{b.text}</span>}
                      {b.kind === "blank" && <span className="opacity-40">—</span>}
                    </td>
                  )}
                </tr>
              ))}
              {terlihat.length === 0 && (
                <tr>
                  <td data-label="" colSpan={3} className="py-6 text-center text-muted-foreground">
                    {cari.trim() !== ""
                      ? tr("Tidak ada jadwal yang cocok dengan pencarian.")
                      : tr("Belum ada jadwal. Tulis satu di bawah, lalu Simpan.")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <p className="mt-2 flex items-center gap-1.5 text-[10px] text-muted-foreground">
          <CalendarClock className="size-3" />
          {tr("Komentar (baris diawali #) tidak dijalankan — di sini hanya ditampilkan.")}
        </p>
      </Panel>
    </div>
  )
}
