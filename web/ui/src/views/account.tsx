import { useEffect, useState } from "react"
import { pesanError } from "@/lib/pesan-error"
import { apiGet, apiSend } from "@/lib/api"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { promptDialog } from "@/components/ui/prompt"
import { useAuth } from "@/stores/auth"
import { Panel } from "@/components/ui/panel"
import { trf, useTr } from "@/stores/i18n"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { UserCheck, Shield, Trash2, Key, UserPlus, Pencil, Lock, Unlock } from "lucide-react"

type LinuxUser = {
  username: string
  uid: number
  gid: number
  home: string
  shell: string
  groups: string[]
  locked: boolean
}

// Grup yang memberi akses setara root. Selalu ditampilkan dan tidak pernah
// ikut terpotong: `sudo` sudah jelas, dan siapa pun di `docker` bisa
// menjalankan container yang mem-bind mount `/` host.
const GRUP_ISTIMEWA = ["sudo", "docker"]
const MAKS_GRUP_BIASA = 3

/**
 * badgeGrup memisahkan grup jadi tiga bagian supaya kolom "Grup / Status"
 * tidak pernah menyembunyikan sesuatu tanpa memberi tahu.
 *
 * Sebelumnya kolom itu menampilkan `groups.slice(0, 3)` begitu saja. Akun
 * dengan delapan grup hanya memperlihatkan tiga yang pertama, tanpa satu pun
 * tanda ada yang dipotong — jadi grup yang justru paling ingin dipastikan
 * (`docker`, yang urutannya di belakang) tidak terlihat, dan layar ini
 * terbaca sebagai "user tidak ada di grup itu" padahal sebenarnya ada.
 */
function badgeGrup(groups: string[] = []) {
  const istimewa = GRUP_ISTIMEWA.filter((g) => groups.includes(g))
  const sisa = groups.filter((g) => !GRUP_ISTIMEWA.includes(g))
  return {
    istimewa,
    tampil: sisa.slice(0, MAKS_GRUP_BIASA),
    tersembunyi: sisa.slice(MAKS_GRUP_BIASA),
  }
}

export function AccountView() {
  const tr = useTr()
  const currentUser = useAuth((s) => s.user)
  const [oldPass, setOldPass] = useState("")
  const [newPass, setNewPass] = useState("")
  const [hostname, setHostname] = useState("")
  const [users, setUsers] = useState<LinuxUser[]>([])
  const [showAddUser, setShowAddUser] = useState(false)
  const [newUserForm, setNewUserForm] = useState({ username: "", password: "", shell: "/bin/bash", sudo: false })
  // Modal Edit User — backend /api/settings/account/users/{name} PUT
  // menerima UserModifyArgs (shell, groups, lock).
  const [editTarget, setEditTarget] = useState<LinuxUser | null>(null)
  const [editForm, setEditForm] = useState<{ shell: string; groups: string; locked: boolean }>({
    shell: "/bin/bash",
    groups: "",
    locked: false,
  })

  const loadUsers = async () => {
    if (!currentUser?.sudo) return
    try {
      const data = await apiGet<LinuxUser[]>("/api/settings/account/users")
      setUsers(data || [])
    } catch {}
  }

  useEffect(() => {
    loadUsers()
  }, [])

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await apiSend("/api/settings/account/password", "PUT", {
        old_password: oldPass,
        new_password: newPass,
      })
      notify.ok(tr("Password berhasil diubah."))
      setOldPass("")
      setNewPass("")
    } catch (e: any) {
      notify.err(trf("Gagal mengubah password: {0}", pesanError(e)))
    }
  }

  const handleSetHostname = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!hostname) return
    const ok = await confirmDialog({
      title: trf("Ubah hostname server jadi \"{0}\"?", hostname),
      message: tr("Berlaku langsung dan menulis ulang /etc/hosts. Sesi SSH yang memakai nama lama bisa perlu dihubungkan ulang."),
      confirmLabel: tr("Ubah"),
    })
    if (!ok) return
    try {
      await apiSend("/api/settings/account/hostname", "PUT", { hostname })
      notify.ok(tr("Hostname berhasil diubah."))
      setHostname("")
    } catch (e: any) {
      notify.err(trf("Gagal mengubah hostname: {0}", pesanError(e)))
    }
  }

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await apiSend("/api/settings/account/users", "POST", {
        username: newUserForm.username,
        password: newUserForm.password,
        shell: newUserForm.shell,
        groups: newUserForm.sudo ? ["sudo"] : [],
      })
      notify.ok(trf("User {0} berhasil dibuat.", newUserForm.username))
      setShowAddUser(false)
      setNewUserForm({ username: "", password: "", shell: "/bin/bash", sudo: false })
      loadUsers()
    } catch (e: any) {
      notify.err(trf("Gagal membuat user: {0}", pesanError(e)))
    }
  }

  const handleDeleteUser = async (u: LinuxUser) => {
    const ok = await confirmDialog({
      title: trf("Hapus akun user \"{0}\"?", u.username),
      message: tr("Akun dicabut dari sistem dan tidak bisa login lagi. Tindakan ini tidak bisa dibatalkan."),
      detail: u.home,
      confirmLabel: tr("Hapus akun"),
      danger: true,
    })
    if (!ok) return
    const removeHome = await confirmDialog({
      title: tr("Hapus juga folder home miliknya?"),
      message: tr("Pilih Batal untuk menyimpan isi home directory-nya."),
      detail: u.home,
      confirmLabel: tr("Hapus home juga"),
      danger: true,
    })
    try {
      await apiSend(`/api/settings/account/users/${encodeURIComponent(u.username)}?remove_home=${removeHome}`, "DELETE")
      loadUsers()
    } catch (e: any) {
      notify.err(trf("Gagal menghapus user: {0}", pesanError(e)))
    }
  }

  const handleResetUserPassword = async (u: LinuxUser) => {
    const p = await promptDialog({
      title: trf("Reset password {0}", u.username),
      label: tr("Password baru"),
      password: true,
      confirmLabel: tr("Reset"),
    })
    if (!p) return
    try {
      await apiSend(`/api/settings/account/users/${encodeURIComponent(u.username)}/password`, "PUT", { new_password: p })
      notify.ok(tr("Password user berhasil direset."))
    } catch (e: any) {
      notify.err(trf("Gagal reset password: {0}", pesanError(e)))
    }
  }

  const openEdit = (u: LinuxUser) => {
    setEditTarget(u)
    setEditForm({
      shell: u.shell,
      groups: u.groups.join(", "),
      locked: u.locked,
    })
  }

  const handleSaveEdit = async () => {
    if (!editTarget) return
    const ok = await confirmDialog({
      title: trf("Simpan perubahan user {0}?", editTarget.username),
      message: editForm.locked
        ? tr("Akun akan dikunci — user ini tidak bisa login sampai dibuka kembali.")
        : tr("Shell dan keanggotaan grup diganti sesuai isian. Grup yang tidak dicantumkan akan dicabut."),
      confirmLabel: tr("Simpan"),
      danger: editForm.locked,
    })
    if (!ok) return
    const groups = editForm.groups
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
    try {
      await apiSend(
        `/api/settings/account/users/${encodeURIComponent(editTarget.username)}`,
        "PUT",
        {
          shell: editForm.shell,
          groups,
          lock: editForm.locked,
        },
      )
      setEditTarget(null)
      loadUsers()
    } catch (e: any) {
      notify.err(trf("Gagal mengubah user: {0}", pesanError(e)))
    }
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        {/* Ganti Password */}
        <Panel title={tr("Ganti Password Sendiri")}>
          <form onSubmit={handleChangePassword} className="space-y-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Password Lama")}</label>
              <Input
                type="password"
                className="mt-1"
                required
                value={oldPass}
                onChange={(e) => setOldPass(e.target.value)}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">{tr("Password Baru (min 8 karakter)")}</label>
              <Input
                type="password"
                className="mt-1"
                required
                value={newPass}
                onChange={(e) => setNewPass(e.target.value)}
              />
            </div>
            <Button type="submit" size="sm" className="w-full">
              {tr("Ubah Password")}
            </Button>
          </form>
        </Panel>

        {/* Set Hostname */}
        {currentUser?.sudo && (
          <Panel title={tr("Ubah Hostname Server")}>
            <form onSubmit={handleSetHostname} className="space-y-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Hostname Baru")}</label>
                <Input
                  className="mt-1"
                  required
                  placeholder={tr("mis. homelab-core")}
                  value={hostname}
                  onChange={(e) => setHostname(e.target.value)}
                />
              </div>
              <Button type="submit" size="sm" className="w-full">
                {tr("Simpan Hostname")}
              </Button>
            </form>
          </Panel>
        )}
      </div>

      {/* Manajemen User Linux (sudo only) */}
      {currentUser?.sudo && (
        <Panel
          title={tr("Manajemen Akun User Linux")}
          hint={trf("{0} user terdaftar", users.length)}
          actions={
            <Button size="sm" onClick={() => setShowAddUser(true)}>
              <UserPlus className="mr-1 size-3.5" /> {tr("Tambah User")}
            </Button>
          }
        >
          <div className="overflow-x-auto">
            <table className="tabel-kartu w-full text-left text-xs">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="pb-2 font-medium">{tr("User / UID")}</th>
                  <th className="pb-2 font-medium">{tr("Home")}</th>
                  <th className="pb-2 font-medium">{tr("Shell")}</th>
                  <th className="pb-2 font-medium">{tr("Grup / Status")}</th>
                  <th className="pb-2 text-right font-medium">{tr("Aksi")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {users.map((u) => (
                  <tr key={u.username} className="hover:bg-secondary/40">
                    <td data-label="" className="py-2.5">
                      <div className="flex items-center gap-2">
                        {u.groups?.includes("sudo") ? <Shield className="size-4 text-signal" /> : <UserCheck className="size-4 text-muted-foreground" />}
                        <div>
                          <p className="font-semibold">{u.username}</p>
                          <p className="num text-[10px] text-muted-foreground">UID: {u.uid}</p>
                        </div>
                      </div>
                    </td>
                    <td data-label={tr("Home")} className="num py-2.5 text-muted-foreground">{u.home}</td>
                    <td data-label={tr("Shell")} className="num py-2.5 text-muted-foreground">{u.shell}</td>
                    <td data-label={tr("Grup / Status")} className="py-2.5">
                      <div className="flex flex-wrap gap-1">
                        {(() => {
                          const g = badgeGrup(u.groups)
                          return (
                            <>
                              {g.istimewa.map((n) => (
                                <Badge key={n} tone="ok">{n}</Badge>
                              ))}
                              {u.locked && <Badge tone="crit">locked</Badge>}
                              {g.tampil.map((n) => (
                                <Badge key={n} tone="muted">{n}</Badge>
                              ))}
                              {g.tersembunyi.length > 0 && (
                                <Badge tone="muted" title={g.tersembunyi.join(", ")}>
                                  +{g.tersembunyi.length}
                                </Badge>
                              )}
                            </>
                          )
                        })()}
                      </div>
                    </td>
                    <td data-label="" className="py-2.5 text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-1.5 text-muted-foreground hover:text-foreground"
                          onClick={() => openEdit(u)}
                          title={tr("Edit User")}
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-1.5 text-muted-foreground hover:text-foreground"
                          onClick={() => handleResetUserPassword(u)}
                          title={tr("Reset Password")}
                        >
                          <Key className="size-3.5" />
                        </Button>
                        {u.username !== currentUser?.username && u.uid !== 0 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-1.5 text-muted-foreground hover:text-crit"
                            onClick={() => handleDeleteUser(u)}
                            title={tr("Hapus User")}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}

      {/* Modal Tambah User */}
      {showAddUser && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">{tr("Tambah User Linux Baru")}</p>
            <form onSubmit={handleCreateUser} className="mt-3 space-y-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Username")}</label>
                <Input
                  className="mt-1"
                  required
                  value={newUserForm.username}
                  onChange={(e) => setNewUserForm({ ...newUserForm, username: e.target.value })}
                  placeholder={tr("mis. devuser")}
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Password Awal")}</label>
                <Input
                  type="password"
                  className="mt-1"
                  required
                  value={newUserForm.password}
                  onChange={(e) => setNewUserForm({ ...newUserForm, password: e.target.value })}
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Login Shell")}</label>
                <Input
                  className="mt-1"
                  value={newUserForm.shell}
                  onChange={(e) => setNewUserForm({ ...newUserForm, shell: e.target.value })}
                  placeholder="/bin/bash"
                />
              </div>
              <label className="flex items-center gap-2 cursor-pointer pt-1 text-xs">
                <input
                  type="checkbox"
                  checked={newUserForm.sudo}
                  onChange={(e) => setNewUserForm({ ...newUserForm, sudo: e.target.checked })}
                />
                <span>{tr("Beri Akses Sudo (Grup sudo)")}</span>
              </label>
              <div className="flex justify-end gap-2 pt-3">
                <Button type="button" variant="outline" size="sm" onClick={() => setShowAddUser(false)}>
                  {tr("Batal")}
                </Button>
                <Button type="submit" size="sm">
                  {tr("Buat User")}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Modal Edit User — shell, grup, lock/unlock. UID/GID sengaja
          tidak dapat diubah dari UI (standar Unix: UID adalah identitas
          permanen yang menentukan ownership file). */}
      {editTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
            <p className="font-semibold text-sm">{trf("Edit User: {0}", editTarget.username)}</p>
            <p className="text-xs text-muted-foreground mt-1">{trf("UID {0} — tidak dapat diubah.", editTarget.uid)}</p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Login Shell")}</label>
                <Input
                  className="mt-1"
                  value={editForm.shell}
                  onChange={(e) => setEditForm({ ...editForm, shell: e.target.value })}
                  placeholder="/bin/bash"
                />
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Pilih dari shell yang terpasang di /etc/shells. Service account biasanya pakai /usr/sbin/nologin.")}
                </p>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">
                  {tr("Grup (pisahkan dengan koma)")}
                </label>
                <Input
                  className="mt-1"
                  value={editForm.groups}
                  onChange={(e) => setEditForm({ ...editForm, groups: e.target.value })}
                  placeholder={tr("sudo, docker, www-data")}
                />
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {tr("Grup menentukan akses (mis. sudo = admin, docker = akses socket docker).")}
                </p>
              </div>
              <label className="flex items-center gap-2 cursor-pointer pt-1 text-xs">
                <input
                  type="checkbox"
                  checked={editForm.locked}
                  onChange={(e) => setEditForm({ ...editForm, locked: e.target.checked })}
                />
                {editForm.locked ? (
                  <span className="flex items-center gap-1.5 text-crit">
                    <Lock className="size-3.5" /> {tr("Akun terkunci (tidak bisa login)")}
                  </span>
                ) : (
                  <span className="flex items-center gap-1.5">
                    <Unlock className="size-3.5" /> {tr("Akun aktif (bisa login)")}
                  </span>
                )}
              </label>
              <div className="flex justify-end gap-2 pt-3">
                <Button type="button" variant="outline" size="sm" onClick={() => setEditTarget(null)}>
                  {tr("Batal")}
                </Button>
                <Button size="sm" onClick={handleSaveEdit}>
                  {tr("Simpan Perubahan")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
