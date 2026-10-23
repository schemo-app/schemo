export interface ResourceIdentifier {
  type: string
  id: string
  meta?: unknown
}

export type LinkValue = string | { href?: string; meta?: unknown }

export interface Links {
  [name: string]: LinkValue | null | undefined
}

export interface Relationship {
  data?: ResourceIdentifier | ResourceIdentifier[] | null
  links?: Links
  meta?: unknown
}

export interface Resource {
  type: string
  id: string
  attributes?: Record<string, unknown>
  relationships?: Record<string, Relationship>
  links?: Links
  meta?: unknown
}

export interface JsonApiDocument {
  jsonapi?: unknown
  meta?: unknown
  links?: Links
  data?: Resource | Resource[] | null
  included?: Resource[]
  errors?: unknown[]
}

/** Generates a compound key (`type/id`) for indexing resources. */
export function resourceKey(type: string, id: string): string {
  return `${type}/${id}`
}

/** Extracts primary resources from a JSON:API document `data` field as an array. */
export function dataResources(doc: JsonApiDocument): Resource[] {
  const d = doc.data
  if (d == null) return []
  if (Array.isArray(d)) return d
  return [d]
}

/** Every resource a document carries: the primary data and whatever it included. */
export function allResources(doc: JsonApiDocument): Resource[] {
  return [...dataResources(doc), ...(doc.included ?? [])]
}

/** Creates a Map index of all primary and included resources keyed by `type/id`. */
export function buildResourceIndex(doc: JsonApiDocument): Map<string, Resource> {
  const index = new Map<string, Resource>()
  for (const resource of allResources(doc)) index.set(resourceKey(resource.type, resource.id), resource)
  return index
}

/** Normalizes relationship data references into an array of ResourceIdentifiers. */
export function relIdentifiers(rel: Relationship): ResourceIdentifier[] {
  const d = rel.data
  if (d == null) return []
  if (Array.isArray(d)) return d
  return [d]
}

/** Extracts string URL from a raw LinkValue or object. */
export function linkHref(link: LinkValue | null | undefined): string {
  if (typeof link === 'string') return link
  if (link && typeof link.href === 'string') return link.href
  return ''
}

/** Resolves a human-readable display label for a resource based on common attribute names. */
export function resourceLabel(r: Resource): string {
  const attrs = r.attributes
  if (!attrs) return `${r.type}/${r.id}`
  for (const key of ['title', 'name', 'label', 'username', 'email', 'slug', 'code', 'key']) {
    const v = attrs[key]
    if (typeof v === 'string' && v) return v
  }
  for (const value of Object.values(attrs)) {
    if (typeof value === 'string' && value) return value
  }
  return `${r.type}/${r.id}`
}

/** Evaluates whether a resource's attributes, type, or ID match a lowercased search query. */
export function resourceMatchesQuery(r: Resource, q: string): boolean {
  if (resourceLabel(r).toLowerCase().includes(q)) return true
  if (r.type.toLowerCase().includes(q) || String(r.id).toLowerCase().includes(q)) return true
  for (const [k, v] of Object.entries(r.attributes ?? {})) {
    if (k.toLowerCase().includes(q)) return true
    const vs = v === null ? 'null' : typeof v === 'object' ? JSON.stringify(v) : String(v)
    if (vs.toLowerCase().includes(q)) return true
  }
  return false
}

/** Extracts declared JSON:API spec version from document metadata. */
export function documentVersion(doc: JsonApiDocument | null): string {
  const declared = (doc?.jsonapi ?? null) as { version?: unknown } | null
  return typeof declared?.version === 'string' ? declared.version : ''
}

/** Type guard checking if an unknown object conforms to JSON:API document structure. */
export function isJsonApi(obj: unknown): boolean {
  if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) return false
  const d = obj as Record<string, unknown>
  if ('jsonapi' in d) return true
  if ('included' in d) return true
  const data = d.data
  if (data == null) return false
  const arr = Array.isArray(data) ? data : [data]
  if (arr.length === 0) return false
  return arr.every((one) => isResourceLike(one))
}

/** Internal validator for JSON:API Resource object shape. */
function isResourceLike(r: unknown): boolean {
  return (
      typeof r === 'object' &&
      r !== null &&
      typeof (r as Record<string, unknown>).type === 'string' &&
      typeof (r as Record<string, unknown>).id === 'string'
  )
}