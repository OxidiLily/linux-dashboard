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
import { UninstallModal, konfirmasiDataSah } from "@/components/ui/uninstall-modal"
import { pesanError } from "@/lib/pesan-error"
import "@/lib/terjemahan-en"
import { tr, trf } from "@/stores/i18n"
import { simpanBahasaPralogin, usePrefs } from "@/stores/prefs"
import { cariBerkas, rootAktif } from "@/views/files"
import { bacaCrontab, cariJadwal, ukuranByte, ukuranCrontabTersimpan } from "@/views/cron"

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

// File Manager: pencarian nama di folder yang sedang terbuka. Yang diuji di
// sini adalah hal-hal yang mudah salah dan tidak kelihatan dari layar: kueri
// kosong harus mengembalikan SEMUA (bukan saringan yang membuang semuanya),
// kapital diabaikan, dan karakter pola diperlakukan literal.
const berkas = [
  { name: "Laporan.pdf" },
  { name: "laporan-lama.pdf" },
  { name: "foto.jpg" },
  { name: "catatan.tar.gz" },
  { name: "arsip.2024.zip" },
]
cek(String(cariBerkas(berkas, "").length), "5", "cari/kosong-semua")
cek(String(cariBerkas(berkas, "   ").length), "5", "cari/spasi-semua")
cek(String(cariBerkas(berkas, "LAPORAN").length), "2", "cari/kapital-diabaikan")
cek(cariBerkas(berkas, "fotO")[0].name, "foto.jpg", "cari/kapital-campur")
cek(String(cariBerkas(berkas, "tidak-ada").length), "0", "cari/tanpa-hasil")
// Titik adalah karakter LITERAL, bukan "apa saja": mencari ".pdf" harus
// menemukan Laporan.pdf dan bukan ikut menarik "pdf" tanpa titik.
cek(String(cariBerkas(berkas, ".pdf").length), "2", "cari/titik-literal")
// Bintang juga literal — orang mencari nama berkas seperti "*.tar.gz", bukan
// regex. Kalau ini diperlakukan sebagai pola, setiap berkas akan cocok.
cek(String(cariBerkas(berkas, "*").length), "0", "cari/bintang-literal")
cek(String(cariBerkas(berkas, "2024").length), "1", "cari/angka")

// Cronjob: pembacaan crontab. Bagian yang paling halus adalah memecah lima
// kolom jadwal dari perintahnya — jadwal yang salah pecah tetap tampil rapi —
// dan bentuk @daily yang hanya satu kata.
const crontab = [
  "SHELL=/bin/bash",
  "PATH=/usr/local/bin:/usr/bin:/bin",
  "",
  "# komentar biasa",
  "*/5  *  * * *   /usr/bin/echo spasi-banyak",
  "@daily /usr/bin/rsync -a /data /backup",
  "0 3 * * 1 echo dua-kata",
  "bukan jadwal",
  "",
].join("\n")
const barisCron = bacaCrontab(crontab)
// Delapan baris, bukan sembilan: newline terakhir hanya menutup baris
// kedelapan, dan potongan kosong sisa split-nya memang dibuang. Baris kosong
// di TENGAH (baris 3) tetap dihitung — itu baris yang benar-benar ada.
cek(String(barisCron.length), "8", "cron/jumlah-baris")
cek(String(barisCron[barisCron.length - 1].kind), "other", "cron/baris-akhir")
cek(String(barisCron[2].kind), "blank", "cron/baris-kosong-tengah")
cek(String(barisCron[2].line), "3", "cron/nomor-baris-kosong")
cek(JSON.stringify(barisCron[0]), '{"kind":"variable","line":1,"name":"SHELL","value":"/bin/bash"}', "cron/variabel")
cek(String(barisCron[1].line), "2", "cron/nomor-baris")
cek(JSON.stringify(barisCron[3]), '{"kind":"comment","line":4,"text":"komentar biasa"}', "cron/komentar")
// Spasi berlebih antar kolom tidak boleh menggeser batas jadwal/perintah.
cek(
  JSON.stringify(barisCron[4]),
  '{"kind":"schedule","line":5,"spec":"*/5 * * * *","command":"/usr/bin/echo spasi-banyak"}',
  "cron/spasi-berlebih",
)
// @daily hanya satu kata jadwal; perintahnya utuh termasuk argumennya.
cek(
  JSON.stringify(barisCron[5]),
  '{"kind":"schedule","line":6,"spec":"@daily","command":"/usr/bin/rsync -a /data /backup"}',
  "cron/at-daily",
)
cek(JSON.stringify(barisCron[6]), '{"kind":"schedule","line":7,"spec":"0 3 * * 1","command":"echo dua-kata"}', "cron/lima-kolom")
// Baris yang tidak dikenali tetap muncul, bukan dibuang diam-diam.
cek(JSON.stringify(barisCron[7]), '{"kind":"other","line":8,"text":"bukan jadwal"}', "cron/baris-asing")
cek(String(bacaCrontab("").length), "0", "cron/kosong")

// Saringan daftar jadwal harus mencocokkan jadwal DAN perintahnya.
cek(String(cariJadwal(barisCron, "rsync").length), "1", "cron/cari-perintah")
cek(String(cariJadwal(barisCron, "0 3").length), "1", "cron/cari-jadwal")
cek(String(cariJadwal(barisCron, "").length), "8", "cron/cari-kosong")
cek(String(cariJadwal(barisCron, "komentar").length), "1", "cron/cari-komentar")
cek(String(cariJadwal(barisCron, "PATH").length), "1", "cron/cari-variabel-nama")
cek(String(cariJadwal(barisCron, "/usr/local/bin").length), "1", "cron/cari-variabel-nilai")

// Batas ukuran dihitung dalam BYTE, bukan jumlah karakter: versi yang memakai
// .length akan meloloskan isi ber-aksen/emoji yang ditolak server.
cek(String(ukuranByte("abc")), "3", "cron/byte-ascii")
cek(String(ukuranByte("é")), "2", "cron/byte-aksen")
cek(String(ukuranByte("🇮🇩")), "8", "cron/byte-emoji")
cek(String(ukuranCrontabTersimpan("abc")), "4", "cron/byte-termasuk-newline-otomatis")
cek(String(ukuranCrontabTersimpan("abc\n")), "4", "cron/byte-newline-tidak-dobel")

// Dialog isian: tombol simpan mati untuk isian kosong/spasi saja.
cek(String(isiValid("")), "false", "prompt/kosong")
cek(String(isiValid("   ")), "false", "prompt/spasi")
cek(String(isiValid(" catatan.txt ")), "true", "prompt/berisi")

// Uninstall: mode penghapus data akun hanya menyala setelah kata konfirmasi
// diketik. Diuji lewat fungsi aslinya, bukan salinan — inilah satu-satunya
// penjaga antara satu klik dan hilangnya ~/DATA di seluruh akun.
cek(String(konfirmasiDataSah("panel", "")), "true", "uninstall/panel-tanpa-ketik")
cek(String(konfirmasiDataSah("total-data", "")), "false", "uninstall/data-kosong")
cek(String(konfirmasiDataSah("total-data", "hapus")), "false", "uninstall/data-sepotong")
cek(String(konfirmasiDataSah("total-data", "HAPUS DATA ARSIP")), "false", "uninstall/data-kebanyakan")
cek(String(konfirmasiDataSah("total-data", "HAPUS DATA")), "true", "uninstall/data-persis")
cek(String(konfirmasiDataSah("total-data", "  hapus data  ")), "true", "uninstall/data-spasi-kapital")

// Modalnya dirender sungguhan: keempat mode muncul, dan mode penghapus data
// menyebut folder DATA supaya user tahu apa yang dipertaruhkan.
const modalUninstall = renderToStaticMarkup(createElement(UninstallModal, { username: "ani", onClose: () => {} }))
for (const judul of ["Hapus panel saja", "Hapus panel dan folder/file panel", "Hapus total (termasuk components)"]) {
  cek(String(modalUninstall.includes(judul)), "true", "uninstall/mode-" + judul.slice(6, 16))
}
cek(String(modalUninstall.includes("data akun")), "true", "uninstall/mode-data-akun")
cek(String(modalUninstall.includes("~/DATA")), "true", "uninstall/sebut-data")

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
