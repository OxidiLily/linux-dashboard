import { useState } from "react"
import { AlertTriangle, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { promptDialog } from "@/components/ui/prompt"
import { tr, trf } from "@/stores/i18n"

type Mode = "panel" | "panel-data" | "total"

// Isi tiap mode ditulis apa adanya di sini: uninstall tidak punya undo, jadi
// user harus bisa membaca persis apa yang akan hilang sebelum menekan tombol.
const MODE: { id: Mode; judul: string; rincian: string[] }[] = [
  {
    id: "panel",
    judul: "Hapus panel saja",
    rincian: [
      "Service dihentikan, unit systemd & binary dihapus",
      "Konfigurasi PAM dan sumber di /usr/local/src dihapus",
      "Database panel, akun panel, dan bookmark tetap ada",
    ],
  },
  {
    id: "panel-data",
    judul: "Hapus panel dan folder/file panel",
    rincian: [
      "Semua yang di atas",
      "Database panel, kunci sesi, dan berkas kerja pembaruan dihapus",
      "Akun sistem linux-dashboard dihapus",
    ],
  },
  {
    id: "total",
    judul: "Hapus total (termasuk components)",
    rincian: [
      "Semua yang di atas",
      "SEMUA components yang bisa dipasang panel dicopot, termasuk Docker, Node.js, Tailscale, cloudflared, dan alat AI",
      "Token tunnel cloudflared dan data component (mis. password 9router) ikut dihapus",
      "Image & volume Docker di /var/lib/docker TIDAK dihapus — isinya milik container Anda",
    ],
  },
]

/**
 * Modal uninstall: pilih cakupan, lalu konfirmasi dengan password akun.
 * Password diverifikasi helper lewat PAM sebelum satu langkah pun dijalankan.
 */
export function UninstallModal({ username, onClose }: { username?: string; onClose: () => void }) {
  const [mode, setMode] = useState<Mode>("panel")
  const [jalan, setJalan] = useState(false)
  const [galat, setGalat] = useState("")

  const mulai = async () => {
    const pilihan = MODE.find((m) => m.id === mode)!
    const sandi = await promptDialog({
      title: tr("Konfirmasi uninstall"),
      label: username === "root" ? tr("Password root") : trf("Password akun {0}", username ?? ""),
      detail: tr(pilihan.judul),
      confirmLabel: tr("OK"),
      password: true,
    })
    if (!sandi) return
    setGalat("")
    setJalan(true)
    try {
      await apiSend("/api/settings/uninstall", "POST", { mode, password: sandi })
    } catch (e: any) {
      setJalan(false)
      setGalat(pesanError(e))
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="uninstall-title"
    >
      <div className="flex max-h-[85dvh] w-full max-w-lg flex-col overflow-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
        <div className="flex items-start gap-3">
          <AlertTriangle className="mt-0.5 size-5 shrink-0 text-crit" />
          <div className="min-w-0 flex-1">
            <p id="uninstall-title" className="text-sm font-semibold">
              {tr("Uninstall panel")}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {tr("Pilih sejauh mana yang dihapus. Tidak ada langkah yang bisa dibatalkan setelah dimulai.")}
            </p>
          </div>
          <button
            className="rounded-md p-1.5 text-muted hover:bg-accent hover:text-foreground"
            aria-label={tr("Tutup")}
            onClick={onClose}
            disabled={jalan}
          >
            <X className="size-4" />
          </button>
        </div>

        {jalan ? (
          <div className="mt-4 rounded border border-border bg-surface-2 p-3">
            <p className="text-sm">{tr("Uninstall berjalan.")}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {tr("Panel berhenti sebentar lagi dan halaman ini akan mati sendiri. Jalannya proses tercatat di /var/log/linux-dashboard-uninstall.log.")}
            </p>
          </div>
        ) : (
          <div className="mt-3 space-y-2">
            {MODE.map((m) => (
              <label
                key={m.id}
                className={`flex cursor-pointer gap-2.5 rounded border p-3 ${
                  mode === m.id ? "border-crit/50 bg-crit/5" : "border-border"
                }`}
              >
                <input
                  type="checkbox"
                  className="mt-0.5 size-4 shrink-0 accent-current text-crit"
                  checked={mode === m.id}
                  onChange={() => setMode(m.id)}
                />
                <span className="min-w-0">
                  <span className="block text-sm">{tr(m.judul)}</span>
                  <ul className="mt-1 space-y-0.5 text-[11px] text-muted-foreground">
                    {m.rincian.map((r) => (
                      <li key={r}>• {tr(r)}</li>
                    ))}
                  </ul>
                </span>
              </label>
            ))}
            <p className="rounded border border-warn/40 bg-warn/10 p-2 text-[11px] text-warn">
              {tr("Berkas pribadi di ~/DATA setiap akun tidak pernah ikut dihapus, mode mana pun. Akun Linux Anda juga tidak — yang dihapus hanya akun sistem linux-dashboard milik service. Konfigurasi Samba, NFS, dan WireGuard ditinggalkan apa adanya; pada mode hapus total, izin firewall milik komponen yang dicopot ikut dicabut.")}
            </p>
          </div>
        )}

        {galat && <p className="mt-2 text-xs text-crit">{galat}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose} disabled={jalan}>
            {jalan ? tr("Tutup") : tr("Batal")}
          </Button>
          {!jalan && (
            <Button size="sm" className="bg-crit text-white hover:bg-crit/90" onClick={mulai}>
              <Trash2 className="mr-1 size-3.5" /> {tr("Uninstall")}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
