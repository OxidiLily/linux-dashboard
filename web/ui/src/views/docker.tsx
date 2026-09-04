import { useEffect, useRef, useState } from "react"
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
  Layers,
  HardDrive,
  Network,
  Eraser,
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

// Label aksi stack yang menyebut stack-nya. Toast lama berbunyi
// `Menjalankan "docker compose up"…` — benar secara teknis, tapi tidak
// menjawab pertanyaan pertama yang muncul di layar berisi banyak stack:
// yang mana? Nama aksi juga dipakai untuk pesan berhasil dan gagal, supaya
// ketiganya menyebut hal yang sama.
const AKSI_STACK: Record<string, string> = {
  up: "Deploy {0}",
  down: "Menghentikan {0}",
  restart: "Restart {0}",
  stop: "Stop {0}",
  start: "Start {0}",
  pull: "Tarik image {0}",
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

// Bentuk balasan /api/docker/{images,volumes,networks} — lihat dockerImage,
// dockerVolume, dan dockerNetwork di internal/api/docker.go.
type DockerImage = {
  id: string
  repository: string
  tag: string
  size: string
  created: string
  /** Image tanpa tag: sisa build/pull yang tergantikan. Ini yang dibuang prune. */
  dangling: boolean
}

type DockerVolume = { name: string; driver: string; mountpoint: string }

type DockerNetwork = {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  /** bridge/host/none — dibuat docker sendiri dan tidak bisa dihapus. */
  builtin: boolean
}

type JenisDaya = "images" | "volumes" | "networks"

const labelDaya = {
  images: "Images",
  volumes: "Volumes",
  networks: "Networks",
}

/** Satu baris `docker system df`. Semua nilai string apa adanya dari docker. */
type DockerDf = {
  /** "Images" | "Containers" | "Local Volumes" | "Build Cache" */
  type: string
  total: string
  active: string
  size: string
  reclaimable: string
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
  const logRef = useRef<HTMLPreElement>(null)
  // Auto-scroll hanya dilakukan kalau user memang sedang di dasar log. Kalau
  // ia menggulir ke atas untuk membaca baris lama, tarikan berikutnya tidak
  // boleh menyentaknya kembali ke bawah.
  const logIkutBawah = useRef(true)
  // Image/volume/network ditaruh di satu panel dengan pemilih, bukan tiga
  // panel bertumpuk: halaman ini sudah memuat stack dan container, dan tiga
  // tabel lagi membuat yang paling sering dipakai terdorong jauh ke bawah.
  const [daya, setDaya] = useState<JenisDaya>("images")
  const [images, setImages] = useState<DockerImage[]>([])
  const [volumes, setVolumes] = useState<DockerVolume[]>([])
  const [networks, setNetworks] = useState<DockerNetwork[]>([])
  const [loadingDaya, setLoadingDaya] = useState(false)
  // Ringkasan pemakaian disk. Dimuat sekali saat halaman dibuka dan saat
  // tombol muat ulang ditekan — BUKAN tiap kali tab dipindah: `docker system
  // df` menghitung ulang pemakaian disk tiap panggilan, dan menempelkannya ke
  // pergantian tab berarti membayarnya tiga kali untuk angka yang sama.
  const [df, setDf] = useState<DockerDf[]>([])

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
    // Aksi yang tidak ada di tabel tetap dapat kalimat yang menyebut stack —
    // kuncinya tidak akan ketemu di kamus dan dipakai apa adanya, itu memang
    // perilaku trf yang diinginkan di sini.
    const nama = stack?.name ?? `#${id}`
    const label = trf(AKSI_STACK[action] ?? `${action} {0}`, nama)
    try {
      await notify.tugas(
        apiSend<{ status: string; output?: string }>(`/api/docker/stacks/${id}/${action}`, "POST"),
        {
          jalan: `${label}…`,
          sukses: trf("{0} selesai.", label),
          gagal: (e) => trf("{0} gagal: {1}", label, pesanError(e)),
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
    // notify.tugas, bukan await-lalu-notify: `docker stop` menunggu container
    // menutup dirinya sampai batas SIGKILL (10 detik bawaan), dan `rm -f`
    // ikut menunggu. Selama itu pola lama tidak menampilkan apa pun — layar
    // tidak berubah sama sekali dan tombolnya terbaca tidak menanggapi.
    const namaAksi = action === "remove" ? tr("Hapus") : action
    try {
      await notify.tugas(
        apiSend(`/api/docker/containers/${encodeURIComponent(c.id)}/${action}`, "POST"),
        {
          jalan: trf("Container {0}: {1}…", c.name, namaAksi),
          sukses: trf("Container {0}: {1} berhasil.", c.name, action),
          gagal: (e) => trf("Gagal {0} container: {1}", action, pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const bukaLog = async (c: DockerContainer) => {
    try {
      const res = await apiGet<{ content: string }>(
        `/api/docker/containers/${encodeURIComponent(c.id)}/logs?tail=200`,
      )
      logIkutBawah.current = true
      setLogModal({ id: c.id, name: c.name, content: res.content || tr("(log kosong)") })
    } catch (e: any) {
      notify.err(trf("Gagal membaca log: {0}", pesanError(e)))
    }
  }

  // Isi log di-tarik ulang selama modal terbuka. Sebelumnya diambil sekali
  // saat modal dibuka, jadi baris baru hanya muncul kalau modal ditutup lalu
  // dibuka lagi — terbaca seperti log yang membeku.
  useEffect(() => {
    const id = logModal?.id
    if (!id) return
    const ctrl = new AbortController()
    const tarik = async () => {
      try {
        const res = await apiGet<{ content: string }>(
          `/api/docker/containers/${encodeURIComponent(id)}/logs?tail=200`,
          ctrl.signal,
        )
        setLogModal((m) => (m && m.id === id ? { ...m, content: res.content || tr("(log kosong)") } : m))
      } catch {
        // Modal tetap menampilkan isi terakhir. Container yang baru dihentikan
        // membuat endpoint ini gagal tiap 3 detik; toast berulang justru
        // menutupi log yang sedang dibaca.
      }
    }
    const t = setInterval(tarik, 3000)
    return () => {
      ctrl.abort()
      clearInterval(t)
    }
  }, [logModal?.id])

  // Menempel ke baris terbaru sesudah isi berubah.
  useEffect(() => {
    const el = logRef.current
    if (el && logIkutBawah.current) el.scrollTop = el.scrollHeight
  }, [logModal?.content])

  // Hanya jenis yang sedang dilihat yang ditarik. Menarik ketiganya di setiap
  // pembukaan halaman berarti tiga panggilan docker tambahan untuk dua tabel
  // yang belum tentu dibuka sama sekali.
  const loadDaya = async (jenis: JenisDaya) => {
    setLoadingDaya(true)
    try {
      if (jenis === "images") setImages(await apiGet<DockerImage[]>("/api/docker/images"))
      else if (jenis === "volumes") setVolumes(await apiGet<DockerVolume[]>("/api/docker/volumes"))
      else setNetworks(await apiGet<DockerNetwork[]>("/api/docker/networks"))
    } catch (e: any) {
      notify.err(trf("Gagal memuat daftar: {0}", pesanError(e)))
    } finally {
      setLoadingDaya(false)
    }
  }

  // Kegagalan df TIDAK memunculkan toast: di mesin tanpa Docker halaman ini
  // sudah menampilkan satu pesan dari daftar container, dan pesan kedua yang
  // mengatakan hal yang sama hanya menambah kebisingan. Barisnya cukup tidak
  // muncul.
  const loadDf = async () => {
    try {
      setDf((await apiGet<DockerDf[]>("/api/docker/df")) || [])
    } catch {
      setDf([])
    }
  }

  useEffect(() => {
    loadDaya(daya)
  }, [daya])

  useEffect(() => {
    loadDf()
  }, [])

  // pruneDf membebaskan ruang untuk satu baris ringkasan. Sesudahnya df DAN
  // tabel yang sedang dibuka dimuat ulang: menghapus image mengubah keduanya,
  // dan angka lama yang bertahan di layar terbaca sebagai aksi yang gagal.
  const pruneDf = async (row: DockerDf) => {
    const aksi = aksiDf[row.type]
    if (!aksi) return
    const ok = await confirmDialog({
      title: aksi.judul,
      message: aksi.pesan,
      confirmLabel: tr("Bersihkan"),
      danger: row.type !== "Build Cache",
    })
    if (!ok) return
    // Aksi paling lama di halaman ini: `image prune -a` pada host yang penuh
    // menghapus puluhan GB dan berjalan beberapa menit. Tanpa toast yang
    // berputar selama itu, satu-satunya tanda bahwa sesuatu terjadi baru
    // muncul saat pekerjaannya selesai — dan kalau user sudah berpindah
    // halaman, toast berhasilnya datang tiba-tiba tanpa konteks.
    try {
      await notify.tugas(apiSend<{ output?: string }>(aksi.path, "POST"), {
        jalan: trf("Membersihkan {0}…", tr(row.type)),
        sukses: trf("{0} dibersihkan.", tr(row.type)),
        gagal: (e) => trf("Gagal membersihkan: {0}", pesanError(e)),
        // Baris "Total reclaimed space" dari docker adalah satu-satunya
        // jawaban yang dicari user sesudah menekan tombol ini.
        detail: (res) => res?.output?.trim() || undefined,
      })
      loadDf()
      loadDaya(daya)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  // Kalimat konfirmasi ditulis per jenis karena akibatnya memang berbeda
  // jauh: image bisa diunduh ulang, isi volume tidak bisa dikembalikan sama
  // sekali. Satu kalimat umum untuk ketiganya akan menyesatkan di kasus yang
  // paling mahal.
  const hapusDaya = async (jenis: JenisDaya, id: string, label: string) => {
    const pesan = {
      images: tr("Container yang memakai image ini harus mengunduhnya lagi sebelum bisa jalan."),
      volumes: tr("SELURUH isi volume ikut terhapus dan tidak bisa dikembalikan. Docker menolak kalau volume ini masih dipakai container mana pun, termasuk yang berhenti."),
      networks: tr("Container yang tersambung ke network ini harus dibuat ulang."),
    }
    const ok = await confirmDialog({
      title: trf("Hapus {0}?", label),
      message: pesan[jenis],
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    // Menghapus satu image berukuran GB tetap butuh beberapa detik: daemon
    // membongkar tiap layer. Toast yang berputar selama itu yang membedakan
    // "sedang jalan" dari "tombolnya tidak berfungsi".
    try {
      await notify.tugas(apiSend(`/api/docker/${jenis}/${encodeURIComponent(id)}`, "DELETE"), {
        jalan: trf("Menghapus {0}…", label),
        sukses: trf("{0} dihapus.", label),
        gagal: (e) => trf("Gagal menghapus {0}: {1}", label, pesanError(e)),
      })
      loadDaya(jenis)
      loadDf()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const prunePesan = {
    images: tr("Hanya image dangling — yang tidak punya tag sama sekali — yang dibuang. Image bertag tetap ada meski tidak sedang dipakai."),
    volumes: tr("Setiap volume yang tidak dipakai container mana pun dihapus BESERTA seluruh isinya. Volume stack yang sedang berhenti ikut terkena."),
    networks: tr("Network yang tidak dipakai container mana pun dihapus. Network bawaan docker tidak ikut."),
  }

  // Aksi pembebasan ruang per baris ringkasan pemakaian disk. Kuncinya nilai
  // .Type milik docker sendiri ("Images", "Local Volumes", "Build Cache"),
  // bukan terjemahannya — itu yang stabil antar versi docker.
  //
  // Containers tidak punya entri: `container prune` tidak ada di whitelist
  // helper, dan container yang berhenti sudah terlihat satu per satu di panel
  // Containers di atas — menghapusnya dari sana lebih jelas daripada satu
  // tombol yang menyapu tanpa menyebut yang mana.
  //
  // Images memakai varian `?semua=1` (`image prune -a`), BUKAN prune polos
  // yang sudah ada di tombol Bersihkan tab Images. Justru selisih itu yang
  // membuat baris ini ada: di host yang penuh image bertag tapi tak terpakai,
  // prune polos mengembalikan 0 B dan terbaca sebagai tombol rusak.
  const aksiDf: Record<string, { path: string; judul: string; pesan: string }> = {
    Images: {
      path: "/api/docker/images/prune?semua=1",
      judul: tr("Buang semua image yang tidak dipakai container?"),
      pesan: tr(
        "Bukan hanya image dangling: SETIAP image yang tidak dipakai container mana pun ikut dibuang, termasuk image stack yang sedang Down — container-nya sudah tidak ada, jadi image-nya dihitung tidak terpakai. Semuanya harus diunduh ulang sebelum stack itu bisa dinyalakan lagi.",
      ),
    },
    "Local Volumes": {
      path: "/api/docker/volumes/prune",
      judul: tr("Bersihkan volume yang tidak terpakai?"),
      pesan: prunePesan.volumes,
    },
    "Build Cache": {
      path: "/api/docker/buildcache/prune",
      judul: tr("Bersihkan cache build?"),
      pesan: tr(
        "Cache build seluruhnya hasil turunan — tidak ada data yang hilang. Yang dibayar cuma build image berikutnya yang mulai dari nol.",
      ),
    },
  }

  const pruneDaya = async () => {
    const ok = await confirmDialog({
      title: trf("Bersihkan {0} yang tidak terpakai?", labelDaya[daya]),
      message: prunePesan[daya],
      confirmLabel: tr("Bersihkan"),
      danger: daya === "volumes",
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend<{ output?: string }>(`/api/docker/${daya}/prune`, "POST"), {
        jalan: trf("Membersihkan {0}…", labelDaya[daya]),
        sukses: trf("{0} dibersihkan.", labelDaya[daya]),
        gagal: (e) => trf("Gagal membersihkan: {0}", pesanError(e)),
        detail: (res) => res?.output?.trim() || undefined,
      })
      loadDaya(daya)
      loadDf()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
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

    {/* Image / Volume / Network — satu panel, satu pemilih. */}
    <Panel
      title={tr("Image, Volume & Network")}
      hint={tr("Sumber daya Docker selain container")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex rounded border border-border">
            {(["images", "volumes", "networks"] as JenisDaya[]).map((j) => (
              <Button
                key={j}
                variant={daya === j ? "default" : "ghost"}
                size="sm"
                className="h-8 px-2 text-xs"
                onClick={() => setDaya(j)}
              >
                {j === "images" ? (
                  <Layers className="size-3.5 sm:mr-1" />
                ) : j === "volumes" ? (
                  <HardDrive className="size-3.5 sm:mr-1" />
                ) : (
                  <Network className="size-3.5 sm:mr-1" />
                )}
                <span className="sr-only sm:not-sr-only">{labelDaya[j]}</span>
              </Button>
            ))}
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={pruneDaya}
            title={prunePesan[daya]}
            disabled={loadingDaya}
          >
            <Eraser className="size-3.5 sm:mr-1" />
            <span className="sr-only sm:not-sr-only">{tr("Bersihkan")}</span>
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              loadDaya(daya)
              loadDf()
            }}
            disabled={loadingDaya}
          >
            <RefreshCw className={`size-3.5 ${loadingDaya ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      {/* Ringkasan pemakaian disk. Ditaruh DI ATAS ketiga tabel, bukan di tab
          tersendiri: pertanyaan "berapa yang bisa saya bebaskan" adalah yang
          membuat orang membuka panel ini, dan jawabannya tidak boleh ikut
          tersembunyi di balik pemilih tab. */}
      {df.length > 0 && (
        <div className="mb-3 overflow-x-auto rounded border border-border">
          <table className="tabel-kartu w-full text-left text-xs">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="pb-2 pl-2 font-medium">{tr("Pemakaian disk")}</th>
                <th className="pb-2 font-medium">{tr("Jumlah")}</th>
                <th className="pb-2 font-medium">{tr("Aktif")}</th>
                <th className="pb-2 font-medium">{tr("Ukuran")}</th>
                <th className="pb-2 font-medium">{tr("Bisa dibebaskan")}</th>
                <th className="pb-2 pr-2" />
              </tr>
            </thead>
            <tbody>
              {df.map((row) => (
                <tr key={row.type} className="border-b border-border/50 last:border-0">
                  <td className="py-1.5 pl-2 font-medium">{tr(row.type)}</td>
                  <td className="num py-1.5">{row.total}</td>
                  <td className="num py-1.5">{row.active}</td>
                  <td className="num py-1.5">{row.size}</td>
                  <td className="num py-1.5">{row.reclaimable}</td>
                  <td className="py-1.5 pr-2 text-right">
                    {aksiDf[row.type] && (
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 px-2 text-xs"
                        onClick={() => pruneDf(row)}
                        title={aksiDf[row.type].pesan}
                      >
                        <Eraser className="size-3.5 sm:mr-1" />
                        <span className="sr-only sm:not-sr-only">{tr("Bersihkan")}</span>
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="overflow-x-auto">
        {daya === "images" && (
          <table className="tabel-kartu w-full text-left text-xs">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="pb-2 font-medium">{tr("Repository")}</th>
                <th className="pb-2 font-medium">Tag</th>
                <th className="pb-2 font-medium">ID</th>
                <th className="pb-2 font-medium">{tr("Ukuran")}</th>
                <th className="pb-2 font-medium">{tr("Dibuat")}</th>
                <th className="pb-2 font-medium"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {images.map((im) => (
                <tr key={im.id} className="hover:bg-secondary/40">
                  <td data-label={tr("Repository")} className="py-2 font-medium">
                    <div className="flex items-center gap-2">
                      <span className="truncate">{im.repository}</span>
                      {im.dangling && <Badge tone="warn">dangling</Badge>}
                    </div>
                  </td>
                  <td data-label="Tag" className="num py-2 text-muted-foreground">{im.tag}</td>
                  <td data-label="ID" className="num py-2 text-muted-foreground">{im.id}</td>
                  <td data-label={tr("Ukuran")} className="num py-2 text-muted-foreground">{im.size}</td>
                  <td data-label={tr("Dibuat")} className="py-2 text-muted-foreground">{im.created}</td>
                  <td data-label="" className="py-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-1.5 text-muted-foreground hover:text-crit"
                      aria-label={trf("Hapus image {0}", im.repository + ":" + im.tag)}
                      title={tr("Hapus")}
                      onClick={() => hapusDaya("images", im.id, `image ${im.repository}:${im.tag}`)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
              {images.length === 0 && !loadingDaya && (
                <tr>
                  <td data-label="" colSpan={6} className="py-6 text-center text-muted-foreground">
                    {tr("Belum ada image di host ini.")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}

        {daya === "volumes" && (
          <table className="tabel-kartu w-full text-left text-xs">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="pb-2 font-medium">{tr("Nama")}</th>
                <th className="pb-2 font-medium">Driver</th>
                <th className="pb-2 font-medium">Mountpoint</th>
                <th className="pb-2 font-medium"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {volumes.map((v) => (
                <tr key={v.name} className="hover:bg-secondary/40">
                  <td data-label={tr("Nama")} className="py-2 font-medium">
                    <span className="break-all">{v.name}</span>
                  </td>
                  <td data-label="Driver" className="num py-2 text-muted-foreground">{v.driver}</td>
                  <td data-label="Mountpoint" className="num py-2 text-muted-foreground">
                    <span className="break-all">{v.mountpoint}</span>
                  </td>
                  <td data-label="" className="py-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-1.5 text-muted-foreground hover:text-crit"
                      aria-label={trf("Hapus volume {0}", v.name)}
                      title={tr("Hapus")}
                      onClick={() => hapusDaya("volumes", v.name, `volume ${v.name}`)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
              {volumes.length === 0 && !loadingDaya && (
                <tr>
                  <td data-label="" colSpan={4} className="py-6 text-center text-muted-foreground">
                    {/* Kalimat kedua ada karena pertanyaan yang sama muncul terus:
                        stack yang compose-nya penuh baris `volumes:` tetap
                        menghasilkan daftar kosong di sini. Bind mount memang bukan
                        volume Docker — ia tidak pernah terdaftar di `docker volume
                        ls`, tidak punya nama, dan tidak dikelola daemon. Tanpa
                        kalimat ini, tabel kosong terbaca sebagai panel yang rusak. */}
                    {tr("Belum ada named volume di host ini.")}
                    <div className="mx-auto mt-1 max-w-md text-[11px] leading-relaxed">
                      {tr("Bind mount (mis. /home/user/data:/data di compose) tidak muncul di sini — itu folder host biasa, bukan volume yang dikelola Docker.")}
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}

        {daya === "networks" && (
          <table className="tabel-kartu w-full text-left text-xs">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="pb-2 font-medium">{tr("Nama")}</th>
                <th className="pb-2 font-medium">Driver</th>
                <th className="pb-2 font-medium">Scope</th>
                <th className="pb-2 font-medium">ID</th>
                <th className="pb-2 font-medium"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {networks.map((n) => (
                <tr key={n.id} className="hover:bg-secondary/40">
                  <td data-label={tr("Nama")} className="py-2 font-medium">
                    <div className="flex items-center gap-2">
                      <span className="break-all">{n.name}</span>
                      {n.builtin && <Badge tone="muted">{tr("bawaan")}</Badge>}
                      {n.internal && <Badge tone="warn">internal</Badge>}
                    </div>
                  </td>
                  <td data-label="Driver" className="num py-2 text-muted-foreground">{n.driver}</td>
                  <td data-label="Scope" className="num py-2 text-muted-foreground">{n.scope}</td>
                  <td data-label="ID" className="num py-2 text-muted-foreground">{n.id}</td>
                  <td data-label="" className="py-2">
                    {/* Network bawaan tidak punya tombol hapus: daemon selalu
                        menolaknya, jadi tombolnya hanya menawarkan kegagalan. */}
                    {!n.builtin && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-1.5 text-muted-foreground hover:text-crit"
                        aria-label={trf("Hapus network {0}", n.name)}
                        title={tr("Hapus")}
                        onClick={() => hapusDaya("networks", n.id, `network ${n.name}`)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
              {networks.length === 0 && !loadingDaya && (
                <tr>
                  <td data-label="" colSpan={5} className="py-6 text-center text-muted-foreground">
                    {tr("Belum ada network di host ini.")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
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

    {/* Modal Log Container — read-only, mono, di-tarik ulang tiap 3 detik dan
        menempel ke baris terbaru selama user tidak menggulir ke atas. */}
    {logModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="flex max-h-[85dvh] w-full max-w-3xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
          <div className="flex items-center justify-between border-b border-border pb-2">
            <div className="min-w-0">
              <p className="text-sm font-semibold">Log {logModal.name}</p>
              <p className="num truncate text-[10px] text-muted-foreground">
                {trf("200 baris terakhir · {0}", logModal.id.substring(0, 12))}
                <span className="ml-1 text-ok">{tr("· live tiap 3 detik")}</span>
              </p>
            </div>
            <Button variant="outline" size="sm" onClick={() => setLogModal(null)}>
              {tr("Tutup")}
            </Button>
          </div>
          <pre
            ref={logRef}
            onScroll={(ev) => {
              const el = ev.currentTarget
              logIkutBawah.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
            }}
            className="mt-3 flex-1 overflow-auto whitespace-pre-wrap rounded border border-border bg-background p-3 font-mono text-[11px] leading-relaxed"
          >
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
