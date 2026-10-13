import { useCallback } from "react"
import { usePrefs } from "@/stores/prefs"

// i18n seadanya: satu kamus datar, tanpa library. Panel ini hanya punya dua
// bahasa dan beberapa ratus string; menambah dependency i18n penuh berarti
// menambah bundel untuk mesin 2 core demi fitur yang bisa dipenuhi satu map.
// Bahasa aktifnya sendiri tinggal di store preferensi (stores/prefs.ts),
// bersama zona waktu, karena keduanya datang dari satu endpoint yang sama.
type Kamus = Record<string, { id: string; en: string }>

const kamus: Kamus = {
  // app shell
  "nav.home": { id: "Overview", en: "Overview" },
  "nav.dashboard": { id: "Dashboard", en: "Dashboard" },
  "nav.fileManagerGroup": { id: "Storage & Files", en: "Storage & Files" },
  "nav.fileManager": { id: "File Manager", en: "File Manager" },
  "nav.samba": { id: "Samba", en: "Samba" },
  "nav.diskPool": { id: "Disk Pool", en: "Disk Pool" },
  "nav.nfs": { id: "NFS Exports", en: "NFS Exports" },
  "nav.bookmarks": { id: "Bookmarks", en: "Bookmarks" },
  "nav.logs": { id: "Logs & Monitoring", en: "Logs & Monitoring" },
  "nav.logsAll": { id: "Logs", en: "Logs" },
  "nav.updates": { id: "Pembaruan", en: "Updates" },
  "nav.fileOperations": { id: "File Operations Log", en: "File Operations Log" },
  "nav.activityLogs": { id: "Activity Logs", en: "Activity Logs" },
  "nav.networkGroup": { id: "Network", en: "Network" },
  "nav.security": { id: "Security", en: "Security" },
  "nav.nineRouter": { id: "9Router", en: "9Router" },
  "nineRouter.mixedContent": { id: "9Router menggunakan HTTP. Panel HTTPS tidak dapat menampilkan iframe HTTP. Gunakan reverse proxy HTTPS untuk 9Router.", en: "9Router uses HTTP. An HTTPS panel cannot display an HTTP iframe. Use an HTTPS reverse proxy for 9Router." },
  "nav.speedtest": { id: "Speedtest", en: "Speedtest" },
  "nav.account": { id: "Akun", en: "Account" },
  "nav.network": { id: "Network", en: "Network" },
  "nav.firewall": { id: "Firewall", en: "Firewall" },
  "nav.fail2ban": { id: "Fail2Ban", en: "Fail2Ban" },
  "nav.fail2banHistory": { id: "Riwayat Fail2ban", en: "Fail2ban history" },
  "nav.alerts": { id: "Alert Thresholds", en: "Alert Thresholds" },
  "nav.components": { id: "Components", en: "Components" },
  "nav.proxy": { id: "Proxy Manager", en: "Proxy Manager" },
  "nav.printServer": { id: "Print Server", en: "Print Server" },
  "nav.ai": { id: "AI & Automation", en: "AI & Automation" },
  "nav.aiAgent": { id: "AI Agent", en: "AI Agent" },
  "nav.system": { id: "System Management", en: "System Management" },
  "nav.processes": { id: "Processes", en: "Processes" },
  "nav.docker": { id: "Docker", en: "Docker" },
  "nav.cron": { id: "Cronjob", en: "Cronjob" },
  "nav.terminal": { id: "Terminal", en: "Terminal" },
  "nav.notFound": { id: "Tidak ditemukan", en: "Not found" },

  // login
  "login.submit": { id: "Masuk", en: "Log in" },

  // topbar
  "topbar.serverTime": { id: "Server Time", en: "Server Time" },
  "topbar.timezone": { id: "Zona waktu", en: "Time zone" },
  "topbar.timezoneServer": { id: "Ikut server", en: "Follow server" },
  "topbar.timezoneSearch": { id: "Cari zona waktu…", en: "Search time zone…" },
  "topbar.notifications": { id: "Notifikasi", en: "Notifications" },
  "topbar.notifTitle": { id: "Pembaruan komponen", en: "Component updates" },
  "topbar.notifKosong": { id: "Tidak ada pembaruan.", en: "No updates available." },
  "topbar.notifOpenPage": { id: "Buka halaman Pembaruan", en: "Open Updates page" },
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
  "nav.reboot": { id: "Reboot", en: "Reboot" },

  "common.sudoer": { id: "sudoer", en: "sudoer" },
  "common.user": { id: "user", en: "user" },
}

// Untuk teks di dalam view, kunci kamus = kalimat bahasa Indonesia-nya sendiri.
// Kalimat lengkap sebagai kunci membuat view tetap terbaca saat dibaca
// manusia, dan string yang belum diterjemahkan otomatis tampil apa adanya
// (bahasa Indonesia) alih-alih memunculkan kunci teknis di layar.
const kamusEn: Record<string, string> = {
  "Proxy Mail memakai Postfix sebagai SMTP relay untuk meneruskan email ke server tujuan berdasarkan domain dan transport map.": "Proxy Mail uses Postfix as an SMTP relay to forward email to the destination server based on domains and transport maps.",
  "Domain email": "Email domain",
  "Host/IP tujuan": "Destination host/IP",
  "Port SMTP": "SMTP port",
  "Tambah tujuan SMTP": "Add SMTP destination",
  "Transport map belum lengkap. Isi domain dan endpoint SMTP tujuan lalu simpan.": "The transport map is incomplete. Enter domains and destination SMTP endpoints, then save.",
  "Transport map akan diganti dan diindeks, Postfix diperiksa lalu direstart. SMTP terputus sementara; pastikan port 25 tersedia.": "The transport map will be replaced and indexed, Postfix checked then restarted. SMTP is briefly interrupted; ensure port 25 is available.",
  "Endpoint SMTP tujuan harus reachable dari Postfix. Tailscale: host/IP tujuan. Cloudflare TCP: endpoint konektor lokal atau private route yang sudah tersedia, bukan hostname HTTP publik. mynetworks adalah klien tepercaya, bukan tujuan forwarding.": "The destination SMTP endpoint must be reachable from Postfix. Tailscale: destination host/IP. Cloudflare TCP: an existing local connector endpoint or private route, not a public HTTP hostname. mynetworks identifies trusted clients, not forwarding destinations.",
  "Simpan dan restart Postfix?": "Save and restart Postfix?",
  "Simpan dan restart": "Save and restart",
  "Isi nilai yang disetujui": "Fill approved values",
  "Konfigurasi Postfix tersimpan; restart berhasil.": "Postfix configuration saved; restart succeeded.",
  "Port SMTP 25 tidak boleh dipakai layanan lain. Batasi mynetworks ke jaringan tepercaya karena dapat mengirim email tanpa autentikasi.": "SMTP port 25 must not be used by another service. Restrict mynetworks to trusted networks because they can send email without authentication.",
}

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

/** Versi hook agar komponen ikut render ulang saat bahasa berganti. Referensi
 * fungsi stabil selama bahasanya sama, sehingga aman dijadikan dependency
 * useCallback/useEffect tanpa memicu fetch ulang setiap render. */
export function useTr(): (teks: string) => string {
  const bahasa = usePrefs((s) => s.bahasa)
  return useCallback((teks: string) => (bahasa === "en" ? (kamusEn[teks] ?? teks) : teks), [bahasa])
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
