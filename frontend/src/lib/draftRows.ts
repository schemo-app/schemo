import type { Draft, Row } from '../../bindings/schemo/internal/domain'

// The rows an address implies, and the address the rows write. The window owns the draft while it
// is edited, so the rule that keeps the two in step lives here: the query rows follow the address
// as it is typed, and a parameter edited as a row writes the address again.

export const mintRowId = () => crypto.randomUUID()

/** The address without its query, and the query (RFC 3986 §3.4); a `?` after `#` is the fragment's. */
export function splitQuery(address: string): { base: string; query: string } {
  const hash = address.indexOf('#')
  let rest = hash < 0 ? address : address.slice(0, hash)
  const fragment = hash < 0 ? '' : address.slice(hash)
  let query = ''
  const mark = rest.indexOf('?')
  if (mark >= 0) {
    query = rest.slice(mark + 1)
    rest = rest.slice(0, mark)
  }
  return { base: rest + fragment, query }
}

/** Decodes as an HTML form reads: `+` is a space and %XX a byte; what does not decode stays. */
function decoded(text: string): string {
  try {
    return decodeURIComponent(text.replace(/\+/g, ' '))
  } catch {
    return text
  }
}

/** The query string as rows, in order and with repeated names kept, values decoded. */
export function paramsFromUrl(address: string): Row[] {
  const { query } = splitQuery(address)
  if (!query) return []
  return query
    .split('&')
    .filter((part) => part !== '')
    .map((part) => {
      const at = part.indexOf('=')
      const name = at < 0 ? part : part.slice(0, at)
      const value = at < 0 ? '' : part.slice(at + 1)
      return { id: '', name: decoded(name), value: decoded(value), enabled: true }
    })
}

const KEPT = /[A-Za-z0-9*\-._~,[\]]/

/**
 * Encodes one name or value of a query by hand: `,` and the brackets stay as typed
 * (`include=author,comments`, `filter[id][in]=1,2`), a space is `+`, and a `{{token}}` keeps its
 * braces because the resolver looks for them.
 */
export function encodeQuery(text: string): string {
  let out = ''
  for (const byte of new TextEncoder().encode(text)) {
    const char = String.fromCharCode(byte)
    if (byte < 0x80 && KEPT.test(char)) out += char
    else if (char === ' ') out += '+'
    else out += '%' + byte.toString(16).toUpperCase().padStart(2, '0')
  }
  return out.replaceAll('%7B%7B', '{{').replaceAll('%7D%7D', '}}')
}

/** The address with the enabled, named rows written as its query, in their order. */
export function urlWithParams(base: string, rows: Row[]): string {
  const query = rows
    .filter((row) => row.enabled && row.name.trim() !== '')
    .map((row) => `${encodeQuery(row.name.trim())}=${encodeQuery(row.value)}`)
    .join('&')
  if (!query) return base
  const hash = base.indexOf('#')
  return hash < 0 ? `${base}?${query}` : `${base.slice(0, hash)}?${query}${base.slice(hash)}`
}

/**
 * The rows an edited address reads as, keeping what the previous rows contribute: the id of a row
 * still in the text (an open editor holds it) and every switched-off row, which the address never
 * mentions.
 */
export function keepRowIdentities(parsed: Row[], existing: Row[]): Row[] {
  const used = existing.map(() => false)
  const out: Row[] = []
  for (const row of parsed) {
    const at = existing.findIndex(
      (old, i) => !used[i] && old.name === row.name && old.value === row.value
    )
    if (at < 0) {
      out.push(row)
      continue
    }
    used[at] = true
    out.push({ ...row, id: existing[at].id, description: existing[at].description })
  }
  existing.forEach((old, i) => {
    if (!used[i] && !old.enabled) out.push(old)
  })
  return out
}

// A `:name` right after a slash, before the query or the fragment. The grammar is the one
// `internal/domain/request.go` reads: a name starts with a letter or underscore.
const PATH_SLOT = /\/:([A-Za-z_][A-Za-z0-9_-]*)/g

/** The names of an address's `:name` segments, in order and each once. */
export function pathNames(address: string): string[] {
  const end = address.search(/[?#]/)
  const path = end < 0 ? address : address.slice(0, end)
  const names: string[] = []
  for (const match of path.matchAll(PATH_SLOT)) {
    if (!names.includes(match[1])) names.push(match[1])
  }
  return names
}

/** One row per `:name`, keeping the value, description and id of a row whose name stayed. */
export function pathRows(address: string, existing: Row[]): Row[] {
  return pathNames(address).map((name) => {
    const old = existing.find((row) => row.name === name)
    return old ?? { id: mintRowId(), name, value: '', enabled: true }
  })
}

/** The draft with its rows brought in line with its address. */
export function followUrl(draft: Draft): Draft {
  const params = keepRowIdentities(paramsFromUrl(draft.url), draft.params ?? []).map((row) =>
    row.id ? row : { ...row, id: mintRowId() }
  )
  return { ...draft, params, pathParams: pathRows(draft.url, draft.pathParams ?? []) }
}

/** The draft with its parameter rows written into its address. */
export function writeParams(draft: Draft): Draft {
  return { ...draft, url: urlWithParams(splitQuery(draft.url).base, draft.params ?? []) }
}
