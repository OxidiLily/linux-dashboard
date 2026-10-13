import { strict as assert } from "node:assert"
import { readFileSync } from "node:fs"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { ProxyManagerView } from "@/views/proxy"
import { ComponentGuard } from "@/components/ui/component-guard"
import { PostfixTab } from "@/views/postfix"
import { usePrefs } from "@/stores/prefs"
import { tr } from "@/stores/i18n"
let calls = 0
globalThis.fetch = (() => { calls++; throw new Error("unexpected fetch") }) as typeof fetch
assert.equal(renderToStaticMarkup(createElement(ComponentGuard, { name: "postfix", label: "Postfix", strict: true, children: createElement(PostfixTab) })), "")
assert.equal(calls, 0)
const components = readFileSync("../../internal/helper/components.go", "utf8")
assert(components.split("func ComponentNames() []string {", 2)[1].split("\n}", 1)[0].includes('"postfix"'))
assert(components.includes('Service: "postfix", Category: katEmail'))
const html = renderToStaticMarkup(createElement(ProxyManagerView))
assert(html.includes("Proxy Mail</button>"))
assert(!html.includes("myhostname"))
const proxy = readFileSync("src/views/proxy.tsx", "utf8")
assert(proxy.includes('tab === "postfix" ? <ComponentGuard name="postfix" label="Postfix" strict><PostfixTab />'))
const guard = readFileSync("src/components/ui/component-guard.tsx", "utf8")
assert(guard.includes("strict && !status?.installed"))
const source = readFileSync("src/views/postfix.tsx", "utf8")
assert(!/mailcow/i.test(source))
assert(source.includes('title="Proxy Mail"'))
assert(source.includes("await confirmDialog"))
assert(source.includes("revision: config.revision, restart: true"))
assert(!readFileSync("src/components/layout/app-shell.tsx", "utf8").includes("nav.postfix"))
for (const bahasa of ["id", "en"] as const) {
  usePrefs.setState({ bahasa })
  for (const key of ["Domain email", "Host/IP tujuan", "Port SMTP", "Tambah tujuan SMTP", "Endpoint SMTP tujuan harus reachable dari Postfix. Tailscale: host/IP tujuan. Cloudflare TCP: endpoint konektor lokal atau private route yang sudah tersedia, bukan hostname HTTP publik. mynetworks adalah klien tepercaya, bukan tujuan forwarding."]) {
    assert(source.includes(key))
    assert.equal(tr(key) === key, bahasa === "id")
  }
  assert(tr("Simpan dan restart").includes(bahasa === "id" ? "Simpan" : "Save"))
  assert(tr("Proxy Mail memakai Postfix sebagai SMTP relay untuk meneruskan email ke server tujuan berdasarkan domain dan transport map.").includes(bahasa === "id" ? "server tujuan" : "destination server"))
}
console.log("PASS: Postfix tab, guard before mount, confirmation, revision, no sidebar, ID/EN")
