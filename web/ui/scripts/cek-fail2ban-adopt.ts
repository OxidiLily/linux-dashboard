import assert from "node:assert/strict"
import React from "react"
import { Fail2banView } from "@/views/fail2ban"
import { ConfirmHost } from "@/components/ui/confirm"

// Exercise real component callbacks without a DOM or additional test dependency.
const internals = (React as any).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
const previous = internals.ReactCurrentDispatcher.current
let state: any[] = [], cursor = 0
const jails = [{ name: "sshd", external: true, enabled: true, maxretry: 3 }, { name: "samba", enabled: true, maxretry: 5 }]
const dispatcher = {
 useState(initial: any) {
  const index = cursor++
  if (!(index in state)) state[index] = typeof initial === "function" ? initial() : initial
  return [state[index], (next: any) => { state[index] = typeof next === "function" ? next(state[index]) : next }]
 },
 useRef: (value: any) => ({ current: value }),
 useEffect: () => {}, useLayoutEffect: () => {}, useDebugValue: () => {},
 useCallback: (fn: any) => fn, useMemo: (fn: any) => fn(),
 useSyncExternalStore: (_subscribe: any, snapshot: any) => snapshot(),
}
function render() { cursor = 0; return Fail2banView() }
function nodes(node: any): any[] {
 if (!node || typeof node !== "object") return []
 return [node, ...React.Children.toArray(node.props?.children).flatMap(nodes), ...nodes(node.props?.actions)]
}
function text(node: any): string {
 if (typeof node === "string" || typeof node === "number") return String(node)
 return node?.props ? React.Children.toArray(node.props.children).map(text).join("") : ""
}
function button(tree: any, label: string) {
 const result = nodes(tree).find(n => n.props?.onClick && (typeof n.type !== "string" || n.type === "button") && (text(n).includes(label) || n.props["aria-label"] === label))
 assert.ok(result, `button ${label}`)
 return result.props.onClick
}
async function main() {
 const originalFetch = globalThis.fetch
 const originalWindow = globalThis.window
 globalThis.window = { location: { pathname: "/security/fail2ban" } } as Window & typeof globalThis
 const payloads: any[] = []
 globalThis.fetch = (async (url: any, options: any) => {
  if (String(url).startsWith("/api/security/fail2ban") && options?.body) payloads.push(JSON.parse(options.body))
  return new Response(JSON.stringify(options?.body ? {} : jails))
 }) as typeof fetch
 internals.ReactCurrentDispatcher.current = dispatcher
 try {
  render()
  state[0] = jails // Seed loaded data; effects deliberately do not run.
  const save = async (confirmed: boolean) => {
   const form = nodes(render()).find(n => n.type === "form")
   assert.ok(form, "editor form")
   const pending = form.props.onSubmit({ preventDefault() {} })
   // Render the actual confirmation host and resolve its real store.
   const savedState = state, savedCursor = cursor
   state = []; cursor = 0
   const dialog = ConfirmHost()
   state = savedState; cursor = savedCursor
   button(dialog, confirmed ? "Simpan" : "Batal")()
   await pending
  }
  button(render(), "Kelola di panel")()
  await save(false)
  assert.equal(payloads.length, 0, "canceled adoption sends nothing")
  await save(true)
  assert.equal(payloads.at(-1).adopt, true, "Kelola explicitly authorizes adoption")
  assert.equal(payloads.at(-1).name, "sshd")
  button(render(), "Edit jail samba")()
  await save(true)
  assert.equal(payloads.at(-1).adopt, false, "normal edit clears adoption intent")
  button(render(), "Kelola di panel")()
  button(render(), "Batal")()
  button(render(), "Tambah Jail")()
  await save(true)
  assert.equal(payloads.at(-1).adopt, false, "create after canceled adoption clears intent")
  console.log("Fail2ban adoption: real click/submit, confirmation cancel, edit/create reset passed")
 } finally {
  internals.ReactCurrentDispatcher.current = previous
  globalThis.fetch = originalFetch
  globalThis.window = originalWindow
 }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
