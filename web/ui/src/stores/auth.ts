import { create } from "zustand"
import { apiGet, apiSend } from "@/lib/api"
import { tr } from "@/stores/i18n"
import type { SessionUser } from "@/lib/types"

interface AuthStore {
  user: SessionUser | null
  /** false selama /api/auth/me pertama masih jalan — guard rute menunggunya. */
  ready: boolean
  busy: boolean
  error: string
  load: () => Promise<void>
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

export const useAuth = create<AuthStore>((set) => ({
  user: null,
  ready: false,
  busy: false,
  error: "",
  async load() {
    try {
      const me = await apiGet<SessionUser>("/api/auth/me")
      set({ user: me })
    } catch {
      set({ user: null })
    } finally {
      set({ ready: true })
    }
  },
  async login(username, password) {
    set({ busy: true, error: "" })
    try {
      // POST /auth/login sudah mengembalikan sessionUser yang lengkap —
      // username, sudo, home, uid, groups, must_change_password. Memanggil
      // /auth/me setelahnya menambah satu round-trip penuh (plus lookup
      // /etc/passwd di server) untuk data yang sudah ada di tangan, dan
      // itu terasa sebagai jeda tiap kali menekan Masuk.
      const me = await apiSend<SessionUser>("/api/auth/login", "POST", { username, password })
      set({ user: me })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : tr("Login gagal") })
      throw e
    } finally {
      set({ busy: false })
    }
  },
  // Sesi lokal dibuang LEBIH DULU, permintaan ke server menyusul.
  //
  // Sebelumnya urutannya terbalik: UI baru berpindah setelah POST /logout
  // selesai, jadi tombol Keluar terasa tidak menanggapi selama round-trip —
  // dan round-trip itu ikut menunggu tulisan SQLite (hapus sesi + catat
  // aktivitas). Tidak ada informasi dari server yang dibutuhkan untuk
  // memutuskan logout, jadi tidak ada alasan menahan tampilan.
  //
  // Kalau permintaannya gagal, sesi di server bisa saja masih hidup — itu
  // dilaporkan lewat error yang dilempar, bukan didiamkan.
  async logout() {
    set({ user: null, error: "" })
    await apiSend("/api/auth/logout", "POST")
  },
}))