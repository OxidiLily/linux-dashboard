import { useState } from "react"
import { AlertTriangle, HardDrive, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ApiError, apiSend } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { tr, trf } from "@/stores/i18n"
import { UnusedDisk } from "@/lib/types"

const FS = ["ext4", "xfs", "btrfs"] as const

/**
 * Menyiapkan disk mentah: format (opsional), tulis /etc/fstab lewat UUID, lalu
 * mount — semuanya di helper. Disk yang ternyata sudah berisi filesystem
 * ditolak backend dengan kode disk_has_filesystem; di situ user memilih sendiri
 * antara mount tanpa format (data aman) atau format ulang (data hilang).
 */
export function DiskPrepareModal({ disk, onClose }: { disk: UnusedDisk; onClose: (dipakai: boolean) => void }) {
  const [mountpoint, setMountpoint] = useState(`/mnt/${disk.name}`)
  const [fstype, setFstype] = useState<string>(FS[0])
  const [jalan, setJalan] = useState(false)
  const [galat, setGalat] = useState("")
  // Terisi kalau backend menolak karena disknya sudah punya filesystem.
  const [fsLama, setFsLama] = useState("")

  const kirim = async (format: boolean, timpa: boolean) => {
    setGalat("")
    setJalan(true)
    // mkfs pada disk besar berjalan menit-menitan. Toast-nya dipasang di
    // app-shell, jadi ia bertahan meski modal ini ditutup atau user pindah
    // halaman — dan berubah sendiri jadi berhasil/gagal di sana.
    //
    // disk_has_filesystem bukan kegagalan biasa: itu penolakan pengaman yang
    // dijawab modal ini dengan tawaran menimpa. Kalimatnya karena itu menunjuk
    // ke jendela penyiapan, bukan mengulang pesan yang sudah tampil di sana —
    // berguna justru buat user yang sudah pindah halaman dan tidak lagi
    // melihat modalnya.
    try {
      await notify.tugas(
        apiSend("/api/storage/disks/prepare", "POST", {
          path: disk.path,
          mountpoint: mountpoint.trim(),
          fstype,
          format,
          timpa,
        }),
        {
          jalan: format
            ? trf("Memformat {0} lalu memasangnya di {1}…", disk.path, mountpoint.trim())
            : trf("Memasang {0} di {1}…", disk.path, mountpoint.trim()),
          sukses: trf("{0} siap dipakai di {1}.", disk.path, mountpoint.trim()),
          gagal: (e) =>
            e instanceof ApiError && e.code === "disk_has_filesystem"
              ? trf("{0} sudah berisi filesystem — pilih Timpa di jendela penyiapan.", disk.path)
              : trf("Gagal menyiapkan {0}: {1}", disk.path, pesanError(e)),
        },
      )
      onClose(true)
    } catch (e) {
      if (e instanceof ApiError && e.code === "disk_has_filesystem") {
        setFsLama(e.params[1] ?? "?")
      } else {
        setGalat(pesanError(e))
      }
      setJalan(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="disk-title"
    >
      <div className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
        <div className="flex items-start gap-3">
          <HardDrive className="mt-0.5 size-5 shrink-0 text-muted" />
          <div className="min-w-0 flex-1">
            <p id="disk-title" className="num text-sm font-semibold">
              {disk.path}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {formatBytes(disk.size)}
              {disk.model ? ` · ${disk.model}` : ""} · {tr("belum dipakai")}
            </p>
          </div>
          <button
            className="rounded-md p-1.5 text-muted hover:bg-accent hover:text-foreground"
            aria-label={tr("Tutup")}
            onClick={() => onClose(false)}
            disabled={jalan}
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="mt-4 space-y-3">
          <div>
            <Label htmlFor="disk-mount">{tr("Mount point")}</Label>
            <Input
              id="disk-mount"
              className="num mt-1"
              value={mountpoint}
              spellCheck={false}
              onChange={(e) => setMountpoint(e.target.value)}
              disabled={jalan}
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              {tr("Folder dibuat kalau belum ada. Entri fstab ditulis memakai UUID disk dan opsi nofail, jadi server tetap bisa boot walau disknya dilepas.")}
            </p>
          </div>

          <div>
            <Label htmlFor="disk-fs">{tr("Filesystem")}</Label>
            <select
              id="disk-fs"
              className="mt-1 flex h-9 w-full rounded-md border border-border bg-input px-3 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              value={fstype}
              onChange={(e) => setFstype(e.target.value)}
              disabled={jalan || fsLama !== ""}
            >
              {FS.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          </div>

          {fsLama ? (
            <div className="rounded border border-warn/40 bg-warn/10 p-3 text-xs text-warn">
              <p className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 size-4 shrink-0" />
                <span>
                  {trf("Disk ini sudah berisi filesystem {0}. Memformatnya menghapus seluruh isinya.", fsLama)}
                </span>
              </p>
            </div>
          ) : (
            <p className="rounded border border-border bg-surface-2 p-3 text-[11px] text-muted-foreground">
              {tr("Disk akan diformat — pastikan tidak ada data yang masih dibutuhkan di sana. Kalau ternyata sudah berisi filesystem, panel berhenti dan menanyakannya dulu.")}
            </p>
          )}
        </div>

        {galat && <p className="mt-2 text-xs text-crit">{galat}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={() => onClose(false)} disabled={jalan}>
            {tr("Batal")}
          </Button>
          {fsLama ? (
            <>
              <Button size="sm" onClick={() => kirim(false, false)} disabled={jalan}>
                {tr("Mount saja")}
              </Button>
              <Button variant="destructive" size="sm" onClick={() => kirim(true, true)} disabled={jalan}>
                {tr("Format ulang")}
              </Button>
            </>
          ) : (
            <Button size="sm" onClick={() => kirim(true, false)} disabled={jalan}>
              {jalan ? tr("Menyiapkan…") : tr("Format & mount")}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
