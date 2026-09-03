import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { RefreshCw, Download, Trash2, Power, ExternalLink } from "lucide-react"

// Backend helperproto.ComponentStatus: Name, Installed, Version, Running, Service.
type ComponentStatus = {
  name: string
  installed: boolean
  version?: string
  running?: boolean
  service?: string
  category?: string
  description?: string
  /** Halaman panel yang tidak bisa dipakai tanpa komponen ini. */
  required_for?: string
  /** Keterangan yang baru relevan setelah terpasang — mis. kredensial awal 9router. */
  note?: string
  /** Komponen menyimpan data di luar paketnya, jadi uninstall bisa menawarkan menghapusnya. */
  has_data?: boolean
  /** Halaman yang memegang kendali service ini — di sini statusnya saja yang tampil. */
  managed_in?: string
}

// Fase apt dari helper ditulis sebagai kalimat, bukan satu kata teknis:
// "indeks" sendirian di bawah bar tidak memberi tahu apa pun kepada yang
// membacanya. Fase yang tidak dikenal ditampilkan apa adanya.
const ketFase: Record<string, string> = {
  indeks: "memperbarui daftar paket",
  unduh: "mengunduh paket",
  pasang: "memasang paket",
}

export function ComponentsView() {
  const tr = useTr()
  const [list, setList] = useState<ComponentStatus[]>([])
  const [loading, setLoading] = useState(false)
  // Aksi yang sedang berjalan: apt bisa makan 1–2 menit, jadi UI harus
  // menunjukkan komponen mana yang sedang dikerjakan dan sudah berapa lama.
  const [aksi, setAksi] = useState<{ name: string; jenis: "install" | "uninstall"; mulai: number } | null>(
    null,
  )
  // Kemajuan nyata dari apt, bukan hitungan detik. Angka dari stopwatch tidak
  // tahu apa-apa soal isi pekerjaannya: pada mesin lambat atau paket besar ia
  // menjanjikan sesuatu yang tidak ia ketahui.
  const [progres, setProgres] = useState<{ persen: number; fase?: string; pesan?: string } | null>(null)
  const actionLoading = aksi?.name ?? null

  useEffect(() => {
    if (!aksi) {
      setProgres(null)
      return
    }
    setProgres(null)
    let batal = false
    const tanya = () => {
      apiGet<{ name: string; persen: number; fase?: string; pesan?: string; aktif: boolean }>(
        "/api/components/progress",
      )
        .then((p) => {
          if (batal) return
          // Laporan untuk komponen lain diabaikan: instalasi yang baru saja
          // selesai bisa sempat terbaca sebelum state helper dibersihkan.
          if (p?.aktif && p.name === aksi.name)
            setProgres({ persen: p.persen, fase: p.fase, pesan: p.pesan })
        })
        .catch(() => undefined)
    }
    tanya()
    const t = setInterval(tanya, 700)
    return () => {
      batal = true
      clearInterval(t)
    }
  }, [aksi])
  // Sembunyikan yang sudah terpasang saat user cuma mencari apa yang bisa dipasang.
  const [hanyaBelum, setHanyaBelum] = useState(false)

  // fresh = paksa helper memeriksa ulang, bukan menjawab dari cache 30 detik.
  // Dipakai tombol Refresh dan setiap kali panel baru saja mengubah sesuatu;
  // pemuatan pertama halaman tetap boleh memakai cache.
  const load = async (fresh = false) => {
    setLoading(true)
    try {
      const data = await apiGet<ComponentStatus[]>(`/api/components${fresh ? "?fresh=1" : ""}`)
      setList(data || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat daftar komponen: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleInstall = async (name: string) => {
    const ok = await confirmDialog({
      title: trf("Pasang komponen {0}?", name),
      message: tr("Paket diunduh dan dipasang ke sistem. Bisa berjalan beberapa menit."),
      confirmLabel: tr("Pasang"),
    })
    if (!ok) return
    setAksi({ name, jenis: "install", mulai: Date.now() })
    try {
      await apiSend(`/api/components/${name}/install`, "POST")
      notify.ok(trf("Komponen {0} berhasil dipasang.", name))
      load(true)
    } catch (e: any) {
      notify.err(trf("Gagal memasang {0}: {1}", name, pesanError(e)))
    } finally {
      setAksi(null)
    }
  }

  const handleUninstall = async (name: string, punyaData = false) => {
    // Ditulis di luar dialog: checkbox mengirim jawabannya lewat onChange,
    // bukan lewat nilai balik promise, supaya pemanggil confirmDialog yang
    // lain tetap memakai boolean biasa.
    let hapusData = false
    const ok = await confirmDialog({
      title: trf("Hapus komponen {0} dari sistem?", name),
      message: punyaData
        ? tr("Paketnya dicopot. Data yang sudah dibuat komponen ini tetap disimpan, kecuali kamu memilih menghapusnya di bawah.")
        : tr("Paket dicopot lewat apt. Konfigurasi dan data yang sudah dibuat komponen ini tidak ikut dibersihkan."),
      checkbox: punyaData
        ? {
            label: tr(
              "Hapus data komponen ini juga — termasuk kredensial, koneksi, dan riwayatnya. Tidak bisa dibatalkan.",
            ),
            onChange: (v) => {
              hapusData = v
            },
          }
        : undefined,
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    setAksi({ name, jenis: "uninstall", mulai: Date.now() })
    try {
      await apiSend(`/api/components/${name}/uninstall${hapusData ? "?purge=1" : ""}`, "POST")
      notify.ok(trf("Komponen {0} berhasil dihapus.", name))
      load(true)
    } catch (e: any) {
      notify.err(trf("Gagal menghapus {0}: {1}", name, pesanError(e)))
    } finally {
      setAksi(null)
    }
  }

  const handleService = async (name: string, action: string) => {
    if (action !== "start") {
      const ok = await confirmDialog({
        title: trf("Jalankan \"{0}\" pada service {1}?", action, name),
        message:
          action === "stop"
            ? tr("Service berhenti sampai dinyalakan lagi secara manual.")
            : tr("Service terputus sesaat selama proses berjalan."),
        confirmLabel: action,
        danger: action === "stop",
      })
      if (!ok) return
    }
    // start/stop service jauh lebih cepat dari apt, tapi tetap dikunci lewat
    // state yang sama supaya tidak ada dua aksi berjalan bersamaan.
    setAksi({ name, jenis: "uninstall", mulai: Date.now() })
    try {
      await apiSend(`/api/components/${name}/${action}`, "POST")
      load(true)
    } catch (e: any) {
      notify.err(trf("Gagal menjalankan aksi {0}: {1}", action, pesanError(e)))
    } finally {
      setAksi(null)
    }
  }

  // Komponen yang punya antarmuka web sendiri — tombol "Buka" muncul di
  // kartunya. Portnya ada di backend (handleOpenURL), bukan di sini: yang
  // perlu diketahui halaman ini cuma komponen mana yang punya halaman.
  const punyaUIWeb = ["9router", "technitium-dns"]

  // bukaUIWeb membuka tab baru ke URL yang dikembalikan server. Pakai
  // endpoint (bukan hard-code "http://localhost:20128") supaya WSL/lxc
  // yang hostname-nya bukan "localhost" tetap mendapat tautan yang benar
  // — menjalankan langsung `window.open("http://localhost:20128")` di
  // WSL akan membuka localhost di Windows, bukan di distro-nya.
  const bukaUIWeb = async (name: string) => {
    try {
      const r = await apiGet<{ url: string }>(`/api/open-url/${name}`)
      window.open(r.url, "_blank", "noopener,noreferrer")
    } catch (e: any) {
      notify.err(trf("Tidak bisa membuka {0}: {1}", name, pesanError(e)))
    }
  }

  const terlihat = hanyaBelum ? list.filter((c) => !c.installed) : list
  // Urutan kategori mengikuti urutan kemunculan dari backend.
  const kategori: string[] = []
  for (const c of terlihat) {
    const k = c.category || "Lainnya"
    if (!kategori.includes(k)) kategori.push(k)
  }
  const terpasang = list.filter((c) => c.installed).length

  return (
    <Panel
      title={tr("Components")}
      hint={
        aksi
          ? aksi.jenis === "install"
            ? trf("Memasang {0} — jangan tutup halaman ini", aksi.name)
            : trf("Memproses {0} — jangan tutup halaman ini", aksi.name)
          : trf(
              "Software opsional yang tidak ikut di instalasi dasar Ubuntu/Debian — {0} dari {1} terpasang",
              terpasang,
              list.length,
            )
      }
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
            <input type="checkbox" checked={hanyaBelum} onChange={(e) => setHanyaBelum(e.target.checked)} />
            {tr("Hanya yang belum terpasang")}
          </label>
          {/* Bukan onClick={load}: handler-nya akan menerima MouseEvent
              sebagai argumen `fresh`. Kebetulan truthy, tapi maksudnya harus
              tertulis, bukan menumpang pada kebetulan itu. */}
          <Button variant="outline" size="sm" onClick={() => load(true)} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-5">
        {kategori.map((k) => (
          <div key={k}>
            <p className="eyebrow mb-2">{tr(k)}</p>
            <div className="space-y-2">
              {terlihat
                .filter((c) => (c.category || "Lainnya") === k)
                .map((c) => {
                  const isInstalled = !!c.installed
                  // Service kosong = paket alat baris perintah, tidak ada yang
                  // bisa dijalankan/dihentikan. managed_in = service-nya ada,
                  // tapi kendalinya milik halaman lain (cloudflared butuh token
                  // tunnel dari Settings → Network; menghidupkannya dari sini
                  // cuma menyalakan daemon tanpa tunnel — atau menghidupkan
                  // kembali tunnel lama). Statusnya tetap ditampilkan.
                  const punyaService = !!c.service
                  const bisaDikontrol = punyaService && !c.managed_in
                  const isActive = c.running
                  const sedangDikerjakan = aksi?.name === c.name
                  // Keterangan di bawah bar: fase apt kalau ada angkanya,
                  // kalau tidak pesan langkah dari helper (npm, pipx, skrip
                  // vendor — jalur yang tidak punya laporan persen).
                  const ketProgres = sedangDikerjakan
                    ? progres?.fase
                      ? ketFase[progres.fase] || progres.fase
                      : progres?.pesan
                    : undefined
                  return (
                    <div
                      key={c.name}
                      className={cn(
                        "relative flex flex-wrap items-center justify-between gap-3 overflow-hidden rounded-md border p-4",
                        sedangDikerjakan
                          ? "border-signal/50 bg-signal/5"
                          : "border-border hover:bg-secondary/40",
                      )}
                    >
                      {/* Garis penanda kartu yang sedang dikerjakan — polos dan
                          diam. Kemajuan sesungguhnya ditampilkan bar di kolom
                          aksi; dua tempat yang sama-sama mengaku menunjukkan
                          kemajuan hanya membuat mata bingung harus melihat
                          yang mana. */}
                      {sedangDikerjakan && (
                        <div className="absolute inset-x-0 top-0 h-0.5 bg-signal" aria-hidden="true" />
                      )}
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="num text-sm font-semibold">{c.name}</p>
                          {/* Selama aksinya berjalan, status lama tidak
                              ditampilkan: "Belum Terpasang" di sebelah bar yang
                              sedang berjalan membuat kartu membantah dirinya
                              sendiri — pembacanya tidak tahu mana yang benar. */}
                          <Badge
                            tone={
                              sedangDikerjakan
                                ? "signal"
                                : isInstalled
                                  ? "ok"
                                  : c.required_for
                                    ? "warn"
                                    : "muted"
                            }
                          >
                            {sedangDikerjakan
                              ? aksi?.jenis === "install"
                                ? tr("Sedang dipasang")
                                : tr("Sedang diproses")
                              : isInstalled
                                ? tr("Terpasang")
                                : tr("Belum Terpasang")}
                          </Badge>
                          {!isInstalled && c.required_for && (
                            <Badge tone="warn">
                              {tr("dibutuhkan")} {c.required_for}
                            </Badge>
                          )}
                          {isInstalled && punyaService && (
                            <Badge tone={isActive ? "signal" : "crit"}>{isActive ? tr("Aktif") : tr("Nonaktif")}</Badge>
                          )}
                          {isInstalled && c.version && (
                            <span className="num text-[10px] text-muted-foreground">{c.version}</span>
                          )}
                        </div>
                        {c.description && (
                          <p className="mt-0.5 text-xs text-muted-foreground">{tr(c.description)}</p>
                        )}
                        {/* Sebagian catatan adalah kalimat tetap (mis. syarat
                            logout setelah masuk grup docker) dan diterjemahkan;
                            sebagian lagi isinya kredensial awal buatan panel
                            yang memang tidak punya terjemahan. tr() melayani
                            keduanya: kunci yang tidak ada di kamus dikembalikan
                            apa adanya. */}
                        {c.note && (
                          <p className="num mt-1 rounded bg-surface-2 px-2 py-1 text-[11px] text-foreground">
                            {tr(c.note)}
                          </p>
                        )}
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        {sedangDikerjakan ? (
                          <div
                            className="w-44 min-w-0"
                            role="progressbar"
                            aria-valuenow={progres?.persen ?? 0}
                            aria-valuemin={0}
                            aria-valuemax={100}
                            aria-label={tr("Kemajuan pemasangan")}
                          >
                            <div className="flex items-baseline justify-between gap-2">
                              {/* Langkah yang sedang berjalan, bukan kata
                                  "Memasang" yang sudah diucapkan badge di
                                  sebelah kiri kartu. Sebelum laporan pertama
                                  datang, jenis aksinya yang dipakai — kolom ini
                                  tidak pernah kosong. */}
                              <span className="truncate text-xs text-muted-foreground">
                                {ketProgres
                                  ? tr(ketProgres)
                                  : aksi?.jenis === "install"
                                    ? tr("Memasang")
                                    : tr("Memproses")}
                              </span>
                              {!!progres?.persen && (
                                <span className="num shrink-0 text-[11px] text-muted-2">
                                  {progres.persen}%
                                </span>
                              )}
                            </div>
                            {/* Bar isian, bukan animasi: posisinya menyatakan
                                sejauh mana pekerjaannya, jadi ia harus diam
                                kalau memang sudah ada kabar dari apt. Tanpa
                                angka — npm dan skrip vendor tidak melaporkan
                                persen — yang berjalan hanya sepotong kecil.
                                Jalur yang terisi penuh terbaca sebagai 100%
                                yang menggantung, persis kebalikan dari yang
                                ingin dikatakan. */}
                            <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
                              <div
                                className={`h-full rounded-full bg-signal ${
                                  progres?.persen
                                    ? "transition-[width] duration-500 ease-out"
                                    : "bar-menunggu"
                                }`}
                                style={progres?.persen ? { width: `${progres.persen}%` } : undefined}
                              />
                            </div>
                          </div>
                        ) : isInstalled ? (
                          <>
                            {bisaDikontrol && (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={actionLoading !== null}
                                onClick={() => handleService(c.name, isActive ? "stop" : "start")}
                              >
                                <Power className="mr-1 size-3.5" /> {isActive ? tr("Hentikan") : tr("Jalankan")}
                              </Button>
                            )}
                            {c.managed_in && (
                              <span className="text-[11px] text-muted-foreground">
                                {trf("Dijalankan dari {0}", tr(c.managed_in))}
                              </span>
                            )}
                            {punyaUIWeb.includes(c.name) && (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={actionLoading !== null}
                                onClick={() => bukaUIWeb(c.name)}
                                title={trf("Buka antarmuka {0} di tab baru", c.name)}
                              >
                                <ExternalLink className="mr-1 size-3.5" /> {tr("Buka")}
                              </Button>
                            )}
                            {["hermes", "claude-code", "codex", "opencode", "openclaw"].includes(c.name) && (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={actionLoading !== null}
                                onClick={() => window.location.assign(`/ai/agent?agent=${c.name}`)}
                                title={tr("Buka terminal AI Agent")}
                              >
                                <ExternalLink className="mr-1 size-3.5" /> {tr("Buka Agent")}
                              </Button>
                            )}
                            <Button
                              variant="outline"
                              size="sm"
                              className="text-crit hover:bg-crit/10 hover:text-crit"
                              disabled={actionLoading !== null}
                              onClick={() => handleUninstall(c.name, c.has_data)}
                            >
                              <Trash2 className="mr-1 size-3.5" /> {tr("Hapus")}
                            </Button>
                          </>
                        ) : (
                          <Button
                            size="sm"
                            disabled={actionLoading !== null}
                            onClick={() => handleInstall(c.name)}
                          >
                            <Download className="mr-1 size-3.5" /> {tr("Pasang")}
                          </Button>
                        )}
                      </div>
                    </div>
                  )
                })}
            </div>
          </div>
        ))}
        {terlihat.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {list.length === 0
              ? tr("Gagal memuat daftar komponen. Pastikan helper daemon aktif.")
              : tr("Semua komponen di katalog sudah terpasang.")}
          </p>
        )}
      </div>
    </Panel>
  )
}
