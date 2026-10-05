import assert from "node:assert/strict"
import React from "react"
import { CloudflareManager } from "@/views/cloudflare-manager"
import { Select } from "@/components/ui/select"
import { usePrefs } from "@/stores/prefs"
import { ConfirmHost } from "@/components/ui/confirm"
import { renderToStaticMarkup } from "react-dom/server"

// ponytail: no DOM test dependency; execute the real component with a small
// hook runner. Browser layout/focus remain outside this regression's scope.
export async function cekCloudflare() {
  const internals = (React as unknown as { __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED: { ReactCurrentDispatcher: { current: unknown } } }).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
  const previous = internals.ReactCurrentDispatcher.current
  const originalFetch = globalThis.fetch
  const slots: { value?: unknown; deps?: unknown[]; cleanup?: () => void }[] = []
  let cursor = 0, dirty = true, enabled = true, mounted = true, writes = 0
  let tree: React.ReactNode
  let effects: (() => void)[] = []
  const same = (a?: unknown[], b?: unknown[]) => !!a && !!b && a.length === b.length && a.every((x, i) => Object.is(x, b[i]))
  const memo = (fn: () => unknown, deps: unknown[]) => {
    const slot = slots[cursor++] ||= {}
    if (!same(slot.deps, deps)) { slot.value = fn(); slot.deps = deps }
    return slot.value
  }
  const dispatcher = {
    useState(initial: unknown) {
      const i = cursor++
      const slot = slots[i] ||= { value: typeof initial === "function" ? initial() : initial }
      return [slot.value, (value: unknown) => {
        writes++
        const next = typeof value === "function" ? value(slot.value) : value
        if (!Object.is(next, slot.value)) { slot.value = next; dirty = true }
      }]
    },
    useRef(initial: unknown) { return (slots[cursor++] ||= { value: { current: initial } }).value },
    useMemo: memo,
    useCallback(fn: unknown, deps: unknown[]) { return memo(() => fn, deps) },
    useDebugValue() {},
    useSyncExternalStore(_subscribe: unknown, snapshot: () => unknown) { return snapshot() },
    useEffect(fn: () => (() => void) | void, deps: unknown[]) {
      const slot = slots[cursor++] ||= {}
      if (!same(slot.deps, deps)) {
        slot.deps = deps
        effects.push(() => { slot.cleanup?.(); slot.cleanup = fn() || undefined })
      }
    },
  }
  const render = () => {
    if (!mounted) return
    for (let n = 0; dirty; n++) {
      assert.ok(n < 30, "render loop")
      dirty = false; cursor = 0
      internals.ReactCurrentDispatcher.current = dispatcher
      try { tree = CloudflareManager({ enabled }) } finally { internals.ReactCurrentDispatcher.current = previous }
      const pending = effects; effects = []; pending.forEach((fn) => fn())
    }
  }
  const flush = async () => { for (let i = 0; i < 12; i++) { await Promise.resolve(); render() } }
  const nodes = (node = tree): React.ReactElement<Record<string, any>>[] => {
    if (Array.isArray(node)) return node.flatMap((child) => nodes(child))
    if (!React.isValidElement<Record<string, any>>(node)) return []
    return [node, ...nodes(node.props.children ?? null)]
  }
  const text = (node: React.ReactNode): string => Array.isArray(node) ? node.map(text).join("") : React.isValidElement<{children?: React.ReactNode}>(node) ? text(node.props.children) : node == null ? "" : String(node)
  const select = () => nodes().find((node) => node.type === Select)!
  const button = (label: string) => nodes().find((node) => typeof node.props.onClick === "function" && text(node.props.children) === label)!
  const requests: { path: string; body: any; resolve: (data: unknown) => void; reject: (e: Error) => void }[] = []
  globalThis.fetch = ((path: string, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    requests.push({ path, body: init?.body ? JSON.parse(String(init.body)) : null, reject,
      resolve: (data) => resolve({ ok: true, json: async () => data } as Response) })
  })) as typeof fetch
  const zones = [{ id: "A", name: "a.test" }, { id: "B", name: "b.test" }]
  const records = (id: string, page = 1) => ({ records: [{ id, name: `${id}.test`, type: "A", content: "192.0.2.1" }], page, total_pages: 2, total_count: 2 })
  try {
    usePrefs.setState({ bahasa: "id" })
    render(); requests.shift()!.resolve(zones); await flush()
    requests.shift()!.resolve(records("A")); await flush()
    select().props.onChange("B"); await flush()
    requests.shift()!.resolve(records("B")); await flush()
    button("Berikutnya").props.onClick(); await flush()
    requests.shift()!.resolve(records("B2", 2)); await flush()
    usePrefs.setState({ bahasa: "en" }); dirty = true; await flush()
    // Resolve any language-triggered reads, then verify selection + page.
    for (let i = 0; requests.length && i < 5; i++) {
      const req = requests.shift()!
      req.resolve(req.body ? records("B2", req.body.page) : zones); await flush()
    }
    assert.equal(select().props.value, "B", "language must preserve zone B")
    assert.match(text(tree), /2\/2/, "language must preserve page 2")

    // Trigger the actual zone callbacks while requests are delayed.
    select().props.onChange("A"); await flush()
    const old = requests.shift()!
    select().props.onChange("B"); await flush()
    const latest = requests.shift()!
    old.resolve(records("stale")); await flush()
    assert.equal(select().props.disabled, true, "stale finally cannot clear current busy")
    assert.ok(!text(tree).includes("stale.test"), "stale response cannot populate rows")
    latest.resolve(records("current")); await flush()
    assert.ok(text(tree).includes("current.test"))
    assert.equal(select().props.disabled, false)

    usePrefs.setState({ bahasa: "id" }); dirty = true; await flush()
    button("Berikutnya").props.onClick(); await flush()
    const stalePage = requests.shift()!
    assert.deepEqual(stalePage.body, { zone_id: "B", page: 2 })
    select().props.onChange("A"); await flush()
    const firstPage = requests.shift()!
    assert.deepEqual(firstPage.body, { zone_id: "A", page: 1 })
    firstPage.resolve(records("current")); await flush()
    stalePage.resolve(records("stale-page", 2)); await flush()
    assert.ok(!text(tree).includes("stale-page.test"), "late B/page2 cannot overwrite A/page1")
    assert.match(text(tree), /1\/2/)
    assert.equal(select().props.disabled, false)
    button("Berikutnya").props.onClick(); await flush()
    const staleError = requests.shift()!
    select().props.onChange("B"); await flush()
    staleError.reject(new Error("stale record error")); await flush()
    assert.equal(select().props.disabled, true, "stale error cannot clear busy")
    requests.shift()!.resolve({ ...records("current"), records: [
      { id: "current", name: "current.test", type: "A" },
      { id: "other-id", name: "current.test", type: "A" },
    ] }); await flush()
    nodes().find((node) => node.type === "input" && node.props.type === "checkbox")!.props.onChange({ target: { checked: true } })
    await flush()
    button("Hapus terpilih").props.onClick(); await flush()
    internals.ReactCurrentDispatcher.current = { ...dispatcher, useState: () => [false, () => {}], useRef: () => ({ current: null }), useEffect: () => {} }
    let dialog: React.ReactNode
    try { dialog = ConfirmHost() } finally { internals.ReactCurrentDispatcher.current = previous }
    const html = renderToStaticMarkup(dialog)
    assert.match(html, /whitespace-pre-line/, "DNS confirmation preserves line breaks locally")
    assert.match(text(dialog), /A current.test \(current\)\nA current.test \(other-id\)/, "same type/name records have distinct IDs on separate lines")
    nodes(dialog).find((node) => text(node.props.children) === "Batal" && node.props.onClick)!.props.onClick()
    await flush()
    assert.equal(requests.length, 0, "cancel confirmation sends no mutation or reload")
    assert.equal(select().props.disabled, false, "cancel confirmation releases busy")

    select().props.onChange("A"); await flush()
    const cancelled = requests.shift()!
    enabled = false; dirty = true; await flush()
    const before = writes
    cancelled.resolve(records("cancelled")); await flush()
    assert.equal(writes, before, "disabled response must not write state")
    enabled = true; dirty = true; await flush()
    requests.shift()!.resolve(zones); await flush()
    assert.equal(select().props.value, "A", "reenable loads zones again")
    const unmounted = requests.shift()!
    mounted = false; slots.forEach((slot) => slot.cleanup?.())
    const beforeUnmount = writes
    unmounted.reject(new Error("late zone error")); await flush()
    assert.equal(writes, beforeUnmount, "unmounted response must not write state")
  } finally {
    slots.forEach((slot) => slot.cleanup?.())
    globalThis.fetch = originalFetch
    internals.ReactCurrentDispatcher.current = previous
    usePrefs.setState({ bahasa: "id" })
  }
}
