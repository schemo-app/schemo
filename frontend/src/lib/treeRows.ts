import type { Collection } from '../../bindings/schemo/internal/domain'
import type { DropTarget } from '../composables/tree/useTreeDrag'
import { childrenOf, findCollection, requestCount } from './collectionTree'

/**
 * A row of the collection tree, flattened: the drawing walks a list, and the nesting is what the
 * indent says.
 */
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

/**
 * The tree as rows. `open` says whether a collection is drawn open; the filter's walk opens a path to
 * every match without touching what the user opened.
 */
export function flattenTree(tree: Collection[], open: (id: string) => boolean): TreeRow[] {
  return tree.flatMap((collection, at) => level(collection, '', at, 0, open))
}

// One collection and everything inside it, in Go's order: requests and collections alternate by
// position on one number line. A row is numbered by its place in the drawn level rather than by the
// number Go stores — the two agree after every move, and a drop names a place the window drew.
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
      method: child.node.method ?? 'GET',
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

/**
 * Whether a collection would take a dragged row. A collection inside itself or one of its own is a
 * ring, which Go refuses too, so it is not offered as a place.
 */
export function acceptsDrop(tree: Collection[], dragged: TreeRow, collectionId: string): boolean {
  if (dragged.kind === 'request') return true
  if (dragged.id === collectionId) return false
  const from = findCollection(tree, dragged.id)
  return !from || findCollection(from.children ?? [], collectionId) === null
}

/**
 * Where a drop lands, as Go is told it: a level and a place among its rows. Into a collection joins
 * its end; above or below a row takes that row's place, counted in the level as it is drawn now.
 */
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

/**
 * The level a new request next to a row would join: a collection's row is that collection, a
 * request's row names the level it sits in.
 */
export function levelOf(row: TreeRow): string {
  return row.kind === 'collection' ? row.id : row.collectionId
}

/**
 * "New collection", "New collection 2"… — a default typed over, that does not silently become a second
 * collection of the same name among `siblings`. It is the user's text from then on, not a label that follows
 * the language.
 */
export function nextCollectionName(
  siblings: Collection[],
  base: string,
  numbered: (n: number) => string
): string {
  const taken = new Set(siblings.map((c) => c.name))
  if (!taken.has(base)) return base
  for (let n = 2; ; n += 1) {
    const name = numbered(n)
    if (!taken.has(name)) return name
  }
}
