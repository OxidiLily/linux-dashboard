import { useEffect, useState } from "react"
import { useAuth } from "@/stores/auth"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import {
  Container,
  Play,
  Square,
  RotateCw,
  Trash2,
  Plus,
  RefreshCw,
  FileCode,
  Pencil,
  ScrollText,
  ExternalLink,
} from "lucide-react"

type DockerContainer = {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: string
}

// Port yang hampir selalu berarti antarmuka web kalau sebuah container
// menerbitkan lebih dari satu. Tanpa urutan ini container seperti AgentDVR —
// yang menerbitkan 3478/tcp (STUN) di samping 8090/tcp (UI-nya) — akan
// ditawari pada port STUN, dan tombolnya membuka halaman kosong.
const PORT_WEB_UMUM = [80, 8080, 8000, 8090, 3000, 5000, 8081, 9000, 443, 8443]

/**
 * portWebContainer memilih satu port terbit yang paling masuk akal dibuka di
 * browser dari kolom "PORTS" milik `docker ps`, mis.
 * `0.0.0.0:3478->3478/tcp, [::]:3478->3478/udp, 0.0.0.0:8090->8090/tcp`.
 *
 * Hanya entri yang benar-benar diterbitkan ke host (ada tanda `->`) dan
 * ber-protokol tcp yang dihitung: port yang cuma terbuka di dalam jaringan
 * container tidak bisa dijangkau browser, dan udp tidak pernah bicara HTTP.
 * Mengembalikan null kalau tidak ada kandidat — tombolnya lalu tidak muncul,
 * bukan muncul lalu gagal.
 */
function portWebContainer(ports: string): number | null {
  const kandidat = new Set<number>()
  for (const bagian of ports.split(",")) {
    const m = bagian.trim().match(/:(\d+)->\d+\/(tcp|udp)$/)
    if (!m || m[2] !== "tcp") continue
    kandidat.add(Number(m[1]))
  }
  if (kandidat.size === 0) return null
  for (const p of PORT_WEB_UMUM) {
    if (kandidat.has(p)) return p
  }
  // Tidak ada yang dikenal — ambil yang terkecil supaya pilihannya tetap
  // sama tiap kali daftar dimuat ulang (urutan `docker ps` tidak dijamin).
  return Math.min(...kandidat)
}

type DockerStack = {
  id: number
  name: string
  compose_path: string
  description?: string
  running: number
  total: number
  error?: string
  /** Sudah jalan di Docker tapi belum terdaftar di panel. */
  external?: boolean
}

export function DockerView() {
  const tr = useTr()
  const home = useAuth((s) => s.user?.home) || "/home/user"
  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [stacks, setStacks] = useState<DockerStack[]>([])
  const [loading, setLoading] = useState(false)
  const [showAddStack, setShowAddStack] = useState(false)
  // id stack yang sedang diubah; null = pendaftaran stack baru.
  const [editingStack, setEditingStack] = useState<number | null>(null)
  const [stackForm, setStackForm] = useState({ name: "", compose_path: "", description: "" })
  const [envModal, setEnvModal] = useState<{ id: number; path: string; content: string } | null>(null)
  const [composeModal, setComposeModal] = useState<{ id: number; path: string; content: string } | null>(
    null,
  )
  const [logModal, setLogModal] = useState<{ id: string; name: string; content: string } | null>(null)
  const [menyimpan, setMenyimpan] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const [c, s] = await Promise.all([
        apiGet<DockerContainer[]>("/api/docker/containers"),
        apiGet<DockerStack[]>("/api/docker/stacks"),
      ])
      setContainers(c || [])
      setStacks(s || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat status Docker: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const stackAction = async (id: number, action: string) => {
    const stack = stacks.find((s) => s.id === id)
    // down/stop mematikan container yang sedang melayani; restart & pull
    // memutus layanan sesaat. up & start tidak merusak apa pun.
    const disruptif = action === "down" || action === "stop"
    if (disruptif || action === "restart" || action === "pull") {
      const ok = await confirmDialog({
        title: trf("Jalankan \"docker compose {0}\" pada stack {1}?", action, stack?.name ?? id),
        message: disruptif
          ? tr("Semua container di stack ini berhenti dan layanannya mati sampai dinyalakan lagi.")
          : tr("Layanan di stack ini terputus sesaat selama proses berjalan."),
        detail: stack?.compose_path,
        confirmLabel: action,
        danger: disruptif,
      })
      if (!ok) return
    }
    // notify.tugas, bukan await-lalu-notify: `docker compose pull` dan `up`
    // bisa makan puluhan detik menarik image, dan selama itu pola lama tidak
    // menampilkan apa pun — tombolnya terlihat tidak menanggapi.
    try {
      await notify.tugas(
        apiSend<{ status: string; output?: string }>(`/api/docker/stacks/${id}/${action}`, "POST"),
        {
          jalan: trf("Menjalankan \"docker compose {0}\"…", action),
          sukses: trf("Stack: \"{0}\" selesai.", action),
          gagal: (e) => trf("Gagal aksi stack: {0}", pesanError(e)),
          detail: (res) => res.output?.trim() || undefined,
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas; catch di sini hanya
      // mencegah unhandled rejection dan menahan load() agar tidak jalan.
    }
  }

  const handleDeleteStack = async (id: number) => {
    const ok = await confirmDialog({
      title: tr("Hapus registrasi stack ini?"),
      message: tr("Hanya dihapus dari daftar dashboard. Container yang sedang jalan tidak dimatikan."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/docker/stacks/${id}`, "DELETE")
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menghapus stack: {0}", pesanError(e)))
    }
  }

  const openTambahStack = () => {
    setEditingStack(null)
    setStackForm({ name: "", compose_path: "", description: "" })
    setShowAddStack(true)
  }

  const openEditStack = (st: DockerStack) => {
    setEditingStack(st.id)
    setStackForm({ name: st.name, compose_path: st.compose_path, description: st.description ?? "" })
    setShowAddStack(true)
  }

  // Stack yang sudah hidup di Docker cukup didaftarkan dengan nama dan path
  // compose-nya sendiri — tidak ada yang perlu diketik ulang.
  const daftarkanStack = async (st: DockerStack) => {
    const ok = await confirmDialog({
      title: trf("Daftarkan stack \"{0}\" ke panel?", st.name),
      message: tr("Stack yang sudah jalan ini akan bisa dikelola dari sini (up/down/restart/.env)."),
      detail: st.compose_path,
      confirmLabel: tr("Daftarkan"),
    })
    if (!ok) return
    try {
      await apiSend("/api/docker/stacks", "POST", {
        name: st.name,
        compose_path: st.compose_path,
        description: "",
      })
      notify.ok(trf("Stack {0} terdaftar.", st.name))
      load()
    } catch (e: any) {
      notify.err(trf("Gagal mendaftarkan stack: {0}", pesanError(e)))
    }
  }

  const aksiContainer = async (c: DockerContainer, action: "start" | "stop" | "restart" | "remove") => {
    // start dan lihat log tidak merusak apa pun; sisanya menghentikan layanan
    // yang mungkin sedang melayani permintaan.
    if (action !== "start") {
      const ok = await confirmDialog({
        title: trf("{0} container \"{1}\"?", action === "remove" ? tr("Hapus") : action, c.name),
        message:
          action === "remove"
            ? tr("Container dihentikan lalu dihapus. Volume dan image tidak ikut terhapus.")
            : tr("Container berhenti melayani permintaan selama aksi ini berjalan."),
        confirmLabel: action === "remove" ? tr("Hapus") : action,
        danger: action !== "restart",
      })
      if (!ok) return
    }
    try {
      await apiSend(`/api/docker/containers/${encodeURIComponent(c.id)}/${action}`, "POST")
      notify.ok(trf("Container {0}: {1} berhasil.", c.name, action))
      load()
    } catch (e: any) {
      notify.err(trf("Gagal {0} container: {1}", action, pesanError(e)))
    }
  }

  const bukaLog = async (c: DockerContainer) => {
    try {
      const res = await apiGet<{ content: string }>(
        `/api/docker/containers/${encodeURIComponent(c.id)}/logs?tail=200`,
      )
      setLogModal({ id: c.id, name: c.name, content: res.content || tr("(log kosong)") })
    } catch (e: any) {
      notify.err(trf("Gagal membaca log: {0}", pesanError(e)))
    }
  }

  const bukaCompose = async (id: number) => {
    try {
      const res = await apiGet<{ path: string; content: string }>(`/api/docker/stacks/${id}/compose`)
      setComposeModal({ id, path: res.path, content: res.content })
    } catch (e: any) {
      notify.err(trf("Gagal membaca docker-compose.yml: {0}", pesanError(e)))
    }
  }

  const simpanCompose = async () => {
    if (!composeModal) return
    const ok = await confirmDialog({
      title: tr("Simpan docker-compose.yml?"),
      message:
        tr("Isi divalidasi Docker dulu; kalau ditolak, file lama tidak disentuh. Versi lama disimpan sebagai .bak. Menyimpan TIDAK men-deploy — jalankan Up/Restart supaya perubahan berlaku."),
      detail: composeModal.path,
      confirmLabel: tr("Simpan"),
    })
    if (!ok) return
    setMenyimpan(true)
    try {
      await apiSend(`/api/docker/stacks/${composeModal.id}/compose`, "PUT", {
        content: composeModal.content,
      })
      notify.ok(tr("docker-compose.yml tersimpan — jalankan Up/Restart agar berlaku."))
      setComposeModal(null)
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan compose: {0}", pesanError(e)))
    } finally {
      setMenyimpan(false)
    }
  }

  const handleCreateStack = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      if (editingStack === null) {
        await apiSend("/api/docker/stacks", "POST", stackForm)
      } else {
        await apiSend(`/api/docker/stacks/${editingStack}`, "PUT", stackForm)
        notify.ok(tr("Stack diperbarui."))
      }
      setShowAddStack(false)
      setEditingStack(null)
      setStackForm({ name: "", compose_path: "", description: "" })
      load()
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan stack: {0}", pesanError(e)))
    }
  }

  const handleOpenEnv = async (id: number) => {
    try {
      const res = await apiGet<{ path: string; content: string }>(`/api/docker/stacks/${id}/env`)
      setEnvModal({ id, path: res.path, content: res.content })
    } catch (e: any) {
      notify.err(trf("Gagal membaca file .env: {0}", pesanError(e)))
    }
  }

  const handleSaveEnv = async () => {
    if (!envModal) return
    const ok = await confirmDialog({
      title: tr("Timpa file .env stack ini?"),
      message: tr(
        "Isi lama tertimpa seluruhnya. Container perlu di-recreate (compose up) agar nilai baru terpakai.",
      ),
      detail: envModal.path,
      confirmLabel: tr("Simpan"),
      danger: true,
    })
    if (!ok) return
    try {
      await apiSend(`/api/docker/stacks/${envModal.id}/env`, "PUT", { content: envModal.content })
      notify.ok(tr("File .env berhasil disimpan."))
      setEnvModal(null)
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan .env: {0}", pesanError(e)))
    }
  }

  return (
  <div className="space-y-4">
    {/* Compose Stacks */}
    <Panel
      title={tr("Compose Stacks")}
      hint={tr("Kelola stack docker-compose")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambahStack}>
            <Plus className="mr-1 size-3.5" /> {tr("Tambah Stack")}
          </Button>
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {stacks.map((st) => (
          <div
            key={st.external ? `ext:${st.compose_path}` : st.id}
            className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div>
              <div className="flex items-center gap-2">
                <p className="font-semibold text-sm">{st.name}</p>
                {st.external && <Badge tone="warn">{tr("belum terdaftar")}</Badge>}
                <Badge tone={st.running > 0 && st.running === st.total ? "ok" : st.running > 0 ? "warn" : "muted"}>
                  {trf("{0} / {1} berjalan", st.running, st.total)}
                </Badge>
              </div>
              <p className="num text-xs text-muted-foreground mt-0.5">{st.compose_path}</p>
              {st.error && <p className="text-xs text-crit mt-0.5">{st.error}</p>}
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              {st.external ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs"
                  onClick={() => daftarkanStack(st)}
                >
                  <Plus className="mr-1 size-3" /> {tr("Daftarkan")}
                </Button>
              ) : (
              <>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => stackAction(st.id, "up")}>
                <Play className="mr-1 size-3 text-ok" /> Up
              </Button>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => stackAction(st.id, "down")}>
                <Square className="mr-1 size-3 text-crit" /> Down
              </Button>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => stackAction(st.id, "restart")}>
                <RotateCw className="mr-1 size-3" /> {tr("Restart")}
              </Button>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => bukaCompose(st.id)}>
                <FileCode className="mr-1 size-3" /> compose
              </Button>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => handleOpenEnv(st.id)}>
                <FileCode className="mr-1 size-3" /> .env
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-muted-foreground hover:text-foreground"
                aria-label={trf("Edit stack {0}", st.name)}
                onClick={() => openEditStack(st)}
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-muted-foreground hover:text-crit"
                aria-label={trf("Hapus stack {0}", st.name)}
                onClick={() => handleDeleteStack(st.id)}
              >
                <Trash2 className="size-3.5" />
              </Button>
              </>
              )}
            </div>
          </div>
        ))}
        {stacks.length === 0 && !loading && (
          <p className="py-4 text-center text-xs text-muted-foreground">
            {tr("Belum ada Compose stack terdaftar maupun berjalan di Docker.")}
          </p>
        )}
      </div>
    </Panel>

    {/* Containers */}
    <Panel title={tr("Docker Containers")} hint={`${containers.length} container terdeteksi`}>
      <div className="overflow-x-auto">
        <table className="tabel-kartu w-full text-left text-xs">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="pb-2 font-medium">{tr("Nama / ID")}</th>
              <th className="pb-2 font-medium">{tr("Image")}</th>
              <th className="pb-2 font-medium">{tr("Status")}</th>
              <th className="pb-2 font-medium">{tr("Port Mappings")}</th>
              <th className="pb-2 text-right font-medium">{tr("Aksi")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {containers.map((c) => (
              <tr key={c.id} className="hover:bg-secondary/40">
                <td data-label="" className="py-2">
                  <div className="flex items-center gap-2">
                    <Container className="size-4 text-signal" />
                    <div>
                      <p className="font-semibold">{c.name}</p>
                      <p className="num text-[10px] text-muted-foreground">{c.id.substring(0, 12)}</p>
                    </div>
                  </div>
                </td>
                <td data-label={tr("Image")} className="num py-2 text-muted-foreground">{c.image}</td>
                <td data-label={tr("Status")} className="py-2">
                  <Badge tone={c.state === "running" ? "ok" : "muted"}>{c.status}</Badge>
                </td>
                <td data-label={tr("Port Mappings")} className="num py-2 text-muted-foreground max-w-xs truncate">{c.ports || "—"}</td>
                <td data-label="" className="py-2">
                  <div className="flex items-center justify-end gap-1">
                    {c.state === "running" ? (
                      <>
                        {(() => {
                          // Host diambil dari address bar, bukan dari hostname
                          // server: itu persis alamat yang sudah terbukti bisa
                          // dijangkau device ini. os.Hostname() di sisi server
                          // sering nama internal yang tidak resolve dari HP di
                          // Wi-Fi yang sama, dan "localhost" akan menunjuk ke
                          // device pengunjung, bukan ke servernya.
                          //
                          // Scheme selalu http: panel bisa dilayani lewat
                          // https (cert self-signed), tapi container di
                          // belakang port terbit hampir selalu plain HTTP —
                          // memakai scheme panel membuat browser menjawab
                          // ERR_SSL_PROTOCOL_ERROR. Pola yang sama dipakai
                          // tombol "Buka" di halaman Components.
                          const port = portWebContainer(c.ports)
                          if (port === null) return null
                          return (
                            <a
                              href={`http://${window.location.hostname}:${port}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex h-7 items-center rounded-md px-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
                              aria-label={trf("Buka {0} di tab baru", c.name)}
                              title={trf("Buka http://{0}:{1} di tab baru", window.location.hostname, String(port))}
                            >
                              <ExternalLink className="size-3.5" />
                            </a>
                          )
                        })()}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-1.5 text-muted-foreground hover:text-foreground"
                          aria-label={trf("Hentikan {0}", c.name)}
                          title={tr("Hentikan")}
                          onClick={() => aksiContainer(c, "stop")}
                        >
                          <Square className="size-3.5 text-crit" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-1.5 text-muted-foreground hover:text-foreground"
                          aria-label={`Restart ${c.name}`}
                          title={tr("Restart")}
                          onClick={() => aksiContainer(c, "restart")}
                        >
                          <RotateCw className="size-3.5" />
                        </Button>
                      </>
                    ) : (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-1.5 text-muted-foreground hover:text-foreground"
                        aria-label={trf("Jalankan {0}", c.name)}
                        title={tr("Jalankan")}
                        onClick={() => aksiContainer(c, "start")}
                      >
                        <Play className="size-3.5 text-ok" />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-1.5 text-muted-foreground hover:text-foreground"
                      aria-label={`Log ${c.name}`}
                      title={tr("Lihat log")}
                      onClick={() => bukaLog(c)}
                    >
                      <ScrollText className="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-1.5 text-muted-foreground hover:text-crit"
                      aria-label={trf("Hapus container {0}", c.name)}
                      title={tr("Hapus")}
                      onClick={() => aksiContainer(c, "remove")}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
            {containers.length === 0 && !loading && (
              <tr>
                <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                  {tr("Tidak ada container yang ditemukan.")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </Panel>

    {/* Modal Tambah Stack */}
    {showAddStack && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="font-semibold text-sm">
            {editingStack === null ? tr("Daftarkan Compose Stack") : tr("Ubah Compose Stack")}
          </p>
          <form onSubmit={handleCreateStack} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Nama Stack")}</label>
              <Input
                className="mt-1"
                required
                value={stackForm.name}
                onChange={(e) => setStackForm({ ...stackForm, name: e.target.value })}
                placeholder={tr("mis. Nextcloud")}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Path Absolut docker-compose.yml")}</label>
              <Input
                className="mt-1"
                required
                value={stackForm.compose_path}
                onChange={(e) => setStackForm({ ...stackForm, compose_path: e.target.value })}
                placeholder={`${home}/DATA/AppData/nextcloud/docker-compose.yml`}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Deskripsi (opsional)")}</label>
              <Input
                className="mt-1"
                value={stackForm.description}
                onChange={(e) => setStackForm({ ...stackForm, description: e.target.value })}
              />
            </div>
            <div className="flex justify-end gap-2 pt-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setShowAddStack(false)
                  setEditingStack(null)
                }}
              >
                {tr("Batal")}
              </Button>
              <Button type="submit" size="sm">
                {editingStack === null ? tr("Daftarkan") : tr("Simpan Perubahan")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}

    {/* Modal Log Container — read-only, mono, auto-scroll ke bawah */}
    {logModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="flex max-h-[85dvh] w-full max-w-3xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
          <div className="flex items-center justify-between border-b border-border pb-2">
            <div className="min-w-0">
              <p className="text-sm font-semibold">Log {logModal.name}</p>
              <p className="num truncate text-[10px] text-muted-foreground">
                {trf("200 baris terakhir · {0}", logModal.id.substring(0, 12))}
              </p>
            </div>
            <Button variant="outline" size="sm" onClick={() => setLogModal(null)}>
              {tr("Tutup")}
            </Button>
          </div>
          <pre className="mt-3 flex-1 overflow-auto whitespace-pre-wrap rounded border border-border bg-background p-3 font-mono text-[11px] leading-relaxed">
            {logModal.content}
          </pre>
        </div>
      </div>
    )}

    {/* Modal Edit docker-compose.yml */}
    {composeModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="flex max-h-[85dvh] w-full max-w-3xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
          <div className="flex items-center justify-between border-b border-border pb-2">
            <div className="min-w-0">
              <p className="text-sm font-semibold">{tr("Edit docker-compose.yml")}</p>
              <p className="num truncate text-[10px] text-muted-foreground">{composeModal.path}</p>
            </div>
            <Button variant="outline" size="sm" onClick={() => setComposeModal(null)}>
              {tr("Tutup")}
            </Button>
          </div>
          <textarea
            className="mt-3 min-h-[50dvh] flex-1 rounded border border-border bg-background p-3 font-mono text-[11px] leading-relaxed"
            spellCheck={false}
            value={composeModal.content}
            onChange={(e) => setComposeModal({ ...composeModal, content: e.target.value })}
          />
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="text-[10px] text-muted-foreground">
              {tr("Divalidasi Docker sebelum ditulis · versi lama disimpan sebagai .bak · menyimpan tidak men-deploy")}
            </p>
            <Button size="sm" disabled={menyimpan} onClick={simpanCompose}>
              {menyimpan ? tr("Menyimpan…") : tr("Simpan")}
            </Button>
          </div>
        </div>
      </div>
    )}

    {/* Modal Edit .env */}
    {envModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="flex max-h-[85dvh] w-full max-w-2xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
          <div className="flex items-center justify-between border-b border-border pb-2">
            <div>
              <p className="font-semibold text-sm">{tr("Edit Environment Variables (.env)")}</p>
              <p className="num text-xs text-muted-foreground">{envModal.path}</p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => setEnvModal(null)}>
              {tr("Tutup")}
            </Button>
          </div>
          <textarea
            className="mt-3 min-h-[300px] flex-1 rounded border border-border bg-background p-3 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-signal"
            value={envModal.content}
            onChange={(e) => setEnvModal({ ...envModal, content: e.target.value })}
            placeholder="# KEY=VALUE"
          />
          <div className="flex justify-end gap-2 pt-3 border-t border-border mt-3">
            <Button variant="outline" size="sm" onClick={() => setEnvModal(null)}>
              {tr("Batal")}
            </Button>
            <Button size="sm" onClick={handleSaveEnv}>
              {tr("Simpan .env")}
            </Button>
          </div>
        </div>
      </div>
    )}
  </div>
  )
}
