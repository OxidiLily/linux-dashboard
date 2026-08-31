import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import "./index.css"
// Tabel terjemahan didaftarkan sekali saat start; view cukup memanggil tr().
import "./lib/terjemahan-en"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)