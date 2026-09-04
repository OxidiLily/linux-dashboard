import { ApiError } from "@/lib/api"
import { pesanBackendEn } from "@/lib/pesan-backend-en"
import { tr, trf } from "@/stores/i18n"
import { usePrefs } from "@/stores/prefs"

// Backend mengirim KODE + data (path, nama, angka); kalimatnya disusun di sini
// sesuai bahasa yang dipilih user. Kalimat bahasa Indonesia dari backend hanya
// dipakai sebagai cadangan untuk kode yang belum punya padanan di sini —
// dengan begitu menambah kode baru di backend tidak pernah membuat UI kosong.
const kalimat: Record<string, string> = {
  path_invalid: "Path tidak valid.",
  folder_missing: "Folder {0} tidak ada.",
  outside_home: "Akses ke {0} butuh hak sudo — di luar home directory Anda.",
  symlink_escape: "Akses ke {0} ditolak: symlink menunjuk keluar home directory Anda.",
  component_unknown: "Komponen {0} tidak dikenal.",
  not_installed: "{0} belum terpasang — pasang dulu lewat menu Components.",
  already_exists: "{0} sudah ada.",
  managed_externally: "{0} dikelola di luar panel — ubah berkasnya langsung.",
  credential_unreadable: "Kredensial tidak terbaca. Tempel token atau perintah lengkapnya.",
  still_connected: "Masih tersambung — tekan Putus dulu sebelum mengganti kredensial.",
  fuse_missing:
    "FUSE tidak tersedia: mergerfs butuh /dev/fuse. Di LXC, aktifkan fuse=1 di konfigurasi container lalu boot ulang.",
  value_invalid: "Nilai tidak valid.",
  password_too_short: "Password minimal {0} karakter.",
  guest_ok_conflict: "Share Guest OK tidak bisa dibatasi ke user tertentu — matikan Guest OK dulu.",
  action_in_progress: "Masih ada aksi berjalan untuk komponen {0} — tunggu sampai selesai.",
  disk_has_filesystem: "{0} sudah berisi filesystem {1}.",
  disk_in_use: "{0} bukan disk kosong — sudah punya partisi, dipakai LVM/RAID, atau sedang ter-mount.",
  requires_sudo: "Aksi ini butuh akses sudo.",
}

const kalimatEn: Record<string, string> = {
  path_invalid: "Invalid path.",
  folder_missing: "Folder {0} does not exist.",
  outside_home: "Access to {0} needs sudo — it is outside your home directory.",
  symlink_escape: "Access to {0} denied: the symlink points outside your home directory.",
  component_unknown: "Unknown component {0}.",
  not_installed: "{0} is not installed — install it from the Components menu first.",
  already_exists: "{0} already exists.",
  managed_externally: "{0} is managed outside the panel — edit its file directly.",
  credential_unreadable: "Credential could not be read. Paste the token or the full command.",
  still_connected: "Still connected — press Disconnect before replacing the credential.",
  fuse_missing:
    "FUSE is unavailable: mergerfs needs /dev/fuse. On LXC, enable fuse=1 in the container config and reboot.",
  value_invalid: "Invalid value.",
  password_too_short: "Password must be at least {0} characters.",
  guest_ok_conflict: "A Guest OK share cannot be limited to specific users — turn Guest OK off first.",
  action_in_progress: "An action is still running for component {0} — wait for it to finish.",
  disk_has_filesystem: "{0} already holds a {1} filesystem.",
  disk_in_use: "{0} is not an empty disk — it has partitions, is used by LVM/RAID, or is mounted.",
  requires_sudo: "This action requires sudo access.",
}

// Didaftarkan ke tabel terjemahan supaya satu jalur yang sama dipakai untuk
// semua teks: trf() memilih bahasa, di sini hanya menyediakan padanannya.
import { daftarkanTerjemahan } from "@/stores/i18n"
daftarkanTerjemahan(
  Object.fromEntries(Object.keys(kalimat).map((k) => [kalimat[k], kalimatEn[k]])),
)

/** Susun pesan error untuk ditampilkan ke user. */
export function pesanError(e: unknown): string {
  if (e instanceof ApiError && e.code && kalimat[e.code]) {
    return trf(kalimat[e.code], ...e.params)
  }
  if (e instanceof Error && e.message) {
    if (usePrefs.getState().bahasa !== "en") return e.message
    // Kalimat tetap dicari lebih dulu di tabel terjemahan biasa; yang berisi
    // nilai sisipan ditangani lewat pencocokan pola.
    const tepat = tr(e.message)
    if (tepat !== e.message) return tepat
    return pesanBackendEn(e.message)
  }
  return String(e)
}
