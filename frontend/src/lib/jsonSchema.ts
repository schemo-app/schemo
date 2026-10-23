import type { JsonApiDocument } from './jsonapi'
import { allResources, linkHref, relIdentifiers } from './jsonapi'

/** Metadata describing an outgoing JSON:API relationship target. */
export interface RelInfo {
  name: string
  targetType: string
  many: boolean
  inDoc: boolean
  relatedUrl: string
}

/** Metadata describing an incoming JSON:API relationship reference. */
export interface IncomingInfo {
  fromType: string
  rel: string
}

/** Aggregate metadata for a JSON:API resource type present in a document. */
export interface TypeInfo {
  type: string
  label: string
  count: number
  attributes: string[]
  rels: RelInfo[]
  incoming: IncomingInfo[]
}

/** Capitalizes resource type name for UI display. */
export function capitalizeType(type: string): string {
  return type.charAt(0).toUpperCase() + type.slice(1)
}

/** Constructs type metadata schema including relationships, attributes, and incoming links from a JSON:API document. */
export function buildTypeInfos(doc: JsonApiDocument): TypeInfo[] {
  const all = allResources(doc)
  const presentTypes = new Set(all.map((resource) => resource.type))

  const map = new Map<string, TypeInfo>()
  for (const resource of all) {
    let t = map.get(resource.type)
    if (!t) {
      t = { type: resource.type, label: capitalizeType(resource.type), count: 0, attributes: [], rels: [], incoming: [] }
      map.set(resource.type, t)
    }
    t.count++
    if (resource.attributes) {
      for (const key of Object.keys(resource.attributes)) {
        if (!t.attributes.includes(key)) t.attributes.push(key)
      }
    }
  }

  for (const resource of all) {
    const t = map.get(resource.type)
    if (!t || !resource.relationships) continue
    for (const [name, rel] of Object.entries(resource.relationships)) {
      const many = Array.isArray(rel.data)
      const relatedUrl = linkHref(rel.links?.related)
      for (const identifier of relIdentifiers(rel)) {
        const existing = t.rels.find((relationship) => relationship.name === name && relationship.targetType === identifier.type)
        if (existing) {
          existing.many = existing.many || many
          if (!existing.relatedUrl && relatedUrl) existing.relatedUrl = relatedUrl
        } else {
          t.rels.push({ name, targetType: identifier.type, many, inDoc: presentTypes.has(identifier.type), relatedUrl })
        }
        const target = map.get(identifier.type)
        if (target && !target.incoming.some((link) => link.fromType === resource.type && link.rel === name)) {
          target.incoming.push({ fromType: resource.type, rel: name })
        }
      }
    }
  }

  return Array.from(map.values()).sort((a, b) => a.label.localeCompare(b.label))
}