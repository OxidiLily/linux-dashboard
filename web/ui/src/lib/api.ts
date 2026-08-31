// API client tipis — credentials: 'include' supaya cookie session PAM
// ikut semua request seperti konvensi Lindash.

export class ApiError extends Error {
  status: number
  code?: string
  /** Data pengisi kalimat (path, nama, angka) — bukan kalimat itu sendiri. */
  params: string[]
  constructor(res: Response, msg?: string, code?: string, params?: string[]) {
    super(msg || `HTTP ${res.status}`)
    this.status = res.status
    this.code = code
    this.params = params ?? []
  }
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { credentials: "include", signal })
  if (!res.ok) {
    let errBody: { error?: string; code?: string; params?: string[] } = {}
    try { errBody = await res.json() } catch {}
    throw new ApiError(res, errBody.error, errBody.code, errBody.params)
  }
  return res.json()
}

export async function apiSend<T>(path: string, method: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "include",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    let errBody: { error?: string; code?: string; params?: string[] } = {}
    try { errBody = await res.json() } catch {}
    throw new ApiError(res, errBody.error, errBody.code, errBody.params)
  }
  return res.json()
}