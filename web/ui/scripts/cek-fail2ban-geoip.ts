import assert from "node:assert/strict"
import React from "react"
import { GeoIPTable, Fail2banView, DetailIPModal, muatDetailIP } from "@/views/fail2ban"
import { ConfirmHost } from "@/components/ui/confirm"
import { GEOIP_HINT, pollGeoIP, statusGeoIP, type GeoIP } from "@/lib/fail2ban-geoip"
import { usePrefs } from "@/stores/prefs"
import { tr } from "@/stores/i18n"
import "@/lib/terjemahan-en"

const internals = (React as any).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
const previous = internals.ReactCurrentDispatcher.current
const language = usePrefs.getState().bahasa
const originalFetch = globalThis.fetch
const originalWindow = globalThis.window
let state: any[] = [], cursor = 0
const dispatcher = {
 useState(initial: any) { const i = cursor++; if (!(i in state)) state[i] = typeof initial === "function" ? initial() : initial; return [state[i], (v: any) => { state[i] = typeof v === "function" ? v(state[i]) : v }] },
 useRef(initial: any) { const i = cursor++; if (!(i in state)) state[i] = { current: initial }; return state[i] },
 useEffect() {}, useLayoutEffect() {}, useDebugValue() {}, useCallback: (fn: any) => fn, useMemo: (fn: any) => fn(),
 useSyncExternalStore: (_: any, snapshot: any) => snapshot(),
}
function nodes(n: any): any[] { return !n || typeof n !== "object" ? [] : [n, ...React.Children.toArray(n.props?.children).flatMap(nodes), ...nodes(n.props?.actions)] }
function text(n: any): string { return typeof n === "string" ? n : React.Children.toArray(n?.props?.children).map(text).join("") }
const item: GeoIP = { ip: "8.8.8.8", country: "United States", country_code: "US", region: "California", city: "Mountain View", isp: "Google", org: "Google LLC", asn: 15169, timezone: "America/Los_Angeles", status: "ready", source: "cache", fetched_at: "2026-10-06T00:00:00Z" }
async function main() {
 globalThis.window = { location: { pathname: "/security/fail2ban" } } as Window & typeof globalThis
 internals.ReactCurrentDispatcher.current = dispatcher
 try {
  for (const bahasa of ["id", "en"] as const) {
   usePrefs.setState({ bahasa })
   assert.equal(statusGeoIP("pending"), bahasa === "id" ? "Menunggu GeoIP" : "GeoIP pending")
   assert.equal(statusGeoIP("ready"), bahasa === "id" ? "Tersedia" : "Available")
   assert.ok(tr(GEOIP_HINT).includes("ipwho.is"))
   assert.notEqual(tr(GEOIP_HINT), bahasa === "en" ? GEOIP_HINT : "")
   for (const status of ["private", "error", "unavailable", "rate_limited"] as const) assert.ok(statusGeoIP(status))
   for (const jail of ["sshd", "nginx"]) {
    cursor = 0; state = []
    let clicked = ""
    const tree = GeoIPTable({ jail, ips: [item.ip, item.ip, "1.1.1.1"], items: { [item.ip]: item }, buka: ip => { clicked = `${jail}/${ip}` } })
    const elements = nodes(tree)
    assert.equal(elements.filter(n => n.type === "td").length, 8, "deduplicated rows with four columns")
    assert.ok(elements.filter(n => n.type === "td").every(n => typeof n.props["data-label"] === "string"))
    for (const value of ["United States", "California", "Mountain View", "Google", "AS15169", "cache", statusGeoIP("pending")]) assert.ok(text(tree).includes(value), value)
    elements.find(n => n.type === "button").props.onClick()
    assert.equal(clicked, `${jail}/${item.ip}`)
   }
  }
  for (const [raw, canonical] of [
   ["::ffff:8.8.8.8", "8.8.8.8"],
   ["0:0:0:0:0:FFFF:0808:0808", "8.8.8.8"],
   ["2001:4860:4860:0000:0000:0000:0000:8888", "2001:4860:4860::8888"],
   ["2001:4860:4860::8888", "2001:4860:4860::8888"],
  ]) {
   state = []; cursor = 0
   let clicked = ""
   const tree = GeoIPTable({ jail: "sshd", ips: [raw, canonical, "1.1.1.1"], items: { [canonical]: { ...item, ip: canonical } }, buka: ip => { clicked = ip } })
   const buttons = nodes(tree).filter(n => n.type === "button")
   assert.equal(buttons.length, 2, `${raw}: canonical dedupe retains distinct IP`)
   assert.ok(text(tree).includes("Mountain View"), `${raw}: canonical location matched`)
   assert.equal(text(buttons[0]), raw, "display preserves first raw IP")
   buttons[0].props.onClick()
   assert.equal(clicked, raw, "detail/action preserves raw IP")
   assert.equal(text(buttons[1]), "1.1.1.1")
  }
  let count = 0
  usePrefs.setState({ bahasa: "id" })
  for (const raw of ["::ffff:8.8.8.8", "2001:4860:4860:0:0:0:0:8888"]) {
   const canonical = raw.startsWith("::ffff") ? "8.8.8.8" : "2001:4860:4860::8888"
   const record = { ...item, ip: canonical }
   const calls: string[] = []
   globalThis.fetch = (async url => {
    const path = String(url); calls.push(path)
    const data = path.endsWith("/geoip") ? { items: [record], pending: false } : path.includes("/detail?") ? { jail: "sshd", ip: raw, location: "Unavailable", events: [], warnings: [] } : path.includes("/unban?") ? {} : [{ name: "sshd", enabled: true, banned_ips: [raw] }]
    return new Response(JSON.stringify(data))
   }) as typeof fetch
   state = []; cursor = 0
   const view = Fail2banView()
   state[6] = { [canonical]: record, "9.9.9.9": item }
   const refresh = nodes(view).find(n => n.props?.onClick && n.props.disabled === false)
   assert.ok(refresh, "refresh button")
   await refresh.props.onClick()
   await new Promise(resolve => setTimeout(resolve, 0))
   assert.deepEqual(Object.keys(state[6]), [canonical], `${raw}: refresh retains canonical cache, filters unrelated IP`)
   assert.equal(state[6][canonical].city, "Mountain View", "poll response not dropped")
   state[6] = {}
   await refresh.props.onClick()
   await new Promise(resolve => setTimeout(resolve, 0))
   assert.equal(state[6][canonical]?.status, "ready", `${raw}: canonical poll result accepted from empty cache`)
   cursor = 0
   const table = nodes(Fail2banView()).find(n => n.type === GeoIPTable)
   table.props.buka(raw)
   cursor = 0
   const modal = nodes(Fail2banView()).find(n => n.type === DetailIPModal)
   assert.equal(modal.props.ip, raw)
   await muatDetailIP(modal.props.jail, modal.props.ip, new AbortController().signal)
   const pending = modal.props.lepas()
   assert.ok(!calls.some(path => path.includes("/unban?")), "unban waits for explicit confirmation")
   const savedState = state, savedCursor = cursor
   state = []; cursor = 0
   const confirmation = ConfirmHost()
   state = savedState; cursor = savedCursor
   nodes(confirmation).find(n => n.props?.onClick && text(n) === "Lepas").props.onClick()
   await pending
   await new Promise(resolve => setTimeout(resolve, 0))
   assert.ok(calls.includes(`/api/security/fail2ban/sshd/detail?ip=${encodeURIComponent(raw)}`), "detail request uses raw IP")
   assert.ok(calls.includes(`/api/security/fail2ban/sshd/unban?ip=${encodeURIComponent(raw)}`), "unban request uses raw IP")
  }
  const delays: number[] = [], maps: Record<string, GeoIP>[] = []
  globalThis.fetch = (async url => {
   assert.equal(String(url), "/api/security/fail2ban/geoip")
   count++
   return new Response(JSON.stringify({ items: count === 1 ? [{ ...item, status: "pending", source: "none" }, item] : [item], pending: count === 1 }))
  }) as typeof fetch
  assert.equal(await pollGeoIP(new AbortController().signal, () => true, m => maps.push(m), async ms => { delays.push(ms) }), false)
  assert.equal(count, 2); assert.deepEqual(delays, [500]); assert.equal(Object.keys(maps[0]).length, 1)
  assert.equal(maps[0][item.ip].source, "cache")
  for (const abort of [true, false]) {
   let resolve!: (r: Response) => void; let current = true; let updates = 0
   globalThis.fetch = (() => new Promise<Response>(r => { resolve = r })) as typeof fetch
   const controller = new AbortController()
   const pending = pollGeoIP(controller.signal, () => current, () => { updates++ })
   if (abort) controller.abort(); else current = false
   resolve(new Response(JSON.stringify({ items: [item], pending: false })))
   await pending; assert.equal(updates, 0, "abort/unmount or refresh sequence rejects late response")
  }
  count = 0
  globalThis.fetch = (async () => { count++; return new Response(JSON.stringify({ items: [], pending: true })) }) as typeof fetch
  assert.equal(await pollGeoIP(new AbortController().signal, () => true, () => {}, async ms => { assert.ok(ms <= 2000) }), true)
  assert.equal(count, 60, "bounded retries")
  globalThis.fetch = (async () => new Response(JSON.stringify({ items: [{ ...item, asn: "bad" }], pending: false }))) as typeof fetch
  await assert.rejects(pollGeoIP(new AbortController().signal, () => true, () => {}))
  // Actual view wiring: both jail tables share map; clicking second jail opens that jail's modal.
  state = []; cursor = 0; Fail2banView()
  state[0] = ["sshd", "nginx"].map(name => ({ name, enabled: true, banned_ips: [item.ip] }))
  state[6] = { [item.ip]: item }
  cursor = 0
  const tables = nodes(Fail2banView()).filter(n => n.type === GeoIPTable)
  assert.equal(tables.length, 2); assert.equal(tables[0].props.items, tables[1].props.items)
  tables[1].props.buka(item.ip)
  cursor = 0
  const modal = nodes(Fail2banView()).find(n => n.type === DetailIPModal)
  assert.equal(modal.props.jail, "nginx"); assert.equal(modal.props.ip, item.ip)
  console.log("Fail2ban GeoIP: ID/EN table/click, canonical IPv6/mapped dedupe/cache/poll, raw detail/unban, shared cache, pending, aborted/stale responses, 60 bounded polls, validation passed")
 } finally { internals.ReactCurrentDispatcher.current = previous; globalThis.fetch = originalFetch; globalThis.window = originalWindow; usePrefs.setState({ bahasa: language }) }
}
main().catch(e => { console.error(e); process.exitCode = 1 })
