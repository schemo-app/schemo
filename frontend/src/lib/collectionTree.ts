import type { Collection, CollectionNode } from '../../bindings/schemo/internal/domain'

/** Represents a single tree node row: either a request item or a sub-collection. */
export type Child =
    | { kind: 'request'; node: CollectionNode }
    | { kind: 'collection'; collection: Collection }

/**
 * Merges items and child collections of a collection into a single array
 * ordered by their `position` property using a two-pointer merge pass.
 */
export function childrenOf(collection: Collection): Child[] {
  const requests = collection.items ?? []
  const nested = collection.children ?? []

  const out: Child[] = []
  let request = 0
  let child = 0
  while (request < requests.length || child < nested.length) {
    if (
        child >= nested.length ||
        (request < requests.length && requests[request].position <= nested[child].position)
    ) {
      out.push({ kind: 'request', node: requests[request] })
      request += 1
      continue
    }
    out.push({ kind: 'collection', collection: nested[child] })
    child += 1
  }
  return out
}

/** Breadcrumb path to a node/collection, specifying its parents in top-down order. */
export interface Trail {
  ancestors: Collection[]
  collection: Collection | null
  node: CollectionNode | null
}

/** Returns the parent collection that owns the target item or sub-collection. */
export function holderOf(trail: Trail | null): Collection | null {
  if (!trail) return null
  return trail.collection ?? trail.ancestors[trail.ancestors.length - 1] ?? null
}

/** Recursively finds a request node by ID in the collection tree. */
export function findNode(tree: Collection[], id: string | null): CollectionNode | null {
  if (!id) return null
  for (const collection of tree) {
    const inLevel = (collection.items ?? []).find((node) => node.id === id)
    if (inLevel) return inLevel
    const below = findNode(collection.children ?? [], id)
    if (below) return below
  }
  return null
}

/** Recursively finds a collection by ID in the collection tree. */
export function findCollection(tree: Collection[], id: string | null): Collection | null {
  if (!id) return null
  for (const collection of tree) {
    if (collection.id === id) return collection
    const below = findCollection(collection.children ?? [], id)
    if (below) return below
  }
  return null
}

/** Finds the path (`Trail`) to a collection or request node by ID. */
export function trailOf(tree: Collection[], id: string | null): Trail | null {
  if (!id) return null

  const walk = (collections: Collection[], ancestors: Collection[]): Trail | null => {
    for (const collection of collections) {
      if (collection.id === id) return { ancestors, collection, node: null }

      const node = (collection.items ?? []).find((row) => row.id === id)
      if (node) return { ancestors: [...ancestors, collection], collection: null, node }

      const found = walk(collection.children ?? [], [...ancestors, collection])
      if (found) return found
    }
    return null
  }
  return walk(tree, [])
}

/** Returns total number of request nodes within a collection and all its sub-collections. */
export function requestCount(collection: Collection): number {
  return childrenOf(collection).reduce(
      (total, child) => total + (child.kind === 'request' ? 1 : requestCount(child.collection)),
      0
  )
}

/**
 * Filters the collection tree by matching item or collection names against the query string.
 * Retains empty ancestor paths so matched nodes remain visible in context.
 */
export function filterTree(tree: Collection[], query: string): Collection[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return tree

  const kept = (collection: Collection): boolean =>
      collection.name.toLowerCase().includes(needle) ||
      (collection.items ?? []).length > 0 ||
      (collection.children ?? []).length > 0

  const keep = (collection: Collection): Collection => {
    const items = (collection.items ?? []).filter((node) => node.name.toLowerCase().includes(needle))
    const children = (collection.children ?? []).map(keep).filter(kept)
    return { ...collection, items, children }
  }

  return tree.map(keep).filter(kept)
}