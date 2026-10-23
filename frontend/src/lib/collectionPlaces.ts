import type { Collection } from '../../bindings/schemo/internal/domain'

/** Represents a flattened folder/collection destination for saving requests. */
export interface Place {
  id: string
  /** Hierarchical path from root to current collection (e.g. `['Root', 'Folder']`). */
  path: string[]
  /** Number of direct request items in this collection level. */
  requests: number
}

/** Recursively flattens a collection tree into a list of selectable places. */
export function collectionPlaces(tree: Collection[]): Place[] {
  const out: Place[] = []
  const walk = (collections: Collection[], path: string[]) => {
    for (const collection of collections) {
      const trail = [...path, collection.name]
      out.push({ id: collection.id, path: trail, requests: (collection.items ?? []).length })
      walk(collection.children ?? [], trail)
    }
  }
  walk(tree, [])
  return out
}