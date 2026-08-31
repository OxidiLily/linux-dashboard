import type { Terminal } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"

// Auto-fit terminal: jaga jumlah kolom/baris xterm selalu sesuai ruang yang
// benar-benar tersedia.
//
// Sebelumnya kedua halaman terminal hanya mendengarkan `window.resize`, dan
// tingginya ditulis sebagai angka tetap (calc(100vh - 14rem)). Dua-duanya
// meleset di pemakaian normal:
//
//  1. Menutup/membuka sidebar mengubah lebar terminal 285 px TANPA mengubah
//     ukuran window — tidak ada satu pun event yang menyala, jadi xterm tetap
//     memakai jumlah kolom lama dan keluaran terpotong di kanan.
//  2. Geist Mono baru selesai dimuat setelah cat pertama. Lebar sel yang
//     dipakai saat fit pertama adalah lebar font fallback, jadi hitungan
//     kolomnya salah sejak awal.
//  3. Tinggi tetap tidak tahu ada banner "password wajib diganti" di atasnya;
//     saat banner tampil, baris terbawah terdorong ke luar area.
//
// ResizeObserver menangani (1) — ia menyala untuk perubahan ukuran dari sumber
// mana pun. document.fonts.ready menangani (2). Tinggi dihitung dari posisi
// elemen terhadap tepi bawah viewport, bukan ditebak, untuk (3).

// Sisa bawah = padding milik <main> (p-3) supaya kotak terminal berhenti
// sejajar dengan panel halaman lain, bukan menempel ke tepi jendela.
const SISA_BAWAH = 12
const TINGGI_MIN = 240

interface OpsiAutoFit {
  container: HTMLElement
  term: Terminal
  fit: FitAddon
  /** Dipanggil setiap ukuran berubah — pengirim frame resize ke PTY. */
  onResize: (cols: number, rows: number) => void
}

/**
 * pasangAutoFit memasang pengamat ukuran dan langsung melakukan fit pertama.
 * Mengembalikan fungsi pembersih untuk dipanggil di cleanup useEffect.
 */
export function pasangAutoFit({ container, term, fit, onResize }: OpsiAutoFit): () => void {
  let hidup = true
  let rafId = 0
  let tinggiTerakhir = -1

  // Tinggi disimpan sebagai nilai terakhir yang DITULIS, bukan dibaca ulang
  // dari clientHeight: pembacaan itu sudah dikurangi border/padding, jadi
  // membandingkannya dengan nilai hitungan akan selalu berbeda dan menulis
  // ulang tanpa henti.
  const terapkanTinggi = () => {
    const atas = container.getBoundingClientRect().top
    // Keyboard virtual HP tidak mengubah window.innerHeight — yang menyusut
    // adalah visual viewport. Tanpa ini kotak terminal tetap dihitung setinggi
    // layar penuh, jadi baris tempat user mengetik justru berada di balik
    // keyboard. offsetTop ikut dipakai karena browser menggeser visual
    // viewport ke bawah saat input difokus.
    const vv = window.visualViewport
    const bawah = vv ? vv.offsetTop + vv.height : window.innerHeight
    const tinggi = Math.max(TINGGI_MIN, Math.round(bawah - atas - SISA_BAWAH))
    if (tinggi !== tinggiTerakhir) {
      tinggiTerakhir = tinggi
      container.style.height = `${tinggi}px`
    }
  }

  const sesuaikan = () => {
    if (!hidup) return
    terapkanTinggi()
    try {
      fit.fit()
    } catch {
      // fit() melempar kalau terminal sudah di-dispose atau elemennya belum
      // punya ukuran (mis. tab tersembunyi). Tidak ada yang perlu dilakukan:
      // pengamat akan memanggil lagi begitu ukurannya nyata.
      return
    }
    onResize(term.cols, term.rows)
  }

  // Perubahan ukuran dijadwalkan ke frame berikutnya. Memanggil fit() langsung
  // di dalam callback ResizeObserver mengubah tata letak di tengah siklus
  // pengamatan, dan browser membalasnya dengan "ResizeObserver loop completed
  // with undelivered notifications".
  const jadwalkan = () => {
    if (rafId) return
    rafId = requestAnimationFrame(() => {
      rafId = 0
      sesuaikan()
    })
  }

  const pengamat = new ResizeObserver(jadwalkan)
  pengamat.observe(container)
  // Induknya ikut diamati karena banner error muncul/hilang DI ATAS kotak
  // terminal: posisi kotak bergeser tanpa ukurannya berubah, jadi pengamat
  // pada kotak itu sendiri tidak menyala dan baris terbawah terdorong ke
  // luar area. Tinggi induk berubah saat banner disisipkan — itu sinyalnya.
  // Perhitungan ini konvergen: setelah tinggi kotak dikoreksi, tinggi induk
  // kembali seperti semula dan tidak ada penyesuaian lanjutan.
  if (container.parentElement) pengamat.observe(container.parentElement)

  // ResizeObserver tidak menyala saat tinggi viewport berubah tanpa mengubah
  // lebar elemen (bilah URL browser mobile, jendela diperpendek).
  window.addEventListener("resize", jadwalkan)
  // Membuka/menutup keyboard virtual tidak memicu window.resize di iOS sama
  // sekali; visualViewport adalah satu-satunya sumber yang melaporkannya.
  window.visualViewport?.addEventListener("resize", jadwalkan)
  window.visualViewport?.addEventListener("scroll", jadwalkan)

  // Font mono baru siap setelah cat pertama; lebar sel berubah saat itu.
  void document.fonts?.ready.then(jadwalkan).catch(() => undefined)

  sesuaikan()

  return () => {
    hidup = false
    if (rafId) cancelAnimationFrame(rafId)
    pengamat.disconnect()
    window.removeEventListener("resize", jadwalkan)
    window.visualViewport?.removeEventListener("resize", jadwalkan)
    window.visualViewport?.removeEventListener("scroll", jadwalkan)
  }
}
