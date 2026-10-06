import assert from "node:assert/strict"
import React from "react"
import { readFileSync } from "node:fs"
import { GeoIPFields, DetailIPModal } from "@/views/fail2ban"
import { HistoryTable, Fail2banHistoryView } from "@/views/fail2ban-history"
import { renderToStaticMarkup } from "react-dom/server"
import { MemoryRouter } from "react-router-dom"
import { t } from "@/stores/i18n"
import { pollGeoIPFull, ratakanGeoIP, riwayatIP, nilaiGeoIP, benderaGeoIP, labelGeoIP } from "@/lib/fail2ban-geoip"
import { usePrefs } from "@/stores/prefs"
import "@/lib/terjemahan-en"
const payload = { connection: { asn: 0, active: false }, empty: "", absent: null, list: [false, 0], object: {}, array: [], flag: { img: "https://example.invalid/flag.svg" }, timezone: { current_time: "snapshot" } }
assert.deepEqual(ratakanGeoIP({ connection: { asn: 0, active: false }, empty: "", absent: null, list: [false, 0], object: {}, array: [] }), [["connection.asn", "0"], ["connection.active", "false"], ["empty", '\"\"'], ["absent", "null"], ["list.0", "false"], ["list.1", "0"], ["object", "{}"], ["array", "[]"]])
const deepPayload = JSON.parse('{"ip":"8.8.8.8","success":true,"extra":' + "[".repeat(6000) + '0' + "]".repeat(6000) + '}')
assert.throws(() => ratakanGeoIP(deepPayload), e => e instanceof Error && !(e instanceof RangeError) && e.message.includes("32"))
const internals = (React as any).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
const previous = internals.ReactCurrentDispatcher.current, language = usePrefs.getState().bahasa, fetchBefore = globalThis.fetch
let state: any[] = [], cursor = 0, effects: (() => any)[] = []
internals.ReactCurrentDispatcher.current = {
 useState(initial: any) { const i = cursor++; if (!(i in state)) state[i] = initial; return [state[i], (v: any) => { state[i] = typeof v === "function" ? v(state[i]) : v }] },
 useRef(initial: any) { const i = cursor++; if (!(i in state)) state[i] = { current: initial }; return state[i] },
 useEffect(fn: any) { effects.push(fn) }, useDebugValue() {}, useCallback: (fn: any) => fn,
 useSyncExternalStore: (_: any, snapshot: any) => snapshot(),
}
function nodes(n: any): any[] { return !n || typeof n !== "object" ? [] : [n, ...React.Children.toArray(n.props?.children).flatMap(nodes)] }
function text(n: any): string { return typeof n === "string" ? n : React.Children.toArray(n?.props?.children).map(text).join("") }
async function main() {
try {
 for (const bahasa of ["id", "en"] as const) {
  usePrefs.setState({ bahasa }); cursor = 0; state = []; effects = []
  const rejected = GeoIPFields({ data: deepPayload })
  assert.ok(text(rejected).includes(bahasa === "id" ? "payload tidak ditampilkan" : "payload not displayed"))
  assert.ok(!nodes(rejected).some(n => n.type === "pre" || n.type === "td"), "rejected payload never reaches raw serialization or partial rows")
  const tree = GeoIPFields({ data: payload }), all = nodes(tree)
  for (const [key] of ratakanGeoIP(payload)) assert.ok(all.some(n => n.props?.["data-field"] === key), `preserved field ${key}`)
  assert.ok(all.filter(n => n.type === "td").every(n => typeof n.props["data-label"] === "string"))
  assert.ok(!all.some(n => n.type === "img" || n.props?.src), "no external image request")
  assert.ok(!all.some(n => ["pre", "details", "summary"].includes(n.type)), "raw JSON removed entirely")
  assert.ok(!text(tree).includes("example.invalid"), "flag image URL replaced with icon")
  assert.ok(!text(tree).includes(bahasa === "id" ? "berbayar" : "paid"))
  assert.ok(!text(tree).includes(bahasa === "id" ? "Tidak tersedia" : "Unavailable"))
  assert.ok(text(tree).includes(bahasa === "id" ? "Tidak" : "No"))
  assert.ok(all.some(n => n.props?.["aria-hidden"] === true), "icons present")
  assert.equal(nodes(GeoIPFields({ data: null })).filter(n => n.type === "table").length, 0)
  const friendly = GeoIPFields({ data: { country_code: "ID", connection: { asn: 0, org: "Network owner", isp: "ISP", domain: "example.net" }, latitude: -6.2, longitude: 0, timezone: { offset: 19801, utc: "+05:30", current_time: "2026-10-06T12:00:00+07:00", id: "Asia/Jakarta" }, flag: { img: "https://evil.invalid/flag.svg", emoji: "🇮🇩", emoji_unicode: "U+1F1EE U+1F1E9" }, security: { proxy: false }, currency: { code: "IDR" }, native: { country: "Indonesia" }, rate: { remaining: 0 }, unknown_group: { strange_field: "<b>untouched</b>" } } })
  for (const label of bahasa === "id" ? ["Kode Negara", "Organisasi", "Koneksi", "Keamanan", "Mata Uang", "Nama Lokal", "Batas Permintaan", "Unicode Emoji"] : ["Country Code", "Organization", "Connection", "Security", "Currency", "Native Names", "Rate Limit", "Emoji Unicode"]) assert.ok(text(friendly).includes(label), label)
  for (const value of ["🇮🇩", "-6.2°", "0°", "UTC+05:30:01", "Asia/Jakarta", "<b>untouched</b>", "Strange Field", "Unknown Group"]) assert.ok(text(friendly).includes(value), value)
  const grouped = GeoIPFields({ data: { flag: { img: "", emoji: "🇸🇬", emoji_unicode: "U+1F1F8 U+1F1EC" }, connection: { asn: 1, org: "owner", isp: "isp", domain: "domain" }, timezone: { id: "Asia/Singapore", abbr: "SGT", is_dst: false, offset: 28800, utc: "+08:00" }, native: { currency: { name: "Dollar" } } } })
  const expectedLabels = bahasa === "id" ? ["Bendera", "Emoji Bendera", "Unicode Emoji", "ASN", "Organisasi", "ISP", "Domain", "ID Zona Waktu", "Singkatan Zona Waktu", "Waktu Musim Panas", "Offset Zona Waktu", "Offset UTC", "Mata Uang · Nama Mata Uang"] : ["Flag", "Flag Emoji", "Emoji Unicode", "ASN", "Organization", "ISP", "Domain", "Timezone ID", "Timezone Abbreviation", "Daylight Saving Time", "Timezone Offset", "UTC Offset", "Currency · Currency Name"]
  assert.deepEqual(nodes(grouped).filter(n => n.props?.["data-field"]).map(n => text(React.Children.toArray(n.props.children)[0])), expectedLabels)
  assert.ok(!text(friendly).includes("evil.invalid"))
  assert.equal(nodes(friendly).filter(n => n.type === "section").length, 9)
  for (const [key, expected] of [["empty", bahasa === "id" ? "Kosong" : "Empty"], ["absent", bahasa === "id" ? "Null (tanpa nilai)" : "Null (no value)"], ["object", bahasa === "id" ? "Objek kosong" : "Empty object"], ["array", bahasa === "id" ? "Daftar kosong" : "Empty list"], ["connection.asn", "0"]]) assert.ok(text(all.find(n => n.props?.["data-field"] === key)).includes(expected))
  for (const value of [0, -19801, 19801]) assert.equal(nilaiGeoIP("timezone.offset", value, {}), value === 0 ? "UTC+00:00" : value < 0 ? "UTC−05:30:01" : "UTC+05:30:01")
  for (const [key, value] of [["timezone.offset", "3600"], ["timezone.offset", 1.5], ["timezone.utc", "+07:00"], ["latitude", 100], ["longitude", "0"], ["timezone.current_time", "snapshot"]] as const) assert.equal(nilaiGeoIP(key, value, {}), String(value), "uncertain value unchanged")
  for (const img of ["data:image/svg+xml,x", "javascript:alert(1)", "http://cdn.ipwhois.io/flags/sg.svg", "https://evil.invalid/flags/sg.svg", "https://cdn.ipwhois.io.evil.invalid/flags/sg.svg", "https://cdn.ipwhois.io/other/sg.svg", "https://cdn.ipwhois.io/flags/zz.svg", "https://cdn.ipwhois.io/flags/sg.svg?x=1", "https://cdn.ipwhois.io/flags/../flags/sg.svg", "https://user@cdn.ipwhois.io/flags/sg.svg"]) {
   cursor = 0; state = []
   assert.ok(!nodes(GeoIPFields({ data: { country_code: "SG", flag: { img } } })).some(n => n.type === "img"), img)
  }
  cursor = 0; state = []
  const validFlag = GeoIPFields({ data: { country_code: "SG", flag: { img: "https://cdn.ipwhois.io/flags/sg.svg" } } })
  const image = nodes(validFlag).find(n => n.type === "img")
  assert.ok(image, "trusted Singapore SVG rendered")
  assert.equal(image.props.src, "https://cdn.ipwhois.io/flags/sg.svg")
  assert.equal(image.props.referrerPolicy, "no-referrer"); assert.equal(image.props.loading, "lazy")
  assert.equal(image.props.alt, bahasa === "id" ? "Bendera" : "Flag")
  assert.ok(image.props.width && image.props.height && image.props.className.includes("object-contain"))
  image.props.onError(); cursor = 0
  const fallback = GeoIPFields({ data: { country_code: "SG", flag: { img: "https://cdn.ipwhois.io/flags/sg.svg" } } })
  assert.ok(!nodes(fallback).some(n => n.type === "img")); assert.ok(text(fallback).includes("🇸🇬"))
  assert.equal(benderaGeoIP({ country_code: "ZZ" }), null)
  assert.equal(benderaGeoIP({ country_code: "ZZ", flag: { emoji: "🇺🇸" } }), "🇺🇸")
  assert.equal(benderaGeoIP({ flag: { emoji: "<img src=x>" } }), null)
  assert.equal(labelGeoIP("constructor.toString"), "Constructor · To String")
  assert.ok(text(nilaiGeoIP("timezone.current_time", "2026-10-06T05:00:00Z", { timezone: { id: "Asia/Jakarta" } })).includes("12"))
  cursor = 0; state = []
  const history = HistoryTable({ data: { jail: "sshd", ip: "8.8.8.8", location: "", warnings: ["Events truncated to 200 entries."], events: [{ time: "unknown", source: "auth.log", action: "Found", message: "RAW LOG <b> unchanged" }] } })
  assert.equal(nodes(history).filter(n => n.type === "td").length, 4)
  assert.ok(text(history).includes("RAW LOG <b> unchanged"))
  assert.ok(text(history).includes(bahasa === "id" ? "Terdeteksi" : "Found"))
  assert.equal(t("nav.fail2banHistory"), bahasa === "id" ? "Riwayat Fail2ban" : "Fail2ban history")
  const shell = readFileSync("src/components/layout/app-shell.tsx", "utf8")
  assert.ok(shell.includes('location.pathname === "/settings/fail2ban/history"') && shell.includes('label: "nav.fail2banHistory"'), "history breadcrumb mapped before 404 fallback")
  const dispatcher = internals.ReactCurrentDispatcher.current
  internals.ReactCurrentDispatcher.current = previous
  const page = renderToStaticMarkup(React.createElement(MemoryRouter, { initialEntries: ["/settings/fail2ban/history"] }, React.createElement(Fail2banHistoryView)))
  internals.ReactCurrentDispatcher.current = dispatcher
  assert.match(page, /<a[^>]*class="[^"]*inline-flex[^>]*href="\/settings\/fail2ban"/, "back link renders Button asChild anchor")
  assert.ok(!/<button[^>]*>\s*<a/.test(page), "no nested interactive button/link")
  cursor = 0; state = []; effects = []
  const modal = DetailIPModal({ jail: "ssh/test", ip: "8.8.8.8", tutup() {}, async lepas() {} })
  const link = nodes(modal).find(n => n.props?.to)
  assert.equal(link.props.to, riwayatIP("ssh/test", "8.8.8.8"))
  assert.equal(text(link), bahasa === "id" ? "Riwayat" : "History")
  assert.ok(nodes(modal).some(n => n.props?.asChild && nodes(n).some(child => child.props?.to === link.props.to)), "History uses visible Button-styled router Link")
 }
 let calls = 0; const updates: any[] = []
 globalThis.fetch = (async url => { assert.equal(String(url), "/api/security/fail2ban/ssh%2Ftest/geoip?ip=8.8.8.8"); calls++; return new Response(JSON.stringify({ ip: "8.8.8.8", status: calls === 1 ? "pending" : "ready", source: calls === 1 ? "none" : "cache", fetched_at: "2026-10-06T00:00:00Z", data: calls === 1 ? null : payload })) }) as typeof fetch
 assert.equal(await pollGeoIPFull("ssh/test", "8.8.8.8", new AbortController().signal, data => updates.push(data), async () => {}), false)
 assert.equal(calls, 2); assert.equal(updates[0].data, null); assert.deepEqual(updates[1].data, payload)
 // Exercise the modal effect and its actual unmount/IP-change cleanup; fetch ignores abort.
 let resolve!: (r: Response) => void
 globalThis.fetch = (() => new Promise<Response>(r => { resolve = r })) as typeof fetch
 cursor = 0; state = []; effects = []
 DetailIPModal({ jail: "sshd", ip: "8.8.8.8", tutup() {}, async lepas() {} })
 const cleanup = effects[1]()
 cleanup()
 resolve(new Response(JSON.stringify({ ip: "8.8.8.8", status: "ready", source: "cache", fetched_at: "", data: payload })))
 await new Promise(r => setTimeout(r, 0))
 assert.equal(state[0], undefined, "closed or changed modal rejects stale response")
 globalThis.fetch = (async () => new Response(JSON.stringify({ ip: "1.1.1.1", status: "ready", source: "cache", fetched_at: "", data: payload }))) as typeof fetch
 await assert.rejects(pollGeoIPFull("sshd", "8.8.8.8", new AbortController().signal, () => {}))
 const router = readFileSync("src/router/index.tsx", "utf8"), lazy = readFileSync("src/router/lazy-routes.ts", "utf8")
 assert.ok(router.includes('path: "settings/fail2ban/history", element: <Dijaga name="fail2ban"'))
 assert.ok(router.includes('name === "fail2ban" && !sudo'))
 assert.ok(lazy.includes('"/settings/fail2ban/history": () => import("@/views/fail2ban-history")'))
 console.log("Fail2ban full: ID/EN friendly fields/icons, no raw JSON/paid placeholders, safe flag icon, faithful values/offset/snapshot, optional tables, unknown keys, raw history, visible History Button/guards, pending/cache, stale response passed")
} finally { internals.ReactCurrentDispatcher.current = previous; usePrefs.setState({ bahasa: language }); globalThis.fetch = fetchBefore }
}
main().catch(e => { console.error(e); process.exitCode = 1 })
