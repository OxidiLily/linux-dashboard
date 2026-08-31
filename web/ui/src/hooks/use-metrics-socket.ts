import { useEffect, useRef } from "react"
import { useMetrics } from "@/stores/metrics"

// Hook koneksi WebSocket ke /ws/metrics. Reconnect exponential backoff.
// Pola sama dengan WebSocket stream handler — lihat TDD §3.2 + §4.1.

export function useMetricsSocket() {
  const apply = useMetrics((s) => s.apply)
  const setConnected = useMetrics((s) => s.setConnected)
  const attempt = useRef(0)

  useEffect(() => {
    let ws: WebSocket | null = null
    let timer: number | null = null
    let alive = true

    const connect = () => {
      if (!alive) return
      const proto = location.protocol === "https:" ? "wss" : "ws"
      ws = new WebSocket(`${proto}://${location.host}/ws/metrics`)
      ws.onopen = () => {
        attempt.current = 0
      }
      // Status "tersambung" baru diset setelah snapshot pertama masuk, bukan
      // saat onopen: sesi kedaluwarsa membuat server menerima upgrade lalu
      // langsung menutupnya — flap seperti itu akan terus mereset hitungan
      // timeout overlay sehingga tombol "Hubungkan kembali" tidak pernah muncul.
      ws.onmessage = (ev) => {
        try {
          apply(JSON.parse(ev.data))
          setConnected(true)
        } catch {
          /* ignore malformed */
        }
      }
      ws.onclose = () => {
        setConnected(false)
        attempt.current++
        // Plafon 5 detik, bukan 30: overlay disconnect menunggu percobaan
        // berikutnya, jadi backoff panjang membuat server yang sudah hidup
        // lagi baru ketahuan setengah menit kemudian.
        const delay = Math.min(500 * 2 ** attempt.current, 5000)
        timer = window.setTimeout(connect, delay)
      }
      ws.onerror = () => {
        ws?.close()
      }
    }

    // Tab kembali aktif atau jaringan pulih = sinyal lebih cepat daripada
    // menunggu timer backoff; percobaan langsung dijalankan saat itu juga.
    const sambungSekarang = () => {
      if (!alive) return
      if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return
      if (timer) {
        window.clearTimeout(timer)
        timer = null
      }
      attempt.current = 0
      connect()
    }
    const onVisible = () => {
      if (!document.hidden) sambungSekarang()
    }
    window.addEventListener("online", sambungSekarang)
    document.addEventListener("visibilitychange", onVisible)

    connect()
    return () => {
      alive = false
      window.removeEventListener("online", sambungSekarang)
      document.removeEventListener("visibilitychange", onVisible)
      if (timer) window.clearTimeout(timer)
      ws?.close()
    }
  }, [apply, setConnected])
}