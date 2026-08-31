import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom"
import {
  LayoutDashboard,
  FolderTree,
  Share2,
  Share,
  ShieldBan,
  HardDriveDownload,
  Bookmark,
  FileClock,
  LogIn,
  UserCog,
  Network,
  ShieldAlert,
  Gauge,
  Package,
  Printer,
  Cpu,
  Container,
  SquareTerminal,
  LogOut,
  PanelLeft,
  Server,
  Loader2,
  RefreshCw,
  Trash2,
  ChevronsUpDown,
  Bot,
} from "lucide-react"
import { apiGet } from "@/lib/api"
import { prefetchRute } from "@/router/lazy-routes"
import { useAuth } from "@/stores/auth"
import { useMetricsSocket } from "@/hooks/use-metrics-socket"
import { useMetrics } from "@/stores/metrics"
import { StatusDot } from "@/components/ui/status-dot"
import { Button } from "@/components/ui/button"
import { ConfirmHost } from "@/components/ui/confirm"
import { PromptHost } from "@/components/ui/prompt"
import { Byline } from "@/components/ui/byline"
import { UpdateModal } from "@/components/ui/update-modal"
import { useUpdateStore } from "@/stores/update"
import { UninstallModal } from "@/components/ui/uninstall-modal"
import { notify } from "@/components/ui/toast"
import { Toaster } from "@/components/ui/sonner"
import { formatJam, setFormatPrefs } from "@/lib/format"
import { tr, useT } from "@/stores/i18n"
import { usePrefs } from "@/stores/prefs"
import { TimezonePicker } from "@/components/ui/timezone-picker"
import { cn } from "@/lib/utils"
import type { ComponentType } from "react"

// Penanda antar-reload: toast "tersambung kembali" harus tampil setelah
// halaman dimuat ulang, bukan sepersekian detik sebelum reload membuangnya.
const TOAST_TERSAMBUNG = "lindash:tersambung-kembali" // i18n-abaikan: kunci sessionStorage

// tersembunyi: rute tetap terdaftar supaya breadcrumb-nya benar, tapi item
// menunya tidak digambar di sidebar — pintu masuknya ada di menu profil.
type NavItem = {
  to: string
  label: string
  icon: ComponentType<{ className?: string }>
  sudo?: boolean
  tersembunyi?: boolean
}
type NavGroup = { group: string; items: NavItem[] }

const NAV: NavGroup[] = [
  { group: "nav.home", items: [{ to: "/", label: "nav.dashboard", icon: LayoutDashboard }] },
  {
    group: "nav.fileManagerGroup",
    items: [
      { to: "/files", label: "nav.fileManager", icon: FolderTree },
      { to: "/files/samba", label: "nav.samba", icon: Share2, sudo: true },
      { to: "/files/pool", label: "nav.diskPool", icon: HardDriveDownload, sudo: true },
      { to: "/files/nfs", label: "nav.nfs", icon: Share, sudo: true },
      { to: "/files/bookmarks", label: "nav.bookmarks", icon: Bookmark },
    ],
  },
  {
    group: "nav.ai",
    items: [
      { to: "/ai/agent", label: "nav.aiAgent", icon: Bot },
    ],
  },
  {
    group: "nav.logs",
    items: [
      { to: "/logs/file-operations", label: "nav.fileOperations", icon: FileClock },
      { to: "/logs/activity", label: "nav.activityLogs", icon: LogIn },
    ],
  },
  {
    group: "nav.settings",
    items: [
      { to: "/settings/account", label: "nav.account", icon: UserCog, tersembunyi: true },
      { to: "/settings/network", label: "nav.network", icon: Network },
      { to: "/settings/firewall", label: "nav.firewall", icon: ShieldAlert, sudo: true },
      { to: "/settings/fail2ban", label: "nav.fail2ban", icon: ShieldBan, sudo: true },
      { to: "/settings/alerts", label: "nav.alerts", icon: Gauge },
      { to: "/settings/print", label: "nav.printServer", icon: Printer, sudo: true },
      { to: "/settings/components", label: "nav.components", icon: Package, sudo: true },
    ],
  },
  {
    group: "nav.system",
    items: [
      { to: "/system/processes", label: "nav.processes", icon: Cpu },
      { to: "/system/docker", label: "nav.docker", icon: Container, sudo: true },
      { to: "/system/terminal", label: "nav.terminal", icon: SquareTerminal },
    ],
  },
]

export function AppShell() {
  const user = useAuth((s) => s.user)
  const logout = useAuth((s) => s.logout)
  const connected = useMetrics((s) => s.connected)
  const system = useMetrics((s) => s.system)
  const loadStatic = useMetrics((s) => s.loadStatic)
  const t = useT()
  const bahasa = usePrefs((s) => s.bahasa)
  const setBahasa = usePrefs((s) => s.setBahasa)
  const timezone = usePrefs((s) => s.timezone)
  const setTimezone = usePrefs((s) => s.setTimezone)
  const muatPrefs = usePrefs((s) => s.muat)
  const navigate = useNavigate()
  const location = useLocation()

  useMetricsSocket()

  // Data statis (hostname, platform, ambang alert) dimuat di shell, bukan di
  // view Dashboard: membuka deep link ke halaman lain berarti Dashboard tidak
  // pernah dirender, sehingga sidebar akan selamanya menampilkan nama
  // sementara dan ambang alert tidak pernah terisi.
  // Tombol Update hanya muncul kalau memang ada versi baru di repo. Cek versi
  // memanggil `git ls-remote` di server, jadi tidak dijalankan tiap render —
  // tapi juga tidak boleh selambat sebelumnya (sekali per 30 menit), karena
  // yang menunggu kabarnya biasanya orang yang barusan push sendiri.
  const cekTerakhir = useRef(0)
  // Versi ref dari adaUpdate: dipakai untuk mengenali PERUBAHAN "tidak ada" →
  // "ada" tanpa membaca state di dalam callback yang nilainya sudah basi.
  const adaUpdateRef = useRef(false)
  const cekUpdate = useCallback(async () => {
    if (!user?.sudo) return
    // Pagar antar-pemicu: interval, tab yang kembali aktif, dan fokus window
    // bisa berbunyi hampir bersamaan — cukup satu ls-remote per menit.
    if (Date.now() - cekTerakhir.current < 60 * 1000) return
    cekTerakhir.current = Date.now()
    try {
      const st = await apiGet<{ tertinggal: boolean; running: boolean }>(
        "/api/settings/update?cek=1",
      )
      const ada = st.tertinggal || st.running
      // Kabar versi baru datang sendiri, tanpa perlu memuat ulang halaman.
      // Toast, bukan modal: pembaruan bukan hal mendesak yang pantas
      // menghentikan pekerjaan yang sedang berjalan di panel.
      if (ada && !adaUpdateRef.current && !st.running) {
        notify.info(tr("Versi baru panel tersedia."), tr("Buka menu Update di sidebar untuk memasangnya."))
      }
      adaUpdateRef.current = ada
      setAdaUpdate(ada)
    } catch {
      // Panel tetap dipakai walau pengecekan versi gagal (offline, GitHub
      // tidak terjangkau) — tombolnya saja yang tidak muncul.
    }
  }, [user?.sudo])

  useEffect(() => {
    cekUpdate()
    const id = window.setInterval(cekUpdate, 5 * 60 * 1000)
    // Tab yang ditinggal tidak perlu dicek: browser menahan timer-nya, dan
    // pengecekan yang penting justru yang terjadi saat user kembali melihat
    // panel — mis. tepat setelah push selesai.
    const kembali = () => {
      if (document.visibilityState === "visible") cekUpdate()
    }
    document.addEventListener("visibilitychange", kembali)
    window.addEventListener("focus", kembali)
    return () => {
      window.clearInterval(id)
      document.removeEventListener("visibilitychange", kembali)
      window.removeEventListener("focus", kembali)
    }
  }, [cekUpdate])

  useEffect(() => {
    loadStatic().catch(() => undefined)
    muatPrefs().catch(() => undefined)
  }, [loadStatic, muatPrefs])

  // Koneksi realtime putus → halaman diblur + overlay. Lewat batas ini
  // overlay berhenti "menyambungkan" dan menawarkan reload manual.
  const [gagalSambung, setGagalSambung] = useState(false)
  // Sambungan pertama saat halaman baru dibuka bukan "putus lalu pulih", jadi
  // ref ini menandai kapan halaman sudah basi dan perlu dimuat ulang begitu
  // koneksi kembali: setelah sempat tersambung, atau setelah gagal menunggu.
  const perluRefresh = useRef(false)
  useEffect(() => {
    if (connected) {
      // Pulih → muat ulang supaya semua view mengambil data segar, bukan sisa
      // state (dan fetch statis yang gagal) dari sebelum koneksi hilang.
      if (perluRefresh.current) {
        // Toast dititipkan ke sessionStorage: reload di baris berikutnya akan
        // membuang state React, jadi notifikasinya baru dimunculkan setelah
        // halaman baru selesai dimuat.
        sessionStorage.setItem(TOAST_TERSAMBUNG, "1")
        window.location.reload()
        return
      }
      perluRefresh.current = true
      setGagalSambung(false)
      return
    }
    const t = setTimeout(() => {
      perluRefresh.current = true
      setGagalSambung(true)
    }, 60000)
    return () => clearTimeout(t)
  }, [connected])

  useEffect(() => {
    if (sessionStorage.getItem(TOAST_TERSAMBUNG)) {
      sessionStorage.removeItem(TOAST_TERSAMBUNG)
      notify.ok(tr("Tersambung kembali."))
    }
  }, [])

  // Di bawah lg sidebar adalah drawer yang menutupi konten, jadi membukanya
  // sejak awal berarti user HP mendarat di layar yang isinya cuma menu.
  const [open, setOpen] = useState(() => window.innerWidth >= 1024)
  const [updateBuka, setUpdateBuka] = useState(false)
  const [adaUpdate, setAdaUpdate] = useState(false)
  const [profilBuka, setProfilBuka] = useState(false)
  const profilRef = useRef<HTMLDivElement>(null)
  const [uninstallBuka, setUninstallBuka] = useState(false)
  const [now, setNow] = useState(new Date())
  const updateBerjalan = useUpdateStore((s) => s.berjalan)
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(t)
  }, [])

  // Jam topbar = jam server: selisih server vs lokal dihitung sekali saat
  // /api/system/info masuk, lalu tick lokal 1 detik (TDD §4.1b).
  const skewMs = useMemo(() => {
    if (!system?.server_time) return 0
    const t = Date.parse(system.server_time)
    return isNaN(t) ? 0 : t - Date.now()
  }, [system?.server_time])
  const serverNow = new Date(now.getTime() + skewMs)
  // Format tanggal/jam di SELURUH aplikasi mengikuti preferensi ini.
  useEffect(() => {
    setFormatPrefs({ timezone, bahasa })
  }, [timezone, bahasa])


  const visible = NAV.map((g) => ({
    ...g,
    items: g.items.filter((i) => !i.tersembunyi && (!i.sudo || user?.sudo)),
  })).filter(
    (g) => g.items.length > 0,
  )

  const crumb = useMemo(() => {
    for (const g of NAV) {
      for (const i of g.items) {
        if (i.to === location.pathname) return { group: g.group, label: i.label }
      }
    }
    // Path yang tidak ada di NAV = rute tak dikenal; breadcrumb "Dashboard"
    // di halaman 404 memberi kesan halaman ini memang ada.
    if (location.pathname !== "/") return { group: "nav.home", label: "nav.notFound" }
    return { group: "nav.home", label: "nav.dashboard" }
  }, [location.pathname])

  const initials = (user?.username ?? "?").slice(0, 2).toUpperCase()
  const peran = `${user?.sudo ? t("common.sudoer") : t("common.user")}${
    user?.uid !== undefined ? ` · uid ${user.uid}` : ""
  }`

  // Escape menutup drawer. Hanya di bawah lg: di desktop sidebar bukan lapisan
  // di atas konten, jadi Escape di sana malah membuang navigasi tanpa diminta.
  useEffect(() => {
    if (!open) return
    const tombol = (e: KeyboardEvent) => {
      if (e.key === "Escape" && window.innerWidth < 1024) setOpen(false)
    }
    document.addEventListener("keydown", tombol)
    return () => document.removeEventListener("keydown", tombol)
  }, [open])

  // Klik di luar & Escape menutup menu profil — sama seperti TimezonePicker.
  useEffect(() => {
    if (!profilBuka) return
    const klik = (e: MouseEvent) => {
      if (profilRef.current && !profilRef.current.contains(e.target as Node)) setProfilBuka(false)
    }
    const tombol = (e: KeyboardEvent) => {
      if (e.key === "Escape") setProfilBuka(false)
    }
    document.addEventListener("mousedown", klik)
    document.addEventListener("keydown", tombol)
    return () => {
      document.removeEventListener("mousedown", klik)
      document.removeEventListener("keydown", tombol)
    }
  }, [profilBuka])

  // Pindah ke /login segera setelah sesi lokal dibuang; permintaan ke server
  // diselesaikan di belakang layar. Menunggunya cuma menahan tampilan tanpa
  // menambah kepastian apa pun — hasilnya sama-sama logout.
  function keluar() {
    void logout().catch(() => {
      notify.err(tr("Sesi di server mungkin masih aktif — periksa koneksi lalu muat ulang halaman."))
    })
    navigate("/login", { replace: true })
  }

  return (
    <div className="flex h-dvh overflow-hidden bg-bg">
      {/* Scrim hanya ada di bawah lg: di desktop sidebar mendorong konten,
          jadi tidak ada apa pun yang perlu ditutup. Sengaja <button> supaya
          bisa disentuh sekaligus dijangkau keyboard, bukan <div> onClick. */}
      {open && (
        <button
          type="button"
          aria-label={t("topbar.hideSidebar")}
          onClick={() => setOpen(false)}
          className="fixed inset-0 z-40 bg-black/60 lg:hidden"
        />
      )}
      <aside
        className={cn(
          "flex w-[285px] shrink-0 flex-col border-r border-border-shell bg-sidebar-background",
          // Di bawah lg sidebar melayang di atas konten dan digeser keluar
          // layar saat tertutup — bukan `hidden`, supaya geserannya terlihat
          // dan lebar konten tidak melompat saat drawer dibuka.
          // `invisible` ikut ditransisikan supaya drawer yang tergeser keluar
          // layar tidak tetap bisa dijangkau Tab. Visibility berpindah di awal
          // saat muncul dan di akhir saat menghilang, jadi geserannya utuh.
          "fixed inset-y-0 left-0 z-50 transition-[transform,visibility] duration-200",
          "lg:static lg:z-auto lg:visible lg:translate-x-0 lg:transition-none",
          open ? "visible translate-x-0" : "invisible -translate-x-full lg:hidden",
        )}
      >
        <div className="flex items-center gap-2.5 px-4 py-3.5">
          <span className="flex size-8 items-center justify-center rounded-md bg-surface-2">
            <Server className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="flex items-center gap-2 truncate text-sm font-semibold">
              {/* Selama data belum datang, tampilkan placeholder — bukan nama
                  tebakan. Teks yang berubah dari "lindash" jadi hostname asli
                  terbaca seperti aplikasi salah menulis nama mesin. */}
              {system?.hostname ?? <span className="inline-block h-3.5 w-24 animate-pulse rounded bg-surface-2" />}
              {/* Status realtime tinggal dot — labelnya dibaca screen reader saja. */}
              <StatusDot
                state={connected ? "ok" : "crit"}
                pulse={connected}
                label={connected ? t("conn.live") : t("conn.lost")}
              />
            </p>
            <p className="truncate text-xs text-muted-2">
              {system?.platform?.display ?? (
                <span className="inline-block h-3 w-36 animate-pulse rounded bg-surface-2" />
              )}
            </p>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-2">
          {visible.map((g) => (
            <div key={g.group} className="mb-4">
              <p className="sidebar-group-label px-2 pb-1.5">{t(g.group)}</p>
              {g.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  // Chunk route ditarik saat kursor/fokus menyentuh menunya,
                  // bukan saat diklik. Waktu unduh chunk jadi tumpang tindih
                  // dengan waktu user menggerakkan tangan ke tombolnya, dan
                  // halaman terbuka seketika alih-alih menampilkan kerangka.
                  onPointerEnter={() => prefetchRute(item.to)}
                  onFocus={() => prefetchRute(item.to)}
                  // Di HP drawer menutupi halaman yang baru dibuka; menutupnya
                  // sendiri menghemat satu ketukan yang selalu harus dilakukan.
                  onClick={() => {
                    if (window.innerWidth < 1024) setOpen(false)
                  }}
                  // `end` untuk SEMUA item, bukan cuma "/": tanpa ini /files
                  // ikut aktif saat membuka /files/samba atau /files/bookmarks
                  // karena NavLink mencocokkan prefix path.
                  end
                  className={({ isActive }) =>
                    cn(
                      // py-2.5 di HP: baris menu 40px jadi target sentuh yang
                      // wajar; di sm ke atas kembali padat seperti semula.
                      "flex items-center gap-2.5 rounded-md px-2 py-2.5 text-sm text-muted hover:bg-sidebar-hover hover:text-foreground sm:py-1.5",
                      // hover:bg-sidebar-accent WAJIB ikut saat aktif: tanpa
                      // itu hover milik state nonaktif menimpa background aktif
                      // dengan warna yang lebih gelap, jadi item yang sedang
                      // aktif justru meredup saat kursor lewat.
                      isActive && "bg-sidebar-accent text-foreground hover:bg-sidebar-accent",
                    )
                  }
                >
                  <item.icon className="size-4 shrink-0" />
                  {t(item.label)}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>

        {/* pb mengikuti home indicator: di HP tanpa poni env() bernilai 0,
            jadi paddingnya kembali persis 0.75rem seperti di desktop. */}
        <div
          className="mt-auto space-y-2 px-3 pb-3"
          style={{ paddingBottom: "calc(0.75rem + env(safe-area-inset-bottom))" }}
        >
          {/* Pembaruan panel hanya untuk sudoer: langkahnya memasang binary,
              menulis unit systemd, dan me-restart service. Icon berputar
              saat pembaruan sedang jalan (di-update oleh UpdateModal lewat
              store bersama) — tetap kelihatan walaupun modal ditutup. */}
          {user?.sudo && (adaUpdate || updateBerjalan) && (
            <button
              className="flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-sm text-warn hover:bg-warn/10"
              onClick={() => setUpdateBuka(true)}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-warn/40 bg-warn/10">
                <RefreshCw className={`size-4 ${updateBerjalan ? "animate-spin" : ""}`} />
              </span>
              <span className="truncate">{t("nav.update")}</span>
            </button>
          )}
          <div className="relative" ref={profilRef}>
            {profilBuka && (
              <div
                role="menu"
                className="absolute bottom-full left-0 z-50 mb-1.5 w-full overflow-hidden rounded-md border border-border bg-surface p-1 shadow-xl"
              >
                {/* Identitas diulang di dalam menu: begitu menu terbuka ia
                    menutupi tombol profil, jadi tanpa baris ini user kehilangan
                    konteks akun mana yang sedang ditindak. */}
                <div className="flex items-center gap-2.5 px-1.5 py-1.5">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-2 text-xs font-semibold">
                    {initials}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm">{user?.username}</span>
                    <span className="block truncate text-xs text-muted">{peran}</span>
                  </span>
                </div>
                <div className="my-1 h-px bg-border" />
                <button
                  role="menuitem"
                  className="flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left text-sm text-muted hover:bg-secondary hover:text-foreground"
                  onClick={() => {
                    setProfilBuka(false)
                    navigate("/settings/account")
                  }}
                >
                  <UserCog className="size-4 shrink-0" />
                  <span className="truncate">{t("nav.account")}</span>
                </button>
                {/* Uninstall tetap disembunyikan di balik menu profil: aksinya
                    menghapus panel dari mesin, jadi tidak boleh sejajar dengan
                    menu biasa. */}
                {user?.sudo && (
                  <button
                    role="menuitem"
                    className="flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left text-sm text-crit hover:bg-crit/10"
                    onClick={() => {
                      setProfilBuka(false)
                      setUninstallBuka(true)
                    }}
                  >
                    <Trash2 className="size-4 shrink-0" />
                    <span className="truncate">{t("nav.uninstall")}</span>
                  </button>
                )}
                <button
                  role="menuitem"
                  className="flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left text-sm text-muted hover:bg-secondary hover:text-foreground"
                  onClick={keluar}
                >
                  <LogOut className="size-4 shrink-0" />
                  <span className="truncate">{t("topbar.logout")}</span>
                </button>
              </div>
            )}
            <button
              className="flex w-full min-w-0 items-center gap-2.5 rounded-md p-1.5 text-left transition-colors hover:bg-sidebar-hover"
              aria-haspopup="menu"
              aria-expanded={profilBuka}
              onClick={() => setProfilBuka((v) => !v)}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-2 text-xs font-semibold">
                {initials}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{user?.username}</span>
                <span className="block truncate text-xs text-muted">{peran}</span>
              </span>
              <ChevronsUpDown className="size-3.5 shrink-0 text-muted" />
            </button>
          </div>
          <Byline />
        </div>
      </aside>

      <div className="relative flex min-w-0 flex-1 flex-col">
        {/* Tinggi topbar tidak dikunci di HP: breadcrumb yang panjang boleh
            turun ke baris kedua, dan gap-2 menahan lebar minimumnya. */}
        <header className="flex min-h-11 shrink-0 flex-wrap items-center gap-x-2 gap-y-1 bg-background px-3 py-1.5 sm:h-11 sm:flex-nowrap sm:gap-3 sm:py-0">
          <button
            className="-ml-1 rounded-md p-2 text-muted hover:bg-accent hover:text-foreground sm:ml-0 sm:p-1.5"
            aria-label={open ? t("topbar.hideSidebar") : t("topbar.showSidebar")}
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
          >
            <PanelLeft className="size-4" />
          </button>
          <span className="h-4 w-px bg-border" aria-hidden />
          <nav aria-label="Breadcrumb" className="flex min-w-0 text-sm">
            {/* Grup disembunyikan di HP: "Settings / Firewall" memakan lebar
                yang dibutuhkan jam, sedangkan halaman aktifnya sudah jelas
                dari nama terakhirnya. */}
            <span className="hidden text-muted sm:inline">{t(crumb.group)}</span>
            <span className="hidden px-1.5 text-muted-2 sm:inline">/</span>
            <span className="truncate">{t(crumb.label)}</span>
          </nav>
          <div className="ml-auto flex items-center gap-2">
            {/* Pemilih bahasa di kiri jam, sesuai permintaan tata letak. */}
            <div className="flex overflow-hidden rounded-md border border-border text-xs" role="group"
              aria-label={t("topbar.language")}>
              {(["id", "en"] as const).map((b) => (
                <button
                  key={b}
                  onClick={() => setBahasa(b)}
                  aria-pressed={bahasa === b}
                  className={cn(
                    "px-2 py-1 uppercase transition-colors",
                    bahasa === b ? "bg-secondary text-foreground" : "text-muted hover:text-foreground",
                  )}
                >
                  {b}
                </button>
              ))}
            </div>
            <span className="rounded-md border border-border px-2.5 py-1 text-xs text-muted">
              {/* Label "Waktu server:" dilepas di HP — angkanya yang dibaca,
                  dan tanpa ini blok ini sendirian menghabiskan separuh topbar. */}
              <span className="hidden sm:inline">{t("topbar.serverTime")}: </span>
              <span className="num font-semibold text-foreground">{formatJam(serverNow, timezone)}</span>
            </span>
            {/* Zona waktu bisa dipilih: server dan user sering berbeda benua,
                dan jam yang "salah" membuat log serta jadwal ban sulit dibaca. */}
            <div className="hidden sm:block">
              <TimezonePicker
                value={timezone}
                onChange={setTimezone}
                labelIkutServer={t("topbar.timezoneServer")}
                labelCari={t("topbar.timezoneSearch")}
                ariaLabel={t("topbar.timezone")}
              />
            </div>
          </div>
        </header>

        {/* overscroll-contain menahan "pull to refresh" Chrome Android: tanpa
            itu menggulir ke atas di daftar apa pun memuat ulang seluruh SPA. */}
        <main
          className={cn(
            "min-w-0 flex-1 overscroll-contain p-2 pt-0 sm:p-3 sm:pt-0",
            connected ? "overflow-y-auto" : "overflow-hidden",
          )}
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          {/* Banner ganti password: hanya tampil saat server menandai akun
              dengan must_change_password (lihat /api/auth/me). Lebih
              informatif daripada banner SSH bawaan yang tidak menunjuk
              ke UI. Tombol langsung ke Settings → Akun. */}
          {user?.must_change_password && (
            <div
              role="alert"
              className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn"
            >
              <span>
                {tr("Password akun ini wajib diganti. Banner SSH \"Default password must be changed\" muncul karena akun masih memakai password bawaan installer.")}
              </span>
              <NavLink to="/settings/account" className="text-signal underline-offset-2 hover:underline">
                {tr("Ganti sekarang")} →
              </NavLink>
            </div>
          )}
          <div
            className={cn(
              "shell-panel min-h-full p-4 lg:p-6",
              !connected && "pointer-events-none select-none blur-sm",
            )}
            aria-hidden={!connected}
          >
            <Outlet />
          </div>
        </main>

        {!connected && (
          <div
            role="alertdialog"
            aria-live="polite"
            className="absolute inset-x-0 bottom-0 top-11 z-20 flex flex-col items-center justify-center gap-3 bg-bg/50 text-center"
          >
            {gagalSambung ? (
              <>
                <p className="text-sm font-medium text-crit">{t("conn.failed")}</p>
                <Button size="sm" onClick={() => window.location.reload()}>
                  {t("conn.retry")}
                </Button>
              </>
            ) : (
              <>
                <Loader2 className="size-6 animate-spin text-muted" />
                <p className="text-sm text-muted">{t("conn.connecting")}</p>
              </>
            )}
          </div>
        )}
      </div>
      {uninstallBuka && (
        <UninstallModal username={user?.username} onClose={() => setUninstallBuka(false)} />
      )}
      {updateBuka && (
        <UpdateModal
          onClose={() => {
            setUpdateBuka(false)
            // Sesudah pembaruan selesai tombolnya harus hilang tanpa menunggu
            // pengecekan berkala berikutnya.
            cekUpdate()
          }}
        />
      )}
      <ConfirmHost />
      <PromptHost />
      <Toaster />
    </div>
  )
}
