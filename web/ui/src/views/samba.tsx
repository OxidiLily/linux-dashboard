import { useEffect, useState } from "react"
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
  public?: boolean
  valid_users?: string[]
  /** Didefinisikan di smb.conf di luar panel — tampil, tapi tidak diedit dari sini. */
  external?: boolean
}

// Database smbpasswd terpisah dari akun Linux — user Linux yang baru dibuat
// belum bisa login ke share sampai didaftarkan di sini.
type SambaUser = {
  username: string
  enabled: boolean
}

const FORM_KOSONG = {
  name: "",
  path: "/home/%U/DATA/Documents",
  comment: "",
  writable: true,
  public: false,
  valid_users: "",
  smb_user: "",
  smb_pass: "",
}

export function SambaView() {
  const tr = useTr()
  const [shares, setShares] = useState<SambaShare[]>([])
  const [users, setUsers] = useState<SambaUser[]>([])
  const [loading, setLoading] = useState(false)
  const [showModal, setShowModal] = useState(false)
  // Nama share jadi kunci di smb.conf, jadi mode edit dikunci ke nama itu.
  const [editing, setEditing] = useState<string | null>(null)
  const [userModal, setUserModal] = useState<{ username: string; password: string; baru: boolean } | null>(null)
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
    setForm(FORM_KOSONG)
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
      public: s.public ?? false,
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
    // Guest OK mematikan autentikasi — backend menolak kombinasi keduanya.
    // Frontend otomatis menonaktifkan Guest OK kalau user mencentang user
    // tertentu, supaya tidak ada kemungkinan kombinasi keduanya sampai
    // backend (yang akan menolaknya dengan pesan kurang jelas).
    const adaUserDipilih = pilihanUser.length > 0 || tambahan.length > 0
    let effectivePublic = form.public
    if (adaUserDipilih && form.public) {
      const ok = await confirmDialog({
        title: tr("Guest OK akan dinonaktifkan"),
        message: tr("Share ini punya daftar user terbatas — Guest OK tidak bisa diaktifkan bersamaan (smbd mengabaikan valid users saat guest ok = yes). Lanjut tanpa Guest OK?"),
        confirmLabel: tr("Lanjut tanpa Guest OK"),
        danger: false,
      })
      if (!ok) return
      effectivePublic = false
    }
    const daftar = effectivePublic ? [] : [...pilihanUser, ...tambahan]
    if (daftar.length > 0) body.valid_users = daftar
    body.public = effectivePublic
    if (form.smb_user.trim() && form.smb_pass) {
      body.smb_user = form.smb_user.trim()
      body.smb_pass = form.smb_pass
    }
    const ok = await confirmDialog({
      title: editing ? trf("Simpan perubahan share \"{0}\"?", form.name) : trf("Simpan share \"{0}\"?", form.name),
      // effectivePublic, bukan form.public: kalau user baru saja memilih
      // "Lanjut tanpa Guest OK", share ini TIDAK lagi terbuka — memperingatkan
      // "siapa pun bisa mengakses tanpa password" di sini justru salah.
      message: effectivePublic
        ? tr("Share ini Guest OK — siapa pun di jaringan lokal bisa mengaksesnya tanpa password.")
        : tr("smb.conf ditulis ulang dan smbd dimuat ulang."),
      detail: form.path,
      confirmLabel: tr("Simpan"),
      danger: effectivePublic,
    })
    if (!ok) return
    // Setiap perubahan share diikuti smb.conf ditulis ulang lalu smbd dimuat
    // ulang; toast yang berputar selama itu yang membedakan "sedang jalan"
    // dari "tombolnya tidak berfungsi".
    try {
      await notify.tugas(apiSend("/api/samba/shares", editing ? "PUT" : "POST", body), {
        jalan: editing ? trf("Menyimpan share {0}…", form.name) : trf("Membuat share {0}…", form.name),
        sukses: editing ? trf("Share \"{0}\" diperbarui.", form.name) : trf("Share \"{0}\" dibuat.", form.name),
        gagal: (e) => trf("Gagal menyimpan share: {0}", pesanError(e)),
      })
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

  const handleDelete = async (name: string) => {
    const ok = await confirmDialog({
      title: trf("Hapus share Samba \"{0}\"?", name),
      message: tr("Definisi share dicabut dari smb.conf. Isi foldernya tidak dihapus."),
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
                  {trf(
                    "{0} share memakai user ini",
                    shares.filter((s) => s.valid_users?.includes(u.username)).length,
                  )}
                </p>
              </div>
              <Badge tone={u.enabled ? "ok" : "muted"}>{u.enabled ? tr("Aktif") : tr("Nonaktif")}</Badge>
            </div>
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
          </div>
        ))}
        {users.length === 0 && !loading && (
          <p className="py-6 text-center text-xs text-muted-foreground">
            {tr("Belum ada user Samba. Akun Linux (mis. yang dibuat di Settings → Akun) tidak otomatis bisa login share — daftarkan di sini dulu dengan password Samba-nya sendiri.")}
          </p>
        )}
      </div>
    </Panel>

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
                {tr("%U diganti nama user yang menyambung, jadi /home/%U/DATA/Documents memberi tiap akun folder datanya sendiri.")}
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
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                {tr("User yang boleh mengakses")}
              </label>
              {form.public ? (
                <p className="mt-1 rounded border border-warn/40 bg-warn/10 p-2 text-[11px] text-warn">
                  {tr("Guest OK aktif — share ini terbuka tanpa login, jadi daftar user diabaikan smbd. Matikan Guest OK dulu kalau ingin membatasi ke user tertentu.")}
                </p>
              ) : users.length === 0 ? (
                <p className="mt-1 text-[11px] text-muted-foreground">
                  {tr("Belum ada user Samba. Tambahkan dulu di panel User Samba — akun Linux saja tidak cukup.")}
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
                            setPilihanUser(
                              e.target.checked
                                ? [...pilihanUser, u.username]
                                : pilihanUser.filter((n) => n !== u.username),
                            )
                          }
                        />
                        <span className="num">{u.username}</span>
                        {!u.enabled && <Badge tone="muted">{tr("nonaktif")}</Badge>}
                      </label>
                    )
                  })}
                </div>
              )}
              {!form.public && (
                <Input
                  className="mt-2"
                  value={form.valid_users}
                  onChange={(e) => setForm({ ...form, valid_users: e.target.value })}
                  placeholder={tr("tambahan, mis. @grup")}
                />
              )}
              <p className="mt-1 text-[10px] text-muted-foreground">
                {tr("Kosong = semua user Samba yang terdaftar boleh login ke share ini.")}
              </p>
            </div>
            <div className="flex items-center gap-4 text-xs pt-1">
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.writable}
                  onChange={(e) => setForm({ ...form, writable: e.target.checked })}
                />
                <span>{tr("Writable (Read/Write)")}</span>
              </label>
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.public}
                  onChange={(e) => setForm({ ...form, public: e.target.checked })}
                />
                <span>{tr("Guest OK")}</span>
              </label>
            </div>
            {/* Peringatan sisi KLIEN, bukan sisi server. Sejak versi 1709
                Windows 10 mematikan "insecure guest logon" untuk SMB2/SMB3:
                share yang guest ok-nya benar dan sudah diuji jalan dari Linux
                atau Android tetap ditolak Windows dengan pesan yang
                menyesatkan ("You can't access this shared folder because your
                organization's security policies block unauthenticated guest
                access"). Tidak ada setelan smb.conf yang bisa membatalkannya
                — satu-satunya jalan adalah mengubah kebijakan di PC Windows,
                atau tidak memakai guest sama sekali. Ditulis di sini supaya
                yang mengaktifkan Guest OK tahu sebelum menyimpan, bukan
                setelah setengah jam menyalahkan servernya. */}
            {form.public && (
              <div className="rounded border border-warn/30 bg-warn/10 px-3 py-2 text-xs">
                <p className="font-semibold">{tr("Windows 10/11 memblokir akses guest secara bawaan")}</p>
                <p className="mt-1 text-muted-foreground">
                  {tr(
                    "Share ini akan bekerja dari Linux, macOS, dan Android, tapi Windows menolak login guest lewat SMB2/SMB3 sejak versi 1709 — bukan karena servernya salah. Dua pilihan: beri user Samba dan password lalu matikan Guest OK (dianjurkan), atau longgarkan kebijakan di PC Windows-nya.",
                  )}
                </p>
                <p className="mt-2 text-muted-foreground">
                  {tr("Di PC Windows, jalankan PowerShell sebagai Administrator:")}
                </p>
                <pre className="num mt-1 overflow-x-auto rounded bg-background p-2 text-[11px]">
{`Set-ItemProperty -Path HKLM:\\SYSTEM\\CurrentControlSet\\Services\\LanmanWorkstation\\Parameters \`
  -Name AllowInsecureGuestAuth -Type DWord -Value 1`}
                </pre>
              </div>
            )}
            <details className="rounded border border-border p-2">
              <summary className="cursor-pointer text-xs text-muted-foreground">
                {tr("Set password Samba untuk share ini (opsional)")}
              </summary>
              <div className="mt-2 space-y-2">
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Samba Username")}</label>
                  <Input
                    className="mt-1"
                    value={form.smb_user}
                    onChange={(e) => setForm({ ...form, smb_user: e.target.value })}
                  />
                </div>
                <div>
                  <label className="text-xs font-medium text-muted-foreground">{tr("Samba Password")}</label>
                  <Input
                    type="password"
                    className="mt-1"
                    value={form.smb_pass}
                    onChange={(e) => setForm({ ...form, smb_pass: e.target.value })}
                  />
                </div>
              </div>
            </details>
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
