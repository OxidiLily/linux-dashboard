import { useEffect, useState } from "react"
import { AlertTriangle, Trash2, X } from "lucide-react"
import { daftarkanEscape } from "@/lib/lapisan-escape"
import { Button } from "@/components/ui/button"
import { apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { promptDialog } from "@/components/ui/prompt"
import { Input } from "@/components/ui/input"
import { tr, trf } from "@/stores/i18n"

// Nama mode dikirim APA ADANYA ke helper sebagai nilai `mode`, jadi tidak
// boleh diterjemahkan — helper menolak mode yang tidak dikenal.
type Mode = "panel" | "panel-data" | "total" | "total-data" // i18n-abaikan: nama mode, bukan teks UI

// Kata yang harus diketik ulang untuk menyalakan mode terakhir. Bukan hiasan:
// di depan mode itu ada satu kotak pilihan yang bunyinya hampir sama dengan
// tiga lainnya, dan yang dipertaruhkan bukan konfigurasi panel melainkan
// dokumen di ~/DATA setiap akun.
const KONFIRMASI_DATA = "HAPUS DATA" // i18n-abaikan: kata yang harus diketik apa adanya

// konfirmasiDataSah menjawab apakah tombol Uninstall boleh menyala. Fungsi
// murni, dipisah dari komponennya supaya bisa diuji tanpa browser
// (scripts/cek-runtime.ts) — inilah satu-satunya penjaga antara satu klik dan
// hilangnya folder DATA di seluruh akun. Spasi tepi diabaikan dan
// kapitalisasinya tidak dipedulikan; yang tidak boleh longgar adalah
// kalimatnya sendiri harus lengkap.
export const konfirmasiDataSah = (mode: string, ketik: string) =>
  mode !== "total-data" || ketik.trim().toUpperCase() === KONFIRMASI_DATA

// Isi tiap mode ditulis apa adanya di sini: uninstall tidak punya undo, jadi
// user harus bisa membaca persis apa yang akan hilang sebelum menekan tombol.
// Kalimatnya menyebut jalur sesungguhnya yang disentuh skrip uninstaller —
// daftar yang lebih pendek dari kenyataan membuat user kehilangan berkas yang
// ia kira aman, dan daftar yang lebih panjang membuatnya tidak berani
// uninstall sama sekali.
const MODE: { id: Mode; judul: string; rincian: string[]; data: boolean }[] = [
  {
    id: "panel",
    judul: "Hapus panel saja",
    rincian: [
      "Service dihentikan, unit systemd, binary web & helper, dan konfigurasi PAM dihapus",
      "Sumber di /usr/local/src/go-react-linux-dashboard dihapus",
      "Perintah CLI uninstall-linuxpanel tetap ada supaya bisa pasang ulang tanpa unduh installer",
      "Database panel, kunci sesi, dan akun panel tetap ada",
    ],
    data: false,
  },
  {
    id: "panel-data",
    judul: "Hapus panel dan folder/file panel",
    rincian: [
      "Semua yang di atas",
      "Perintah CLI uninstall-linuxpanel ikut dihapus",
      "Database panel (akun, bookmark, threshold, log aktivitas), kunci sesi, dan berkas kerja pembaruan dihapus",
      "/etc/default/linux-dashboard dan /etc/linux-dashboard dihapus — termasuk sertifikat TLS dan setelan port",
      "Akun sistem linux-dashboard dihapus (hanya kalau memang akun sistem buatan installer)",
    ],
    data: false,
  },
  {
    id: "total",
    judul: "Hapus total (termasuk components)",
    rincian: [
      "Semua yang di atas",
      "SEMUA components yang bisa dipasang panel dicopot, termasuk Docker, Node.js, Tailscale, cloudflared, dan alat AI",
      "Token tunnel cloudflared dan data component (mis. password 9router) ikut dihapus",
      "Server DNS Technitium ikut dicopot — zona, blocklist, dan setelannya hilang; resolusi nama mesin dikembalikan ke systemd-resolved",
      "Semua container, volume, network, dan data Docker dibersihkan",
    ],
    data: false,
  },
  {
    id: "total-data", // i18n-abaikan: nama mode, bukan teks UI
    judul: "Hapus total + data akun (~/DATA) — tidak bisa dikembalikan",
    rincian: [
      "Semua yang di atas",
      "Folder DATA di SETIAP home akun dihapus — ~/DATA/AppData, Documents, Downloads, Gallery, Media",
      "Termasuk dokumen, foto, unduhan, kode, dan catatan pribadi yang ada di dalamnya",
      "Kerangka /etc/skel/DATA ikut dihapus supaya akun baru tidak dibuat lagi",
      "Akun Linux dan home directory-nya sendiri TIDAK dihapus — yang hilang hanya folder DATA di dalamnya",
    ],
    data: true,
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
  // Ketikan konfirmasi untuk mode yang menghapus data akun. Direset tiap
  // pilihan mode berubah: kalimat yang sudah diketik untuk mode lain tidak
  // boleh ikut menyalakan mode ini.
  const [ketik, setKetik] = useState("")

  const pilihan = MODE.find((m) => m.id === mode)!
  const konfirmasiSah = konfirmasiDataSah(mode, ketik)

  const pilih = (m: Mode) => {
    setMode(m)
    setKetik("")
  }

  const mulai = async () => {
    if (!konfirmasiSah) return
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


  // Escape menutup modal ini — kecuali saat uninstaller sedang berjalan,
  // sama seperti tombol X yang mati selama itu (lib/lapisan-escape.ts).
  useEffect(() => daftarkanEscape(() => { if (jalan) return false; onClose() }), [jalan, onClose])
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
                  onChange={() => pilih(m.id)}
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
              {pilihan.data
                ? tr(
                    "Mode ini menghapus folder DATA di setiap akun — dokumen, foto, dan kode di dalamnya hilang permanen. Akun Linux Anda sendiri tidak dihapus, dan home directory-nya juga tidak; hanya folder DATA di dalamnya. Konfigurasi Samba dan NFS tetap ditinggalkan apa adanya.",
                  )
                : tr(
                    "Berkas pribadi di ~/DATA setiap akun tidak ikut dihapus pada mode ini. Akun Linux Anda juga tidak — yang dihapus hanya akun sistem linux-dashboard milik service. Konfigurasi Samba dan NFS ditinggalkan apa adanya; pada mode hapus total, izin firewall milik komponen yang dicopot ikut dicabut.",
                  )}
            </p>
            {pilihan.data && (
              <div className="rounded border border-crit/40 bg-crit/5 p-2">
                <label className="text-[11px] font-medium text-crit">
                  {trf("Ketik {0} untuk menyalakan tombol Uninstall.", KONFIRMASI_DATA)}
                </label>
                <Input
                  className="num mt-1 h-8 text-xs"
                  value={ketik}
                  onChange={(e) => setKetik(e.target.value)}
                  placeholder={KONFIRMASI_DATA}
                  autoComplete="off"
                  aria-label={trf("Ketik {0}", KONFIRMASI_DATA)}
                />
                {ketik.trim() !== "" && !konfirmasiSah && (
                  <p className="mt-1 text-[11px] text-crit">
                    {trf("Tulisan belum sama — harus persis {0}.", KONFIRMASI_DATA)}
                  </p>
                )}
              </div>
            )}
          </div>
        )}

        {galat && <p className="mt-2 text-xs text-crit">{galat}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose} disabled={jalan}>
            {jalan ? tr("Tutup") : tr("Batal")}
          </Button>
          {!jalan && (
            <Button
              size="sm"
              className="bg-crit text-white hover:bg-crit/90"
              disabled={!konfirmasiSah}
              onClick={mulai}
            >
              <Trash2 className="mr-1 size-3.5" /> {tr("Uninstall")}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
