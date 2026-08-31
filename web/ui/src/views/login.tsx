import { useEffect, useState, type FormEvent } from "react"
import { trf, useT, useTr } from "@/stores/i18n"
import { useNavigate, useLocation, Navigate } from "react-router-dom"
import { useAuth } from "@/stores/auth"
import { usePrefs, simpanBahasaPralogin, type Bahasa } from "@/stores/prefs"
import { apiGet } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Byline } from "@/components/ui/byline"
import { cn } from "@/lib/utils"
import { Terminal } from "lucide-react"

// Pemilih bahasa yang sama dengan topbar. Di halaman login belum ada sesi, jadi
// pilihannya dititipkan ke localStorage; store preferensi yang mendorongnya ke
// server begitu login berhasil (lihat simpanBahasaPralogin di stores/prefs.ts).
function PilihBahasa({ className }: { className?: string }) {
  const tr = useTr()
  const bahasa = usePrefs((s) => s.bahasa)
  const setBahasa = usePrefs((s) => s.setBahasa)
  return (
    <div
      className={cn("flex overflow-hidden rounded-md border border-border text-xs", className)}
      role="group"
      aria-label={tr("Bahasa")}
    >
      {(["id", "en"] as const).map((b: Bahasa) => (
        <button
          key={b}
          type="button"
          onClick={() => {
            simpanBahasaPralogin(b)
            void setBahasa(b)
          }}
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
  )
}

export function LoginView() {
  const tr = useTr()
  const t = useT()
  const user = useAuth((s) => s.user)
  const login = useAuth((s) => s.login)
  const busy = useAuth((s) => s.busy)
  const error = useAuth((s) => s.error)
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [hostname, setHostname] = useState("")

  // Nama host dibaca dari endpoint publik: sebelum login tidak ada sesi, dan
  // user perlu tahu mesin mana yang sedang ia masuki.
  useEffect(() => {
    apiGet<{ hostname: string }>("/api/hostname")
      .then((d) => setHostname(d.hostname))
      .catch(() => {})
  }, [])

  const next = new URLSearchParams(location.search).get("next") ?? "/"

  if (user) return <Navigate to={next} replace />

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    try {
      await login(username, password)
      navigate(next, { replace: true })
    } catch {
      /* error displayed via store */
    }
  }

  return (
    <div className="grid min-h-dvh lg:grid-cols-2">
      {/* Sisi kiri: identitas mesin. Disembunyikan di layar sempit — di sana
          form yang harus dapat seluruh lebar, dan isi panel ini tidak membawa
          informasi yang wajib untuk masuk. */}
      <aside className="relative hidden flex-col justify-between overflow-hidden border-r border-border-shell bg-sidebar-background p-8 lg:flex">
        {/* Pola titik: dekorasi murni CSS, tanpa aset gambar. aria-hidden
            supaya tidak ikut terbaca screen reader. */}
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 opacity-[0.35]"
          style={{
            backgroundImage: "radial-gradient(var(--color-surface-3) 1px, transparent 1px)",
            backgroundSize: "22px 22px",
            maskImage: "radial-gradient(120% 90% at 50% 40%, #000 30%, transparent 100%)",
            WebkitMaskImage: "radial-gradient(120% 90% at 50% 40%, #000 30%, transparent 100%)",
          }}
        />

        <div className="relative flex items-center gap-2.5">
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-2">
            <Terminal className="size-4" aria-hidden />
          </span>
          <div className="min-w-0">
            <p className="eyebrow">{tr("Server")}</p>
            {/* Sebelum jawaban endpoint datang, barisnya diisi placeholder
                setinggi teks supaya tata letak tidak melompat. */}
            <p className="num truncate text-sm font-semibold" title={hostname}>
              {hostname || " "}
            </p>
          </div>
        </div>

        <div className="relative">
          <p className="max-w-sm text-sm leading-relaxed text-muted-foreground">
            {tr("Panel ini mengelola mesin Linux apa adanya: akun, berkas, layanan, dan jaringan. Tidak ada basis data pengguna terpisah — semua yang Anda lihat adalah keadaan sistem yang sebenarnya.")}
          </p>
        </div>

        {/* w-fit: aside ini flex-col, tanpa itu bordernya melar selebar kolom. */}
        <div className="relative w-fit">
          <PilihBahasa />
        </div>
      </aside>

      {/* Sisi kanan: form. */}
      <main className="flex items-center justify-center px-4 py-10">
        <div className="w-full max-w-sm">
          <div className="mb-7">
            <p className="eyebrow">Linux Server Dashboard</p>
            <h1 className="num mt-1.5 text-2xl font-semibold tracking-tight">
              {/* Nama server ikut di judul; sebelum jawabannya datang judulnya
                  tetap utuh, bukan "Masuk ke" yang menggantung. */}
              {hostname ? trf("Masuk ke {0}", hostname) : tr("Masuk ke server")}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">
              {tr("Pakai username dan password akun Linux Anda di mesin ini. Tidak ada akun terpisah.")}
            </p>
          </div>
          <form className="panel space-y-4 p-5" onSubmit={onSubmit}>
            <div className="space-y-1.5">
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                placeholder={tr("mis. oxidilily")}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>
            {error && (
              <p className="rounded-md border border-crit/40 bg-crit/10 px-3 py-2 text-sm text-crit">
                {error}
              </p>
            )}
            <Button type="submit" disabled={busy} className="w-full">
              {busy ? tr("Memeriksa…") : t("login.submit")}
            </Button>
          </form>

          <Byline className="mt-6" />

          {/* Layar sempit tidak menampilkan panel kiri, jadi nama host dan
              pemilih bahasa tetap harus ada jalannya di sini. */}
          <div className="mt-6 flex items-center justify-between lg:hidden">
            <p className="num truncate text-xs text-muted-2" title={hostname}>
              {hostname}
            </p>
            <PilihBahasa />
          </div>
        </div>
      </main>
    </div>
  )
}
