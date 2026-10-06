import { apiGet } from "@/lib/api"
import { daftarkanTerjemahan, tr } from "@/stores/i18n"

export type GeoIP = {
  ip: string; country: string; country_code: string; region: string; city: string
  isp: string; org: string; asn: number; timezone: string
  status: "ready" | "pending" | "unavailable" | "private" | "rate_limited" | "error"
  source: "cache" | "ipwho.is" | "none"; fetched_at: string
}
export const GEOIP_HINT = "Lokasi IP merupakan perkiraan. IP publik yang belum ada di cache dikirim oleh server ke ipwho.is."
daftarkanTerjemahan({
  "GeoIP ditolak: kedalaman JSON melebihi 32; payload tidak ditampilkan.": "GeoIP rejected: JSON nesting exceeds 32; payload not displayed.",
  "Riwayat": "History", "Waktu pengambilan cache": "Cache fetched at",
  "Waktu snapshot menunjukkan waktu saat data diambil, bukan jam saat ini.": "Snapshot time shows when the data was fetched, not the current clock.",
  "Memuat GeoIP…": "Loading GeoIP…", "Identitas dan lokasi": "Identity and location", "Kolom": "Field", "Nilai": "Value",
  "Tidak tersedia": "Unavailable", "Riwayat Fail2ban": "Fail2ban history", "Kembali ke Fail2ban": "Back to Fail2ban", "Jail dan IP wajib diisi.": "Jail and IP are required.",
  [GEOIP_HINT]: "IP locations are approximate. Public IPs missing from the cache are sent by the server to ipwho.is.",
  "Cache GeoIP": "GeoIP cache", "Tersedia": "Ready", "Menunggu GeoIP": "GeoIP pending",
  "GeoIP tidak tersedia": "GeoIP unavailable", "IP privat/lokal": "Private/local IP",
  "Batas permintaan GeoIP": "GeoIP rate limited", "Gagal memuat GeoIP": "Failed to load GeoIP",
  "Belum ada IP diblokir.": "No banned IPs.", "Terakhir diperbarui": "Last updated",
  "Respons GeoIP tidak valid": "Invalid GeoIP response", "Polling GeoIP berhenti; muat ulang untuk mencoba lagi.": "GeoIP polling stopped; refresh to try again.",
})
export function statusGeoIP(status: GeoIP["status"] = "pending") {
  return tr({ ready: "Tersedia", pending: "Menunggu GeoIP", unavailable: "GeoIP tidak tersedia", private: "IP privat/lokal", rate_limited: "Batas permintaan GeoIP", error: "Gagal memuat GeoIP" }[status])
}
export function lokasiGeoIP(item?: GeoIP) {
  return item ? [...new Set([item.country || item.country_code, item.region, item.city].filter(Boolean))].join(" · ") || "—" : "—"
}
// URL's IPv6 parser compresses hextets; unmap the same ::ffff/96 prefix as netip.Unmap.
export function kunciGeoIP(ip: string): string {
  if (!ip.includes(":")) return ip
  try {
    const host = new URL(`http://[${ip}]/`).hostname.slice(1, -1)
    const mapped = /^::ffff:([\da-f]+):([\da-f]+)$/.exec(host)
    if (!mapped) return host
    const high = parseInt(mapped[1], 16), low = parseInt(mapped[2], 16)
    return [high >> 8, high & 255, low >> 8, low & 255].join(".")
  } catch { return ip }
}
const fields = ["ip", "country", "country_code", "region", "city", "isp", "org", "timezone", "fetched_at"] as const
function valid(item: GeoIP) {
  return item && fields.every(key => typeof item[key] === "string") && !!item.ip && Number.isFinite(item.asn) &&
    ["ready", "pending", "unavailable", "private", "rate_limited", "error"].includes(item.status) && ["cache", "ipwho.is", "none"].includes(item.source)
}
export type GeoIPFull = Pick<GeoIP, "ip" | "status" | "source" | "fetched_at"> & { data: Record<string, unknown> | null }
export function poleGeoIP(data: unknown, prefix = "", depth = 0): [string, unknown][] {
  if (data !== null && typeof data === "object") {
    if (depth >= 32) throw new Error(tr("GeoIP ditolak: kedalaman JSON melebihi 32; payload tidak ditampilkan."))
    if (Object.keys(data).length) return Object.entries(data).flatMap(([key, value]) => poleGeoIP(value, prefix ? `${prefix}.${key}` : key, depth + 1))
  }
  return [[prefix, data]]
}
export function ratakanGeoIP(data: unknown, prefix = "", depth = 0): [string, string][] {
  return poleGeoIP(data, prefix, depth).map(([key, value]) => [key, typeof value === "string" && value !== "" ? value : JSON.stringify(value)])
}
const geoLabels: Record<string, [string, string]> = {
  ip: ["Alamat IP", "IP Address"], success: ["Berhasil", "Success"], type: ["Jenis IP", "IP Type"],
  continent: ["Benua", "Continent"], continent_code: ["Kode Benua", "Continent Code"],
  country: ["Negara", "Country"], country_code: ["Kode Negara", "Country Code"],
  region: ["Wilayah", "Region"], region_code: ["Kode Wilayah", "Region Code"], city: ["Kota", "City"],
  latitude: ["Lintang", "Latitude"], longitude: ["Bujur", "Longitude"], is_eu: ["Anggota Uni Eropa", "EU Member"],
  postal: ["Kode Pos", "Postal Code"], calling_code: ["Kode Telepon", "Calling Code"], capital: ["Ibu Kota", "Capital"],
  borders: ["Negara Berbatasan", "Bordering Countries"], flag: ["Bendera", "Flag"], connection: ["Koneksi", "Connection"],
  timezone: ["Zona Waktu", "Timezone"], native: ["Nama Lokal", "Native Names"], currency: ["Mata Uang", "Currency"],
  security: ["Keamanan", "Security"], rate: ["Batas Permintaan", "Rate Limit"],
  asn: ["ASN", "ASN"], org: ["Organisasi", "Organization"], isp: ["ISP", "ISP"], domain: ["Domain", "Domain"],
  img: ["Ikon Bendera", "Flag Icon"], emoji: ["Emoji Bendera", "Flag Emoji"], emoji_unicode: ["Unicode Emoji", "Emoji Unicode"],
  id: ["ID Zona Waktu", "Timezone ID"], abbr: ["Singkatan Zona Waktu", "Timezone Abbreviation"],
  is_dst: ["Waktu Musim Panas", "Daylight Saving Time"], offset: ["Offset Zona Waktu", "Timezone Offset"],
  utc: ["Offset UTC", "UTC Offset"], current_time: ["Waktu Snapshot", "Snapshot Time"],
  code: ["Kode Mata Uang", "Currency Code"], name: ["Nama Mata Uang", "Currency Name"],
  symbol: ["Simbol Mata Uang", "Currency Symbol"], plural: ["Nama Jamak Mata Uang", "Currency Plural Name"],
  exchange_rate: ["Kurs", "Exchange Rate"],
  anonymous: ["Anonim", "Anonymous"], proxy: ["Proksi", "Proxy"], vpn: ["VPN", "VPN"], tor: ["Tor", "Tor"],
  hosting: ["Hosting", "Hosting"], relay: ["Relai Privat", "Private Relay"], mobile: ["Seluler", "Mobile"],
  upgrade_url: ["URL Peningkatan Paket", "Upgrade URL"],
  limit: ["Batas", "Limit"], remaining: ["Sisa Permintaan", "Remaining Requests"], reset: ["Reset", "Reset"],
  message: ["Pesan", "Message"], active: ["Aktif", "Active"],
}
daftarkanTerjemahan(Object.fromEntries(Object.values(geoLabels)))
daftarkanTerjemahan({ "Ya": "Yes", "Tidak": "No", "Kosong": "Empty", "Null (tanpa nilai)": "Null (no value)", "Objek kosong": "Empty object", "Daftar kosong": "Empty list" })
export function labelGeoIP(key: string): string {
  return key.split(".").map(part => Object.hasOwn(geoLabels, part) ? tr(geoLabels[part][0]) : part.replace(/([a-z\d])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").replace(/\b\w/g, c => c.toUpperCase())).join(" · ")
}
const countryCodes = new Set("AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW".split(" "))
export function gambarBenderaGeoIP(value: unknown): string | null {
  if (typeof value !== "string") return null
  const match = /^https:\/\/cdn\.ipwhois\.io\/flags\/([a-z]{2})\.svg$/.exec(value)
  return match && countryCodes.has(match[1].toUpperCase()) ? value : null
}
export function benderaGeoIP(data: Record<string, unknown>): string | null {
  const code = typeof data.country_code === "string" ? data.country_code.toUpperCase() : ""
  if (countryCodes.has(code)) return String.fromCodePoint(...[...code].map(c => c.charCodeAt(0) + 127397))
  const emoji = data.flag && typeof data.flag === "object" ? (data.flag as Record<string, unknown>).emoji : null
  return typeof emoji === "string" && /^(?:[\u{1F1E6}-\u{1F1FF}]{2}|🏴(?:[\u{E0061}-\u{E007A}]+\u{E007F})?)$/u.test(emoji) ? emoji : null
}
export function nilaiGeoIP(key: string, value: unknown, data: Record<string, unknown>): string {
  if (typeof value === "boolean") return tr(value ? "Ya" : "Tidak")
  if (value === null) return tr("Null (tanpa nilai)")
  if (value === "") return tr("Kosong")
  if (typeof value === "object") return tr(Array.isArray(value) ? "Daftar kosong" : "Objek kosong")
  if (["latitude", "longitude"].includes(key) && typeof value === "number" && Number.isFinite(value) && Math.abs(value) <= (key === "latitude" ? 90 : 180)) return `${value}°`
  if (key === "timezone.offset" && typeof value === "number" && Number.isInteger(value) && Math.abs(value) <= 86400) {
    const seconds = Math.abs(value)
    return `UTC${value < 0 ? "−" : "+"}${String(Math.floor(seconds / 3600)).padStart(2, "0")}:${String(Math.floor(seconds % 3600 / 60)).padStart(2, "0")}${seconds % 60 ? `:${String(seconds % 60).padStart(2, "0")}` : ""}`
  }
  if (key === "timezone.current_time" && typeof value === "string" && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})$/.test(value)) {
    const zone = data.timezone && typeof data.timezone === "object" ? (data.timezone as Record<string, unknown>).id : null
    if (typeof zone === "string" && Number.isFinite(Date.parse(value))) {
      try { return `${new Intl.DateTimeFormat(tr("Ya") === "Yes" ? "en-GB" : "id-ID", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZoneName: "longOffset" }).format(new Date(value))} · ${zone}` } catch { /* Unknown timezone: retain the provider value. */ }
    }
  }
  return String(value)
}
export function riwayatIP(jail: string, ip: string) {
  return `/settings/fail2ban/history?jail=${encodeURIComponent(jail)}&ip=${encodeURIComponent(ip)}`
}
export async function pollGeoIPFull(jail: string, ip: string, signal: AbortSignal,
  update: (data: GeoIPFull) => void, wait = pause): Promise<boolean> {
  for (let attempt = 0; attempt < 60 && !signal.aborted; attempt++) {
    const result = await apiGet<GeoIPFull>(`/api/security/fail2ban/${encodeURIComponent(jail)}/geoip?ip=${encodeURIComponent(ip)}`, signal)
    if (signal.aborted) return false
    if (!result || kunciGeoIP(result.ip) !== kunciGeoIP(ip) ||
      !["ready", "pending", "unavailable", "private", "rate_limited", "error"].includes(result.status) ||
      !["cache", "ipwho.is", "none"].includes(result.source) || typeof result.fetched_at !== "string" ||
      !(result.data === null || (typeof result.data === "object" && !Array.isArray(result.data)))) throw new Error(tr("Respons GeoIP tidak valid"))
    update(result)
    if (result.status !== "pending") return false
    if (attempt < 59) await wait(Math.min(2000, 500 * 2 ** Math.min(attempt, 2)), signal)
  }
  return !signal.aborted
}
function pause(ms: number, signal: AbortSignal) {
  return new Promise<void>(resolve => {
    if (signal.aborted) return resolve()
    const finish = () => { clearTimeout(timer); signal.removeEventListener("abort", finish); resolve() }
    const timer = setTimeout(finish, ms)
    signal.addEventListener("abort", finish, { once: true })
  })
}
// One server lookup stream per list, shared by all jails; never contact the provider from the browser.
export async function pollGeoIP(signal: AbortSignal, current: () => boolean,
  update: (items: Record<string, GeoIP>) => void, wait = pause): Promise<boolean> {
  for (let attempt = 0; attempt < 60 && !signal.aborted && current(); attempt++) {
    const data = await apiGet<{ items: GeoIP[]; pending: boolean }>("/api/security/fail2ban/geoip", signal)
    if (signal.aborted || !current()) return false
    if (!data || typeof data.pending !== "boolean" || !Array.isArray(data.items) || !data.items.every(valid)) throw new Error(tr("Respons GeoIP tidak valid"))
    update(Object.fromEntries(data.items.map(item => [item.ip, item])))
    if (!data.pending) return false
    if (attempt < 59) await wait(Math.min(2000, 500 * 2 ** Math.min(attempt, 2)), signal)
  }
  return !signal.aborted && current()
}
