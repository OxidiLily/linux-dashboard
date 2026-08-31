// Format angka network/storage auto-scale B→KB→MB→GB→TB (basis 1024).
// Throughput pakai basis desimal (bit/s) sesuai konvensi ISP — lihat PRD §5.2.
//
// Zona waktu & locale disimpan di level modul, bukan dioper ke setiap
// pemanggil: tanggal dirender di belasan tempat (log, daftar file, header
// dashboard), dan menambah parameter di semua call site berarti satu tempat
// yang terlupa akan menampilkan waktu dengan zona berbeda dari yang dipilih
// user — persis kelas bug yang paling sulit dilihat.

let zonaAktif = ""
let localeAktif = "id-ID"

/** Dipanggil app shell setiap kali preferensi zona/bahasa berubah. */
export function setFormatPrefs(opts: { timezone?: string; bahasa?: string }) {
  if (opts.timezone !== undefined) zonaAktif = opts.timezone
  if (opts.bahasa !== undefined) localeAktif = opts.bahasa === "en" ? "en-US" : "id-ID"
}

export function zonaTampilan(): string {
  return zonaAktif
}

const UNITS = ["B", "KB", "MB", "GB", "TB"]
const BIT_UNITS = ["b/s", "kb/s", "Mb/s", "Gb/s", "Tb/s"]

/**
 * Angka yang sudah diskalakan, dipecah dari satuannya.
 *
 * formatBytes/formatRate mengembalikan string jadi — angka, satuan, dan
 * jumlah desimalnya menyatu. Itu cukup untuk teks statis, tapi NumberFlow
 * butuh angkanya sebagai number, satuannya sebagai suffix, dan desimalnya
 * sebagai Intl option. Ketiganya dihitung di satu tempat supaya teks biasa
 * dan angka beranimasi tidak pernah menampilkan hasil yang berbeda.
 */
export type AngkaSkala = { nilai: number; satuan: string; desimal: number }

/** Desimal menyusut saat angkanya membesar: "9.87" tapi "987". */
function desimalUntuk(v: number): number {
  return v >= 100 ? 0 : v >= 10 ? 1 : 2
}

function skalakan(n: number, units: string[], basis: number): AngkaSkala {
  let i = 0
  let v = n
  while (v >= basis && i < units.length - 1) {
    v /= basis
    i++
  }
  return { nilai: v, satuan: units[i] ?? units[0]!, desimal: desimalUntuk(v) }
}

/** Byte → B/KB/MB/GB/TB (basis 1024). */
export function pecahBytes(n: number): AngkaSkala {
  if (!isFinite(n) || n < 0) return { nilai: 0, satuan: "B", desimal: 0 }
  if (n < 1) return { nilai: n, satuan: "B", desimal: 0 }
  return skalakan(n, UNITS, 1024)
}

/** Byte/detik → b/s, kb/s, … (basis 1000, konvensi ISP — PRD §5.2). */
export function pecahRate(bytesPerSec: number): AngkaSkala {
  if (!isFinite(bytesPerSec) || bytesPerSec < 0) return { nilai: 0, satuan: "b/s", desimal: 0 }
  return skalakan(bytesPerSec * 8, BIT_UNITS, 1000)
}

function rakit(a: AngkaSkala): string {
  return a.nilai.toFixed(a.desimal) + " " + a.satuan
}

export function formatBytes(n: number): string {
  return rakit(pecahBytes(n))
}

export function formatRate(bytesPerSec: number): string {
  return rakit(pecahRate(bytesPerSec))
}

export function formatPercent(p: number, digits = 1): string {
  if (!isFinite(p)) return "0%"
  return p.toFixed(digits) + "%"
}

export function formatUptime(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return "—"
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  // Satuan ikut bahasa: "2h 3j 4m" tidak berarti apa-apa buat pembaca Inggris.
  const en = localeAktif.startsWith("en")
  const [sh, sj, sm] = en ? ["d", "h", "m"] : ["h", "j", "m"]
  if (d > 0) return `${d}${sh} ${h}${sj} ${m}${sm}`
  if (h > 0) return `${h}${sj} ${m}${sm}`
  return `${m}${sm}`
}

export function formatTanggal(d: Date): string {
  return d.toLocaleDateString(localeAktif, {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: zonaAktif || undefined,
  })
}

// tz opsional hanya untuk pemanggil yang perlu memaksa zona tertentu;
// default-nya mengikuti preferensi user.
export function formatJam(d: Date, tz?: string): string {
  return d.toLocaleTimeString(localeAktif, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZone: tz || zonaAktif || undefined,
  })
}
// created_at dari SQLite berformat "2026-08-18 09:00:00" — UTC, pakai spasi,
// tanpa penanda zona. Firefox & Safari menolak format itu di new Date(),
// jadi dinormalkan ke ISO dulu. Angka = epoch milidetik.
export function formatWaktu(t: number | string): string {
  // Backend kirim RFC3339 ("2026-08-18T11:45:52Z"); baris lama dari SQLite
  // bisa berbentuk "2026-08-18 11:45:52" tanpa zona → baru itu yang di-suffix Z.
  const d =
    typeof t === "number"
      ? new Date(t)
      : new Date(
          /(Z|[+-]\d{2}:?\d{2})$/.test(t.trim())
            ? t.trim()
            : t.trim().replace(" ", "T") + "Z",
        )
  if (isNaN(d.getTime())) return "—"
  return d.toLocaleString(localeAktif, {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: zonaAktif || undefined,
  })
}
