import assert from "node:assert/strict"
import { createElement } from "react"
import { MemoryRouter } from "react-router-dom"
import { renderToStaticMarkup } from "react-dom/server"
import * as view from "@/views/fail2ban"
import { readFileSync } from "node:fs"
import "@/lib/terjemahan-en"
import { aksiFail2ban, lokasiFail2ban, peringatanFail2ban } from "@/lib/fail2ban-detail"
import { tr } from "@/stores/i18n"
import { usePrefs } from "@/stores/prefs"

async function main() {
const { DetailIPModal, muatDetailIP } = view as unknown as {
  DetailIPModal: (props: { jail: string; ip: string; tutup: () => void; lepas: () => Promise<void> }) => React.ReactNode
  muatDetailIP: (jail: string, ip: string, signal: AbortSignal) => Promise<unknown>
}
assert.equal(typeof DetailIPModal, "function", "IP detail dialog exists")
const html = renderToStaticMarkup(createElement(MemoryRouter, {}, createElement(DetailIPModal, { jail: "ssh/test", ip: "2001:db8::1", tutup: () => {}, lepas: async () => {} })))
for (const text of ['role="dialog"', 'aria-modal="true"', "ssh/test", "2001:db8::1", "Memuat GeoIP", "Riwayat", "Lepas blokir"]) assert.ok(html.includes(text), text)
const source = readFileSync("src/views/fail2ban.tsx", "utf8")
assert.ok(source.includes("buka={ip => setDetail({ jail: j.name, ip })}"), "IP table opens detail, not unban")

const originalFetch = globalThis.fetch
try {
  let resolve!: (response: Response) => void
  let path = ""
  globalThis.fetch = (async (url: string | URL | Request) => {
    path = String(url)
    return await new Promise<Response>((done) => { resolve = done })
  }) as typeof fetch
  const controller = new AbortController()
  const pending = muatDetailIP("ssh/test", "2001:db8::1", controller.signal)
  assert.equal(path, "/api/security/fail2ban/ssh%2Ftest/detail?ip=2001%3Adb8%3A%3A1")
  controller.abort()
  resolve(new Response(JSON.stringify({ jail: "ssh/test", ip: "2001:db8::1", events: [], warnings: [], location: "" })))
  assert.equal(await pending, undefined, "closed/unmounted request cannot deliver stale data")
  const payload = { jail: "sshd", ip: "192.0.2.1", events: [{ time: "2026-10-05T03:04:05Z", source: "auth.log", action: "Found", message: "Failed password" }], warnings: ["Log rotated"], location: "" }
  globalThis.fetch = (async () => new Response(JSON.stringify(payload))) as typeof fetch
  assert.deepEqual(await muatDetailIP(payload.jail, payload.ip, new AbortController().signal), payload)
  globalThis.fetch = (async () => new Response(JSON.stringify({ ...payload, events: [null] }))) as typeof fetch
  await assert.rejects(muatDetailIP(payload.jail, payload.ip, new AbortController().signal), /Invalid Fail2ban detail response/)
  globalThis.fetch = (async () => new Response(JSON.stringify({ error: "Unavailable" }), { status: 503 })) as typeof fetch
  await assert.rejects(muatDetailIP(payload.jail, payload.ip, new AbortController().signal), /Unavailable/)
} finally { globalThis.fetch = originalFetch }
// Derive every warning template from the helper so new emissions cannot silently miss i18n.
const helper = readFileSync("../../internal/helper/fail2ban_detail.go", "utf8")
const emitted = [
  ...Array.from(helper.matchAll(/warn\("([^"]+)"\)/g), (m) => m[1]),
  ...Array.from(helper.matchAll(/warn\(fmt\.Sprintf\("([^"]+)"/g), (m) => m[1].replace("%s", "/var/log/custom path.{0}.1")),
  ...Array.from(helper.matchAll(/warn\(source \+ "([^"]+)"\)/g), (m) => ["journal:fail2ban", "journal:ssh"].map((s) => s + m[1])).flat(),
]
assert.equal(emitted.length, 20, "all static, path and both journal warning variants exercised")
const originalLanguage = usePrefs.getState().bahasa
try {
  for (const bahasa of ["id", "en"] as const) {
    usePrefs.setState({ bahasa })
    for (const warning of emitted) {
      const translated = peringatanFail2ban(warning)
      if (bahasa === "en") assert.equal(translated, warning)
      else {
        assert.notEqual(translated, warning, warning)
        for (const parameter of ["/var/log/custom path.{0}.1", "journal:fail2ban", "journal:ssh"]) {
          if (warning.includes(parameter)) assert.ok(translated.includes(parameter), "preserve parameter verbatim")
        }
      }
    }
    for (const location of [undefined, "", "  ", "Unavailable", " Unavailable "]) assert.equal(lokasiFail2ban(location), tr("Lokasi tidak tersedia dari log."))
    assert.equal(lokasiFail2ban("Jakarta, Indonesia"), "Jakarta, Indonesia")
    for (const [action, id, en] of [["Found", "Terdeteksi", "Found"], ["Ban", "Pemblokiran", "Ban"], ["Unban", "Lepas blokir", "Unban"], ["SSH", "Konteks SSH", "SSH context"]]) assert.equal(aksiFail2ban(action), bahasa === "id" ? id : en)
    assert.equal(aksiFail2ban("Future"), "Future")
    assert.equal(aksiFail2ban("toString"), "toString")
    assert.equal(aksiFail2ban(""), "—")
    for (const unknown of ["toString", "__proto__", "Future warning.", "service unavailable.", "raw [sshd] Found 192.0.2.1 - original text"]) assert.equal(peringatanFail2ban(unknown), unknown)
  }
  const history = readFileSync("src/views/fail2ban-history.tsx", "utf8")
  assert.ok(history.includes("peringatanFail2ban(warning)"))
  assert.ok(history.includes("{aksiFail2ban(event.action)}"))
  assert.ok(history.includes("{event.message}"), "original log messages stay raw on separate page")
  assert.ok(!source.includes("{event.message}"), "modal does not show history logs")
} finally { usePrefs.setState({ bahasa: originalLanguage }) }
console.log(`Fail2ban: dialog, encoded GET, stale responses, ${emitted.length} warning variants ID/EN, locations/actions/raw logs passed`)
}
main().catch((error) => { console.error(error); process.exitCode = 1 })
