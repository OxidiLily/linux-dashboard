import { lazy, Suspense, useState } from "react"
import { useMetrics } from "@/stores/metrics"
import { useAuth } from "@/stores/auth"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Meter } from "@/components/ui/meter"
import { Sparkline } from "@/components/ui/sparkline"
import { Badge } from "@/components/ui/badge"
import { MetricTile } from "@/components/ui/metric-tile"
import { NilaiSkalaFlow } from "@/components/ui/nilai-flow"
import { StorageCard } from "@/components/ui/storage-card"
import { DiskPrepareModal } from "@/components/ui/disk-prepare-modal"
import { HardDrive, Unplug, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { UnusedDisk } from "@/lib/types"
import { cn } from "@/lib/utils"
import { formatBytes, formatPercent, formatRate, formatUptime, formatTanggal, pecahRate } from "@/lib/format"

// Seluruh pustaka chart (@visx, d3-array, motion — ~233 kB) masuk lewat satu
// modul ini, dan modulnya di-lazy-load. Dashboard ada di chunk utama supaya
// halaman pertama setelah login tidak menunggu unduhan; mengimpor chart
// secara langsung akan membebankan 233 kB itu ke SETIAP user pada first
// paint, termasuk yang langsung membuka halaman lain.
const LiveMetricChart = lazy(() => import("@/components/ui/live-metric-chart"))

// Kerangka setinggi chart supaya tata letak panel tidak melompat saat
// chunk-nya selesai diunduh.
function KerangkaChart({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded bg-surface-2", className)} aria-hidden />
}

// Byte → GB (basis 1024), 1 desimal — StorageCard menampilkan angka apa adanya.
function gb(bytes: number): number {
  return Number((bytes / 1024 ** 3).toFixed(1))
}

// Mount yang boleh dilepas dari panel: hanya mount data di /mnt atau /media,
// dan bukan yang punya halaman pengelolanya sendiri (pool mergerfs, klien
// NFS). Pagar yang sama ditegakkan ulang di helper — ini hanya menyembunyikan
// tombol yang pasti ditolak.
export function bisaDilepas(mount: string, fstype: string): boolean {
  if (!mount.startsWith("/mnt/") && !mount.startsWith("/media/")) return false
  return !fstype.startsWith("fuse.") && !fstype.startsWith("nfs") && fstype !== "cifs"
}

// Segmen storage netral putih (PRD §4.3); warna hanya saat lewat threshold.
function diskColor(level: string): string {
  if (level === "crit") return "bg-crit"
  if (level === "warn") return "bg-warn"
  return "bg-foreground"
}

export function DashboardView() {
  const tr = useTr()
  const snap = useMetrics((s) => s.snapshot)
  const levelFor = useMetrics((s) => s.levelFor)
  const cpuHistory = useMetrics((s) => s.cpuHistory)
  const ramHistory = useMetrics((s) => s.ramHistory)
  const rxHistory = useMetrics((s) => s.rxHistory)
  const txHistory = useMetrics((s) => s.txHistory)
  const system = useMetrics((s) => s.system)
  const user = useAuth((s) => s.user)
  const sudo = user?.sudo ?? false
  // Disk yang sedang disiapkan lewat dialog. Hook dipanggil sebelum early
  // return di bawah — urutan hook tidak boleh berubah antar render.
  const [diskDisiapkan, setDiskDisiapkan] = useState<UnusedDisk | null>(null)
  // Melepas mount = pekerjaan kernel yang bisa memakan beberapa detik (dan
  // lebih lama lagi kalau disknya sudah dicabut), jadi lewat notify.tugas.
  const lepasDisk = async (mount: string, lupakan: boolean) => {
    const ok = await confirmDialog({
      title: lupakan ? trf("Lepas dan lupakan {0}?", mount) : trf("Lepas {0}?", mount),
      message: lupakan
        ? tr("Mount dilepas, barisnya dibuang dari /etc/fstab, lalu folder mount point-nya dihapus. Isi disknya TIDAK dihapus — pasang lagi lewat baris disk yang belum di-mount di daftar ini.")
        : tr("Mount dilepas sekarang. Barisnya di /etc/fstab dibiarkan, jadi disknya terpasang lagi setelah server boot."),
      confirmLabel: lupakan ? tr("Lepas & lupakan") : tr("Lepas mount"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend("/api/storage/disks/unmount", "POST", { mountpoint: mount, lupakan }), {
        jalan: trf("Melepas {0}…", mount),
        sukses: lupakan ? trf("{0} dilepas dan dilupakan.", mount) : trf("{0} dilepas.", mount),
        gagal: (e) => trf("Gagal melepas {0}: {1}", mount, pesanError(e)),
      })
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas. Daftar mount ikut
      // metrik yang menyegarkan diri sendiri, jadi tidak ada yang perlu
      // dimuat ulang dari sini.
    }
  }
  if (!snap) {
    return (
      <div className="panel p-8 text-center text-sm text-muted-foreground">
        {tr("Menunggu data metrik dari server…")}
      </div>
    )
  }

  // Disk yang terpasang tapi belum diformat/di-mount tidak punya angka
  // pemakaian, jadi tidak ikut total storage — tapi tetap ditampilkan, kalau
  // tidak disk yang baru ditambah di hypervisor seolah tidak terbaca panel.
  const unusedDisks = snap.unused_disks ?? []

  const fullDisks = snap.disks.filter((d) => levelFor("storage", d.used_pct) !== "idle").map((d) => d.mount)

  const cpuLevel = levelFor("cpu", snap.cpu.total_pct)
  const ramLevel = levelFor("ram", snap.memory.used_pct)
  // Dipakai dua kali di panel GPU: WSL tidak punya sumber metrik GPU sama
  // sekali, jadi baik saat kosong maupun saat kartunya ketemu, penjelasannya
  // berbeda dari mesin biasa.
  const diWSL = system?.platform?.platform_type === "wsl2" || system?.platform?.platform_type === "wsl1"

  const diskTotal = snap.disks.reduce((a, d) => a + d.total, 0)
  const diskUsed = snap.disks.reduce((a, d) => a + d.used, 0)
  const diskPct = diskTotal > 0 ? (diskUsed / diskTotal) * 100 : 0
  const diskLevel = levelFor("storage", diskPct)
  const gpu = snap.gpus[0]

  const netAgg = snap.network.reduce(
    (acc, n) => ({
      rx_rate: acc.rx_rate + n.rx_rate,
      tx_rate: acc.tx_rate + n.tx_rate,
      rx_bytes: acc.rx_bytes + n.rx_bytes,
      tx_bytes: acc.tx_bytes + n.tx_bytes,
    }),
    { rx_rate: 0, tx_rate: 0, rx_bytes: 0, tx_bytes: 0 },
  )

  return (
    <div className="space-y-5">
      {diskDisiapkan && (
        <DiskPrepareModal disk={diskDisiapkan} onClose={() => setDiskDisiapkan(null)} />
      )}
      {/* Baris pertama dashboard — PRD §4.3.1 & §5.2. */}
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">
          {trf("Selamat datang kembali, {0}", user?.username ?? "—")}
        </h1>
        <p className="mt-1 text-sm text-muted">
          {formatTanggal(new Date())} · <span className="num">{system?.hostname ?? "—"}</span>
        </p>
        {system?.platform && (
          <p className="mt-0.5 text-xs text-muted-2">{system.platform.display}</p>
        )}
      </header>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricTile
          label="CPU"
          angka={snap.cpu.total_pct}
          satuan="%"
          value={formatPercent(snap.cpu.total_pct)}
          sub={`${snap.cpu.cores} core · load ${snap.cpu.load1.toFixed(2)}`}
          state={cpuLevel}
        />
        <MetricTile
          label="RAM"
          angka={snap.memory.used_pct}
          satuan="%"
          value={formatPercent(snap.memory.used_pct)}
          sub={`${formatBytes(snap.memory.used)} / ${formatBytes(snap.memory.total)}`}
          state={ramLevel}
        />
        <MetricTile
          label={tr("Storage")}
          angka={diskPct}
          satuan="%"
          desimal={0}
          value={formatPercent(diskPct, 0)}
          sub={
            unusedDisks.length > 0
              ? trf(
                  "{0} / {1} · {2} mount · {3} disk belum dipakai",
                  formatBytes(diskUsed),
                  formatBytes(diskTotal),
                  snap.disks.length,
                  unusedDisks.length,
                )
              : `${formatBytes(diskUsed)} / ${formatBytes(diskTotal)} · ${snap.disks.length} mount`
          }
          state={diskLevel}
        />
        <MetricTile
          label="GPU"
          // Tanpa GPU tidak ada angka yang bisa dianimasikan — tile jatuh ke
          // teks "—" lewat jalur `value`.
          angka={gpu ? gpu.utilization_pct : undefined}
          satuan="%"
          desimal={0}
          value={gpu ? formatPercent(gpu.utilization_pct, 0) : "—"}
          sub={gpu ? gpu.name : tr("tidak terdeteksi")}
          state={gpu ? levelFor("gpu", gpu.utilization_pct) : undefined}
        />
      </div>

      {/* items-start: tiap kartu setinggi isinya sendiri.
          Default grid adalah `stretch`, sehingga panel sempit di tiap baris
          (RAM, GPU, Ringkasan) ikut diregangkan setinggi panel lebar di
          sebelahnya. Dulu selisihnya kecil karena CPU memakai sparkline 36 px;
          setelah diganti live chart 112 px, RAM meregang ~76 px melewati
          isinya dan menyisakan ruang kosong di dalam bordernya. */}
      {/* Dua kolom vertikal, bukan grid tiga kolom.
          Grid meletakkan kartu per BARIS: kartu pendek di baris yang sama
          dengan kartu tinggi menyisakan ruang mati di bawahnya sampai baris
          berikutnya mulai — dan CSS grid tidak punya cara mengemasnya ke atas
          (masonry belum didukung luas). Dengan dua kolom yang masing-masing
          menumpuk sendiri, RAM tetap di tempatnya sementara GPU dan Ringkasan
          naik merapat ke kartu di atasnya.

          Konsekuensi di layar sempit (< xl): urutannya jadi CPU, Storage,
          Network lebih dulu, baru RAM, GPU, Ringkasan — bukan berselang-seling
          seperti sebelumnya. */}
      <div className="grid items-start gap-4 xl:grid-cols-3">
        <div className="min-w-0 space-y-4 xl:col-span-2">
        <Panel title="CPU" hint={snap.cpu.model} level={cpuLevel}
          actions={<Badge tone="muted">{snap.cpu.cores} core</Badge>}>
          <p className="num text-xs text-muted-foreground">
            load {snap.cpu.load1.toFixed(2)} · {snap.cpu.load5.toFixed(2)} · {snap.cpu.load15.toFixed(2)}
          </p>
          <Suspense fallback={<KerangkaChart className="mt-3 h-28" />}>
            <LiveMetricChart
              className="mt-3 h-28"
              data={cpuHistory}
              value={snap.cpu.total_pct}
              formatValue={(v) => formatPercent(v, 0)}
            />
          </Suspense>
          <Meter className="mt-1" value={snap.cpu.total_pct} level={cpuLevel} label={tr("Pemakaian CPU total")} />
          <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
            {snap.cpu.per_core.map((core, i) => (
              <div key={i}>
                <div className="mb-1 flex justify-between text-[0.6875rem] text-muted-foreground">
                  <span className="num">cpu{i}</span>
                  <span className="num">{core.toFixed(0)}%</span>
                </div>
                <Meter value={core} level={levelFor("cpu", core)} ticks={false} label={trf("Core {0}", i)} />
              </div>
            ))}
          </div>
        </Panel>

        <StorageCard
          title={tr("Storage")}
          seeAllHref="/files"
          unit="GB"
          totalStorage={gb(snap.disks.reduce((a, d) => a + d.total, 0))}
          categories={snap.disks.map((d) => ({
            name: d.mount,
            size: gb(d.used),
            color: diskColor(levelFor("storage", d.used_pct)),
          }))}
          applicationsTitle={`Mount point (${snap.disks.length})`}
          applications={[
            ...snap.disks.map((d) => ({
              name: d.mount,
              size: gb(d.used),
              detail: trf(
                "{0} · {1} · sisa {2} · {3}",
                d.device,
                d.fstype,
                formatBytes(d.free),
                formatPercent(d.used_pct, 0),
              ),
              href: `/files?path=${encodeURIComponent(d.mount)}`,
              actions:
                sudo && bisaDilepas(d.mount, d.fstype) ? (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 px-2 text-muted-foreground hover:text-foreground"
                      title={tr("Lepas mount ini (kembali setelah boot)")}
                      aria-label={trf("Lepas {0}", d.mount)}
                      onClick={() => lepasDisk(d.mount, false)}
                    >
                      <Unplug className="size-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 px-2 text-muted-foreground hover:text-crit"
                      title={tr("Lepas lalu buang barisnya dari /etc/fstab")}
                      aria-label={trf("Lepas dan lupakan {0}", d.mount)}
                      onClick={() => lepasDisk(d.mount, true)}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </>
                ) : undefined,
              icon: (
                <div className="flex h-10 w-10 items-center justify-center rounded-lg border bg-secondary">
                  <HardDrive className="h-5 w-5 text-muted-foreground" />
                </div>
              ),
            })),
            // size 0: kapasitasnya belum bisa dipakai, jadi tidak boleh ikut
            // mengisi bar pemakaian di atas.
            ...unusedDisks.map((d) => ({
              name: d.path,
              size: 0,
              detail: sudo
                ? trf("{0} · belum di-mount — klik untuk format & mount{1}", formatBytes(d.size), d.model ? ` · ${d.model}` : "")
                : trf("{0} · belum di-mount{1}", formatBytes(d.size), d.model ? ` · ${d.model}` : ""),
              // Format disk butuh sudo; tanpa itu barisnya hanya informasi.
              onClick: sudo ? () => setDiskDisiapkan(d) : undefined,
              icon: (
                <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-dashed bg-secondary">
                  <HardDrive className="h-5 w-5 text-muted-2" />
                </div>
              ),
            })),
          ]}
          alertMessage={
            fullDisks.length > 0
              ? trf("Kapasitas hampir penuh pada {0} — bebaskan ruang sebelum layanan gagal menulis.", fullDisks.join(", "))
              : undefined
          }
        />

        <Panel title="Network" hint="ethernet">
          <div className="flex flex-wrap items-end gap-x-8 gap-y-2">
            <div>
              <p className="eyebrow">{tr("Masuk")}</p>
              <p className="num text-xl font-semibold"><NilaiSkalaFlow angka={pecahRate(netAgg.rx_rate)} /></p>
            </div>
            <div>
              <p className="eyebrow">{tr("Keluar")}</p>
              <p className="num text-xl font-semibold"><NilaiSkalaFlow angka={pecahRate(netAgg.tx_rate)} /></p>
            </div>
            <p className="num ml-auto text-xs text-muted-foreground">
              {tr("total")} ↓ {formatBytes(netAgg.rx_bytes)} · ↑ {formatBytes(netAgg.tx_bytes)}
            </p>
          </div>
          {/* Dua chart terpisah, bukan dua garis dalam satu chart: titik "now"
              disintesis komponen hanya sebagai { time, value }, sehingga garis
              kedua akan terjun ke nol di ujungnya tiap frame (lihat getY di
              live-line.tsx yang mengembalikan 0 untuk key yang hilang). */}
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            <Suspense fallback={<KerangkaChart className="h-24" />}>
              <LiveMetricChart
                className="h-24"
                data={rxHistory}
                value={netAgg.rx_rate}
                stroke="var(--color-signal)"
                formatValue={formatRate}
              />
            </Suspense>
            <Suspense fallback={<KerangkaChart className="h-24" />}>
              <LiveMetricChart
                className="h-24"
                data={txHistory}
                value={netAgg.tx_rate}
                stroke="var(--color-muted)"
                formatValue={formatRate}
              />
            </Suspense>
          </div>
        </Panel>
        </div>

        <div className="min-w-0 space-y-4">
        <Panel title="RAM" level={ramLevel}>
          <p className="num text-xs text-muted-foreground">
            {formatBytes(snap.memory.used)} / {formatBytes(snap.memory.total)}
          </p>
          <Sparkline values={ramHistory.map((t) => t.value)} max={100} className="mt-3" />
          <Meter className="mt-1" value={snap.memory.used_pct} level={ramLevel} label={tr("Pemakaian RAM")} />
          <dl className="mt-4 space-y-1.5 text-xs">
            <div className="flex justify-between">
              <dt className="text-muted-foreground">{tr("Tersedia")}</dt>
              <dd className="num">{formatBytes(snap.memory.available)}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-muted-foreground">{tr("Cache")}</dt>
              <dd className="num">{formatBytes(snap.memory.cached)}</dd>
            </div>
            {snap.memory.swap_total > 0 && (
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{tr("Swap")}</dt>
                <dd className="num">
                  {formatBytes(snap.memory.swap_used)} / {formatBytes(snap.memory.swap_total)}
                </dd>
              </div>
            )}
          </dl>
        </Panel>

        <Panel title="GPU" hint={snap.gpus.length > 0 ? trf("{0} terdeteksi", snap.gpus.length) : undefined}>
          {snap.gpus.length === 0 ? (
            <div className="space-y-2 py-4 text-center text-sm text-muted-foreground">
              <p>{tr("Tidak terdeteksi")}</p>
              {/* Petunjuknya dibedakan per platform: di WSL dan container,
                  "pasang driver amdgpu/nvidia" adalah saran yang tidak bisa
                  dijalankan — GPU-nya milik host dan tidak pernah muncul di
                  /sys/class/drm milik guest. Untuk WSL disebut eksplisit
                  bahwa `apt install rocm-smi` tidak menolong, karena itu
                  langkah pertama yang wajar dicoba dan tetap gagal. */}
              <p className="text-[11px]">
                {diWSL
                  ? tr("WSL meneruskan GPU lewat /dev/dxg, bukan /sys/class/drm, jadi rocm-smi dari repo distro tidak akan melihatnya. Panel menanyakannya ke Windows lewat interop — kalau tetap kosong, pastikan powershell.exe bisa dipanggil dari WSL, lalu tunggu semenit.")
                  : system?.platform?.platform_type === "lxc" || system?.platform?.platform_type === "docker"
                  ? tr("Container ini tidak dapat passthrough GPU. Tambahkan perangkat /dev/dri dari host, atau pantau GPU dari host-nya langsung.")
                  : system?.platform?.platform_type === "vm"
                  ? tr("VM tanpa PCI passthrough hanya melihat adapter display virtual, bukan GPU. Teruskan kartunya lewat vfio di hypervisor, atau pantau GPU dari host-nya.")
                  : tr("Pastikan driver amdgpu / nvidia / i915 aktif. Alat vendor (rocm-smi untuk AMD, nvidia-smi untuk NVIDIA, intel_gpu_top untuk Intel) dipasang otomatis oleh installer saat GPU-nya terdeteksi.")}
              </p>
            </div>
          ) : (
            <ul className="space-y-4">
              {snap.gpus.map((g, i) => {
                const lvl = levelFor("gpu", g.utilization_pct)
                // Driver yang tidak mengekspos util maupun VRAM melaporkan nol,
                // dan nol tidak sama dengan "tidak terpakai": bar 0% terbaca
                // sebagai GPU menganggur, padahal angkanya memang tidak ada.
                // Angka besarnya sudah "—", jadi bar-nya ikut disembunyikan.
                const adaAngka = g.mem_total_mb > 0 || g.utilization_pct > 0
                return (
                  <li key={i}>
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                      <div className="min-w-0">
                        <p className="truncate text-sm">{g.name}</p>
                        <p className="text-[0.6875rem] text-muted-foreground">{g.vendor}</p>
                      </div>
                      <span className="num text-lg font-semibold">
                        {adaAngka ? formatPercent(g.utilization_pct, 0) : "—"}
                      </span>
                    </div>
                    {adaAngka && <Meter value={g.utilization_pct} level={lvl} label={trf("Utilisasi {0}", g.name)} />}
                    {/* Metrik dilaporkan sendiri-sendiri: driver lawas sering
                        hanya mengekspos suhu, tanpa util maupun VRAM. Suhu itu
                        dulu ikut tersembunyi karena hanya dirender bersama VRAM. */}
                    <p className="num mt-1 text-[0.6875rem] text-muted-foreground">
                      {[
                        g.mem_total_mb > 0 ? trf("VRAM {0} / {1} MB", g.mem_used_mb, g.mem_total_mb) : null,
                        g.temp_c ? `${g.temp_c.toFixed(0)}°C` : null,
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </p>
                    {!adaAngka && (
                      <p className="mt-1 text-[0.6875rem] text-muted-foreground">
                        {diWSL
                          ? tr("Nama kartu diambil dari Windows lewat interop WSL — util dan VRAM tidak ikut terbaca dari sana.")
                          : tr("Driver tidak mengekspos util/VRAM (cukup umum di amdgpu lawas atau LXC passthrough).")}
                      </p>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </Panel>

        <Panel title={tr("Ringkasan")}>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-3">
              <dt className="text-muted-foreground">{tr("Uptime")}</dt>
              <dd className="num">{formatUptime(snap.uptime)}</dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-muted-foreground">{tr("Proses")}</dt>
              <dd className="num">{snap.processes}</dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-muted-foreground">{tr("Sesi terminal")}</dt>
              <dd className="num">{system ? `${system.terminal.active} / ${system.terminal.max}` : "—"}</dd>
            </div>
          </dl>
        </Panel>
        </div>
      </div>
    </div>
  )
}