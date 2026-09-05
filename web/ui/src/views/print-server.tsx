import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { Printer as PrinterIcon, Trash2, Plus, RefreshCw, Star, Power, PowerOff, X, Search, Download, CheckCircle2 } from "lucide-react"

import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"

type Printer = {
  name: string
  description?: string
  location?: string
  uri?: string
  state: string
  state_message?: string
  default: boolean
  enabled: boolean
  shared: boolean
}
type PrinterDevice = { uri: string; info?: string }
type PrinterModel = { model: string; name: string }
type PrintJob = { id: string; printer: string; user: string; title?: string; size: number }
type Deteksi = {
  uri: string
  info?: string
  vendor?: string
  produk?: string
  model?: string
  model_name?: string
  siap_pakai: boolean
  paket_driver?: string[]
  sudah_terdaftar: boolean
}

const FORM_KOSONG = { name: "", uri: "", model: "everywhere", description: "", location: "", shared: true }

export function PrintServerView() {
  const tr = useTr()
  const [list, setList] = useState<Printer[]>([])
  const [jobs, setJobs] = useState<PrintJob[]>([])
  const [loading, setLoading] = useState(false)
  const [modal, setModal] = useState(false)
  const [form, setForm] = useState(FORM_KOSONG)
  const [devices, setDevices] = useState<PrinterDevice[]>([])
  const [models, setModels] = useState<PrinterModel[]>([])
  const [scanning, setScanning] = useState(false)
  const [deteksi, setDeteksi] = useState<Deteksi[] | null>(null)
  const [mendeteksi, setMendeteksi] = useState(false)
  const [sibuk, setSibuk] = useState("")
  // Status Avahi ikut dibaca karena backend dnssd/mdns milik CUPS bergantung
  // padanya: tanpa Avahi, penemuan otomatis hanya menjangkau printer yang
  // menjawab SNMP, dan justru printer modern yang mengumumkan diri lewat mDNS
  // tidak akan pernah muncul. Kalau tidak diberi tahu, itu terbaca sebagai
  // deteksi yang rusak, bukan sebagai jalur penemuan yang memang mati.
  const [mdnsMati, setMdnsMati] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const [p, j] = await Promise.all([
        apiGet<Printer[]>("/api/print/printers"),
        apiGet<PrintJob[]>("/api/print/jobs"),
      ])
      setList(p || [])
      setJobs(j || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat printer: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    apiGet<{ name: string; installed: boolean; running: boolean }[]>("/api/components")
      .then((list) => {
        const a = (list || []).find((c) => c.name === "avahi")
        setMdnsMati(!a?.installed || !a?.running)
      })
      // Kegagalan membaca status komponen tidak boleh memunculkan peringatan
      // palsu: kalau tidak tahu, jangan mengklaim mDNS mati.
      .catch(() => setMdnsMati(false))
  }, [])

  // Antrean berubah tanpa aksi dari halaman ini — cetakan dari file manager,
  // dari mesin lain, atau pekerjaan yang selesai sendiri. Tanpa penyegaran
  // berkala, daftar job yang sudah kosong tetap terlihat penuh.
  useEffect(() => {
    const t = setInterval(() => {
      apiGet<PrintJob[]>("/api/print/jobs")
        .then((j) => setJobs(j || []))
        .catch(() => undefined)
    }, 5000)
    return () => clearInterval(t)
  }, [])

  // Penemuan perangkat memindai USB dan jaringan, jadi hanya dijalankan saat
  // dialog tambah dibuka — bukan setiap kali halaman dimuat.
  const openTambah = async () => {
    setForm(FORM_KOSONG)
    setModal(true)
    setScanning(true)
    try {
      const [d, m] = await Promise.all([
        apiGet<PrinterDevice[]>("/api/print/devices"),
        apiGet<PrinterModel[]>("/api/print/models"),
      ])
      setDevices(d || [])
      setModels(m || [])
    } catch (e: any) {
      notify.err(trf("Gagal memindai printer: {0}", pesanError(e)))
    } finally {
      setScanning(false)
    }
  }

  // Alur "colok lalu pakai": pindai perangkat, lihat driver mana yang kurang,
  // pasang drivernya, daftarkan antreannya — semuanya dari halaman ini. Tanpa
  // ini printer USB hanya muncul sebagai perangkat yang tidak bisa diapa-apakan.
  const jalankanDeteksi = async () => {
    setMendeteksi(true)
    try {
      setDeteksi((await apiGet<Deteksi[]>("/api/print/detect")) || [])
    } catch (e: any) {
      notify.err(trf("Gagal mendeteksi printer: {0}", pesanError(e)))
    } finally {
      setMendeteksi(false)
    }
  }

  const pasangDriver = async (d: Deteksi) => {
    const vendor = d.vendor || "generic"
    setSibuk(d.uri)
    try {
      // apt bisa berjalan puluhan detik; tombolnya dikunci selama itu supaya
      // tidak ada dua pemasangan berjalan bersamaan, dan toast-nya berputar
      // selama itu supaya user yang pindah halaman tetap melihat hasilnya.
      await notify.tugas(apiSend("/api/print/drivers", "POST", { vendor }), {
        jalan: trf("Memasang driver {0}…", vendor),
        sukses: trf("Driver {0} terpasang", vendor),
        gagal: (e) => trf("Gagal memasang driver: {0}", pesanError(e)),
      })
      await jalankanDeteksi()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setSibuk("")
    }
  }

  // Nama antrean diturunkan dari nama produk: CUPS menolak spasi dan garis
  // miring, dan user tidak punya alasan mengarang nama untuk printer yang
  // baru saja ia colok.
  const namaDariProduk = (d: Deteksi) => {
    const dasar = (d.produk || d.info || "printer").trim().replace(/[^A-Za-z0-9]+/g, "_").replace(/^_+|_+$/g, "")
    return dasar.slice(0, 40) || "printer"
  }

  const tambahDariDeteksi = async (d: Deteksi) => {
    setSibuk(d.uri)
    try {
      await notify.tugas(
        apiSend("/api/print/printers", "POST", {
          name: namaDariProduk(d),
          uri: d.uri,
          model: d.model,
          description: d.info || d.produk || "",
          location: "",
          shared: true,
        }),
        {
          jalan: trf("Menambah printer {0}…", namaDariProduk(d)),
          sukses: trf("Printer {0} ditambahkan", namaDariProduk(d)),
          gagal: (e) => trf("Gagal menambah printer: {0}", pesanError(e)),
        },
      )
      await Promise.all([load(), jalankanDeteksi()])
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setSibuk("")
    }
  }

  const simpan = async (ev: React.FormEvent) => {
    ev.preventDefault()
    try {
      await notify.tugas(apiSend("/api/print/printers", "POST", form), {
        jalan: trf("Menambah printer {0}…", form.name),
        sukses: trf("Printer {0} ditambahkan", form.name),
        gagal: (e) => trf("Gagal menambah printer: {0}", pesanError(e)),
      })
      setModal(false)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const hapus = async (p: Printer) => {
    const ok = await confirmDialog({
      title: trf("Hapus printer {0}?", p.name),
      message: tr("Antrean dan konfigurasinya dihapus dari CUPS. Pekerjaan yang belum tercetak ikut hilang."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend(`/api/print/printers/${encodeURIComponent(p.name)}`, "DELETE"), {
        jalan: trf("Menghapus printer {0}…", p.name),
        sukses: trf("Printer {0} dihapus", p.name),
        gagal: (e) => trf("Gagal menghapus printer: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const jadikanDefault = async (p: Printer) => {
    try {
      await notify.tugas(apiSend(`/api/print/printers/${encodeURIComponent(p.name)}/default`, "POST"), {
        jalan: trf("Menjadikan {0} printer default…", p.name),
        sukses: trf("{0} jadi printer default", p.name),
        gagal: (e) => trf("Gagal set default: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const ubahAktif = async (p: Printer) => {
    try {
      await notify.tugas(
        apiSend(`/api/print/printers/${encodeURIComponent(p.name)}/enable`, "POST", { enable: !p.enabled }),
        {
          jalan: p.enabled ? trf("Mematikan antrean {0}…", p.name) : trf("Menyalakan antrean {0}…", p.name),
          sukses: p.enabled ? trf("Antrean {0} dimatikan", p.name) : trf("Antrean {0} dinyalakan", p.name),
          gagal: (e) => trf("Gagal mengubah status antrean: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const batalkan = async (j: PrintJob) => {
    try {
      await notify.tugas(apiSend(`/api/print/jobs/${encodeURIComponent(j.id)}`, "DELETE"), {
        jalan: trf("Membatalkan cetakan {0}…", j.id),
        sukses: trf("Cetakan {0} dibatalkan", j.id),
        gagal: (e) => trf("Gagal membatalkan cetakan: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  return (
    <div className="space-y-4">
      <Panel
        title={tr("Deteksi printer")}
        hint={tr("Colok printer USB atau sambungkan ke jaringan, lalu jalankan deteksi. Driver yang kurang dipasang dari sini juga.")}
        actions={
          <Button size="sm" variant="outline" onClick={jalankanDeteksi} disabled={mendeteksi}>
            <Search className={`mr-1 size-3.5 ${mendeteksi ? "animate-pulse" : ""}`} />
            {mendeteksi ? tr("Memindai…") : tr("Deteksi")}
          </Button>
        }
      >
        {mdnsMati && (
          <div className="mb-3 rounded-md border border-warn/40 bg-warn/10 p-2.5 text-[11px] leading-relaxed">
            {tr("Penemuan lewat mDNS/Bonjour sedang mati karena komponen avahi belum aktif. Printer USB dan printer yang menjawab SNMP tetap terdeteksi, tapi printer jaringan modern mungkin tidak muncul — tambahkan lewat URI manual, atau pasang komponen avahi.")}{" "}
            <Link to="/settings/components" className="text-signal underline underline-offset-2">
              {tr("Buka Components")}
            </Link>
          </div>
        )}
        {deteksi === null ? (
          <p className="py-4 text-center text-xs text-muted-foreground">
            {tr("Belum dipindai. Tekan Deteksi untuk mencari printer yang terpasang.")}
          </p>
        ) : deteksi.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">
            {tr("Tidak ada printer terdeteksi. Pastikan kabel USB tersambung dan printer menyala.")}
          </p>
        ) : (
          <div className="space-y-2">
            {deteksi.map((d) => (
              <div
                key={d.uri}
                className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="text-sm font-semibold">{d.info || d.produk || d.uri}</p>
                    {d.sudah_terdaftar && <Badge tone="ok">{tr("sudah terdaftar")}</Badge>}
                    {!d.sudah_terdaftar && d.siap_pakai && <Badge tone="ok">{tr("driver siap")}</Badge>}
                    {!d.sudah_terdaftar && !d.siap_pakai && <Badge tone="warn">{tr("driver belum ada")}</Badge>}
                  </div>
                  <p className="num mt-0.5 break-all text-[11px] text-muted-2">{d.uri}</p>
                  {d.siap_pakai && d.model_name && (
                    <p className="mt-0.5 text-[11px] text-muted-foreground">{d.model_name}</p>
                  )}
                  {!d.siap_pakai && d.paket_driver && d.paket_driver.length > 0 && (
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      {trf("Perlu paket: {0}", d.paket_driver.join(", "))}
                    </p>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {d.sudah_terdaftar ? (
                    <span className="flex items-center gap-1 px-2 text-[11px] text-muted-foreground">
                      <CheckCircle2 className="size-3.5" /> {tr("siap dipakai")}
                    </span>
                  ) : d.siap_pakai ? (
                    <Button size="sm" disabled={sibuk === d.uri} onClick={() => tambahDariDeteksi(d)}>
                      <Plus className="mr-1 size-3.5" />
                      {sibuk === d.uri ? tr("Menambahkan…") : tr("Tambahkan")}
                    </Button>
                  ) : (
                    <Button size="sm" disabled={sibuk === d.uri} onClick={() => pasangDriver(d)}>
                      <Download className="mr-1 size-3.5" />
                      {sibuk === d.uri ? tr("Memasang…") : tr("Pasang driver")}
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <Panel
        title={tr("Printer")}
        hint={tr("Antrean cetak CUPS. Printer di sini bisa dipakai lewat menu Print di file manager.")}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={openTambah}>
              <Plus className="mr-1 size-3.5" /> {tr("Tambah Printer")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => load()} disabled={loading}>
              <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </div>
        }
      >
        <div className="space-y-3">
          {list.map((p) => (
            <div
              key={p.name}
              className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
            >
              <div className="flex min-w-0 items-start gap-3">
                <PrinterIcon className="mt-0.5 size-5 text-signal" />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="num text-sm font-semibold">{p.name}</p>
                    {p.default && <Badge tone="ok">{tr("default")}</Badge>}
                    {/* State datang dari cupsd, Enabled dari antreannya. Antrean
                        bisa "idle" tapi menolak pekerjaan kalau di-disable, jadi
                        keduanya ditampilkan terpisah. */}
                    <Badge tone={p.state === "idle" ? "ok" : p.state === "processing" ? "warn" : "crit"}>
                      {p.state}
                    </Badge>
                    {!p.enabled && <Badge tone="crit">{tr("antrean dimatikan")}</Badge>}
                    {p.shared && <Badge tone="warn">{tr("dibagikan ke jaringan")}</Badge>}
                  </div>
                  {p.description && <p className="mt-1 text-xs text-muted-foreground">{p.description}</p>}
                  <p className="num mt-0.5 text-[11px] text-muted-2">
                    {p.uri}
                    {p.location ? ` · ${p.location}` : ""}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-1">
                {!p.default && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 px-2 text-muted-foreground hover:text-foreground"
                    aria-label={trf("Jadikan {0} printer default", p.name)}
                    onClick={() => jadikanDefault(p)}
                  >
                    <Star className="size-4" />
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-foreground"
                  aria-label={p.enabled ? trf("Matikan antrean {0}", p.name) : trf("Nyalakan antrean {0}", p.name)}
                  onClick={() => ubahAktif(p)}
                >
                  {p.enabled ? <PowerOff className="size-4" /> : <Power className="size-4" />}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-crit"
                  aria-label={trf("Hapus printer {0}", p.name)}
                  onClick={() => hapus(p)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </div>
          ))}
          {list.length === 0 && !loading && (
            <p className="py-6 text-center text-xs text-muted-foreground">
              {tr("Belum ada printer terdaftar. Tambahkan satu supaya menu Print di file manager bisa dipakai.")}
            </p>
          )}
        </div>
      </Panel>

      <Panel title={tr("Antrean cetak")} hint={tr("Pekerjaan yang sedang menunggu atau sedang dicetak")}>
        <div className="space-y-2">
          {jobs.map((j) => (
            <div
              key={j.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2.5"
            >
              <div className="min-w-0">
                <p className="num text-xs font-semibold">{j.title || j.id}</p>
                <p className="text-[11px] text-muted-foreground">
                  {j.id} · {j.printer} · {j.user}
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-muted-foreground hover:text-crit"
                aria-label={trf("Batalkan cetakan {0}", j.id)}
                onClick={() => batalkan(j)}
              >
                <X className="size-4" />
              </Button>
            </div>
          ))}
          {jobs.length === 0 && (
            <p className="py-4 text-center text-xs text-muted-foreground">{tr("Antrean kosong.")}</p>
          )}
        </div>
      </Panel>

      {modal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="text-sm font-semibold">{tr("Tambah Printer")}</p>
            <form onSubmit={simpan} className="mt-3 space-y-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Nama antrean")}</label>
                <Input
                  className="mt-1"
                  required
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="Printer_Depan"
                />
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Huruf, angka, titik, dan garis saja — CUPS menolak spasi.")}
                </p>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">
                  {scanning ? tr("Perangkat (memindai…)") : tr("Perangkat")}
                </label>
                <select
                  className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                  value={form.uri}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      uri: e.target.value,
                      // Driver bawaan hanya masuk akal untuk printer jaringan.
                      model: e.target.value.startsWith("usb://") && form.model === "everywhere" ? "" : form.model,
                    })
                  }
                >
                  <option value="">{tr("— pilih perangkat terdeteksi —")}</option>
                  {devices.map((d) => (
                    <option key={d.uri} value={d.uri}>
                      {d.info ? `${d.info} — ${d.uri}` : d.uri}
                    </option>
                  ))}
                </select>
                <Input
                  className="mt-2"
                  required
                  value={form.uri}
                  onChange={(e) => setForm({ ...form, uri: e.target.value })}
                  placeholder="socket://192.168.2.60:9100"
                />
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Printer jaringan tanpa penemuan otomatis bisa diisi manual, mis. socket://IP:9100 atau ipp://IP/ipp/print.")}
                </p>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Driver")}</label>
                <select
                  className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                  value={form.model}
                  onChange={(e) => setForm({ ...form, model: e.target.value })}
                >
                  <option value="">{tr("— pilih driver —")}</option>
                  <option value="everywhere">IPP Everywhere (driverless)</option>
                  {models.map((m) => (
                    <option key={m.model} value={m.model}>
                      {m.name}
                    </option>
                  ))}
                </select>
                {/* Kombinasi yang pasti gagal, dan gagalnya tidak terlihat:
                    CUPS menerima antrean USB dengan driver "everywhere" tanpa
                    keluhan, lalu setiap cetakan keluar sebagai halaman kosong
                    atau job yang menggantung. Lebih baik dicegat di sini. */}
                {form.uri.startsWith("usb://") && form.model === "everywhere" ? (
                  <p className="mt-1 text-[10px] text-crit">
                    {tr("Printer USB tidak mendukung IPP Everywhere — pilih driver yang cocok dari daftar, kalau tidak cetakan akan keluar kosong.")}
                  </p>
                ) : (
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    {tr("Printer terbitan 2015 ke atas hampir selalu jalan dengan IPP Everywhere tanpa driver tambahan.")}
                  </p>
                )}
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Deskripsi")}</label>
                <Input
                  className="mt-1"
                  value={form.description}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Lokasi")}</label>
                <Input
                  className="mt-1"
                  value={form.location}
                  onChange={(e) => setForm({ ...form, location: e.target.value })}
                  placeholder={tr("Ruang depan")}
                />
              </div>
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={form.shared}
                  onChange={(e) => setForm({ ...form, shared: e.target.checked })}
                />
                {tr("Bagikan ke jaringan (klien lain bisa mencetak lewat server ini)")}
              </label>
              <div className="flex justify-end gap-2 pt-2">
                <Button type="button" variant="outline" size="sm" onClick={() => setModal(false)}>
                  {tr("Batal")}
                </Button>
                <Button type="submit" size="sm">
                  {tr("Tambah Printer")}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
