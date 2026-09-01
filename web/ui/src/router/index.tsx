import { lazy, Suspense } from "react"
import { createBrowserRouter, Navigate, useLocation } from "react-router-dom"
import { AppShell } from "@/components/layout/app-shell"
import { LoginView } from "@/views/login"
import { DashboardView } from "@/views/dashboard"
import { useAuth } from "@/stores/auth"
import { ComponentGuard } from "@/components/ui/component-guard"
import { pemuatRute } from "@/router/lazy-routes"
import type { ReactNode } from "react"

// Lazy load semua view kecuali Dashboard & Login. Dashboard adalah halaman
// pertama yang dibuka setelah login, memuatnya di main chunk agar tidak ada
// jeda "spinner" pada first paint. View lain di-chunk per-route — vite
// otomatis nama file jadi views--<hash>.js. Bundle awal turun
// dari ~826 kB ke ~dashboard+login+shell+deps chunk saja.
// Helper: lazy() mengharapkan module export default, tapi semua view di
// project ini pakai named export. Fungsi kecil ini membungkus module yang
// di-import agar sesuai kontrak lazy<T> tanpa harus menambah `export default`
// di setiap file view. Tipe parameter dibiarkan longgar — view modules sudah
// terdefinisi baik oleh TS saat dipanggil, dan helper ini cuma adaptor.
type LazyFactory = () => Promise<{ default: React.ComponentType }>
function lazyNamed(loader: () => Promise<unknown>, name: string): React.ComponentType {
  const wrapped: LazyFactory = () => loader().then(m => ({ default: (m as Record<string, React.ComponentType>)[name] }))
  return lazy(wrapped)
}

function rute(path: string, name: string): React.ComponentType {
  return lazyNamed(pemuatRute[path], name)
}

const FileManagerView = rute("/files", "FileManagerView")
const SambaView = rute("/files/samba", "SambaView")
const MergerfsView = rute("/files/pool", "MergerfsView")
const NFSView = rute("/files/nfs", "NFSView")
const Fail2banView = rute("/settings/fail2ban", "Fail2banView")
const BookmarksView = rute("/files/bookmarks", "BookmarksView")
const LogsView = rute("/logs/alerts", "LogsView")
const FileOperationsView = rute("/logs/file-operations", "FileOperationsView")
const ActivityLogsView = rute("/logs/activity", "ActivityLogsView")
const AccountView = rute("/settings/account", "AccountView")
const NetworkView = rute("/settings/network", "NetworkView")
const FirewallView = rute("/settings/firewall", "FirewallView")
const AlertThresholdsView = rute("/settings/alerts", "AlertThresholdsView")
const ComponentsView = rute("/settings/components", "ComponentsView")
const PrintServerView = rute("/settings/print", "PrintServerView")
const AIAgentView = rute("/ai/agent", "AIAgentView")
const ProcessesView = rute("/system/processes", "ProcessesView")
const DockerView = rute("/system/docker", "DockerView")
const TerminalView = rute("/system/terminal", "TerminalView")
const NotFoundView = lazyNamed(() => import("@/views/error"), "NotFoundView")
const RouteErrorView = lazyNamed(() => import("@/views/error"), "RouteErrorView")

// Wrapper Suspense saat chunk route dimuat.
//
// Dulu fallback-nya null: area konten kosong sama sekali, tanpa satu pun
// tanda bahwa sesuatu sedang berjalan. Layar diam setelah klik terbaca
// sebagai aplikasi yang menggantung, bukan sebagai halaman yang sedang
// dimuat — kerangka panel menjawab pertanyaan itu tanpa spinner penuh layar.
function Lazy({ children }: { children: ReactNode }) {
  return <Suspense fallback={<KerangkaHalaman />}>{children}</Suspense>
}

// Butuh membungkus route yang isinya tidak berguna — dan berisik — tanpa
// software tertentu di sistem. ComponentGuard sengaja dipasang DI SINI, di
// atas <Lazy>, bukan di dalam berkas view: penjaga di dalam view tidak
// mencegah useEffect view itu memanggil API, sehingga error mentah seperti
// "exec: docker: executable file not found in $PATH" tetap muncul sebagai
// toast sebelum penjaganya sempat menampilkan apa pun.
function Dijaga({ name, label, children }: { name: string; label: string; children: ReactNode }) {
  return (
    <ComponentGuard name={name} label={label}>
      <Lazy>{children}</Lazy>
    </ComponentGuard>
  )
}

function KerangkaHalaman() {
  return (
    <div className="panel space-y-3 p-4" aria-busy="true" aria-live="polite">
      <div className="h-4 w-40 animate-pulse rounded bg-surface-2" />
      <div className="h-3 w-64 animate-pulse rounded bg-surface-2" />
      <div className="space-y-2 pt-2">
        <div className="h-10 animate-pulse rounded bg-surface-2" />
        <div className="h-10 animate-pulse rounded bg-surface-2" />
        <div className="h-10 animate-pulse rounded bg-surface-2" />
      </div>
    </div>
  )
}

function RequireAuth({ children }: { children: ReactNode }) {
  const user = useAuth((s) => s.user)
  const ready = useAuth((s) => s.ready)
  const location = useLocation()
  // Tanpa penjaga ini, deep link / refresh sempat dianggap belum login →
  // dilempar ke /login, lalu /login melihat user sudah ada → mendarat di "/".
  if (!ready) return null
  if (!user) {
    const next = encodeURIComponent(location.pathname + location.search)
    return <Navigate to={`/login?next=${next}`} replace />
  }
  return <>{children}</>
}

export const router = createBrowserRouter([
  {
    path: "/login",
    element: <LoginView />,
    errorElement: <RouteErrorView />,
  },
  {
    path: "/",
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    errorElement: <RouteErrorView />,
    children: [
      { index: true, element: <DashboardView /> },
      { path: "files", element: <Lazy><FileManagerView /></Lazy> },
      { path: "files/samba", element: <Dijaga name="samba" label="Samba"><SambaView /></Dijaga> },
      { path: "files/pool", element: <Dijaga name="mergerfs" label="mergerfs"><MergerfsView /></Dijaga> },
      { path: "files/nfs", element: <Dijaga name="nfs-server" label="NFS server"><NFSView /></Dijaga> },
      { path: "files/bookmarks", element: <Lazy><BookmarksView /></Lazy> },
      { path: "logs/alerts", element: <Lazy><LogsView /></Lazy> },
      { path: "logs/file-operations", element: <Lazy><FileOperationsView /></Lazy> },
      { path: "logs/activity", element: <Lazy><ActivityLogsView /></Lazy> },
      { path: "settings/account", element: <Lazy><AccountView /></Lazy> },
      { path: "settings/network", element: <Lazy><NetworkView /></Lazy> },
      { path: "settings/firewall", element: <Lazy><FirewallView /></Lazy> },
      { path: "settings/fail2ban", element: <Dijaga name="fail2ban" label="fail2ban"><Fail2banView /></Dijaga> },
      { path: "settings/alerts", element: <Lazy><AlertThresholdsView /></Lazy> },
      { path: "settings/components", element: <Lazy><ComponentsView /></Lazy> },
      { path: "settings/print", element: <Dijaga name="print-server" label="CUPS"><PrintServerView /></Dijaga> },
      { path: "ai/agent", element: <Lazy><AIAgentView /></Lazy> },
      { path: "system/processes", element: <Lazy><ProcessesView /></Lazy> },
      { path: "system/docker", element: <Dijaga name="docker" label="Docker"><DockerView /></Dijaga> },
      { path: "system/terminal", element: <Lazy><TerminalView /></Lazy> },
      // Rute tak dikenal sebelumnya dilempar diam-diam ke "/" — salah ketik URL
      // jadi terlihat seperti dashboard biasa, tanpa petunjuk apa pun.
      { path: "*", element: <Lazy><NotFoundView /></Lazy> },
    ],
  },
])
