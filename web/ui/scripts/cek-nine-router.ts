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

  // --- HTTP: iframe langsung ditampilkan (tanpa health check frontend).
  window.location.protocol = "http:"
  window.location.hostname = "100.89.81.126"
  const html = renderToStaticMarkup(createElement(NineRouterView))
  assert(html.includes('<iframe src="http://100.89.81.126:20128/"'))
  assert(html.includes('title="9Router"'))
  assert(html.includes('referrerPolicy="no-referrer"'))
  assert(html.includes('sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"'))
  assert(!html.includes("allow-top-navigation"))

  // --- HTTPS: mixed content warning, tanpa iframe.
  window.location.protocol = "https:"
  const blocked = renderToStaticMarkup(createElement(NineRouterView))
  assert(!blocked.includes("<iframe"))
  assert(blocked.includes('role="alert"'))
  assert(blocked.includes("HTTP"))
  assert(t("nineRouter.mixedContent").includes(bahasa === "id" ? "Panel HTTPS" : "An HTTPS panel"))

  // --- Terjemahan unavailable tersedia.
  assert(t("nineRouter.unavailable").includes(bahasa === "id" ? "tidak tersedia" : "is unavailable"))
}
console.log("PASS: 9Router iframe HTTP, sandbox, referrer, HTTPS/unavailable warnings ID/EN")
