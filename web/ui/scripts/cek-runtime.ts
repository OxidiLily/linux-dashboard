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
import { notify } from "@/components/ui/toast"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { DialogIsian, isiValid } from "@/components/ui/prompt"
import { UninstallModal, konfirmasiDataSah } from "@/components/ui/uninstall-modal"
import { pesanError } from "@/lib/pesan-error"
import "@/lib/terjemahan-en"
import { tr, trf } from "@/stores/i18n"
import { simpanBahasaPralogin, usePrefs } from "@/stores/prefs"
import { cariBerkas, detailItemLog, rootAktif, unggahLaluMuatUlang } from "@/views/files"
import { bacaCrontab, cariJadwal, ukuranByte, ukuranCrontabTersimpan } from "@/views/cron"
import { isianCertificatesValid } from "@/views/certificates"


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

// Pencarian ke dalam subfolder: kalimat statusnya harus ikut bahasa aktif.
// Diletakkan di blok `en` karena pemeriksaan bahasa memakai state bahasa yang
// sedang aktif — menaruhnya setelah blok `id` membuatnya selalu gagal.
cek(tr("Cari berkas di folder ini dan subfoldernya…"), "Search this folder and its subfolders…", "en/cari/tempat")
cek(trf("{0} hasil di dalam {1}", 3, "/home/pc/DATA"), "3 results inside /home/pc/DATA", "en/cari/hasil")
cek(trf("({0} folder ditelusuri)", 12), "(12 folders walked)", "en/cari/ditelusuri")
cek(tr("Hasil dipotong — persempit kata kuncinya."), "Results were cut off — narrow your keyword.", "en/cari/dipotong")
cek(trf('Tidak ada berkas atau folder yang memuat "{0}" sampai ke subfolder.', "x"), 'No file or folder contains "x" down to the subfolders.', "en/cari/kosong")

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

// Log operasi jamak harus menyebut item yang benar-benar diproses. Ringkasan
// jumlah saja tidak cukup untuk audit setelah toast hilang.
cek(
  detailItemLog(["/home/ani/a.txt", "/home/ani/foto"]),
  "/home/ani/a.txt\n/home/ani/foto",
  "log/detail-item",
)
cek(detailItemLog([]), "", "log/detail-item-kosong")
const detailPanjang = detailItemLog(Array.from({ length: 500 }, (_, i) => `/home/ani/foto-${i}-😀.jpg`))
cek(String(new TextEncoder().encode(detailPanjang).length <= 4000), "true", "log/detail-batas-byte")
cek(String(detailPanjang.includes("item lain tidak ditampilkan")), "true", "log/detail-sebut-terpotong")
cek(String(detailPanjang.includes("�")), "false", "log/detail-unicode-utuh")

// Upload yang sudah diterima server harus selalu diikuti pembacaan ulang daftar,
// termasuk ketika response akhirnya 413. Proxy bisa menolak response sesudah body
// 100% terkirim, sementara backend sudah selesai menyimpan berkasnya.
const ujiUpload = (async () => {
  const urutanUpload: string[] = []
  await unggahLaluMuatUlang(
    async () => {
      urutanUpload.push("unggah")
    },
    async () => {
      urutanUpload.push("muat")
    },
  )
  cek(urutanUpload.join(","), "unggah,muat", "upload/sukses-muat-ulang")
  let errorUpload: unknown
  try {
    await unggahLaluMuatUlang(
      async () => {
        urutanUpload.push("gagal")
        throw new Error("Upload error 413")
      },
      async () => {
        urutanUpload.push("muat-setelah-gagal")
      },
    )
  } catch (e) {
    errorUpload = e
  }
  cek(pesanError(errorUpload), "Upload error 413", "upload/error-tetap-dilaporkan")
  cek(urutanUpload.at(-1) ?? "", "muat-setelah-gagal", "upload/gagal-tetap-muat-ulang")

  const errorUploadAsli = new Error("Upload error 413")
  let errorUploadDanMuat: unknown
  try {
    await unggahLaluMuatUlang(
      async () => {
        throw errorUploadAsli
      },
      async () => {
        throw new Error("reload gagal")
      },
    )
  } catch (e) {
    errorUploadDanMuat = e
  }
  cek(String(errorUploadDanMuat === errorUploadAsli), "true", "upload/error-asli-menang-dari-error-reload")

  let errorMuat: unknown
  try {
    await unggahLaluMuatUlang(
      async () => undefined,
      async () => {
        throw new Error("reload gagal")
      },
    )
  } catch (e) {
    errorMuat = e
  }
  cek(pesanError(errorMuat), "reload gagal", "upload/sukses-error-reload-dilaporkan")
})()

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

// Certificates: dua path wajib diisi bersama; menonaktifkan TLS berarti keduanya kosong.
cek(String(isianCertificatesValid("", "")), "true", "certificates/nonaktif")
cek(String(isianCertificatesValid("/cert.pem", "/key.pem")), "true", "certificates/pasangan")
cek(String(isianCertificatesValid("/cert.pem", "")), "false", "certificates/key-kosong")
cek(String(isianCertificatesValid("", "/key.pem")), "false", "certificates/cert-kosong")
cek(String(isianCertificatesValid("cert.pem", "key.pem")), "false", "certificates/path-relatif")

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

// Toast mencatat ke /api/logs/notifications lewat fetch, dan pencatatannya
// memakai window.location.pathname. Keduanya disediakan di sini supaya yang
// diuji adalah ISI yang benar-benar dikirim, bukan teks sumber.
const tercatat: { tone?: string; message?: string; detail?: string; page?: string }[] = []
;(globalThis as unknown as { fetch: typeof fetch }).fetch = ((url: unknown, init?: { body?: string }) => {
  if (String(url).endsWith("/api/logs/notifications")) {
    try {
      tercatat.push(JSON.parse(String(init?.body ?? "{}")))
    } catch {
      tercatat.push({})
    }
  }
  return Promise.resolve({ ok: true } as Response)
}) as typeof fetch
;(globalThis as unknown as { window: { location: { pathname: string } } }).window = {
  location: { pathname: "/files" },
}

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

// ---------------------------------------------------------------------------
// Setiap lapisan modal harus menutup dengan Escape.
//
// Penjaganya membaca SUMBER, bukan hasil render: pendaftaran Escape hidup di
// dalam useEffect, dan efek tidak berjalan di renderToStaticMarkup — modal yang
// lupa didaftarkan akan tampak sempurna dari markup-nya. Yang diperiksa karena
// itu adalah pasangan yang wajib ada: pembungkus gelap `bg-black/60` (itulah
// yang membuat sesuatu disebut lapisan modal) DAN panggilan daftarkanEscape.
//
// Pengecualiannya sengaja kosong. Kalau nanti ada modal yang memang tidak boleh
// ditutup, itu keputusan sadar — dan tempatnya di daftar pengecualian di bawah,
// bukan di lubang yang tak terlihat.
// Jalurnya dijangkarkan ke cwd proyek, bukan import.meta.url: berkas ini
// dijalankan dari BUNDEL di dalam node_modules, jadi URL-nya menunjuk ke sana.
// scripts/cek-runtime.sh sudah `cd` ke web/ui sebelum menjalankannya.
const bacaSumber = (jalur: string): string => {
  try {
    return readFileSync(resolve(process.cwd(), jalur), "utf8")
  } catch (e) {
    return `__GAGAL_BACA__ ${String(e)}`
  }
}

// Toast "rincian" diuji lewat PERILAKU: apa yang benar-benar dikirim ke
// /api/logs/notifications. Pemeriksaan teks sumber hanya membuktikan ada baris
// tertentu di berkas — ia gagal hanya karena formatnya ditulis ulang, dan tidak
// membuktikan apa pun tentang yang dicatat saat operasi benar-benar gagal.
notify.err("Hapus gagal", "rincian-gagal")
cek(tercatat.at(-1)?.tone ?? "__kosong__", "err", "log/detail-gagal-tone")
cek(tercatat.at(-1)?.detail ?? "__kosong__", "rincian-gagal", "log/detail-gagal-direkam")
cek(tercatat.at(-1)?.page ?? "__kosong__", "/files", "log/detail-halaman")
notify.ok("Hapus berhasil", "rincian-sukses")
cek(tercatat.at(-1)?.tone ?? "__kosong__", "ok", "log/detail-sukses-tone")
cek(tercatat.at(-1)?.detail ?? "__kosong__", "rincian-sukses", "log/detail-sukses-direkam")

const pemeriksaLapisan: [string, [string, string, string][]][] = [
  ["src/components/ui/confirm.tsx", []],
  ["src/components/ui/prompt.tsx", []],
  ["src/components/ui/update-modal.tsx", []],
  ["src/components/ui/uninstall-modal.tsx", []],
  ["src/components/ui/disk-prepare-modal.tsx", []],
  ["src/components/ui/iface-editor.tsx", []],
  ["src/views/files.tsx", [
    ["printTarget", "setPrintTarget", "null"], ["previewContent", "setPreviewContent", "null"],
    ["permTarget", "setPermTarget", "null"], ["editor", "setEditor", "null"],
    ["renameTarget", "setRenameTarget", "null"],
  ]],
  ["src/views/account.tsx", [
    ["showAddUser", "setShowAddUser", "false"], ["editTarget", "setEditTarget", "null"],
  ]],
  ["src/views/docker.tsx", [
    ["showAddStack", "setShowAddStack", "false"], ["logModal", "setLogModal", "null"],
    ["composeModal", "setComposeModal", "null"], ["envModal", "setEnvModal", "null"],
  ]],
  ["src/views/firewall.tsx", [["showAdd", "tutupForm", ""]]],
  ["src/views/nfs.tsx", [["modal", "setModal", "false"], ["modalMount", "setModalMount", "false"]]],
  ["src/views/samba.tsx", [["userModal", "setUserModal", "null"], ["showModal", "setShowModal", "false"]]],
  ["src/views/fail2ban.tsx", [["modal", "setModal", "false"]]],
  ["src/views/mergerfs.tsx", [["modal", "setModal", "false"]]],
  ["src/views/print-server.tsx", [["modal", "setModal", "false"]]],
]

for (const [jalur, penutupWajib] of pemeriksaLapisan) {
  const isi = bacaSumber(jalur)
  cek(String(isi.includes("bg-black/60")), "true", `escape/${jalur}/punya-lapisan`)
  cek(String(isi.includes("daftarkanEscape")), "true", `escape/${jalur}/terdaftar`)

  // Setiap lapisan harus punya pendaftaran Escape LENGKAP: guard yang hidup
  // saat modal terbuka (`if (!kondisi) return`), penutup dengan setter dan
  // argumen yang benar, dan daftar dependensi yang cocok dengan guardnya.
  //
  // Bentuk yang pernah benar-benar lolos dan mematikan SELURUH fitur ini:
  // `if (printTarget) return` — pendaftarannya tidak pernah berjalan, padahal
  // barisnya ada di berkas sehingga pemeriksaan yang hanya mencari kata
  // "daftarkanEscape" tetap hijau. Karena itu arah guard-nya diperiksa
  // eksplisit di sini, lengkap dengan kecocokan dependensinya.
  for (const [kondisi, setter, arg] of penutupWajib) {
    const blok = new RegExp(
      `if \\(!${kondisi}\\) return\\s*\\n\\s*return daftarkanEscape\\(\\(\\) => ${setter}\\(${arg.replace(/[()]/g, "\\$&")}\\)\\)\\s*\\n\\s*\\}, \\[${kondisi}\\]\\)`,
    )
    cek(String(blok.test(isi)), "true", `escape/${jalur}/blok-${kondisi}`)
  }

  // Tidak boleh ada pendaftaran yang MEMBUKA modal: argumen truthy berarti satu
  // tekanan Escape justru memunculkan lapisan, bukan menutupnya.
  for (const m of isi.matchAll(/daftarkanEscape\(\(\) => (\w+)\(([^)]*)\)\)/g)) {
    if (m[2].trim() === "true") cek("membuka", "menutup", `escape/${jalur}/${m[1]}(true)`)
  }
}

// Berapa banyak pembungkus gelap di seluruh aplikasi, dan berapa yang terdaftar.
// Angka ini yang menangkap lapisan BARU: menambah modal tanpa mendaftarkannya
// membuat jumlahnya tidak lagi sepadan, dan pesannya menyebut angka yang
// diharapkan sehingga jelas apa yang kurang.
const semuaSumber = [
  ...pemeriksaLapisan.map(([j]) => j),
  "src/components/layout/app-shell.tsx",
]
let jumlahLapisan = 0
for (const jalur of semuaSumber) {
  const isi = bacaSumber(jalur)
  // app-shell memakai z-40: itu lencana gelap drawer, bukan lapisan modal, dan
  // penutupnya sudah memakai adaLapisanEscape() supaya tidak menutup di
  // belakang modal.
  const potongan = jalur.endsWith("app-shell.tsx") ? 0 : isi.split("bg-black/60").length - 1
  jumlahLapisan += potongan
}
cek(String(jumlahLapisan), "25", "escape/jumlah-lapisan")

// notify.tugas mencatat detail di dalam callback success/error, yang baru
// berjalan setelah promise pekerjaannya settle. Jadi jalur gagal (detailGagal)
// diperiksa di microtask berikutnya, dan laporan akhirnya menyusul di sana.
notify
  .tugas(Promise.reject(new Error("boom")), {
    jalan: "Menghapus",
    gagal: () => "Hapus gagal",
    detail: () => "tidak-dipakai-saat-gagal",
    detailGagal: (e) => "detail:" + String((e as Error).message),
  })
  .catch(() => undefined)
notify
  .tugas(Promise.resolve("selesai"), {
    jalan: "Menghapus",
    sukses: () => "Hapus berhasil",
    gagal: () => "Hapus gagal",
    detail: (hasil) => "tugas-ok:" + String(hasil),
  })
  .catch(() => undefined)

// Dua microtask: satu untuk settle-nya promise pekerjaan, satu untuk callback
// sonner.promise yang memanggil rekam(). Uji upload ditunggu juga agar seluruh
// assertion asynchronous selesai sebelum hasil akhir dicetak.
void Promise.all([ujiUpload, Promise.resolve().then(() => Promise.resolve())])
  .then(() => {
    const tugasGagal = tercatat.find((t) => t.detail === "detail:boom")
    const tugasSukses = tercatat.find((t) => t.detail === "tugas-ok:selesai")
    cek(tugasGagal?.tone ?? "__kosong__", "err", "log/tugas-gagal-tone")
    cek(tugasGagal?.detail ?? "__kosong__", "detail:boom", "log/tugas-gagal-detailGagal")
    cek(tugasSukses?.tone ?? "__kosong__", "ok", "log/tugas-sukses-tone")

    if (gagal.length) {
      console.error("[✗] " + gagal.join("\n[✗] "))
      process.exit(1)
    }
    console.log(`[✓] runtime: ${jumlah} pemeriksaan lolos`)
  })
