// Peta path → dynamic import chunk-nya.
//
// Modul terpisah dari router/index.tsx dengan sengaja: sidebar (app-shell)
// butuh prefetchRute, sementara router/index.tsx meng-import AppShell. Kalau
// peta ini tinggal di router/index.tsx, keduanya saling meng-import dan
// nilainya bisa belum terisi saat modul dievaluasi.
//
// Satu peta dipakai dua kali — untuk membangun komponen lazy di router dan
// untuk prefetch dari sidebar. Ditulis terpisah, path yang baru ditambahkan
// akan diam-diam kehilangan prefetch-nya.
export const pemuatRute: Record<string, () => Promise<unknown>> = {
  "/files": () => import("@/views/files"),
  "/files/samba": () => import("@/views/samba"),
  "/files/pool": () => import("@/views/mergerfs"),
  "/files/nfs": () => import("@/views/nfs"),
  "/files/bookmarks": () => import("@/views/bookmarks"),
  "/logs/alerts": () => import("@/views/logs"),
  "/logs/file-operations": () => import("@/views/file-operations"),
  "/logs/activity": () => import("@/views/activity-logs"),
  "/settings/account": () => import("@/views/account"),
  "/settings/network": () => import("@/views/network"),
  "/settings/firewall": () => import("@/views/firewall"),
  "/settings/fail2ban": () => import("@/views/fail2ban"),
  "/settings/alerts": () => import("@/views/alerts"),
  "/settings/components": () => import("@/views/components"),
  "/settings/print": () => import("@/views/print-server"),
  "/ai/agent": () => import("@/views/ai-agent"),
  "/system/processes": () => import("@/views/processes"),
  "/system/docker": () => import("@/views/docker"),
  "/system/terminal": () => import("@/views/terminal"),
}

// prefetchRute menarik chunk sebuah route lebih awal — dipanggil sidebar saat
// kursor/fokus menyentuh menunya, sebelum kliknya terjadi.
//
// Tanpa ini, chunk baru diminta pada saat klik: highlight sidebar sudah
// pindah sementara area konten masih kosong menunggu unduhan, dan itu yang
// terbaca sebagai "UI-nya delay, tidak sinkron dengan backend". import()
// menyimpan hasilnya, jadi memanggil ini berkali-kali tidak menambah
// permintaan jaringan.
export function prefetchRute(path: string): void {
  const muat = pemuatRute[path]
  if (muat) void muat().catch(() => undefined)
}
