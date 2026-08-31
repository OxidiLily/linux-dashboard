import { create } from "zustand"

import { apiGet, apiSend } from "@/lib/api"
import { setFormatPrefs } from "@/lib/format"

export type Bahasa = "id" | "en"

// Satu store untuk seluruh isi /api/settings/preferences. Zona waktu dan bahasa
// pernah punya store sendiri-sendiri, dan itu berarti dua GET ke endpoint yang
// sama saat halaman dibuka plus satu GET lagi tiap kali menyimpan — sekaligus
// membuka peluang dua penulis saling menimpa field milik yang lain.
interface PrefStore {
  /** Kosong = ikut zona waktu server, yaitu perilaku panel sebelum fitur ini ada. */
  timezone: string
  bahasa: Bahasa
  siap: boolean
  muat: () => Promise<void>
  setTimezone: (tz: string) => Promise<void>
  setBahasa: (b: Bahasa) => Promise<void>
}

// Field lain milik endpoint yang sama (interval polling, tampilan file manager)
// disimpan apa adanya supaya PUT dari sini tidak menghapusnya.
let mentah: Record<string, unknown> = {}

// Bahasa yang dipilih di halaman login belum bisa disimpan ke server (belum ada
// sesi), padahal muat() sesudah login akan menimpanya dengan preferensi lama.
// Pilihan itu dititipkan di localStorage, lalu dipakai dan didorong ke server
// satu kali begitu preferensi dimuat.
const KUNCI_PRALOGIN = "lindash:bahasa-pralogin" // i18n-abaikan: kunci localStorage

function bacaPralogin(): Bahasa | null {
  try {
    const v = localStorage.getItem(KUNCI_PRALOGIN)
    return v === "id" || v === "en" ? v : null
  } catch {
    return null // private mode / storage diblokir
  }
}

export function simpanBahasaPralogin(b: Bahasa) {
  try {
    localStorage.setItem(KUNCI_PRALOGIN, b)
  } catch {
    /* pilihan tetap berlaku untuk sesi ini, cuma tidak terbawa setelah login */
  }
}

function hapusPralogin() {
  try {
    localStorage.removeItem(KUNCI_PRALOGIN)
  } catch {
    /* tidak ada yang perlu dibersihkan kalau storage memang tidak bisa dipakai */
  }
}

async function simpan(patch: Record<string, unknown>) {
  mentah = { ...mentah, ...patch }
  try {
    // Preferensi disimpan di server, bukan localStorage: akun yang sama di
    // browser lain harus mendapat tampilan yang sama.
    await apiSend("/api/settings/preferences", "PUT", mentah)
  } catch {
    /* tampilan sudah berubah; kegagalan simpan tidak memblokir pemakaian */
  }
}

export const usePrefs = create<PrefStore>((set, get) => ({
  timezone: "",
  // Default mengikuti dokumen produk: Indonesia, sampai preferensi user dimuat.
  // Pilihan di halaman login menang atas default itu supaya layar login tidak
  // berganti bahasa sendiri saat komponen lain dirender.
  bahasa: bacaPralogin() ?? "id",
  siap: false,
  async muat() {
    try {
      const p = await apiGet<Record<string, unknown>>("/api/settings/preferences")
      mentah = p
      const tz = typeof p.timezone === "string" ? p.timezone : ""
      const pralogin = bacaPralogin()
      const b: Bahasa = pralogin ?? (p.language === "en" ? "en" : "id")
      // Format tanggal/jam dipakai view yang mungkin dirender lebih dulu dari
      // efek di app shell; set langsung di sini supaya tidak ada satu render
      // pun yang memakai locale atau zona lama.
      setFormatPrefs({ timezone: tz, bahasa: b })
      document.documentElement.lang = b
      set({ timezone: tz, bahasa: b, siap: true })
      if (pralogin) {
        hapusPralogin()
        // Baru sekarang ada sesi, jadi pilihan di layar login bisa disimpan.
        if (p.language !== pralogin) await get().setBahasa(pralogin)
      }
    } catch {
      set({ siap: true })
    }
  },
  async setTimezone(tz) {
    setFormatPrefs({ timezone: tz })
    set({ timezone: tz })
    await simpan({ timezone: tz })
  },
  async setBahasa(b) {
    setFormatPrefs({ bahasa: b })
    // Atribut lang dipakai screen reader dan pemenggalan kata browser.
    document.documentElement.lang = b
    set({ bahasa: b })
    await simpan({ language: b })
  },
}))

/**
 * Daftar zona waktu diambil dari browser (`Intl.supportedValuesOf`), bukan
 * daftar buatan sendiri — daftar manual pasti basi setiap kali IANA menambah
 * atau mengganti nama zona. Browser lama yang belum punya API itu jatuh ke
 * daftar ringkas berisi zona paling umum.
 */
export function daftarTimezone(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] }
  if (typeof intl.supportedValuesOf === "function") {
    try {
      return intl.supportedValuesOf("timeZone")
    } catch {
      /* jatuh ke cadangan */
    }
  }
  return [
    "UTC", "Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura", "Asia/Singapore",
    "Asia/Kuala_Lumpur", "Asia/Bangkok", "Asia/Tokyo", "Asia/Shanghai",
    "Asia/Kolkata", "Asia/Dubai", "Europe/London", "Europe/Paris",
    "Europe/Amsterdam", "Europe/Berlin", "America/New_York", "America/Chicago",
    "America/Denver", "America/Los_Angeles", "America/Sao_Paulo",
    "Australia/Sydney", "Pacific/Auckland",
  ]
}

/** Offset UTC zona tertentu dalam bentuk "UTC+07:00". */
export function offsetUTC(tz: string, saat: Date = new Date()): string {
  try {
    const bagian = new Intl.DateTimeFormat("en-US", { timeZone: tz, timeZoneName: "longOffset" })
      .formatToParts(saat)
      .find((p) => p.type === "timeZoneName")?.value
    if (bagian) return bagian.replace("GMT", "UTC")
  } catch {
    /* zona tidak dikenal browser */
  }
  return ""
}
