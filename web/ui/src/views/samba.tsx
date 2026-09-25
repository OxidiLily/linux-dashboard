import { useEffect, useState } from "react"
import { daftarkanEscape } from "@/lib/lapisan-escape"
import { useAuth } from "@/stores/auth"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Share2, Trash2, Plus, RefreshCw, Pencil, UserCog, KeyRound, Lock, Unlock } from "lucide-react"

// Backend helperproto.SambaShare: Name, Path, Writable, Public, Comment,
// ValidUsers []string, SmbUser, SmbPass. Public = guest_ok di smb.conf.
type SambaShare = {
  name: string
  path: string
  comment?: string
  writable?: boolean
  public?: boolean // legacy/external only; panel tidak membuat share anonim baru
  valid_users?: string[]
  /** Didefinisikan di smb.conf di luar panel — tampil, tapi tidak diedit dari sini. */
  external?: boolean
}

// Database smbpasswd terpisah dari akun Linux — user Linux yang baru dibuat
// belum bisa login ke share sampai didaftarkan di sini.
type SambaUser = {
  username: string
  enabled: boolean
  managed?: boolean
}

type SambaCredential = { username: string; password?: string }

const FORM_KOSONG = {
  name: "",
  path: "",
  comment: "",
  writable: true,
  public: false,
  valid_users: "",
  smb_user: "",
  smb_pass: "",
}

export function SambaView() {
  const tr = useTr()
  const home = useAuth((s) => s.user?.home) || "/home/user"
  const [shares, setShares] = useState<SambaShare[]>([])
  const [users, setUsers] = useState<SambaUser[]>([])
  const [loading, setLoading] = useState(false)
  const [showModal, setShowModal] = useState(false)
  // Nama share jadi kunci di smb.conf, jadi mode edit dikunci ke nama itu.
  const [editing, setEditing] = useState<string | null>(null)
  const [userModal, setUserModal] = useState<{ username: string; password: string; baru: boolean } | null>(null)
  const [credential, setCredential] = useState<SambaCredential | null>(null)
  // User Samba yang dicentang untuk share yang sedang diisi.
  const [pilihanUser, setPilihanUser] = useState<string[]>([])
  const [form, setForm] = useState<{
    name: string
    path: string
    comment: string
    writable: boolean
    public: boolean
    valid_users: string
    smb_user: string
    smb_pass: string
  }>(FORM_KOSONG)

  const load = async () => {
    setLoading(true)
    try {
      const [data, us] = await Promise.all([
        apiGet<SambaShare[]>("/api/samba/shares"),
        apiGet<SambaUser[]>("/api/samba/users"),
      ])
      setShares(data || [])
      setUsers(us || [])
    } catch (e: any) {
      notify.err(trf("Gagal memuat Samba: {0}", pesanError(e)))
    } finally {
      setLoading(false)
    }
  }

  const openTambah = () => {
    setEditing(null)
    setForm({ ...FORM_KOSONG, path: `${home}/DATA/Documents` })
    setPilihanUser([])
    setShowModal(true)
  }

  const openEdit = (s: SambaShare) => {
    setEditing(s.name)
    const terdaftar = users.map((u) => u.username)
    const semua = s.valid_users ?? []
    setPilihanUser(semua.filter((u) => terdaftar.includes(u)))
    setForm({
      name: s.name,
      path: s.path,
      comment: s.comment ?? "",
      writable: s.writable ?? false,
      public: false,
      // Entri yang bukan user Samba terdaftar (mis. @grup) tetap bisa diedit.
      valid_users: semua.filter((u) => !terdaftar.includes(u)).join(", "),
      smb_user: "",
      smb_pass: "",
    })
    setShowModal(true)
  }

  useEffect(() => {
    load()
  }, [])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    // Bangun body yang persis sama dengan helperproto.SambaShare — backend
    // akan menolak kalau valid_users dikirim sebagai string (Go []string).
    const body: Record<string, unknown> = {
      name: form.name,
      path: form.path,
      writable: form.writable,
      public: form.public,
    }
    if (form.comment.trim()) body.comment = form.comment.trim()
    const tambahan = form.valid_users
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
    const daftar = [...pilihanUser, ...tambahan]
    if (daftar.length > 0) body.valid_users = daftar
    // Guest OK tidak pernah dikirim. Backend juga menolaknya secara fail-closed
    // agar klien/API lama tidak bisa membuka share anonim.
    body.public = false
    if (form.smb_user.trim() && form.smb_pass) {
      body.smb_user = form.smb_user.trim()
      body.smb_pass = form.smb_pass
    }
    const ok = await confirmDialog({
      title: editing ? trf("Simpan perubahan share \"{0}\"?", form.name) : trf("Simpan share \"{0}\"?", form.name),
      message: tr("Share wajib memakai autentikasi Samba. Konfigurasi ditulis ulang dan smbd dimuat ulang."),
      detail: form.path,
      confirmLabel: tr("Simpan"),
      danger: false,
    })
    if (!ok) return
    // Setiap perubahan share diikuti smb.conf ditulis ulang lalu smbd dimuat
    // ulang; toast yang berputar selama itu yang membedakan "sedang jalan"
    // dari "tombolnya tidak berfungsi".
    try {
      const hasil = await notify.tugas(apiSend<SambaCredential>("/api/samba/shares", editing ? "PUT" : "POST", body), {
        jalan: editing ? trf("Menyimpan share {0}…", form.name) : trf("Membuat share {0}…", form.name),
        sukses: editing ? trf("Share \"{0}\" diperbarui.", form.name) : trf("Share \"{0}\" dibuat.", form.name),
        gagal: (e) => trf("Gagal menyimpan share: {0}", pesanError(e)),
      })
      if (hasil?.password) setCredential(hasil)
      setShowModal(false)
      setEditing(null)
      setForm(FORM_KOSONG)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  // ---- user Samba ----

  const simpanUser = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!userModal) return
    const um = userModal
    try {
      await notify.tugas(
        apiSend(
          um.baru ? "/api/samba/users" : `/api/samba/users/${encodeURIComponent(um.username)}`,
          um.baru ? "POST" : "PUT",
          { username: um.username, password: um.password },
        ),
        {
          jalan: um.baru
            ? trf("Menambah user Samba {0}…", um.username)
            : trf("Menyimpan password Samba {0}…", um.username),
          sukses: um.baru
            ? trf("User Samba \"{0}\" ditambahkan.", um.username)
            : tr("Password Samba diperbarui."),
          gagal: (e) => trf("Gagal menyimpan user Samba: {0}", pesanError(e)),
        },
      )
      setUserModal(null)
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const toggleUser = async (u: SambaUser) => {
    try {
      await notify.tugas(
        apiSend(`/api/samba/users/${encodeURIComponent(u.username)}`, "PUT", { disable: u.enabled }),
        {
          jalan: u.enabled
            ? trf("Mematikan user Samba {0}…", u.username)
            : trf("Mengaktifkan user Samba {0}…", u.username),
          sukses: u.enabled
            ? trf("User Samba {0} dimatikan.", u.username)
            : trf("User Samba {0} diaktifkan.", u.username),
          gagal: (e) => trf("Gagal mengubah status user: {0}", pesanError(e)),
        },
      )
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const hapusUser = async (username: string) => {
    const ok = await confirmDialog({
      title: trf("Hapus user Samba \"{0}\"?", username),
      message: tr("Akun Linux-nya tidak ikut terhapus — hanya kredensial Samba dan namanya di daftar valid users share."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend(`/api/samba/users/${encodeURIComponent(username)}`, "DELETE"), {
        jalan: trf("Menghapus user Samba {0}…", username),
        sukses: trf("User Samba {0} dihapus.", username),
        gagal: (e) => trf("Gagal menghapus user Samba: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  const rotatePassword = async (name: string) => {
    const ok = await confirmDialog({ title: trf("Putar password share \"{0}\"?", name), message: tr("Password lama langsung tidak berlaku. Password baru hanya ditampilkan sekali."), confirmLabel: tr("Putar Password"), danger: false })
    if (!ok) return
    try {
      const hasil = await notify.tugas(apiSend<SambaCredential>(`/api/samba/shares/${encodeURIComponent(name)}/rotate`, "POST", {}), {
        jalan: tr("Memutar password…"), sukses: tr("Password baru dibuat."), gagal: (e) => trf("Gagal memutar password: {0}", pesanError(e)),
      })
      setCredential(hasil)
    } catch { /* notify.tugas sudah menampilkan error */ }
  }

  const handleDelete = async (name: string) => {
    const ok = await confirmDialog({
      title: trf("Hapus share Samba \"{0}\"?", name),
      message: tr("Definisi share, akun system khusus, kredensial Samba, dan ACL milik akun itu akan dihapus. Isi folder serta owner/group aslinya tidak dihapus."),
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(apiSend(`/api/samba/shares/${encodeURIComponent(name)}`, "DELETE"), {
        jalan: trf("Menghapus share {0}…", name),
        sukses: trf("Share {0} dihapus.", name),
        gagal: (e) => trf("Gagal menghapus share: {0}", pesanError(e)),
      })
      load()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }


  // Escape menutup modal ini — lewat tumpukan lapisan bersama supaya hanya
  // lapisan teratas yang tertutup (lihat lib/lapisan-escape.ts).
  useEffect(() => {
    if (!userModal) return
    return daftarkanEscape(() => setUserModal(null))
  }, [userModal])

  // Escape menutup modal ini — lewat tumpukan lapisan bersama supaya hanya
  // lapisan teratas yang tertutup (lihat lib/lapisan-escape.ts).
  useEffect(() => {
    if (!showModal) return
    return daftarkanEscape(() => setShowModal(false))
  }, [showModal])
  return (
  <div className="space-y-4">
    <Panel
      title={tr("Samba Sharing")}
      hint={tr("Kelola folder sharing jaringan lokal (smbd)")}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={openTambah}>
            <Plus className="mr-1 size-3.5" /> {tr("Tambah Share")}
          </Button>
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`size-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        {shares.map((s) => (
          <div
            key={s.name}
            className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div className="flex items-center gap-3">
              <Share2 className="size-5 text-signal" />
              <div>
                <div className="flex items-center gap-2">
                  <p className="font-semibold text-sm">{s.name}</p>
                  {s.external && <Badge tone="warn">{tr("dari smb.conf")}</Badge>}
                  <Badge tone={s.writable ? "ok" : "muted"}>{s.writable ? "Read/Write" : "Read-Only"}</Badge>
                  {s.public && <Badge tone="warn">{tr("Guest OK")}</Badge>}
                  {!s.public &&
                    (s.valid_users && s.valid_users.length > 0 ? (
                      <Badge tone="signal">{s.valid_users.join(", ")}</Badge>
                    ) : (
                      <Badge tone="muted">{tr("semua user Samba")}</Badge>
                    ))}
                </div>
                <p className="num text-xs text-muted-foreground mt-0.5">{s.path}</p>
                {s.comment && <p className="text-xs text-muted-foreground">{s.comment}</p>}
              </div>
            </div>
            <div className="flex items-center gap-1">
              {/* Share milik smb.conf tidak diberi tombol ubah/hapus: panel
                  menulis ke file include terpisah, jadi "mengedit" hanya akan
                  membuat definisi kedua dengan nama sama dan smbd memakai
                  yang pertama — perubahan terlihat tersimpan padahal tidak. */}
              {s.external ? (
                <span className="px-2 text-[10px] text-muted-foreground">{tr("dikelola di smb.conf")}</span>
              ) : (
              <Button
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-muted-foreground hover:text-foreground"
                aria-label={trf("Edit share {0}", s.name)}
                onClick={() => openEdit(s)}
              >
                <Pencil className="size-4" />
              </Button>
              )}
              {!s.external && !s.path.includes("%U") && (
                <Button variant="ghost" size="sm" className="h-8 px-2 text-muted-foreground hover:text-foreground" aria-label={trf("Putar password {0}", s.name)} onClick={() => rotatePassword(s.name)}>
                  <KeyRound className="size-4" />
                </Button>
              )}
              {!s.external && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-crit"
                  aria-label={trf("Hapus share {0}", s.name)}
                  onClick={() => handleDelete(s.name)}
                >
                  <Trash2 className="size-4" />
                </Button>
              )}
            </div>
          </div>
        ))}
        {shares.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">{tr("Belum ada Samba share yang dikonfigurasi.")}</p>
        )}
      </div>
    </Panel>

    <Panel
      title={tr("User Samba")}
      hint={tr("Kredensial login share — terpisah dari password akun Linux")}
      actions={
        <Button size="sm" onClick={() => setUserModal({ username: "", password: "", baru: true })}>
          <Plus className="mr-1 size-3.5" /> {tr("Tambah User")}
        </Button>
      }
    >
      <div className="space-y-2">
        {users.map((u) => (
          <div
            key={u.username}
            className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3 hover:bg-secondary/40"
          >
            <div className="flex items-center gap-3">
              <UserCog className="size-5 text-signal" />
              <div>
                <p className="num text-sm font-semibold">{u.username}</p>
                <p className="text-xs text-muted-foreground">
                  {u.managed
                    ? tr("Dikelola otomatis oleh share")
                    : trf(
                        "{0} share memakai user ini",
                        shares.filter((s) => s.valid_users?.includes(u.username)).length,
                      )}
                </p>
              </div>
              <Badge tone={u.enabled ? "ok" : "muted"}>{u.enabled ? tr("Aktif") : tr("Nonaktif")}</Badge>
            </div>
            {!u.managed && (
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-foreground"
                  aria-label={trf("Ganti password Samba {0}", u.username)}
                  onClick={() => setUserModal({ username: u.username, password: "", baru: false })}
                >
                  <KeyRound className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-foreground"
                  aria-label={u.enabled ? trf("Nonaktifkan {0}", u.username) : trf("Aktifkan {0}", u.username)}
                  onClick={() => toggleUser(u)}
                >
                  {u.enabled ? <Lock className="size-4" /> : <Unlock className="size-4" />}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-foreground hover:text-crit"
                  aria-label={trf("Hapus user Samba {0}", u.username)}
                  onClick={() => hapusUser(u.username)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            )}
          </div>
        ))}
        {users.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada user Samba. Akun Linux (mis. yang dibuat di Settings → Akun) tidak otomatis bisa login share — daftarkan di sini dulu dengan password Samba-nya sendiri.")}
          </p>
        )}
      </div>
    </Panel>

    {credential && credential.password && (
      <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4">
        <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">{tr("Simpan Kredensial Samba")}</p>
          <p className="mt-1 text-xs text-warn">{tr("Password ini hanya ditampilkan sekali.")}</p>
          <label className="mt-3 block text-xs text-muted-foreground">{tr("Username")}</label>
          <div className="mt-1 flex gap-2"><Input readOnly value={credential.username} /><Button type="button" variant="outline" onClick={() => navigator.clipboard.writeText(credential.username)}>{tr("Salin")}</Button></div>
          <label className="mt-3 block text-xs text-muted-foreground">{tr("Password")}</label>
          <div className="mt-1 flex gap-2"><Input readOnly value={credential.password} /><Button type="button" variant="outline" onClick={() => navigator.clipboard.writeText(credential.password ?? "")}>{tr("Salin")}</Button></div>
          <div className="mt-4 flex justify-end"><Button type="button" onClick={() => setCredential(null)}>{tr("Sudah Disimpan")}</Button></div>
        </div>
      </div>
    )}

    {userModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-sm overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="text-sm font-semibold">
            {userModal.baru ? tr("Tambah User Samba") : trf("Ganti Password \"{0}\"", userModal.username)}
          </p>
          <form onSubmit={simpanUser} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Username Linux")}</label>
              <Input
                className="mt-1"
                required
                disabled={!userModal.baru}
                value={userModal.username}
                onChange={(e) => setUserModal({ ...userModal, username: e.target.value })}
                placeholder="asdf"
              />
              <p className="mt-1 text-[10px] text-muted-foreground">
                {tr("Harus akun Linux yang sudah ada — Samba memetakan login ke UID Unix.")}
              </p>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Password Samba")}</label>
              <Input
                type="password"
                className="mt-1"
                required
                value={userModal.password}
                onChange={(e) => setUserModal({ ...userModal, password: e.target.value })}
              />
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setUserModal(null)}>
                {tr("Batal")}
              </Button>
              <Button type="submit" size="sm">
                {tr("Simpan")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}

    {showModal && (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
        <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
          <p className="font-semibold text-sm">
            {editing ? trf("Edit Share \"{0}\"", editing) : tr("Tambah Samba Share")}
          </p>
          <form onSubmit={handleSave} className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Nama Share")}</label>
              <Input
                className="mt-1"
                required
                disabled={!!editing}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder={tr("mis. Media")}
              />
              {editing && (
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Nama share adalah kunci di smb.conf — hapus lalu buat baru kalau ingin ganti nama.")}
                </p>
              )}
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Path Folder")}</label>
              <Input
                className="mt-1"
                required
                value={form.path}
                onChange={(e) => setForm({ ...form, path: e.target.value })}
                placeholder="/home/%U/DATA/Documents"
              />
              <p className="mt-1 text-xs text-muted-foreground">
                {form.path.includes("%U")
                  ? tr("Mode legacy %U memakai akun Samba manual per user. Gunakan path direktori konkret agar panel membuat akun khusus share otomatis.")
                  : tr("Panel membuat satu akun system khusus untuk direktori ini dan menampilkan kredensialnya sekali.")}
              </p>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Komentar / Deskripsi")}</label>
              <Input
                className="mt-1"
                value={form.comment}
                onChange={(e) => setForm({ ...form, comment: e.target.value })}
              />
            </div>
            {form.path.includes("%U") && (
              <div>
                <label className="text-xs font-medium text-muted-foreground">
                  {tr("User yang boleh mengakses")}
                </label>
                {users.length === 0 ? (
                  <p className="mt-1 text-[11px] text-warn">
                    {tr("Mode %U membutuhkan user Samba manual; Guest OK tetap dinonaktifkan.")}
                  </p>
                ) : (
                  <div className="mt-1 space-y-1 rounded border border-border p-2">
                    {users.map((u) => {
                      const dipilih = pilihanUser.includes(u.username)
                      return (
                        <label key={u.username} className="flex cursor-pointer items-center gap-2 text-xs">
                          <input
                            type="checkbox"
                            checked={dipilih}
                            onChange={(e) =>
                              setPilihanUser(e.target.checked ? [...pilihanUser, u.username] : pilihanUser.filter((n) => n !== u.username))
                            }
                          />
                          <span className="num">{u.username}</span>
                          {!u.enabled && <Badge tone="muted">{tr("nonaktif")}</Badge>}
                        </label>
                      )
                    })}
                  </div>
                )}
                <Input
                  className="mt-2"
                  value={form.valid_users}
                  onChange={(e) => setForm({ ...form, valid_users: e.target.value })}
                  placeholder={tr("tambahan, mis. @grup")}
                />
              </div>
            )}
            <div className="flex items-center gap-4 text-xs pt-1">
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.writable}
                  onChange={(e) => setForm({ ...form, writable: e.target.checked })}
                />
                <span>{tr("Writable (Read/Write)")}</span>
              </label>
            </div>
            <div className="rounded border border-warn/30 bg-warn/10 px-3 py-2 text-xs">
              <p className="font-semibold">{tr("Guest OK dinonaktifkan")}</p>
              <p className="mt-1 text-muted-foreground">
                {tr("Akses SMB anonim memungkinkan malware atau ransomware dari satu perangkat LAN mengubah seluruh share tanpa kredensial. Gunakan user Samba dan password.")}
              </p>
            </div>
            {form.path.includes("%U") && (
              <details className="rounded border border-border p-2">
                <summary className="cursor-pointer text-xs text-muted-foreground">
                  {tr("Set password Samba untuk user legacy (opsional)")}
                </summary>
                <div className="mt-2 space-y-2">
                  <Input value={form.smb_user} onChange={(e) => setForm({ ...form, smb_user: e.target.value })} placeholder={tr("Samba Username")} />
                  <Input type="password" value={form.smb_pass} onChange={(e) => setForm({ ...form, smb_pass: e.target.value })} placeholder={tr("Samba Password")} />
                </div>
              </details>
            )}
            <div className="flex justify-end gap-2 pt-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setShowModal(false)
                  setEditing(null)
                }}
              >
                {tr("Batal")}
              </Button>
              <Button type="submit" size="sm">
                {editing ? tr("Simpan Perubahan") : tr("Simpan Share")}
              </Button>
            </div>
          </form>
        </div>
      </div>
    )}
  </div>
  )
}
