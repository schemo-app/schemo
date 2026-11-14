import type { Collection } from '../../bindings/schemo/internal/domain'
import type { DropTarget } from '../composables/useTreeDrag'
import { requestLabel } from './protocol'
import { childrenOf, findCollection, requestCount } from './collectionTree'

/** A flattened tree row; nesting is the indent. */
export interface TreeRow {
  id: string
  kind: 'collection' | 'request'
  name: string
  method: string
  count: number
  depth: number
  expandable: boolean
  expanded: boolean
  // The level the row is dropped into and stands in: a top-level collection stands in none, the empty
  // id Go reads as "the top level". A collection's own row is not inside itself.
  collectionId: string
  // Where the row stands among the rows of that level, requests and collections counted together —
  // the index a drop names.
  position: number
}

/** The tree as rows; `open` says which collections are expanded. */
export function flattenTree(tree: Collection[], open: (id: string) => boolean): TreeRow[] {
  return tree.flatMap((collection, at) => level(collection, '', at, 0, open))
}

// Rows are numbered by their place in the drawn level, which matches Go's positions after every move.
function level(
  collection: Collection,
  holderId: string,
  position: number,
  depth: number,
  open: (id: string) => boolean
): TreeRow[] {
  const expanded = open(collection.id)
  const children = childrenOf(collection)
  const rows: TreeRow[] = [
    {
      id: collection.id,
      kind: 'collection',
      name: collection.name,
      method: '',
      count: requestCount(collection),
      depth,
      expandable: children.length > 0,
      expanded,
      collectionId: holderId,
      position,
    },
  ]
  if (!expanded) return rows

  children.forEach((child, at) => {
    if (child.kind === 'collection') {
      rows.push(...level(child.collection, collection.id, at, depth + 1, open))
      return
    }
    rows.push({
      id: child.node.id,
      kind: 'request',
      name: child.node.name,
      method: requestLabel(child.node),
      count: 0,
      depth: depth + 1,
      expandable: false,
      expanded: false,
      collectionId: collection.id,
      position: at,
    })
  })
  return rows
}

/** Whether a collection takes the dragged row; into itself or a descendant is refused. */
export function acceptsDrop(tree: Collection[], dragged: TreeRow, collectionId: string): boolean {
  if (dragged.kind === 'request') return true
  if (dragged.id === collectionId) return false
  const from = findCollection(tree, dragged.id)
  return !from || findCollection(from.children ?? [], collectionId) === null
}

/** Where a drop lands: into a collection appends, above or below takes that row's place. */
export function dropPlace(
  tree: Collection[],
  rows: TreeRow[],
  dragged: TreeRow,
  target: DropTarget
): { collectionId: string; position: number } | null {
  if (target.zone === 'inside') {
    const collection = findCollection(tree, target.id)
    return collection ? { collectionId: collection.id, position: childrenOf(collection).length } : null
  }

  const at = rows.find((row) => row.id === target.id)
  if (!at) return null

  // A request cannot stand at the top level: beside a top-level collection, the line above its row is
  // that collection's head and the line below its tail.
  if (at.collectionId === '' && dragged.kind === 'request') {
    const root = findCollection(tree, at.id)
    if (!root) return null
    return { collectionId: root.id, position: target.zone === 'before' ? 0 : childrenOf(root).length }
  }

  return { collectionId: at.collectionId, position: at.position + (target.zone === 'after' ? 1 : 0) }
}

/** The collection a new request beside this row would join. */
export function levelOf(row: TreeRow): string {
  return row.kind === 'collection' ? row.id : row.collectionId
}

/** "New collection", "New collection 2"… unique among `siblings`. */
export function nextCollectionName(
  siblings: Collection[],
  base: string,
  numbered: (n: number) => string
): string {
  const taken = new Set(siblings.map((sibling) => sibling.name))
  if (!taken.has(base)) return base
  for (let n = 2; ; n += 1) {
    const name = numbered(n)
    if (!taken.has(name)) return name
  }
}
