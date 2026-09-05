import { useEffect, useState, useRef, useCallback } from "react"
import { pesanError } from "@/lib/pesan-error"
import { useSearchParams } from "react-router-dom"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { promptDialog } from "@/components/ui/prompt"
import { useAuth } from "@/stores/auth"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { formatBytes, formatWaktu } from "@/lib/format"
import { salinKeClipboard } from "@/lib/utils"
import {
  Folder,
  File,
  Upload,
  FolderUp,
  FolderPlus,
  FilePlus,
  RefreshCw,
  Trash2,
  Download,
  Eye,
  Printer,
  ArrowLeft,
  Grid,
  List,
  MoreVertical,
  BookmarkPlus,
  Lock,
  Copy as CopyIcon,
  Scissors,
  Clipboard,
  ClipboardCopy,
  Edit3,
} from "lucide-react"

type FileEntry = {
  name: string
  path: string
  size: number
  mode: string
  mode_octal: number
  is_dir: boolean
  mod_time: number
  owner: string
  group: string
}

type FileRoot = {
  name: string
  path: string
  // Pintasan pool mergerfs; dikelompokkan di belakang label "Disk pool :".
  pool?: boolean
}

type PrinterRingkas = { name: string; state: string; default: boolean; enabled: boolean }

// Format yang bisa dicetak CUPS tanpa paket filter tambahan: PDF/PostScript,
// teks polos, dan gambar raster. Sengaja daftar tertutup — mengirim .docx ke
// lp hanya menghasilkan halaman berisi sampah biner, dan itu terlihat sebagai
// "printer rusak", bukan sebagai format yang tidak didukung.
//
// ponytail: keputusan diambil dari ekstensi, bukan dari tipe MIME hasil
// sniffing isi berkas. Berkas tanpa ekstensi atau yang ekstensinya keliru
// tidak akan menawarkan menu Print. Jalan naiknya: pakai Content-Type dari
// endpoint preview kalau nanti ada yang membutuhkannya.
const EKSTENSI_CETAK = [
  "pdf", "ps",
  "txt", "text", "log", "md", "csv", "conf", "cfg", "ini", "json", "xml", "yaml", "yml", "sh",
  "png", "jpg", "jpeg", "gif", "bmp", "tif", "tiff", "webp",
]

function bisaDicetak(e: FileEntry): boolean {
  if (e.is_dir) return false
  const ext = e.name.split(".").pop()?.toLowerCase()
  return !!ext && ext !== e.name.toLowerCase() && EKSTENSI_CETAK.includes(ext)
}

// Ekstensi yang dibuka dengan pemutar, bukan dibaca sebagai teks. Sebelumnya
// setiap berkas non-gambar diambil utuh lalu ditampilkan lewat res.text():
// satu klik pada rekaman 10 MB menarik seluruh isinya ke memori tab dan
// menumpahkannya sebagai byte mentah di dalam <pre>.
//
// Daftar ini SENGAJA lebih luas daripada yang bisa diputar browser. Yang
// menentukan berhasil tidaknya adalah codec di dalam wadah, dan itu tidak
// bisa diketahui dari ekstensi — .mkv berisi H.264+AAC diputar Chrome,
// .mkv berisi HEVC atau AV1 tidak; Firefox menolak wadah Matroska apa pun.
// Menyaring daftar ini lebih ketat hanya akan menolak berkas yang sebenarnya
// bisa diputar. Yang gagal ditangkap onError dan dijawab dengan tawaran
// unduh, bukan dengan layar hitam tanpa keterangan.
//
// ponytail: tidak ada transcoding. Menambahkan ffmpeg berarti satu dependensi
// besar, satu antrean kerja, dan CPU server dipakai tiap kali seseorang
// mengklik berkas. Jalan naiknya kalau nanti benar-benar dibutuhkan: remux
// on-the-fly ke fragmented MP4 lewat `ffmpeg -c copy`, yang murah selama
// codec-nya memang sudah didukung browser.
const EKSTENSI_VIDEO = ["mp4", "m4v", "mkv", "webm", "ogv", "mov", "avi", "mpeg", "mpg", "ts"]
const EKSTENSI_AUDIO = ["mp3", "m4a", "aac", "ogg", "opus", "flac", "wav"]

type ClipItem = { path: string; name: string }

type ClipboardOp = { kind: "none" } | { kind: "copy" | "cut"; items: ClipItem[] }

// Mode oktal baku Linux. Daftar tertutup: input bebas sebelumnya menerima
// ketikan seperti "7555" atau "-rwxr-xr-x" yang baru ditolak di backend.
const MODE_PRESETS = [
  { value: "755", symbolic: "rwxr-xr-x", hint: "folder & program, semua boleh baca/masuk" },
  { value: "775", symbolic: "rwxrwxr-x", hint: "folder kerja bersama satu grup" },
  { value: "750", symbolic: "rwxr-x---", hint: "folder terbatas pemilik & grup" },
  { value: "700", symbolic: "rwx------", hint: "folder privat, hanya pemilik" },
  { value: "644", symbolic: "rw-r--r--", hint: "file umum, semua boleh baca" },
  { value: "664", symbolic: "rw-rw-r--", hint: "file yang diedit satu grup" },
  { value: "640", symbolic: "rw-r-----", hint: "file terbatas pemilik & grup" },
  { value: "600", symbolic: "rw-------", hint: "file privat, mis. kunci & kredensial" },
  { value: "777", symbolic: "rwxrwxrwx", hint: "semua akses — hindari" },
]

// Mode file yang sedang berlaku belum tentu ada di daftar preset (mis. 2775
// setgid, atau 751). Tetap tampilkan supaya dropdown tidak diam-diam
// mengganti mode saat modal dibuka.
function modeOptions(current: string) {
  if (MODE_PRESETS.some((m) => m.value === current)) return MODE_PRESETS
  return [{ value: current, symbolic: symbolicMode(current), hint: "mode saat ini" }, ...MODE_PRESETS]
}

function symbolicMode(octal: string): string {
  const rwx = ["---", "--x", "-w-", "-wx", "r--", "r-x", "rw-", "rwx"]
  const digits = octal.slice(-3).padStart(3, "0")
  return [...digits].map((d) => rwx[Number(d)] ?? "???").join("")
}

// Root mana yang sedang dibuka. `startsWith` saja salah untuk dua hal:
// "Root (/)" cocok dengan SEMUA path sehingga selalu terlihat aktif, dan
// "~/DATA" ikut cocok untuk "~/DATABASE". Yang dipakai adalah root terdalam
// yang benar-benar memuat path saat ini.
function memuat(path: string, root: string): boolean {
  if (path === root) return true
  return path.startsWith(root === "/" ? "/" : root + "/")
}

export function rootAktif(path: string, roots: FileRoot[]): string {
  let terpilih = ""
  for (const r of roots) {
    if (memuat(path, r.path) && r.path.length > terpilih.length) terpilih = r.path
  }
  return terpilih
}

export function FileManagerView() {
  const tr = useTr()
  const user = useAuth((s) => s.user)
  const [searchParams, setSearchParams] = useSearchParams()
  const initialPath = searchParams.get("path") || user?.home || "/"
  const [currentPath, setCurrentPath] = useState(initialPath)
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [roots, setRoots] = useState<FileRoot[]>([])
  const [loading, setLoading] = useState(false)
  const [viewMode, setViewMode] = useState<"list" | "grid">("list")
  const [previewContent, setPreviewContent] = useState<{
    path: string
    text?: string
    isImg?: boolean
    media?: "video" | "audio"
  } | null>(null)
  // Diset saat elemen <video>/<audio> gagal men-decode. Browser tidak
  // memberitahu ALASAN kegagalan lewat API mana pun, jadi yang bisa
  // ditawarkan hanya unduhan — lihat catatan di EKSTENSI_VIDEO.
  const [mediaGagal, setMediaGagal] = useState(false)
  const [permTarget, setPermTarget] = useState<FileEntry | null>(null)
  const [permMode, setPermMode] = useState("755")
  // Editor teks: dipakai untuk membuat file baru dan mengubah isi file yang ada.
  const [editor, setEditor] = useState<{ path: string; content: string; baru: boolean } | null>(null)
  const [menyimpanFile, setMenyimpanFile] = useState(false)
  const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null)
  const [renameValue, setRenameValue] = useState("")
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; entry: FileEntry } | null>(null)
  // Dialog cetak: entry yang dipilih dari context menu, plus daftar printer
  // yang baru diambil saat dialog dibuka — bukan saat halaman dimuat, karena
  // sebagian besar kunjungan ke file manager tidak berakhir dengan mencetak.
  const [printTarget, setPrintTarget] = useState<FileEntry | null>(null)
  const [printers, setPrinters] = useState<PrinterRingkas[]>([])
  const [printForm, setPrintForm] = useState({ printer: "", copies: 1, media: "", sides: "" })
  const [mencetak, setMencetak] = useState(false)
  const [clipboard, setClipboard] = useState<ClipboardOp>({ kind: "none" })
  // Seleksi disimpan sebagai path, bukan indeks: isi direktori bisa berubah
  // di antara refresh, dan indeks lama akan menunjuk berkas yang salah.
  const [selected, setSelected] = useState<Set<string>>(new Set())
  // stat() pada direktori mengembalikan ukuran inode-nya sendiri, bukan
  // isinya, jadi ukuran folder dihitung terpisah lewat /api/files/usage.
  // Kunci = path, nilai < 0 = folder tidak terbaca. Hasil sengaja TIDAK
  // dibuang saat pindah direktori: kembali ke folder yang sama langsung
  // menampilkan angka lama sementara nilai barunya dihitung ulang di latar.
  const [ukuranFolder, setUkuranFolder] = useState<Record<string, number>>({})
  const [parsial, setParsial] = useState<Set<string>>(new Set())
  const fileInputRef = useRef<HTMLInputElement>(null)
  const folderInputRef = useRef<HTMLInputElement>(null)

  const loadRoots = async () => {
    try {
      const data = await apiGet<FileRoot[]>("/api/files/roots")
      setRoots(data)
    } catch {}
  }

  const loadDir = useCallback(async (path: string) => {
    setLoading(true)
    try {
      const res = await apiGet<{ path: string; entries: FileEntry[] }>(`/api/files?path=${encodeURIComponent(path)}`)
      setEntries(res.entries || [])
      setCurrentPath(res.path)
      setContextMenu(null)
      setSelected(new Set())
      // Sinkronkan path ke URL supaya bookmark bisa di-share dan tombol
      // back browser bekerja.
      setSearchParams((p) => {
        if (p.get("path") === res.path) return p
        p.set("path", res.path)
        return p
      }, { replace: true })
    } catch (e: any) {
      notify.err(trf("Gagal membuka direktori: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }, [setSearchParams])

  useEffect(() => {
    loadRoots()
    loadDir(initialPath)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Tutup context menu saat klik di tempat lain atau tekan Escape.
  useEffect(() => {
    if (!contextMenu) return
    const close = () => setContextMenu(null)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close()
    }
    document.addEventListener("click", close)
    document.addEventListener("scroll", close, true)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("click", close)
      document.removeEventListener("scroll", close, true)
      document.removeEventListener("keydown", onKey)
    }
  }, [contextMenu])

  // Ukuran tiap folder di layar dihitung setelah listing tampil. Penelusuran
  // sendiri sudah satu worker per folder (file.usage di helper), jadi yang
  // tersisa adalah menunggu disk — dan itu ditutupi dengan beberapa folder
  // dihitung berbarengan. Pindah direktori membatalkan sisanya lewat
  // AbortController.
  //
  // ponytail: 4 sekaligus, angka yang cukup menutupi latensi disk tanpa
  // membuat helper melahirkan worker sebanyak isi folder. Naikkan hanya kalau
  // pengukuran menunjukkan disk masih menganggur.
  useEffect(() => {
    const dirs = entries.filter((e) => e.is_dir)
    if (dirs.length === 0) return
    const ac = new AbortController()
    let berikutnya = 0
    const pekerja = async () => {
      while (!ac.signal.aborted) {
        const d = dirs[berikutnya++]
        if (!d) return
        try {
          const res = await apiGet<{ size: number; partial: boolean }>(
            `/api/files/usage?path=${encodeURIComponent(d.path)}`,
            ac.signal,
          )
          setUkuranFolder((u) => ({ ...u, [d.path]: res.size }))
          setParsial((p) => {
            if (p.has(d.path) === res.partial) return p
            const n = new Set(p)
            if (res.partial) n.add(d.path)
            else n.delete(d.path)
            return n
          })
        } catch {
          // Folder tanpa izin baca cukup ditandai "—"; memberi notifikasi per
          // folder hanya membanjiri layar saat membuka direktori sistem.
          if (!ac.signal.aborted) setUkuranFolder((u) => ({ ...u, [d.path]: -1 }))
        }
      }
    }
    void Promise.all(Array.from({ length: Math.min(4, dirs.length) }, pekerja))
    return () => ac.abort()
  }, [entries])

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files?.length) return
    const files = Array.from(e.target.files)
    const formData = new FormData()
    // webkitRelativePath berisi "folder/sub/berkas.txt" saat user memilih
    // folder. Dikirim sebagai nama part supaya server bisa membentuk ulang
    // strukturnya; upload file biasa tetap mengirim nama polos.
    files.forEach((f) => formData.append("files", f, f.webkitRelativePath || f.name))

    setLoading(true)
    try {
      const res = await fetch(`/api/files/upload?path=${encodeURIComponent(currentPath)}`, {
        method: "POST",
        credentials: "include",
        body: formData,
      })
      if (!res.ok) throw new Error(`Upload error ${res.status}`)
      loadDir(currentPath)
    } catch (err: any) {
      notify.err(trf("Upload gagal: {0}", pesanError(err)))
    } finally {
      setLoading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
      if (folderInputRef.current) folderInputRef.current.value = ""
    }
  }

  const handleMkdir = async () => {
    const name = await promptDialog({
      title: tr("Folder baru"),
      label: tr("Nama folder"),
      detail: currentPath,
      confirmLabel: tr("Buat"),
    })
    if (!name) return
    try {
      await notify.tugas(apiSend("/api/files/mkdir", "POST", { path: `${currentPath}/${name}` }), {
        jalan: trf("Membuat folder {0}…", name),
        sukses: trf("Folder {0} dibuat.", name),
        gagal: (e) => trf("Gagal membuat folder: {0}", pesanError(e)),
      })
      loadDir(currentPath)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleFileBaru = async () => {
    const name = await promptDialog({
      title: tr("File baru"),
      label: tr("Nama file"),
      detail: currentPath,
      confirmLabel: tr("Buat"),
    })
    if (!name) return
    setEditor({ path: `${currentPath}/${name}`.replace("//", "/"), content: "", baru: true })
  }

  const bukaEditor = async (entry: FileEntry) => {
    try {
      const res = await apiGet<{ path: string; content: string }>(
        `/api/files/content?path=${encodeURIComponent(entry.path)}`,
      )
      setEditor({ path: res.path, content: res.content, baru: false })
    } catch (e: any) {
      notify.err(trf("Tidak bisa membuka file: {0}", pesanError(e)))
    }
  }

  const simpanEditor = async () => {
    if (!editor) return
    setMenyimpanFile(true)
    const baru = editor.baru
    try {
      await notify.tugas(
        apiSend("/api/files/content", "PUT", { path: editor.path, content: editor.content }),
        {
          jalan: tr("Menyimpan file…"),
          sukses: baru ? tr("File dibuat.") : tr("Perubahan tersimpan."),
          gagal: (e) => trf("Gagal menyimpan file: {0}", pesanError(e)),
        },
      )
      setEditor(null)
      loadDir(currentPath)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setMenyimpanFile(false)
    }
  }

  const handleDelete = async (entry: FileEntry) => {
    const ok = await confirmDialog({
      title: trf("Hapus {0} {1}?", entry.is_dir ? tr("folder") : tr("file"), entry.name),
      message: entry.is_dir
        ? tr("Folder beserta seluruh isinya dihapus permanen — tidak masuk Trash.")
        : tr("File dihapus permanen — tidak masuk Trash."),
      detail: entry.path,
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    // Menghapus folder besar berjalan rekursif di server — bisa puluhan detik
    // untuk pohon berisi ribuan berkas.
    try {
      await notify.tugas(apiSend("/api/files/delete", "POST", { path: entry.path }), {
        jalan: trf("Menghapus {0}…", entry.name),
        sukses: trf("{0} dihapus.", entry.name),
        gagal: (e) => trf("Gagal menghapus: {0}", pesanError(e)),
      })
      loadDir(currentPath)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleAddBookmark = async () => {
    const name = await promptDialog({
      title: tr("Simpan bookmark"),
      label: tr("Nama bookmark"),
      detail: currentPath,
      defaultValue: currentPath.split("/").pop() || currentPath,
    })
    if (!name) return
    try {
      await apiSend("/api/bookmarks", "POST", { name, path: currentPath })
      notify.ok(tr("Bookmark disimpan."))
    } catch (e: any) {
      notify.err(trf("Gagal menyimpan bookmark: {0}", pesanError(e)))
    }
  }

  // Kegagalan dilaporkan, bukan didiamkan: tombol salin yang tidak berbunyi
  // apa-apa membuat user menempel path lama tanpa sadar. Lihat catatan di
  // salinKeClipboard soal kenapa jalur cadangan wajib ada di panel http.
  const salinPath = async (path: string) => {
    if (await salinKeClipboard(path)) {
      notify.ok(tr("Path disalin"), path)
    } else {
      notify.err(tr("Gagal menyalin path"), path)
    }
  }

  const handlePreview = (entry: FileEntry) => {
    const ext = entry.name.split(".").pop()?.toLowerCase()
    const imgExts = ["png", "jpg", "jpeg", "gif", "webp", "svg"]
    setMediaGagal(false)
    if (ext && imgExts.includes(ext)) {
      setPreviewContent({ path: entry.path, isImg: true })
      return
    }
    // Media tidak diambil lebih dulu: elemen <video>/<audio> yang menarik
    // sendiri lewat Range, sepotong demi sepotong sesuai posisi pemutaran.
    if (ext && EKSTENSI_VIDEO.includes(ext)) {
      setPreviewContent({ path: entry.path, media: "video" })
      return
    }
    if (ext && EKSTENSI_AUDIO.includes(ext)) {
      setPreviewContent({ path: entry.path, media: "audio" })
      return
    }
    void (async () => {
      try {
        const res = await fetch(`/api/files/preview?path=${encodeURIComponent(entry.path)}`, { credentials: "include" })
        if (!res.ok) throw new Error(tr("Gagal membaca preview"))
        const text = await res.text()
        setPreviewContent({ path: entry.path, text, isImg: false })
      } catch (e: any) {
        notify.err(trf("Gagal preview file: {0}", pesanError(e)))
      }
    })()
  }

  // Daftar printer diambil saat dialog dibuka. Kalau CUPS belum terpasang,
  // endpoint mengembalikan daftar kosong (bukan error), jadi dialognya tetap
  // terbuka dan menjelaskan apa yang kurang alih-alih melempar toast merah.
  const bukaDialogPrint = async (e: FileEntry) => {
    setPrintTarget(e)
    setPrintForm({ printer: "", copies: 1, media: "", sides: "" })
    try {
      const p = (await apiGet<PrinterRingkas[]>("/api/print/printers")) || []
      setPrinters(p)
      const bawaan = p.find((x) => x.default) || p.find((x) => x.enabled) || p[0]
      if (bawaan) setPrintForm((f) => ({ ...f, printer: bawaan.name }))
    } catch (err: any) {
      notify.err(trf("Gagal memuat daftar printer: {0}", pesanError(err)))
    }
  }

  const kirimCetak = async (ev: React.FormEvent) => {
    ev.preventDefault()
    if (!printTarget) return
    setMencetak(true)
    const namaCetak = printTarget.name
    try {
      await notify.tugas(
        apiSend<{ job_id?: string; printer?: string }>("/api/print/file", "POST", {
          path: printTarget.path,
          printer: printForm.printer || undefined,
          copies: printForm.copies > 1 ? printForm.copies : undefined,
          media: printForm.media || undefined,
          sides: printForm.sides || undefined,
        }),
        {
          jalan: trf("Mengirim {0} ke printer…", namaCetak),
          sukses: (hasil) =>
            hasil?.job_id
              ? trf("Dikirim ke {0} — antrean {1}", hasil.printer || printForm.printer, hasil.job_id)
              : trf("{0} dikirim ke printer", namaCetak),
          detail: () => tr("Pantau progresnya di Settings → Print server."),
          gagal: (e) => trf("Gagal mencetak: {0}", pesanError(e)),
        },
      )
      setPrintTarget(null)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setMencetak(false)
    }
  }

  const handleSavePerms = async () => {
    if (!permTarget) return
    const ok = await confirmDialog({
      title: trf("Ubah permission jadi {0}?", permMode),
      message: trf("Mode simbolik: {0}. Salah set bisa membuat file tidak terbaca atau justru terbuka untuk semua user.", symbolicMode(permMode)),
      detail: permTarget.path,
      confirmLabel: tr("Ubah"),
      danger: permMode === "777",
    })
    if (!ok) return
    const namaPerm = permTarget.name
    try {
      await notify.tugas(
        apiSend("/api/files/permissions", "PUT", { path: permTarget.path, mode: permMode }),
        {
          jalan: trf("Mengubah permission {0}…", namaPerm),
          sukses: trf("Permission {0} jadi {1}.", namaPerm, permMode),
          gagal: (e) => trf("Gagal ubah permission: {0}", pesanError(e)),
        },
      )
      setPermTarget(null)
      loadDir(currentPath)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleRename = async () => {
    if (!renameTarget || !renameValue || renameValue === renameTarget.name) {
      setRenameTarget(null)
      return
    }
    const newPath = `${currentPath}/${renameValue}`
    const ok = await confirmDialog({
      title: trf('Ganti nama jadi "{0}"?', renameValue),
      message: tr("Kalau sudah ada berkas dengan nama itu di folder ini, berkas tersebut akan tertimpa."),
      detail: renameTarget.path,
      confirmLabel: tr("Ganti nama"),
    })
    if (!ok) return
    const namaLama = renameTarget.name
    try {
      await notify.tugas(
        apiSend("/api/files/rename", "POST", { source: renameTarget.path, dest: newPath }),
        {
          jalan: trf("Mengganti nama {0}…", namaLama),
          sukses: trf("{0} diganti nama jadi {1}.", namaLama, renameValue),
          gagal: (e) => trf("Gagal rename: {0}", pesanError(e)),
        },
      )
      setRenameTarget(null)
      loadDir(currentPath)
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const handleCopy = (entry: FileEntry) => {
    setClipboard({ kind: "copy", items: [{ path: entry.path, name: entry.name }] })
    setContextMenu(null)
  }
  const handleCut = (entry: FileEntry) => {
    setClipboard({ kind: "cut", items: [{ path: entry.path, name: entry.name }] })
    setContextMenu(null)
  }
  const handlePaste = async () => {
    if (clipboard.kind === "none") return
    const label =
      clipboard.items.length === 1
        ? `"${clipboard.items[0].name}"`
        : trf("{0} item", clipboard.items.length)
    const ok = await confirmDialog({
      title:
        clipboard.kind === "copy"
          ? trf("Salin {0} ke sini?", label)
          : trf("Pindahkan {0} ke sini?", label),
      message: tr("Berkas dengan nama sama di folder tujuan akan tertimpa."),
      detail: currentPath,
      confirmLabel: clipboard.kind === "copy" ? tr("Salin") : tr("Pindahkan"),
      danger: clipboard.kind === "cut",
    })
    if (!ok) return
    const url = clipboard.kind === "copy" ? "/api/files/copy" : "/api/files/move"
    const salin = clipboard.kind === "copy"
    const jumlah = clipboard.items.length
    // Seluruh perulangan dibungkus SATU promise supaya toast-nya berputar dari
    // item pertama sampai item terakhir. Menyalin folder besar berjalan
    // menit-menitan; tanpa ini layar diam sepanjang itu dan hasilnya baru
    // muncul di akhir — persis keluhan "tiba-tiba selesai".
    const kerjakan = async () => {
      let gagal = 0
      for (const it of clipboard.items) {
        try {
          await apiSend(url, "POST", {
            source: it.path,
            dest: `${currentPath}/${it.name}`.replace(/\/+/g, "/"),
          })
        } catch {
          gagal++
        }
      }
      // Dilempar, bukan dikembalikan: kegagalan sebagian tidak boleh tampil
      // sebagai toast hijau. Pesannya sama dengan versi lama.
      if (gagal) throw new Error(trf("{0} item gagal dipaste.", gagal))
      return jumlah
    }
    try {
      await notify.tugas(kerjakan(), {
        jalan: salin ? trf("Menyalin {0} item…", jumlah) : trf("Memindahkan {0} item…", jumlah),
        sukses: salin ? trf("{0} item disalin.", jumlah) : trf("{0} item dipindahkan.", jumlah),
        gagal: (e) => pesanError(e),
      })
      // Cut baru dianggap selesai kalau semuanya pindah; sisanya masih di
      // tempat lama dan tetap butuh clipboard-nya.
      if (!salin) setClipboard({ kind: "none" })
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      loadDir(currentPath)
    }
  }

  const pilihan = entries.filter((e) => selected.has(e.path))

  const togglePilih = (path: string) =>
    setSelected((s) => {
      const n = new Set(s)
      if (n.has(path)) n.delete(path)
      else n.add(path)
      return n
    })

  const toggleSemua = () =>
    setSelected((s) => (s.size === entries.length ? new Set() : new Set(entries.map((e) => e.path))))

  // Satu berkas diunduh apa adanya; folder atau pilihan jamak dibungkus zip
  // oleh server supaya strukturnya utuh.
  const unduhTerpilih = () => {
    if (pilihan.length === 0) return
    window.location.href =
      pilihan.length === 1 && !pilihan[0].is_dir
        ? `/api/files/download?path=${encodeURIComponent(pilihan[0].path)}`
        : `/api/files/archive?${pilihan.map((e) => `path=${encodeURIComponent(e.path)}`).join("&")}`
  }

  const kolomUkuran = (e: FileEntry) => {
    if (!e.is_dir) return formatBytes(e.size)
    const u = ukuranFolder[e.path]
    if (u === undefined) return "…"
    if (u < 0) return "—"
    // Penelusuran yang berhenti di batas hanya boleh dilaporkan sebagai
    // batas bawah; menampilkannya polos berarti angka yang salah.
    return (parsial.has(e.path) ? "≥ " : "") + formatBytes(u)
  }

  const salinTerpilih = (kind: "copy" | "cut") => {
    setClipboard({ kind, items: pilihan.map((e) => ({ path: e.path, name: e.name })) })
    setSelected(new Set())
  }

  const hapusTerpilih = async () => {
    if (pilihan.length === 0) return
    const ok = await confirmDialog({
      title: trf("Hapus {0} item terpilih?", pilihan.length),
      message: tr("Folder beserta seluruh isinya dihapus permanen — tidak masuk Trash."),
      detail: pilihan.map((e) => e.name).join(", "),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    const jumlah = pilihan.length
    // ponytail: hapus satu per satu lewat endpoint yang sudah ada; endpoint
    // batch baru sepadan kalau seleksi ribuan berkas jadi hal biasa.
    const kerjakan = async () => {
      let gagal = 0
      for (const e of pilihan) {
        try {
          await apiSend("/api/files/delete", "POST", { path: e.path })
        } catch {
          gagal++
        }
      }
      if (gagal) throw new Error(trf("{0} item gagal dihapus.", gagal))
      return jumlah
    }
    try {
      await notify.tugas(kerjakan(), {
        jalan: trf("Menghapus {0} item…", jumlah),
        sukses: trf("{0} item dihapus.", jumlah),
        gagal: (e) => pesanError(e),
      })
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setSelected(new Set())
      loadDir(currentPath)
    }
  }

  const aktif = rootAktif(currentPath, roots)

  const upDir = () => {
    const parts = currentPath.split("/").filter(Boolean)
    parts.pop()
    const parent = "/" + parts.join("/")
    loadDir(parent || "/")
  }

  const openEntry = (e: FileEntry) => {
    if (e.is_dir) loadDir(e.path)
    else handlePreview(e)
  }

  const startRename = (e: FileEntry) => {
    setRenameTarget(e)
    setRenameValue(e.name)
    setContextMenu(null)
  }

  return (
    <div className="space-y-4" onClick={() => setContextMenu(null)}>
      <Panel
        title={tr("File Manager")}
        hint={currentPath}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleUpload} />
            {/* webkitdirectory belum ada di tipe JSX bawaan React, tapi didukung
                Chrome, Edge, Firefox, dan Safari untuk memilih satu folder utuh. */}
            <input
              ref={folderInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleUpload}
              {...({ webkitdirectory: "", directory: "" } as Record<string, string>)}
            />
            <Button size="sm" onClick={() => fileInputRef.current?.click()} disabled={loading}>
              <Upload className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Upload File")}</span>
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => folderInputRef.current?.click()}
              disabled={loading}
              title={tr("Unggah satu folder beserta seluruh isinya")}
            >
              <FolderUp className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Upload Folder")}</span>
            </Button>
            <Button variant="outline" size="sm" onClick={handleMkdir}>
              <FolderPlus className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Buat Folder")}</span>
            </Button>
            <Button variant="outline" size="sm" onClick={handleFileBaru}>
              <FilePlus className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("File Baru")}</span>
            </Button>
            <Button variant="outline" size="sm" onClick={handleAddBookmark} title={tr("Simpan folder saat ini ke Bookmarks")}>
              <BookmarkPlus className="size-3.5" />
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => salinPath(currentPath)}
              title={tr("Salin path folder ini")}
              aria-label={tr("Salin path folder ini")}
            >
              <ClipboardCopy className="size-3.5" />
            </Button>
            {clipboard.kind !== "none" && (
              <Button
                variant="outline"
                size="sm"
                onClick={handlePaste}
                title={`Paste ${clipboard.kind}: ${clipboard.items.map((i) => i.name).join(", ")}`}
              >
                <Clipboard className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Paste")}</span>
              </Button>
            )}
            <div className="flex rounded border border-border">
              <Button
                variant={viewMode === "list" ? "default" : "ghost"}
                size="sm"
                className="h-8 px-2"
                onClick={() => setViewMode("list")}
              >
                <List className="size-3.5" />
              </Button>
              <Button
                variant={viewMode === "grid" ? "default" : "ghost"}
                size="sm"
                className="h-8 px-2"
                onClick={() => setViewMode("grid")}
              >
                <Grid className="size-3.5" />
              </Button>
            </div>
            <Button variant="outline" size="sm" onClick={() => loadDir(currentPath)} disabled={loading}>
              <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </div>
        }
      >
        <div className="mb-3 flex flex-wrap items-center gap-1.5 border-b border-border pb-3">
          <Button variant="ghost" size="sm" onClick={upDir} disabled={currentPath === "/" || loading}>
            <ArrowLeft className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Naik")}</span>
          </Button>
          {roots
            .filter((r) => !r.pool)
            .map((r) => (
              <Badge
                key={r.path}
                tone={r.path === aktif ? "signal" : "muted"}
                className="cursor-pointer"
                onClick={() => loadDir(r.path)}
              >
                {r.name}
              </Badge>
            ))}
          {roots.some((r) => r.pool) && (
            <>
              <span className="ml-1 text-xs text-muted-foreground">{tr("Disk pool :")}</span>
              {roots
                .filter((r) => r.pool)
                .map((r) => (
                  <Badge
                    key={r.path}
                    tone={r.path === aktif ? "signal" : "muted"}
                    className="cursor-pointer"
                    onClick={() => loadDir(r.path)}
                  >
                    {r.name}
                  </Badge>
                ))}
            </>
          )}
        </div>

        {selected.size > 0 && (
          <div className="mb-3 flex flex-wrap items-center gap-2 rounded border border-border bg-secondary/40 px-3 py-2">
            <span className="text-xs font-medium">{trf("{0} item terpilih", selected.size)}</span>
            <Button variant="outline" size="sm" onClick={unduhTerpilih}>
              <Download className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Download")}</span>
            </Button>
            <Button variant="outline" size="sm" onClick={() => salinTerpilih("copy")}>
              <CopyIcon className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">Copy</span>
            </Button>
            <Button variant="outline" size="sm" onClick={() => salinTerpilih("cut")}>
              <Scissors className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">Cut</span>
            </Button>
            <Button variant="outline" size="sm" className="text-crit" onClick={hapusTerpilih}>
              <Trash2 className="size-3.5 sm:mr-1" /> <span className="sr-only sm:not-sr-only">{tr("Hapus")}</span>
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
              {tr("Batal pilih")}
            </Button>
          </div>
        )}

        {viewMode === "list" ? (
          <div className="overflow-x-auto">
            <table className="tabel-kartu w-full text-left text-xs">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="w-8 pb-2">
                    <input
                      type="checkbox"
                      aria-label={tr("Pilih semua")}
                      title={tr("Pilih semua")}
                      checked={entries.length > 0 && selected.size === entries.length}
                      onChange={toggleSemua}
                    />
                  </th>
                  <th className="pb-2 font-medium">{tr("Nama")}</th>
                  <th className="pb-2 font-medium">{tr("Ukuran")}</th>
                  <th className="pb-2 font-medium">{tr("Izin")}</th>
                  <th className="pb-2 font-medium">{tr("Owner/Group")}</th>
                  <th className="pb-2 font-medium">{tr("Modifikasi")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {entries.map((e) => (
                  <tr
                    key={e.name}
                    className="hover:bg-secondary/40"
                    onContextMenu={(ev) => {
                      ev.preventDefault()
                      setContextMenu({ x: ev.clientX, y: ev.clientY, entry: e })
                    }}
                  >
                    <td data-label="" className="py-2">
                      <input
                        type="checkbox"
                        aria-label={e.name}
                        checked={selected.has(e.path)}
                        onChange={() => togglePilih(e.path)}
                      />
                    </td>
                    <td data-label="" className="py-2">
                      <div
                        className="flex w-full cursor-pointer items-center gap-2 font-medium"
                        onClick={() => openEntry(e)}
                        onDoubleClick={() => openEntry(e)}
                      >
                        {e.is_dir ? (
                          <Folder className="size-4 text-amber-500 fill-amber-500/20" />
                        ) : (
                          <File className="size-4 text-muted-foreground" />
                        )}
                        <span className="truncate max-w-xs">{e.name}</span>
                        {/* Rename, Edit teks, dan Ubah Permission HANYA ada di
                            menu klik-kanan. Layar sentuh tidak punya klik
                            kanan yang bisa diandalkan — iOS membalas tekan-lama
                            dengan menu bawaannya sendiri — jadi tanpa tombol
                            ini ketiga aksi itu tidak terjangkau dari HP. */}
                        <button
                          className="ml-auto shrink-0 rounded p-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
                          aria-label={trf("Salin path {0}", e.name)}
                          title={tr("Salin path")}
                          onClick={(ev) => {
                            ev.stopPropagation()
                            salinPath(e.path)
                          }}
                        >
                          <ClipboardCopy className="size-4" />
                        </button>
                        <button
                          className="shrink-0 rounded p-1.5 text-muted-foreground hover:bg-secondary sm:hidden"
                          aria-label={trf("Aksi untuk {0}", e.name)}
                          onClick={(ev) => {
                            ev.stopPropagation()
                            const r = ev.currentTarget.getBoundingClientRect()
                            setContextMenu({ x: r.right, y: r.bottom, entry: e })
                          }}
                        >
                          <MoreVertical className="size-4" />
                        </button>
                      </div>
                    </td>
                    <td data-label={tr("Ukuran")} className="num py-2 text-muted-foreground">{kolomUkuran(e)}</td>
                    <td data-label={tr("Izin")} className="num py-2 text-muted-foreground">{e.mode}</td>
                    <td data-label={tr("Owner/Group")} className="num py-2 text-muted-foreground">
                      {e.owner}:{e.group}
                    </td>
                    <td data-label={tr("Modifikasi")} className="py-2 text-muted-foreground">
                      {formatWaktu(e.mod_time * 1000)}
                    </td>
                  </tr>
                ))}
                {entries.length === 0 && (
                  <tr>
                    <td data-label="" colSpan={6} className="py-6 text-center text-muted-foreground">
                      {tr("Direktori kosong")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 md:grid-cols-6">
            {entries.map((e) => (
              <div
                key={e.name}
                className="group relative flex flex-col items-center rounded-lg border border-border p-3 text-center hover:bg-secondary/40 cursor-pointer"
                onClick={() => openEntry(e)}
                onContextMenu={(ev) => {
                  ev.preventDefault()
                  setContextMenu({ x: ev.clientX, y: ev.clientY, entry: e })
                }}
              >
                <input
                  type="checkbox"
                  aria-label={e.name}
                  className="absolute left-2 top-2"
                  checked={selected.has(e.path)}
                  onClick={(ev) => ev.stopPropagation()}
                  onChange={() => togglePilih(e.path)}
                />
                {e.is_dir ? (
                  <Folder className="size-10 text-amber-500 fill-amber-500/20" />
                ) : (
                  <File className="size-10 text-muted-foreground" />
                )}
                <span className="mt-2 w-full truncate text-xs font-medium" title={e.name}>
                  {e.name}
                </span>
                <span className="text-[10px] text-muted-foreground">{kolomUkuran(e)}</span>
              </div>
            ))}
          </div>
        )}
      </Panel>

      {/* Context menu (klik kanan) — semua aksi file dipusatkan di sini,
          kolom Aksi di tabel dihapus supaya list view lebih bersih. */}
      {contextMenu && (
        <div
          className="fixed z-50 min-w-[200px] rounded-md border border-border bg-surface p-1 shadow-xl"
          // Posisi dijepit ke dalam layar: di HP menu 200px yang dibuka dekat
          // tepi kanan atau bawah akan setengahnya berada di luar viewport dan
          // aksi terakhirnya tidak pernah bisa disentuh.
          style={{
            left: Math.max(8, Math.min(contextMenu.x, window.innerWidth - 216)),
            top: Math.max(8, Math.min(contextMenu.y, window.innerHeight - 320)),
          }}
          onClick={(ev) => ev.stopPropagation()}
        >
          {!contextMenu.entry.is_dir && (
            <button
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
              onClick={() => { const e = contextMenu.entry; setContextMenu(null); handlePreview(e) }}
            >
              <Eye className="size-3.5" /> {tr("Preview")}
            </button>
          )}
          {/* Print hanya muncul untuk format yang benar-benar bisa diteruskan
              CUPS. Menampilkannya pada .docx atau .zip cuma menghasilkan
              halaman berisi sampah biner dan kertas terbuang. */}
          {bisaDicetak(contextMenu.entry) && (
            <button
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
              onClick={() => { const e = contextMenu.entry; setContextMenu(null); bukaDialogPrint(e) }}
            >
              <Printer className="size-3.5" /> {tr("Print")}
            </button>
          )}
          <a
            href={
              contextMenu.entry.is_dir
                ? `/api/files/archive?path=${encodeURIComponent(contextMenu.entry.path)}`
                : `/api/files/download?path=${encodeURIComponent(contextMenu.entry.path)}`
            }
            download
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => setContextMenu(null)}
          >
            <Download className="size-3.5" /> {contextMenu.entry.is_dir ? tr("Download (.zip)") : tr("Download")}
          </a>
          {!contextMenu.entry.is_dir && (
            <button
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
              onClick={() => { const e = contextMenu.entry; setContextMenu(null); bukaEditor(e) }}
            >
              <FilePlus className="size-3.5" /> {tr("Edit teks")}
            </button>
          )}
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => { const e = contextMenu.entry; startRename(e) }}
          >
            <Edit3 className="size-3.5" /> {tr("Rename")}
          </button>
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => { const e = contextMenu.entry; handleCopy(e) }}
          >
            <CopyIcon className="size-3.5" /> Copy
          </button>
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => { const e = contextMenu.entry; handleCut(e) }}
          >
            <Scissors className="size-3.5" /> Cut
          </button>
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => { const e = contextMenu.entry; setContextMenu(null); salinPath(e.path) }}
          >
            <ClipboardCopy className="size-3.5" /> {tr("Salin path")}
          </button>
          <div className="my-1 border-t border-border" />
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-secondary"
            onClick={() => { const e = contextMenu.entry; setContextMenu(null); setPermTarget(e); setPermMode(e.mode_octal.toString(8).padStart(3, "0")) }}
          >
            <Lock className="size-3.5" /> {tr("Ubah Permission")}
          </button>
          <div className="my-1 border-t border-border" />
          <button
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-crit hover:bg-crit/10"
            onClick={() => { const e = contextMenu.entry; setContextMenu(null); handleDelete(e) }}
          >
            <Trash2 className="size-3.5" /> {tr("Hapus")}
          </button>
        </div>
      )}

      {/* Modal Print */}
      {printTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="text-sm font-semibold">{tr("Print")}</p>
            <p className="num mt-1 break-all text-xs text-muted-foreground">{printTarget.name}</p>
            {printers.length === 0 ? (
              <div className="mt-4 space-y-3">
                <p className="text-xs text-muted-foreground">
                  {tr("Belum ada printer yang terdaftar di server ini. Tambahkan satu di Settings → Print server, lalu coba lagi.")}
                </p>
                <div className="flex justify-end">
                  <Button size="sm" variant="outline" onClick={() => setPrintTarget(null)}>
                    {tr("Tutup")}
                  </Button>
                </div>
              </div>
            ) : (
              <form onSubmit={kirimCetak} className="mt-3 space-y-3">
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Printer")}</label>
                  <select
                    className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                    value={printForm.printer}
                    onChange={(e) => setPrintForm({ ...printForm, printer: e.target.value })}
                  >
                    {printers.map((p) => (
                      <option key={p.name} value={p.name} disabled={!p.enabled}>
                        {p.name}
                        {p.default ? ` (${tr("default")})` : ""}
                        {!p.enabled ? ` — ${tr("antrean dimatikan")}` : ""}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="text-xs font-medium text-muted-foreground">{tr("Salinan")}</label>
                    <Input
                      className="mt-1"
                      type="number"
                      min={1}
                      max={100}
                      value={printForm.copies}
                      onChange={(e) => setPrintForm({ ...printForm, copies: Number(e.target.value) || 1 })}
                    />
                  </div>
                  <div>
                    <label className="text-xs font-medium text-muted-foreground">{tr("Ukuran kertas")}</label>
                    <select
                      className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                      value={printForm.media}
                      onChange={(e) => setPrintForm({ ...printForm, media: e.target.value })}
                    >
                      <option value="">{tr("bawaan printer")}</option>
                      <option value="A4">A4</option>
                      <option value="Letter">Letter</option>
                      <option value="Legal">Legal</option>
                    </select>
                  </div>
                </div>
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Sisi")}</label>
                  <select
                    className="mt-1 w-full rounded border border-border bg-background p-2 text-xs"
                    value={printForm.sides}
                    onChange={(e) => setPrintForm({ ...printForm, sides: e.target.value })}
                  >
                    <option value="">{tr("bawaan printer")}</option>
                    <option value="one-sided">{tr("satu sisi")}</option>
                    <option value="two-sided-long-edge">{tr("bolak-balik (sisi panjang)")}</option>
                    <option value="two-sided-short-edge">{tr("bolak-balik (sisi pendek)")}</option>
                  </select>
                </div>
                <div className="flex justify-end gap-2 pt-2">
                  <Button type="button" variant="outline" size="sm" onClick={() => setPrintTarget(null)}>
                    {tr("Batal")}
                  </Button>
                  <Button type="submit" size="sm" disabled={mencetak}>
                    {mencetak ? tr("Mengirim…") : tr("Print")}
                  </Button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}

      {/* Modal Preview */}
      {previewContent && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="flex max-h-[85dvh] w-full max-w-3xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
            <div className="flex items-center justify-between gap-2 border-b border-border pb-2">
              {/* title= supaya path yang ter-truncate tetap bisa dibaca utuh
                  lewat hover — tombol salin mengambil path LENGKAP, bukan
                  teks terpotong yang terlihat di layar. */}
              <p className="font-semibold text-sm truncate" title={previewContent.path}>
                {previewContent.path}
              </p>
              <div className="flex shrink-0 items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  title={tr("Salin path berkas")}
                  aria-label={tr("Salin path berkas")}
                  onClick={() => void salinPath(previewContent.path)}
                >
                  <CopyIcon className="size-3.5" />
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setPreviewContent(null)}>
                  {tr("Tutup")}
                </Button>
              </div>
            </div>
            <div className="mt-3 flex-1 overflow-auto">
              {previewContent.isImg ? (
                <img
                  src={`/api/files/preview?path=${encodeURIComponent(previewContent.path)}`}
                  alt={tr("Preview")}
                  className="max-h-[60dvh] object-contain mx-auto"
                />
              ) : previewContent.media ? (
                <div className="space-y-3">
                  {previewContent.media === "video" ? (
                    <video
                      key={previewContent.path}
                      src={`/api/files/preview?path=${encodeURIComponent(previewContent.path)}`}
                      controls
                      autoPlay
                      // Pemutaran dimulai tanpa suara supaya autoplay tidak
                      // ditolak kebijakan browser; kontrol volume tetap ada.
                      muted
                      className="max-h-[60dvh] w-full rounded bg-black"
                      onError={() => setMediaGagal(true)}
                    />
                  ) : (
                    <audio
                      key={previewContent.path}
                      src={`/api/files/preview?path=${encodeURIComponent(previewContent.path)}`}
                      controls
                      autoPlay
                      className="w-full"
                      onError={() => setMediaGagal(true)}
                    />
                  )}
                  {mediaGagal && (
                    <div className="rounded border border-warn/30 bg-warn/10 px-3 py-2 text-xs">
                      <p className="font-semibold">{tr("Browser tidak bisa memutar berkas ini")}</p>
                      <p className="mt-1 text-muted-foreground">
                        {tr(
                          "Wadah atau codec di dalamnya tidak didukung browser ini — MKV dengan HEVC/AV1 dan AVI lama adalah kasus yang paling sering. Berkasnya sendiri tidak rusak: unduh lalu putar dengan VLC atau mpv.",
                        )}
                      </p>
                      <a
                        className="mt-2 inline-flex items-center gap-1 font-medium text-signal hover:underline"
                        href={`/api/files/download?path=${encodeURIComponent(previewContent.path)}`}
                        download
                      >
                        <Download className="size-3.5" /> {tr("Download")}
                      </a>
                    </div>
                  )}
                </div>
              ) : (
                <pre className="font-mono text-xs text-muted-foreground bg-background p-3 rounded overflow-x-auto whitespace-pre-wrap">
                  {previewContent.text}
                </pre>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Modal Permissions */}
      {permTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">{tr("Ubah Permission")}</p>
            <p className="text-xs text-muted-foreground mt-1 truncate">{permTarget.path}</p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs font-medium" htmlFor="perm-mode">
                  {tr("Mode Oktal")}
                </label>
                <select
                  id="perm-mode"
                  className="mt-1 flex h-9 w-full rounded-md border border-border bg-input px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={permMode}
                  onChange={(e) => setPermMode(e.target.value)}
                >
                  {modeOptions(permMode).map((m) => (
                    <option key={m.value} value={m.value}>
                      {m.value} · {m.symbolic} — {tr(m.hint)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setPermTarget(null)}>
                  {tr("Batal")}
                </Button>
                <Button size="sm" onClick={handleSavePerms}>
                  {tr("Simpan")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Editor teks — buat file baru & ubah isi file */}
      {editor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="flex max-h-[85dvh] w-full max-w-3xl flex-col rounded-lg border border-border bg-surface p-4 shadow-xl">
            <div className="flex items-center justify-between border-b border-border pb-2">
              <div className="min-w-0">
                <p className="text-sm font-semibold">{editor.baru ? tr("File Baru") : tr("Edit File")}</p>
                <p className="num truncate text-[10px] text-muted-foreground">{editor.path}</p>
              </div>
              <Button variant="outline" size="sm" onClick={() => setEditor(null)}>
                {tr("Tutup")}
              </Button>
            </div>
            <textarea
              className="mt-3 min-h-[50dvh] flex-1 rounded border border-border bg-background p-3 font-mono text-[11px] leading-relaxed"
              spellCheck={false}
              value={editor.content}
              onChange={(e) => setEditor({ ...editor, content: e.target.value })}
            />
            <div className="mt-3 flex items-center justify-between gap-3">
              <p className="text-[10px] text-muted-foreground">
                {tr("Ditulis dengan izin akun Linux Anda · maksimal 1 MB · file biner tidak bisa dibuka di sini")}
              </p>
              <Button size="sm" disabled={menyimpanFile} onClick={simpanEditor}>
                {menyimpanFile ? tr("Menyimpan…") : tr("Simpan")}
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Modal Rename */}
      {renameTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">{tr("Rename")}</p>
            <p className="text-xs text-muted-foreground mt-1 truncate">{renameTarget.path}</p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs font-medium">{tr("Nama baru")}</label>
                <Input
                  className="mt-1"
                  autoFocus
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") handleRename() }}
                />
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setRenameTarget(null)}>
                  {tr("Batal")}
                </Button>
                <Button size="sm" onClick={handleRename}>
                  {tr("Simpan")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
