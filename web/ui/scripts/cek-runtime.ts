// Uji runtime yang menjalankan kode aslinya, bukan salinannya:
//   - tr()/trf()/pesanError() memang menghasilkan bahasa Inggris saat bahasa
//     aktif = en (bukan sekadar ada di tabel);
//   - rootAktif() menandai root yang benar di File Manager.
// Dijalankan lewat bundel SSR vite — lihat scripts/cek-runtime.sh.
// prefs.ts membaca localStorage saat modul dimuat; di node belum ada.
const simpanan = new Map<string, string>()
;(globalThis as unknown as { localStorage: Storage }).localStorage = {
  getItem: (k: string) => simpanan.get(k) ?? null,
  setItem: (k: string, v: string) => void simpanan.set(k, v),
  removeItem: (k: string) => void simpanan.delete(k),
  clear: () => simpanan.clear(),
  key: () => null,
  length: 0,
} as Storage

import { ApiError } from "@/lib/api"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { DialogIsian, isiValid } from "@/components/ui/prompt"
import { pesanError } from "@/lib/pesan-error"
import "@/lib/terjemahan-en"
import { tr, trf } from "@/stores/i18n"
import { simpanBahasaPralogin, usePrefs } from "@/stores/prefs"
import { rootAktif } from "@/views/files"

const gagal: string[] = []
let jumlah = 0
const cek = (dapat: string, harap: string, nama: string) => {
  jumlah++
  if (dapat !== harap) gagal.push(`${nama}: dapat ${JSON.stringify(dapat)}, harap ${JSON.stringify(harap)}`)
}

// Bahasa Indonesia: kalimat dikembalikan apa adanya.
cek(tr("Simpan Perubahan"), "Simpan Perubahan", "id/tr")
cek(trf("Hapus {0} {1}?", "folder", "foto"), "Hapus folder foto?", "id/trf")

usePrefs.setState({ bahasa: "en" })
cek(tr("Simpan Perubahan"), "Save Changes", "en/tr")
cek(tr("Belum ada bookmark folder."), "No folder bookmarks yet.", "en/tr-baru")
cek(trf("Hapus {0} {1}?", tr("folder"), "foto"), "Delete folder foto?", "en/trf")
cek(trf("{0} total proses", 12), "12 processes total", "en/trf-angka")
cek(tr("Warning (Amber %)"), "Warning (Amber %)", "en/tr-sama")

// Error backend: lewat kode, lewat kalimat tetap, dan lewat pola berparameter.
const res = { status: 400 } as Response
cek(pesanError(new ApiError(res, "Folder /x tidak ada.", "folder_missing", ["/x"])),
  "Folder /x does not exist.", "en/kode")
cek(pesanError(new Error("password lama salah")), "the current password is wrong", "en/kalimat")
cek(pesanError(new Error('user "budi" sudah ada')), 'user "budi" already exists', "en/pola")
cek(pesanError(new Error("exit status 1")), "exit status 1", "en/asing")
// Kalimat spesifik harus menang atas kalimat umum yang mirip: tabel pola
// dicocokkan berurutan, jadi "username tidak valid" tidak boleh menelan
// varian panjangnya.
cek(pesanError(new Error("username tidak valid (huruf kecil, angka, - dan _)")),
  "invalid username (lowercase letters, digits, - and _)", "en/pola-spesifik")
cek(pesanError(new Error("username tidak valid")), "invalid username", "en/pola-umum")
cek(pesanError(new Error("alamat DNS tidak valid: 8.8.8.8.8")),
  "invalid DNS address: 8.8.8.8.8", "en/pola-nilai")
cek(pesanError(new Error("/home/ani/DATA/foto adalah direktori")), "/home/ani/DATA/foto is a directory", "en/pola-awalan")
cek(pesanError(new Error('zona waktu "Mars/Olympus" tidak dikenal')),
  'unknown time zone "Mars/Olympus"', "en/pola-kutip")

usePrefs.setState({ bahasa: "id" })
cek(pesanError(new Error("password lama salah")), "password lama salah", "id/kalimat")

// File Manager: root mana yang ditandai aktif. Dulu memakai startsWith()
// sehingga "Root (/)" cocok dengan semua path dan selalu terlihat aktif.
const roots = [
  { name: "Home", path: "/home/ani" },
  { name: "Documents", path: "/home/ani/DATA/Documents" },
  { name: "Media", path: "/home/ani/DATA/Media" },
  { name: "Root (/)", path: "/" },
]
cek(rootAktif("/home/ani/DATA/Documents/foto", roots), "/home/ani/DATA/Documents", "root/terdalam")
cek(rootAktif("/home/ani/DATA/Documents", roots), "/home/ani/DATA/Documents", "root/persis")
cek(rootAktif("/home/ani/skrip", roots), "/home/ani", "root/home")
cek(rootAktif("/etc", roots), "/", "root/di-luar-root-lain")
cek(rootAktif("/", roots), "/", "root/akar")
// Tetangga dengan awalan sama tidak boleh ikut aktif.
cek(rootAktif("/home/ani/DATA/MediaLama", roots), "/home/ani", "root/prefiks-mirip")

// Dialog isian: tombol simpan mati untuk isian kosong/spasi saja.
cek(String(isiValid("")), "false", "prompt/kosong")
cek(String(isiValid("   ")), "false", "prompt/spasi")
cek(String(isiValid(" catatan.txt ")), "true", "prompt/berisi")

// Dialog isian dirender sungguhan (bukan snapshot yang ditulis tangan).
const render = (req: Parameters<typeof DialogIsian>[0]["req"]) =>
  renderToStaticMarkup(createElement(DialogIsian, { req, close: () => {} }))

const modalKosong = render({ title: "Folder baru", label: "Nama folder", confirmLabel: "Buat" })
cek(String(modalKosong.includes("Folder baru")), "true", "prompt/judul")
cek(String(modalKosong.includes("Nama folder")), "true", "prompt/label")
cek(String(modalKosong.includes('type="text"')), "true", "prompt/tipe-teks")
// Isian masih kosong → tombol buat harus mati, bukan bisa diklik.
cek(String(modalKosong.includes('disabled=""')), "true", "prompt/tombol-mati")

// Password tidak boleh terbaca di layar seperti pada window.prompt.
const modalSandi = render({ title: "Reset password uji", label: "Password baru", password: true })
cek(String(modalSandi.includes('type="password"')), "true", "prompt/tipe-password")

// Nilai default sudah terisi saat dialog muncul, jadi tombolnya langsung hidup.
const modalDefault = render({ title: "Simpan bookmark", defaultValue: "Media" })
cek(String(modalDefault.includes('value="Media"')), "true", "prompt/nilai-default")
cek(String(modalDefault.includes('disabled=""')), "false", "prompt/tombol-hidup")

// Bahasa yang dipilih di halaman login dititipkan ke localStorage supaya
// preferensi server tidak menimpanya begitu user masuk.
simpanBahasaPralogin("en")
cek(localStorage.getItem("lindash:bahasa-pralogin") ?? "", "en", "prefs/titip-bahasa")

if (gagal.length) {
  console.error("[✗] " + gagal.join("\n[✗] "))
  process.exit(1)
}
console.log(`[✓] runtime: ${jumlah} pemeriksaan lolos`)
