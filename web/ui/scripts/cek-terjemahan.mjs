// Pemeriksa cakupan terjemahan.
// Menandai teks berbahasa Indonesia di dalam src/ yang (a) tidak dibungkus
// tr()/trf(), atau (b) sudah dibungkus tapi belum punya padanan bahasa Inggris.
// Jalankan: node scripts/cek-terjemahan.mjs
import fs from "fs"
import path from "path"

const ABAIKAN_FILE = /terjemahan-en\.ts|pesan-error\.ts|pesan-backend-en\.ts|stores\/i18n\.ts/

function daftarBerkas(dir, hasil = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) daftarBerkas(p, hasil)
    else if (/\.tsx?$/.test(e.name)) hasil.push(p)
  }
  return hasil
}

// --- kamus Inggris yang tersedia ---
const kunciEn = new Set()
const bacaLiteral = (raw) => {
  try {
    return JSON.parse('"' + raw + '"')
  } catch {
    return null
  }
}
const tambahKunci = (raw) => {
  const v = bacaLiteral(raw)
  if (v !== null) kunciEn.add(v)
}
for (const f of ["src/lib/terjemahan-en.ts", "src/lib/pesan-error.ts", "src/lib/pesan-backend-en.ts"]) {
  const s = fs.readFileSync(f, "utf8")
  for (const m of s.matchAll(/"((?:[^"\\]|\\.)*)"\s*:/g)) tambahKunci(m[1])
  for (const m of s.matchAll(/:\s*\n?\s*"((?:[^"\\]|\\.)*)"/g)) tambahKunci(m[1])
}

// Teks tanpa kata Indonesia sama sekali (nama produk, istilah teknis, satuan)
// tidak berubah saat bahasa berganti.
const samaDuaBahasa = (v) => !teksIndonesia(v)

// --- deteksi bahasa Indonesia ---
const KATA_ID =
  /(^|[^a-z])(yang|tidak|belum|sudah|akan|dari|untuk|dengan|dan|atau|ke|di|pada|ini|itu|bisa|dapat|harus|jangan|semua|ada|hanya|lagi|saat|jadi|beri|buat|hapus|ubah|simpan|tambah|pilih|cari|muat|kosong|kosongkan|gagal|berhasil|baru|lama|nama|kembali|masuk|keluar|tekan|klik|isi|nilai|salah|benar|milik|punya|butuh|lewat|dulu|masih|terpasang|dipakai|diblokir|dijalankan|jalan|jalankan|matikan|aktifkan|nonaktif|banyak|sedikit|antar|sedang|berkas|folder|berjalan|terhubung|terputus|sesi|pengguna|catatan|riwayat|pesan|peringatan|tampilkan|sembunyikan|urutkan|salin|pindah|pindahkan|unduh|unggah|ganti|atur|kelola|daftar|daftarkan|rincian|wajib|opsional|mis|seluruh|setiap|antara|karena|supaya|tanpa|juga|belum|hentikan|nyalakan|periksa|cek|kuota|penuh|milikmu|anda|kamu|dibuka|dimulai|dimuat|terakhir|pertama|sendiri|bersama|hindari|tertimpa|terlihat|menolak|memuat|memakai|membuka|menghubungi|memblokir|melayani|menunjuk|butuhkan|diperbarui|dibuat|ditambahkan|dihapus|ditutup|ditolak|tersambung|selesai|proses|tanggal|waktu|ukuran|jumlah|total|sumber|tujuan|bahasa|zona|terjadi|kesalahan|bermasalah|tersedia|beberapa|menit|detik|jam|hari|tutup|notifikasi|merah|hijau|kuning|naik|turun|hubungkan|putuskan|sambung|lepas|lanjutkan|selamat|datang|sisa|jumlah|terakhir|oktal|izin|ukuran|pilihan|tambahkan|pastikan|gunakan|biarkan|ditulis|dibaca|terbaca|dicabut|dikelola|disimpan|tersimpan|dinyalakan|dimatikan|dipasang|dicopot|kosongkan|ketikan|bawaan|utuh|menyatu|terpisah)([^a-z]|$)/i
const AFIKS_ID = /\b(di|me|ter|ber|pe|ke)[a-z]{3,}(kan|nya|an|i)\b/i

const teksIndonesia = (v) => KATA_ID.test(v) || AFIKS_ID.test(v)

// Potongan yang jelas berisi kode, bukan kalimat untuk user: muncul saat
// regex teks JSX menyeberangi baris di antara dua elemen.
const KODE = /=>|===|!==|\bconst\b|\breturn \(|\bawait \b|\bfunction\b|:\s*(string|number|boolean)\b|\?\s*\(|&&\s*\(|\}\)|\.\w+\(/

const TEKNIS =
  /^(\/|https?:|[a-z0-9_.-]+\/[a-z0-9_./-]+$|#|\$|@)|^[A-Z_]+$|^\w+=|^\d/

const masalah = []
for (const f of daftarBerkas("src")) {
  if (ABAIKAN_FILE.test(f)) continue
  const asli = fs.readFileSync(f, "utf8")
  // tandai wilayah yang sudah dibungkus tr()/trf(): argumen pertamanya dianggap terjemahkan
  const dibungkus = new Set()
  for (const m of asli.matchAll(
    /\b(?:tr|trf)\(\s*(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|`([^`$]*)`)/g,
  )) {
    const v = m[1] !== undefined ? bacaLiteral(m[1]) : (m[2] ?? m[3])
    if (v === null) continue
    dibungkus.add(v)
    // Kalimat yang identik di kedua bahasa (nama produk, istilah teknis,
    // "Dashboard") tidak butuh entri: tr() mengembalikan kuncinya apa adanya.
    // Menyimpan entri id→id yang sama persis hanya menambah baris tanpa
    // mengubah satu pun tampilan.
    if (!kunciEn.has(v) && !samaDuaBahasa(v)) masalah.push({ f, jenis: "TANPA-EN", v })
  }
  const bersih = asli
    // Baris bertanda i18n-abaikan bukan teks UI (mis. pencocokan pesan
    // mentah dari sistem) — jangan ikut diperiksa.
    .replace(/^.*i18n-abaikan.*$/gm, "")
    .replace(/\b(?:tr|trf)\(\s*(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`[^`]*`)/g, "TR")
    .replace(/^\s*\/\/.*$/gm, "")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*import[\s\S]*?from\s+"[^"]*"/gm, "")

  const lihat = (v) => {
    v = v.trim()
    if (!v || v.length < 3 || TEKNIS.test(v) || KODE.test(v)) return
    if (!teksIndonesia(v)) return
    // Teks yang sudah punya padanan Inggris umumnya dipakai lewat variabel
    // (tabel pesan, label opsi) lalu diterjemahkan saat render — tandai
    // terpisah supaya yang benar-benar belum tergarap tetap menonjol.
    masalah.push({ f, jenis: kunciEn.has(v) ? "INDIREK" : "TANPA-TR", v })
  }
  // Template literal dibatasi satu baris: mencocokkan backtick lintas baris
  // menyatukan dua literal yang berbeda dan menghasilkan temuan palsu.
  for (const m of bersih.matchAll(/"((?:[^"\\\n]|\\.)*)"|'((?:[^'\\\n]|\\.)*)'|`([^`\n]*)`/g))
    lihat(m[1] ?? m[2] ?? m[3] ?? "")
  // Teks JSX boleh diselingi ekspresi ({nama}); potongan teks di sekitarnya
  // tetap harus diterjemahkan, jadi ekspresinya dibuang lalu sisanya diperiksa.
  for (const m of bersih.matchAll(/>([^<>]{3,})</g)) {
    for (const bagian of m[1].split(/\{[^{}]*\}/)) lihat(bagian)
  }
}

const uniq = new Map()
for (const m of masalah) {
  const k = m.jenis + "|" + m.v
  if (!uniq.has(k)) uniq.set(k, { ...m, file: new Set() })
  uniq.get(k).file.add(m.f)
}
const list = [...uniq.values()]
for (const m of list) console.log(`[${m.jenis}] ${JSON.stringify(m.v)}  <- ${[...m.file].join(", ")}`)
const berat = list.filter((m) => m.jenis !== "INDIREK")
console.log(
  `\ntotal: ${list.length} (TANPA-TR: ${list.filter((m) => m.jenis === "TANPA-TR").length}, TANPA-EN: ${list.filter((m) => m.jenis === "TANPA-EN").length}, INDIREK: ${list.filter((m) => m.jenis === "INDIREK").length})`,
)
process.exit(berat.length ? 1 : 0)
