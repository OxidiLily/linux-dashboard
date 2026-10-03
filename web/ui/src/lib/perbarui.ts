import { apiSend } from "@/lib/api"
import { pesanError } from "@/lib/pesan-error"
import { notify } from "@/components/ui/toast"
import { tr, trf } from "@/stores/i18n"

/**
 * Menjalankan pembaruan SATU komponen (agent AI / 9router) lewat helper,
 * lengkap dengan toast berjalan/berhasil/gagal.
 *
 * Dipakai bersama oleh dropdown notifikasi topbar dan halaman Pembaruan —
 * satu jalur aksi supaya pesannya identik dan tidak ada dua logika update
 * yang menyebar di banyak view.
 */
export async function perbaruiKomponen(name: string): Promise<void> {
  try {
    await notify.tugas(
      apiSend<{ status?: string; updated?: boolean; message?: string }>(
        `/api/components/${name}/update`,
        "POST",
      ),
      {
        jalan: trf("Memeriksa pembaruan {0}…", name),
        sukses: (res) => {
          if (res && res.updated === false) {
            return tr("Sudah di versi yang terbaru")
          }
          return trf("{0} berhasil diperbarui.", name)
        },
        gagal: (e) => trf("Gagal memperbarui {0}: {1}", name, pesanError(e)),
      },
    )
  } catch {
    // Pesan gagalnya sudah ditampilkan notify.tugas.
  }
}
