import assert from "node:assert/strict"
import React from "react"
import { FileManagerView } from "@/views/files"
import { ConfirmHost } from "@/components/ui/confirm"

// React 18 callback harness, matching the existing component tests.
const internals = (React as any).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
const previous = internals.ReactCurrentDispatcher.current
let state: any[] = [], cursor = 0
const dispatcher = {
  useState(initial: any) {
    const i = cursor++
    if (!(i in state)) state[i] = typeof initial === "function" ? initial() : initial
    return [state[i], (next: any) => { state[i] = typeof next === "function" ? next(state[i]) : next }]
  },
  useRef(initial: any) {
    const i = cursor++
    return state[i] ??= { current: initial }
  },
  useEffect() {}, useLayoutEffect() {}, useDebugValue() {},
  useCallback: (fn: any) => fn, useMemo: (fn: any) => fn(),
  useContext: () => ({ navigator: {}, location: { search: "", pathname: "/files" }, matches: [], basename: "/", future: {} }),
  useSyncExternalStore: (_subscribe: any, snapshot: any) => snapshot(),
}
function nodes(node: any): any[] {
  if (!node || typeof node !== "object") return []
  return [node, ...React.Children.toArray(node.props?.children).flatMap(nodes), ...nodes(node.props?.actions)]
}
function text(node: any): string {
  if (typeof node === "string" || typeof node === "number") return String(node)
  return node?.props ? React.Children.toArray(node.props.children).map(text).join("") : ""
}
function render() { cursor = 0; return FileManagerView() }
function button(tree: any, label: string) {
  const n = nodes(tree).find(n => n.props?.onClick && (n.type === "button" || typeof n.type !== "string") && (text(n).trim() === label || n.props["aria-label"] === label))
  assert.ok(n, `button ${label}`)
  return n.props.onClick
}
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve() }
async function main() {
  const fetchBefore = globalThis.fetch, windowBefore = globalThis.window
  const file = { name: "sample-5s.mp4", path: "/nested/sample-5s.mp4", rel: "nested/sample-5s.mp4", is_dir: false, size: 500, mod_time: 1, mode: "-rw-r-----", mode_octal: 0o640, owner: "user", group: "user" }
  const folder = { ...file, name: "sample-folder", path: "/nested/sample-folder", rel: "nested/sample-folder", is_dir: true, mode_octal: 0o750 }
  const requests: any[] = []
  globalThis.window = { innerWidth: 1200, innerHeight: 900, location: { pathname: "/files" } } as Window & typeof globalThis
  globalThis.fetch = (async (url: any, options: any) => {
    requests.push({ url: String(url), body: options?.body && JSON.parse(options.body) })
    return new Response(JSON.stringify(String(url).startsWith("/api/files/search") ? { hits: [file, folder].map(({ mode, mode_octal, owner, group, ...h }) => h), dirs: 2, truncated: false } : { path: "/nested", entries: [file, folder] }))
  }) as typeof fetch
  internals.ReactCurrentDispatcher.current = dispatcher
  try {
    const search = () => nodes(render()).find(n => n.props?.type === "search")!
    search().props.onChange({ target: { value: "sample" } })
    button(render(), "Cari")()
    await flush()
    for (const entry of [file, folder]) {
      const row = nodes(render()).find(n => n.type === "tr" && n.props.title === entry.path)
      assert.ok(row, "recursive search row")
      assert.equal(typeof row.props.onContextMenu, "function", "search result right-click opens the existing context menu")
      let prevented = false
      await row.props.onContextMenu({ preventDefault() { prevented = true }, clientX: 100, clientY: 200 })
      await flush()
      assert.ok(prevented, "native menu suppressed")
      const tree = render()
      const download = nodes(tree).find(n => n.type === "a" && n.props.download)
      assert.equal(download?.props.href, `/api/files/${entry.is_dir ? "archive" : "download"}?path=${encodeURIComponent(entry.path)}`)
      button(tree, "Ubah Permission")()
      assert.equal(nodes(render()).find(n => n.props.id === "perm-mode")?.props.value, entry.mode_octal.toString(8), "real permission, not fabricated 000")
      button(render(), "Batal")()
    }
    // A delayed menu lookup must not reopen after Clear; newer right-click wins.
    const immediateFetch = globalThis.fetch
    const pending: ((r: Response) => void)[] = []
    globalThis.fetch = (() => new Promise<Response>(resolve => pending.push(resolve))) as typeof fetch
    const searchRow = (entry: typeof file) => nodes(render()).find(n => n.type === "tr" && n.props.title === entry.path)!
    searchRow(file).props.onContextMenu({ preventDefault() {}, clientX: 1, clientY: 2 })
    searchRow(folder).props.onContextMenu({ preventDefault() {}, clientX: 3, clientY: 4 })
    pending[1](new Response(JSON.stringify({ entries: [folder] })))
    await flush()
    pending[0](new Response(JSON.stringify({ entries: [file] })))
    await flush()
    assert.ok(nodes(render()).some(n => n.type === "a" && n.props.href === `/api/files/archive?path=${encodeURIComponent(folder.path)}`), "newest context request wins")
    searchRow(file).props.onContextMenu({ preventDefault() {}, clientX: 1, clientY: 2 })
    button(render(), "Bersihkan pencarian")()
    pending[2](new Response(JSON.stringify({ entries: [file] })))
    await flush()
    assert.ok(!nodes(render()).some(n => n.type === "a" && n.props.download), "cleared search cannot reopen stale menu")
    globalThis.fetch = immediateFetch
    search().props.onChange({ target: { value: "sample" } })
    button(render(), "Cari")()
    await flush()
    const row = nodes(render()).find(n => n.type === "tr" && n.props.title === file.path)!
    await row.props.onContextMenu({ preventDefault() {}, clientX: 1, clientY: 2 })
    await flush()
    button(render(), "Rename")()
    const input = nodes(render()).find(n => n.props.value === file.name && n.props.onChange)
    assert.ok(input, "rename input")
    input.props.onChange({ target: { value: "renamed.mp4" } })
    button(render(), "Simpan")()
    const saved = state, savedCursor = cursor
    state = []; cursor = 0
    button(ConfirmHost(), "Ganti nama")()
    state = saved; cursor = savedCursor
    await flush()
    assert.deepEqual(requests.find(r => r.url === "/api/files/rename")?.body, { source: file.path, dest: "/nested/renamed.mp4" }, "rename stays in result's own directory")
    console.log("Files context: search click, file/folder right-click, download/archive, real permissions, nested rename passed")
  } finally {
    internals.ReactCurrentDispatcher.current = previous
    globalThis.fetch = fetchBefore
    globalThis.window = windowBefore
  }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
