import { useCallback, useEffect, useState } from "react"
import { QRCodeSVG } from "qrcode.react"
import { Plus, Server, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { apiGet, apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { confirmDialog } from "@/components/ui/confirm"
import { promptDialog } from "@/components/ui/prompt"
import { tr, trf } from "@/stores/i18n"
import { salinKeClipboard } from "@/lib/utils"

type WGPeer = {
  nama: string
  public_key: string
  ip: string
  /** Unix detik handshake terakhir; kosong berarti klien belum pernah masuk. */
  handshake?: string
  transfer?: string
}

type WGServerInfo = {
  ada: boolean
  server: boolean
  iface: string
  subnet?: string
  port?: number
  endpoint?: string
  peers: WGPeer[]
}

type PeerBaru = { peer: WGPeer; config: string }

const waktuHandshake = (detik?: string) => {
  if (!detik) return tr("belum pernah")
  const lalu = Math.max(0, Math.floor(Date.now() / 1000) - Number(detik))
  if (lalu < 60) return trf("{0} dtk lalu", lalu)
  if (lalu < 3600) return trf("{0} mnt lalu", Math.floor(lalu / 60))
  if (lalu < 86400) return trf("{0} jam lalu", Math.floor(lalu / 3600))
  return trf("{0} hari lalu", Math.floor(lalu / 86400))
}

/**
 * Panel mode server WireGuard: menyiapkan config server, lalu menambah dan
 * menghapus klien. Mode klien (tempel config jadi) tetap ditangani daftar VPN
 * di halaman Network — komponen ini hanya muncul untuk sisi server.
 */
export function WireGuardServer({ versi, onBerubah }: { versi: number; onBerubah: () => void }) {
  const [info, setInfo] = useState<WGServerInfo | null>(null)
  const [form, setForm] = useState({ subnet: "10.8.0.0/24", port: "51820", endpoint: "" })
  const [siapkan, setSiapkan] = useState(false)
  const [sibuk, setSibuk] = useState(false)
  const [baru, setBaru] = useState<PeerBaru | null>(null)

  const muat = useCallback(async () => {
    try {
      setInfo(await apiGet<WGServerInfo>("/api/settings/network/wireguard"))
    } catch (e: any) {
      notify.err(trf("Gagal membaca config WireGuard: {0}", pesanError(e)))
    }
  }, [])

  // versi berubah tiap halaman Network memuat ulang daftar VPN — termasuk
  // sesudah config dihapus, yang harus mengosongkan panel ini.
  useEffect(() => {
    muat()
    // Endpoint default: alamat yang dipakai user untuk membuka panel ini —
    // hampir selalu alamat yang sama yang dituju klien.
    setForm((f) => ({ ...f, endpoint: f.endpoint || location.hostname }))
  }, [muat, versi])

  const buatServer = async () => {
    const ok = await confirmDialog({
      title: tr("Siapkan server WireGuard?"),
      message: tr(
        "Panel membuat kunci server, menulis config, menyalakan IP forwarding, menambah aturan NAT, dan membuka port UDP di firewall kalau ufw aktif.",
      ),
      detail: `${form.subnet} · :${form.port} · ${form.endpoint}`,
      confirmLabel: tr("Siapkan"),
    })
    if (!ok) return
    setSibuk(true)
    try {
      await notify.tugas(
        apiSend("/api/settings/network/wireguard/server", "POST", {
          subnet: form.subnet.trim(),
          port: Number(form.port),
          endpoint: form.endpoint.trim(),
        }),
        {
          jalan: tr("Menyiapkan server WireGuard…"),
          sukses: tr("Server WireGuard siap."),
          gagal: (e) => trf("Gagal menyiapkan server: {0}", pesanError(e)),
        },
      )
      setSiapkan(false)
      await muat()
      onBerubah()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setSibuk(false)
    }
  }

  const tambahKlien = async () => {
    const nama = await promptDialog({
      title: tr("Tambah klien WireGuard"),
      label: tr("Nama klien"),
      placeholder: tr("mis. laptop-kerja"),
      confirmLabel: tr("Tambah"),
    })
    if (!nama) return
    setSibuk(true)
    try {
      const hasil = await notify.tugas(
        apiSend<PeerBaru>("/api/settings/network/wireguard/peers", "POST", { nama }),
        {
          jalan: trf("Menambah klien {0}…", nama),
          sukses: trf("Klien {0} ditambahkan.", nama),
          gagal: (e) => trf("Gagal menambah klien: {0}", pesanError(e)),
        },
      )
      setBaru(hasil)
      await muat()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    } finally {
      setSibuk(false)
    }
  }

  const hapusKlien = async (p: WGPeer) => {
    const ok = await confirmDialog({
      title: trf("Hapus klien {0}?", p.nama || p.ip),
      message: tr("Klien ini langsung kehilangan akses ke tunnel. Config yang ada di perangkatnya tidak akan bisa dipakai lagi."),
      detail: p.ip,
      confirmLabel: tr("Hapus"),
      danger: true,
    })
    if (!ok) return
    try {
      await notify.tugas(
        apiSend("/api/settings/network/wireguard/peers", "DELETE", {
          nama: p.nama,
          public_key: p.public_key,
        }),
        {
          jalan: trf("Menghapus klien {0}…", p.nama || p.ip),
          sukses: tr("Klien dihapus."),
          gagal: (e) => trf("Gagal menghapus klien: {0}", pesanError(e)),
        },
      )
      await muat()
      onBerubah()
    } catch {
      // Pesan gagalnya sudah ditampilkan notify.tugas.
    }
  }

  if (!info) return null

  // Config klien yang ditempel user sendiri tidak dikelola di sini.
  if (info.ada && !info.server) {
    return (
      <p className="mt-2 text-[10px] text-muted-foreground">
        {trf("Config klien terpasang di {0}. Mode server tidak aktif.", `/etc/wireguard/${info.iface}.conf`)}
      </p>
    )
  }

  if (!info.ada) {
    return (
      <div className="mt-3">
        {!siapkan ? (
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setSiapkan(true)}>
              <Server className="mr-1 size-3" /> {tr("Jadikan server")}
            </Button>
            <p className="text-[10px] text-muted-foreground">
              {tr("Atau tekan Sambung untuk memakai mesin ini sebagai klien dengan menempel config.")}
            </p>
          </div>
        ) : (
          <div className="space-y-2 rounded border border-border p-3">
            <div className="grid gap-2 sm:grid-cols-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Subnet tunnel")}</label>
                <Input
                  className="mt-1 font-mono text-[11px]"
                  value={form.subnet}
                  onChange={(e) => setForm({ ...form, subnet: e.target.value })}
                  placeholder="10.8.0.0/24"
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Port UDP")}</label>
                <Input
                  className="mt-1 font-mono text-[11px]"
                  value={form.port}
                  onChange={(e) => setForm({ ...form, port: e.target.value })}
                  placeholder="51820"
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">{tr("Endpoint untuk klien")}</label>
                <Input
                  className="mt-1 font-mono text-[11px]"
                  value={form.endpoint}
                  onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
                  placeholder="vpn.contoh.com"
                />
              </div>
            </div>
            <p className="text-[10px] text-muted-foreground">
              {tr("Endpoint adalah alamat yang dituju klien dari luar — IP publik atau hostname. Server di balik NAT tidak bisa menebaknya sendiri, dan port UDP-nya harus diteruskan router.")}
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setSiapkan(false)}>
                {tr("Batal")}
              </Button>
              <Button size="sm" disabled={sibuk} onClick={buatServer}>
                {tr("Siapkan")}
              </Button>
            </div>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="mt-3 space-y-2">
      <div className="flex items-center justify-between gap-2">
        <p className="num truncate text-[11px] text-muted-foreground">
          {`${info.subnet ?? "—"} · :${info.port ?? "—"} · ${info.endpoint || tr("endpoint belum tercatat")}`}
        </p>
        <Button variant="outline" size="sm" className="h-7 text-xs" disabled={sibuk} onClick={tambahKlien}>
          <Plus className="mr-1 size-3" /> {tr("Tambah klien")}
        </Button>
      </div>

      {info.peers.length === 0 ? (
        <p className="text-[10px] text-muted-foreground">{tr("Belum ada klien. Tambahkan satu untuk mendapat config + QR.")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="tabel-kartu w-full text-xs">
            <thead className="text-muted-foreground">
              <tr className="text-left">
                <th className="py-1 pr-2 font-medium">{tr("Klien")}</th>
                <th className="py-1 pr-2 font-medium">{tr("Alamat")}</th>
                <th className="py-1 pr-2 font-medium">{tr("Handshake")}</th>
                <th className="py-1" />
              </tr>
            </thead>
            <tbody>
              {info.peers.map((p) => (
                <tr key={p.public_key} className="border-t border-border">
                  <td data-label={tr("Klien")} className="truncate py-1 pr-2">{p.nama || "—"}</td>
                  <td data-label={tr("Alamat")} className="num py-1 pr-2">{p.ip}</td>
                  <td data-label={tr("Handshake")} className="py-1 pr-2 text-muted-foreground">{waktuHandshake(p.handshake)}</td>
                  <td data-label="" className="py-1 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-1.5 text-muted-foreground hover:text-crit"
                      aria-label={trf("Hapus klien {0}", p.nama || p.ip)}
                      onClick={() => hapusKlien(p)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {baru && <ModalConfigKlien data={baru} onClose={() => setBaru(null)} />}
    </div>
  )
}

/**
 * Config klien hanya ditampilkan SEKALI: private key-nya dibuat di server lalu
 * langsung dilupakan, jadi tidak ada tempat untuk menampilkannya lagi nanti.
 */
function ModalConfigKlien({ data, onClose }: { data: PeerBaru; onClose: () => void }) {
  const [tersalin, setTersalin] = useState(false)

  const salin = async () => {
    // navigator.clipboard tidak ada saat panel dibuka lewat http ke IP LAN;
    // salinKeClipboard menyediakan jalur textarea untuk keadaan itu.
    if (await salinKeClipboard(data.config)) setTersalin(true)
    else notify.err(tr("Browser menolak akses clipboard — salin manual dari kotak di atas."))
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true">
      <div className="flex max-h-[85dvh] w-full max-w-lg flex-col overflow-auto rounded-lg border border-border bg-surface p-4 shadow-xl">
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <p className="text-sm font-semibold">{trf("Config klien {0}", data.peer.nama)}</p>
            <p className="mt-1 text-xs text-warn">
              {tr("Simpan sekarang — private key-nya tidak disimpan di server, jadi config ini tidak bisa ditampilkan lagi.")}
            </p>
          </div>
          <button className="rounded-md p-1.5 text-muted hover:bg-accent hover:text-foreground" aria-label={tr("Tutup")} onClick={onClose}>
            <X className="size-4" />
          </button>
        </div>

        <pre className="num mt-3 overflow-auto whitespace-pre-wrap break-words rounded bg-surface-2 p-3 text-[11px] text-muted-foreground">
          {data.config}
        </pre>

        <div className="mt-3 flex flex-col items-center gap-2">
          <div className="rounded bg-white p-2">
            <QRCodeSVG value={data.config} size={196} />
          </div>
          <p className="text-[10px] text-muted-foreground">
            {tr("Scan dari app WireGuard di HP: Tambah tunnel → Buat dari QR code.")}
          </p>
        </div>

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={salin}>
            {tersalin ? tr("Tersalin") : tr("Salin config")}
          </Button>
          <Button size="sm" onClick={onClose}>
            {tr("Selesai")}
          </Button>
        </div>
      </div>
    </div>
  )
}
