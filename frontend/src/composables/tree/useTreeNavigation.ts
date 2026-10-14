import { nextTick, watch } from 'vue'
import type { TreeRow as Row } from '../../lib/treeRows'
import { trailOf } from '../../lib/collectionTree'
import { useListKeys } from '../useListKeys'
import type { useCollectionsStore } from '../../stores/collections'

// Walking the tree without the mouse: the arrows and the keys that open and close, with the row
// brought on screen — opening every collection above it on the way. `walkable` includes closed ones.
export function useTreeNavigation(
  store: ReturnType<typeof useCollectionsStore>,
  opts: {
    rows: () => Row[]
    walkable: () => Row[]
    elementOf: (id: string) => HTMLElement | null
    pick: (row: Row) => Promise<void>
  }
) {
  // A row inside a closed collection would be selected nowhere.
  function reveal(row: Row) {
    const trail = trailOf(store.tree, row.id)
    if (!trail) return
    for (const ancestor of trail.ancestors) store.expanded[ancestor.id] = true
  }

  useListKeys({
    ids: () => opts.walkable().map((row) => row.id),
    current: () => store.selectedId,
    move: (id) => {
      const row = opts.walkable().find((r) => r.id === id)
      if (!row) return
      reveal(row)
      void opts.pick(row)
    },
    selected: '.tree-panel .panel-row.active',
    onSideKey: (key, id) => {
      const rows = opts.rows()
      const at = rows.findIndex((row) => row.id === id)
      if (at < 0) return false
      const row = rows[at]

      // Right opens a closed collection and steps into an open one.
      if (key === 'ArrowRight') {
        if (!row.expandable) return false
        if (row.expanded) {
          const child = rows[at + 1]
          if (child && child.depth > row.depth) void opts.pick(child)
          return true
        }
        store.toggleExpand(row.id)
        return true
      }

      if (row.expandable && row.expanded) {
        store.toggleExpand(row.id)
        return true
      }
      for (let i = at - 1; i >= 0; i--) {
        if (rows[i].depth < row.depth) {
          void opts.pick(rows[i])
          break
        }
      }
      return true
    },
  })

  // A row selected from elsewhere is opened up to and scrolled into view — after the tree is drawn.
  watch(
    () => store.selectedId,
    async (id) => {
      if (!id) return
      if (!opts.rows().some((row) => row.id === id)) {
        const trail = trailOf(store.tree, id)
        if (!trail) return
        for (const ancestor of trail.ancestors) store.expanded[ancestor.id] = true
        await nextTick()
      }
      opts.elementOf(id)?.scrollIntoView({ block: 'nearest' })
    },
    { immediate: true, flush: 'post' }
  )
}
