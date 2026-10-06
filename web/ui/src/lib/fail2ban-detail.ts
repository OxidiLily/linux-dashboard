import { daftarkanTerjemahan, tr, trf } from "@/stores/i18n"

import { apiGet } from "@/lib/api"

export type DetailIP = {
  jail: string
  ip: string
  location: string
  events: { time: string; source: string; action: string; message: string }[]
  warnings: string[]
}

export async function muatDetailIP(jail: string, ip: string, signal: AbortSignal): Promise<DetailIP | undefined> {
  const data = await apiGet<DetailIP>(`/api/security/fail2ban/${encodeURIComponent(jail)}/detail?ip=${encodeURIComponent(ip)}`, signal)
  if (signal.aborted) return
  if (!data || data.jail !== jail || data.ip !== ip || typeof data.location !== "string" ||
    !Array.isArray(data.events) || !Array.isArray(data.warnings) ||
    !data.events.every((e) => e && [e.time, e.source, e.action, e.message].every((v) => typeof v === "string")) ||
    !data.warnings.every((w) => typeof w === "string")) throw new Error("Invalid Fail2ban detail response")
  return data
}

// Match legacy helper responses too; translate UI metadata, never original log messages.
const warnings: Record<string, string> = {
  "Location unavailable: no local Geo-IP facility configured; IP was not shared externally.": "Lokasi tidak tersedia: fasilitas Geo-IP lokal belum dikonfigurasi; IP tidak dibagikan ke pihak luar.",
  "History limited to current and .1 Fail2ban logs, or the latest 2000 journal entries within 7 days; older events unavailable.": "Riwayat terbatas pada log Fail2ban saat ini dan .1, atau 2000 entri jurnal terbaru dalam 7 hari; peristiwa lebih lama tidak tersedia.",
  "Event messages truncated to 2048 bytes.": "Pesan peristiwa dipotong hingga 2048 byte.",
  "Events truncated to 200 entries.": "Peristiwa dibatasi hingga 200 entri.",
  "Fail2ban file timestamps lack timezone: server local timezone assumed; historical timezone changes/DST may be ambiguous.": "Timestamp berkas Fail2ban tidak memiliki zona waktu: diasumsikan zona waktu lokal server; perubahan zona waktu historis/DST dapat menimbulkan ambiguitas.",
  "Found event time unavailable or unrecognized; processing timestamp used when available.": "Waktu peristiwa Found tidak tersedia atau tidak dikenali; timestamp pemrosesan digunakan jika tersedia.",
  "Some event timestamps unavailable or unrecognized.": "Sebagian timestamp peristiwa tidak tersedia atau tidak dikenali.",
  "Fail2ban log unavailable: {0}.": "Log Fail2ban tidak tersedia: {0}.",
  "Fail2ban log truncated to latest 2 MiB: {0}.": "Log Fail2ban dipotong hingga 2 MiB terbaru: {0}.",
  "{0} unavailable.": "{0} tidak tersedia.",
  "{0} output truncated to complete entries within latest 2 MiB; older entries discarded.": "Output {0} dipotong menjadi entri lengkap dalam 2 MiB terbaru; entri lebih lama dibuang.",
  "{0} entries unavailable or malformed.": "Entri {0} tidak tersedia atau formatnya rusak.",
  "{0} truncated to latest 2000 entries.": "{0} dibatasi hingga 2000 entri terbaru.",
  "SSH journal attempts are IP-correlated context, not proof that each attempt caused this jail's ban.": "Percobaan dalam jurnal SSH merupakan konteks yang berkorelasi dengan IP, bukan bukti bahwa setiap percobaan menyebabkan pemblokiran oleh jail ini.",
  "Service authentication details unavailable for this jail; Found events identify Fail2ban detections.": "Detail autentikasi layanan tidak tersedia untuk jail ini; peristiwa Found menunjukkan deteksi Fail2ban.",
  "No matching events available in the bounded history; this does not mean no attempts occurred.": "Tidak ada peristiwa yang cocok dalam riwayat terbatas; ini bukan berarti tidak ada percobaan.",
  "Log scan truncated: oversized line or read failure.": "Pemindaian log terpotong: baris terlalu besar atau pembacaan gagal.",
}
const actions: Record<string, string> = { Found: "Terdeteksi", Ban: "Pemblokiran", Unban: "Lepas blokir", SSH: "Konteks SSH" }
daftarkanTerjemahan(Object.fromEntries(Object.entries(warnings).map(([en, id]) => [id, en])))
daftarkanTerjemahan({ "Terdeteksi": "Found", "Pemblokiran": "Ban", "Konteks SSH": "SSH context" })

export function peringatanFail2ban(warning: string): string {
  if (Object.hasOwn(warnings, warning)) return tr(warnings[warning])
  for (const [en, id] of Object.entries(warnings)) {
    if (!en.includes("{0}")) continue
    const [prefix, suffix] = en.split("{0}")
    if (!warning.startsWith(prefix) || !warning.endsWith(suffix)) continue
    const value = warning.slice(prefix.length, warning.length - suffix.length)
    // Generic suffixes apply only to helper journal sources, not unknown warnings.
    if (!prefix && !["journal:fail2ban", "journal:ssh"].includes(value)) continue
    return trf(id, value)
  }
  return warning
}

export function lokasiFail2ban(location = ""): string {
  return !location.trim() || location.trim() === "Unavailable" ? tr("Lokasi tidak tersedia dari log.") : location
}

export function aksiFail2ban(action: string): string {
  return Object.hasOwn(actions, action) ? tr(actions[action]) : action || "—"
}
