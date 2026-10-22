import { strict as assert } from "node:assert"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { NineRouterView } from "@/views/nine-router"
import { usePrefs } from "@/stores/prefs"
import { t } from "@/stores/i18n"

Object.defineProperty(globalThis, "window", { value: { location: { protocol: "http:", hostname: "100.89.81.126" } }, configurable: true })
for (const bahasa of ["id", "en"] as const) {
  usePrefs.setState({ bahasa })
  usePrefs.getInitialState = usePrefs.getState

  // --- HTTP: SSR tidak menjalankan useEffect, jadi health check belum jalan.
  // Komponen menampilkan loading state (role="status"), bukan iframe langsung.
  window.location.protocol = "http:"
  window.location.hostname = "100.89.81.126"
  const html = renderToStaticMarkup(createElement(NineRouterView))
  // SSR: useEffect tidak jalan → available === null → loading state
  assert(html.includes('role="status"'), "HTTP SSR harus menampilkan loading state")
  assert(!html.includes("<iframe"), "HTTP SSR tidak boleh langsung menampilkan iframe (health check belum jalan)")

  // --- HTTPS: mixed content warning langsung, tanpa health check.
  window.location.protocol = "https:"
  const blocked = renderToStaticMarkup(createElement(NineRouterView))
  assert(!blocked.includes("<iframe"))
  assert(blocked.includes('role="alert"'))
  // SSR memakai snapshot awal Zustand; terjemahan aktif diuji langsung.
  assert(blocked.includes("HTTP"))
  assert(t("nineRouter.mixedContent").includes(bahasa === "id" ? "Panel HTTPS" : "An HTTPS panel"))

  // --- Terjemahan unavailable dan checking tersedia.
  assert(t("nineRouter.unavailable").includes(bahasa === "id" ? "tidak tersedia" : "is unavailable"))
  assert(t("nineRouter.checking").includes(bahasa === "id" ? "Memeriksa" : "Checking"))
}
console.log("PASS: 9Router iframe HTTP loading/HTTPS warning, unavailable/checking translations ID/EN")
