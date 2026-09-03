import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * salinKeClipboard menyalin teks ke clipboard sistem.
 *
 * navigator.clipboard TIDAK tersedia di panel ini pada pemakaian normal: API
 * itu hanya ada di secure context — https atau localhost — sedangkan panel
 * dibuka lewat http ke IP LAN (mis. http://192.168.2.11:1122). Memanggilnya
 * langsung akan gagal di justru kasus yang paling sering, jadi jalur cadangan
 * bukan hiasan melainkan jalur utamanya.
 *
 * textarea + execCommand("copy") memang sudah usang, tapi itu satu-satunya
 * cara menyalin tanpa secure context dan masih didukung semua browser yang
 * bisa menjalankan panel ini.
 */
export async function salinKeClipboard(teks: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(teks)
      return true
    }
  } catch {
    // Izin ditolak, atau dokumen sedang tidak fokus. Bukan alasan menyerah —
    // jatuh ke cara lama di bawah.
  }
  try {
    const ta = document.createElement("textarea")
    ta.value = teks
    // Harus ikut dirender: elemen display:none tidak bisa dipilih, dan tanpa
    // seleksi execCommand tidak menyalin apa pun. Digeser ke luar layar, dan
    // readonly supaya keyboard virtual tidak muncul di HP.
    ta.setAttribute("readonly", "")
    ta.style.position = "fixed"
    ta.style.top = "-9999px"
    document.body.appendChild(ta)
    ta.select()
    // select() sendiri tidak cukup di Safari iOS.
    ta.setSelectionRange(0, teks.length)
    const ok = document.execCommand("copy")
    ta.remove()
    return ok
  } catch {
    return false
  }
}
