// Cloudflare DNS API: structured `data` keys, not arbitrary JSON entered by users.
export type Field = { key: string; label: string; number?: boolean; required?: boolean; options?: string[] }
const text = (key: string, label = key, required = true): Field => ({ key, label, required })
const num = (key: string, label = key, required = true): Field => ({ key, label, number: true, required })
export const recordFields: Record<string, Field[]> = {
  CAA: [num("flags", "Flags"), text("tag", "Tag"), text("value", "Value")],
  CERT: [num("type", "Certificate type"), num("key_tag", "Key tag"), num("algorithm", "Algorithm"), text("certificate", "Certificate")],
  DNSKEY: [num("flags", "Flags"), num("protocol", "Protocol"), num("algorithm", "Algorithm"), text("public_key", "Public key")],
  DS: [num("key_tag", "Key tag"), num("algorithm", "Algorithm"), num("digest_type", "Digest type"), text("digest", "Digest")],
  HTTPS: [num("priority", "Priority"), text("target", "Target"), text("value", "Parameters", false)],
  LOC: [num("lat_degrees", "Latitude degrees"), num("lat_minutes", "Latitude minutes"), num("lat_seconds", "Latitude seconds"), { ...text("lat_direction", "Latitude direction"), options: ["N", "S"] }, num("long_degrees", "Longitude degrees"), num("long_minutes", "Longitude minutes"), num("long_seconds", "Longitude seconds"), { ...text("long_direction", "Longitude direction"), options: ["E", "W"] }, num("altitude", "Altitude"), num("size", "Size", false), num("precision_horz", "Horizontal precision", false), num("precision_vert", "Vertical precision", false)],
  NAPTR: [num("order", "Order"), num("preference", "Preference"), text("flags", "Flags"), text("service", "Service"), text("regex", "Regex", false), text("replacement", "Replacement", false)],
  SMIMEA: [num("usage", "Usage"), num("selector", "Selector"), num("matching_type", "Matching type"), text("certificate", "Certificate")],
  SRV: [num("priority", "Priority"), num("weight", "Weight"), num("port", "Port"), text("target", "Target")],
  SSHFP: [num("algorithm", "Algorithm"), num("type", "Fingerprint type"), text("fingerprint", "Fingerprint")],
  SVCB: [num("priority", "Priority"), text("target", "Target"), text("value", "Parameters", false)],
  TLSA: [num("usage", "Usage"), num("selector", "Selector"), num("matching_type", "Matching type"), text("certificate", "Certificate")],
  URI: [text("target", "Target"), num("weight", "Weight")],
}
export const recordTypes = ["A", "AAAA", "CAA", "CERT", "CNAME", "DNSKEY", "DS", "HTTPS", "LOC", "MX", "NAPTR", "NS", "OPENPGPKEY", "PTR", "SMIMEA", "SRV", "SSHFP", "SVCB", "TLSA", "TXT", "URI"]
export const priorityTypes = new Set(["MX", "URI"])
export const proxiedTypes = new Set(["A", "AAAA", "CNAME"])

export function recordPayload(draft: { id?: string; type: string; name: string; ttl?: number; content?: string; priority?: number; proxied?: boolean; comment?: string; tags?: string[]; data?: unknown }, fields: Record<string, string>, proxiable = true): Record<string, unknown> {
  const type = draft.type.toUpperCase()
  if ((!recordTypes.includes(type) && !(draft.id && /^[A-Z][A-Z0-9]{0,15}$/.test(type))) || !draft.name.trim()) throw Error("Jenis atau nama record tidak valid.")
  if (!Number.isInteger(draft.ttl) || (draft.ttl !== 1 && (draft.ttl! < 60 || draft.ttl! > 86400))) throw Error("TTL harus Auto (1) atau 60–86400 detik.")
  const record: Record<string, unknown> = { type, name: draft.name.trim(), ttl: draft.ttl }
  if (draft.comment !== undefined) record.comment = draft.comment
  if (draft.tags !== undefined) record.tags = draft.tags
  if (priorityTypes.has(type)) {
    if (!Number.isInteger(draft.priority) || draft.priority! < 0 || draft.priority! > 65535) throw Error("Priority wajib berupa bilangan 0–65535.")
    record.priority = draft.priority
  }
  if (proxiedTypes.has(type) && proxiable && draft.proxied !== undefined) record.proxied = draft.proxied
  const schema = recordFields[type]
  // ponytail: record terstruktur lama berbasis content tetap dapat diedit; gunakan data untuk record baru.
  if (schema && !(draft.id && draft.data == null && draft.content?.trim())) {
    const data: Record<string, string | number> = {}
    for (const field of schema) {
      const raw = fields[field.key]?.trim() ?? ""
      if (!raw) { if (field.required) throw Error(`${field.label} wajib diisi.`); continue }
      if (field.options && !field.options.includes(raw)) throw Error(`${field.label} tidak valid.`)
      if (field.number) {
        const value = Number(raw)
        if (!Number.isFinite(value) || (value < 0 && field.key !== "altitude") || (!Number.isInteger(value) && field.key !== "altitude" && !field.key.endsWith("seconds"))) throw Error(`${field.label} harus angka valid.`)
        data[field.key] = value
      } else data[field.key] = raw
    }
    record.data = data
  } else if (draft.content?.trim()) {
    record.content = draft.content.trim()
  } else if (draft.id && !recordTypes.includes(type) && draft.data && typeof draft.data === "object") {
    // ponytail: jenis provider baru tetap dapat diedit tanpa mengubah data yang belum dikenal; tambah schema saat didukung.
    record.data = draft.data
  } else throw Error("Content wajib diisi.")
  return record
}
