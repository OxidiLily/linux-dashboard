import { useEffect } from "react"
// react-router v7: RouterProvider pindah ke subpath 'react-router/dom' untuk
// menandakan komponen ini tergantung ReactDOM (opsional sebagai peer dep).
// lihat CHANGELOG v7.0.0 react-router.
import { RouterProvider } from "react-router/dom"
import { router } from "@/router"
import { useAuth } from "@/stores/auth"

export function App() {
  const load = useAuth((s) => s.load)
  useEffect(() => {
    load().catch(() => undefined)
  }, [load])
  return <RouterProvider router={router} />
}