import { create } from "zustand"
import { apiGet, apiSend } from "@/lib/api"
import { tr } from "@/stores/i18n"
import type { SessionUser } from "@/lib/types"

type LoginResponse = SessionUser | { totp_required: true; challenge: string }

interface AuthStore {
  user: SessionUser | null
  /** false selama /api/auth/me pertama masih jalan — guard rute menunggunya. */
  ready: boolean
  busy: boolean
  error: string
  totpChallenge: string
  load: () => Promise<void>
  login: (username: string, password: string) => Promise<boolean>
  verifyTOTP: (code: string) => Promise<void>
  cancelTOTP: () => void
  logout: () => Promise<void>
}

export const useAuth = create<AuthStore>((set) => ({
  user: null,
  ready: false,
  busy: false,
  error: "",
  totpChallenge: "",
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
    set({ busy: true, error: "", totpChallenge: "" })
    try {
      const result = await apiSend<LoginResponse>("/api/auth/login", "POST", { username, password })
      if ("totp_required" in result) {
        set({ totpChallenge: result.challenge })
        return false
      }
      set({ user: result })
      return true
    } catch (e) {
      set({ error: e instanceof Error ? e.message : tr("Login gagal") })
      throw e
    } finally {
      set({ busy: false })
    }
  },
  async verifyTOTP(code) {
    set({ busy: true, error: "" })
    try {
      const challenge = useAuth.getState().totpChallenge
      const me = await apiSend<SessionUser>("/api/auth/totp", "POST", { challenge, code })
      set({ user: me, totpChallenge: "" })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : tr("Kode autentikasi salah") })
      throw e
    } finally {
      set({ busy: false })
    }
  },
  cancelTOTP() {
    set({ totpChallenge: "", error: "" })
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