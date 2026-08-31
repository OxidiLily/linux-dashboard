import { create } from "zustand"

// Status "pembaruan sedang berjalan" dibagi antara UpdateModal (satu-satunya
// yang memicu pembaruan) dan AppShell (sidebar yang menampilkan icon
// berputar saat proses jalan). Tanpa store bersama, sidebar tidak tahu
// status terkini karena polling modal berada di scope komponen itu saja.
//
// Catatan: status ini tidak perlu real-time dengan server — cukup
// boolean lokal yang di-update oleh UpdateModal saat mulai/selesai.
// Jika halaman di-reload saat pembaruan jalan, store kembali false
// (icon berhenti spin), tapi UpdateModal yang baru dibuka akan polling
// /api/settings/update dan mulai memutar lagi jika server masih
// menjalankan installer.
interface UpdateStore {
  berjalan: boolean
  mulai: () => void
  selesai: () => void
}

export const useUpdateStore = create<UpdateStore>((set) => ({
  berjalan: false,
  mulai: () => set({ berjalan: true }),
  selesai: () => set({ berjalan: false }),
}))
