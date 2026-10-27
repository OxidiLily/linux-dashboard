import { useEffect } from "react"
// react-router v7: RouterProvider pindah ke subpath 'react-router/dom' untuk
// menandakan komponen ini tergantung ReactDOM (opsional sebagai peer dep).
// lihat CHANGELOG v7.0.0 react-router.
import { RouterProvider } from "react-router/dom"
import { router } from "@/router"
import { useAuth } from "@/stores/auth"
import { apiGet } from "@/lib/api"

export function App() {
  const load = useAuth((s) => s.load)
  useEffect(() => {
    load().catch(() => undefined)
  }, [load])
  // Judul tab mengikuti nama mesin, bukan nama produk: satu panel untuk satu
  // server, dan dengan beberapa tab terbuka user langsung tahu milik mesin
  // mana. Gagal ambil hostname dibiarkan memakai judul statis di index.html.
  useEffect(() => {
    apiGet<{ hostname: string }>("/api/hostname")
      .then((d) => {
        if (d.hostname) document.title = `${d.hostname} Dashboard`
      })
      .catch(() => undefined)
  }, [])
  return <RouterProvider router={router} />
}
