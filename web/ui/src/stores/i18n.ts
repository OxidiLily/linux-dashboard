import { usePrefs } from "@/stores/prefs"

// i18n seadanya: satu kamus datar, tanpa library. Panel ini hanya punya dua
// bahasa dan beberapa ratus string; menambah dependency i18n penuh berarti
// menambah bundel untuk mesin 2 core demi fitur yang bisa dipenuhi satu map.
// Bahasa aktifnya sendiri tinggal di store preferensi (stores/prefs.ts),
// bersama zona waktu, karena keduanya datang dari satu endpoint yang sama.
type Kamus = Record<string, { id: string; en: string }>

const kamus: Kamus = {
  // app shell
  "nav.home": { id: "Home", en: "Home" },
  "nav.dashboard": { id: "Dashboard", en: "Dashboard" },
  "nav.fileManagerGroup": { id: "File manager", en: "File manager" },
  "nav.fileManager": { id: "File Manager", en: "File Manager" },
  "nav.samba": { id: "Samba", en: "Samba" },
  "nav.diskPool": { id: "Disk Pool", en: "Disk Pool" },
  "nav.nfs": { id: "NFS Exports", en: "NFS Exports" },
  "nav.bookmarks": { id: "Bookmarks", en: "Bookmarks" },
  "nav.logs": { id: "Logs", en: "Logs" },
  "nav.fileOperations": { id: "File Operations", en: "File Operations" },
  "nav.activityLogs": { id: "Activity Logs", en: "Activity Logs" },
  "nav.settings": { id: "Settings", en: "Settings" },
  "nav.account": { id: "Akun", en: "Account" },
  "nav.network": { id: "Network", en: "Network" },
  "nav.firewall": { id: "Firewall", en: "Firewall" },
  "nav.fail2ban": { id: "Fail2ban", en: "Fail2ban" },
  "nav.alerts": { id: "Alert Thresholds", en: "Alert Thresholds" },
  "nav.components": { id: "Components", en: "Components" },
  "nav.printServer": { id: "Print server", en: "Print server" },
  "nav.ai": { id: "AI", en: "AI" },
  "nav.aiAgent": { id: "AI Agent", en: "AI Agent" },
  "nav.system": { id: "System", en: "System" },
  "nav.processes": { id: "Processes", en: "Processes" },
  "nav.docker": { id: "Docker", en: "Docker" },
  "nav.terminal": { id: "Terminal", en: "Terminal" },
  "nav.notFound": { id: "Tidak ditemukan", en: "Not found" },

  // login
  "login.submit": { id: "Masuk", en: "Log in" },

  // topbar
  "topbar.serverTime": { id: "Server Time", en: "Server Time" },
  "topbar.timezone": { id: "Zona waktu", en: "Time zone" },
  "topbar.timezoneServer": { id: "Ikut server", en: "Follow server" },
  "topbar.timezoneSearch": { id: "Cari zona waktu…", en: "Search time zone…" },
  "topbar.language": { id: "Bahasa", en: "Language" },
  "topbar.hideSidebar": { id: "Sembunyikan sidebar", en: "Hide sidebar" },
  "topbar.showSidebar": { id: "Tampilkan sidebar", en: "Show sidebar" },
  "topbar.logout": { id: "Keluar", en: "Log out" },

  // status koneksi
  "conn.connecting": { id: "Menyambungkan…", en: "Connecting…" },
  "conn.failed": { id: "Gagal menyambungkan", en: "Connection failed" },
  "conn.retry": { id: "Hubungkan kembali", en: "Reconnect" },
  "conn.restored": {
    id: "Tersambung kembali — data realtime dimuat ulang.",
    en: "Reconnected — realtime data reloaded.",
  },
  "conn.live": { id: "Realtime aktif", en: "Realtime active" },
  "conn.lost": { id: "Koneksi terputus", en: "Connection lost" },

  // umum
  "nav.update": { id: "Update", en: "Update" },
  "nav.uninstall": { id: "Uninstall", en: "Uninstall" },

  "common.sudoer": { id: "sudoer", en: "sudoer" },
  "common.user": { id: "user", en: "user" },
}

// Untuk teks di dalam view, kunci kamus = kalimat bahasa Indonesia-nya sendiri.
// Kalimat lengkap sebagai kunci membuat view tetap terbaca saat dibaca
// manusia, dan string yang belum diterjemahkan otomatis tampil apa adanya
// (bahasa Indonesia) alih-alih memunculkan kunci teknis di layar.
const kamusEn: Record<string, string> = {}

export function daftarkanTerjemahan(entri: Record<string, string>) {
  Object.assign(kamusEn, entri)
}

/** Terjemahkan kalimat Indonesia ke bahasa aktif. */
export function tr(teks: string): string {
  if (usePrefs.getState().bahasa !== "en") return teks
  return kamusEn[teks] ?? teks
}

/**
 * Versi berparameter: kunci memakai placeholder {0}, {1}, … supaya kalimat
 * ber-interpolasi tetap bisa diterjemahkan utuh. Menerjemahkan potongan
 * kalimat terpisah menghasilkan tata bahasa yang salah begitu urutan katanya
 * berbeda antar bahasa.
 */
export function trf(teks: string, ...arg: (string | number)[]): string {
  const dasar = tr(teks)
  return dasar.replace(/\{(\d+)\}/g, (_, i) => String(arg[Number(i)] ?? ""))
}

/** Versi hook agar komponen ikut render ulang saat bahasa berganti. */
export function useTr(): (teks: string) => string {
  const bahasa = usePrefs((s) => s.bahasa)
  return (teks: string) => (bahasa === "en" ? (kamusEn[teks] ?? teks) : teks)
}

/** Terjemahkan satu kunci. Kunci yang belum ada dikembalikan apa adanya. */
export function t(kunci: string): string {
  const entri = kamus[kunci]
  if (!entri) return kunci
  return entri[usePrefs.getState().bahasa]
}

/** Hook agar komponen ikut render ulang saat bahasa berganti. */
export function useT(): (kunci: string) => string {
  const bahasa = usePrefs((s) => s.bahasa)
  return (kunci: string) => kamus[kunci]?.[bahasa] ?? kunci
}
