import { nextTick, watch } from 'vue'
import type { TreeRow as Row } from '../lib/treeRows'
import { trailOf } from '../lib/collectionTree'
import type { useCollectionsStore } from '../stores/collections'
import { useEventListener } from '@vueuse/core'

// Walking the tree without the mouse: the arrows and the keys that open and close, with the row
// brought on screen — opening every collection above it on the way. `walkable` includes closed ones.
export function useTreeNavigation(
  collections: ReturnType<typeof useCollectionsStore>,
  opts: {
    rows: () => Row[]
    walkable: () => Row[]
    elementOf: (id: string) => HTMLElement | null
    pick: (row: Row) => Promise<void>
  }
) {
  // A row inside a closed collection would be selected nowhere.
  function reveal(row: Row) {
    const trail = trailOf(collections.tree, row.id)
    if (!trail) return
    for (const ancestor of trail.ancestors) collections.expanded[ancestor.id] = true
  }

  useListKeys({
    ids: () => opts.walkable().map((row) => row.id),
    current: () => collections.selectedId,
    move: (id) => {
      const row = opts.walkable().find((one) => one.id === id)
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
        collections.toggleExpand(row.id)
        return true
      }

      if (row.expandable && row.expanded) {
        collections.toggleExpand(row.id)
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
    () => collections.selectedId,
    async (id) => {
      if (!id) return
      if (!opts.rows().some((row) => row.id === id)) {
        const trail = trailOf(collections.tree, id)
        if (!trail) return
        for (const ancestor of trail.ancestors) collections.expanded[ancestor.id] = true
        await nextTick()
      }
      opts.elementOf(id)?.scrollIntoView({ block: 'nearest' })
    },
    { immediate: true, flush: 'post' }
  )
}

/** Interface defining list navigation callbacks and DOM selectors. */
interface ListKeys {
  /** Returns rendered element IDs in display order. */
  ids(): string[]
  /** Returns the currently selected element ID. */
  current(): string | null
  /** Moves selection to the specified ID. */
  move(id: string): void
  /** CSS selector targeting the currently selected DOM element. */
  selected: string
  /** Optional handler for horizontal arrow keys. Returning true consumes the event. */
  onSideKey?(key: 'ArrowLeft' | 'ArrowRight', id: string | null): boolean
}

/** Composable providing keyboard list navigation (Arrow keys) with automatic auto-scroll support. */
function useListKeys(keys: ListKeys): void {
  function onKeydown(e: KeyboardEvent) {
    if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return
    if (isTyping(e.target) || isCovered()) return

    const key = e.key
    if (key === 'ArrowLeft' || key === 'ArrowRight') {
      if (keys.onSideKey?.(key, keys.current()) && e.cancelable) e.preventDefault()
      return
    }
    if (key !== 'ArrowDown' && key !== 'ArrowUp') return

    const list = keys.ids()
    if (list.length === 0) return
    const at = keys.current() ? list.indexOf(keys.current() as string) : -1
    const next = neighbour(list, at, key === 'ArrowDown' ? 1 : -1)
    if (next < 0) return

    keys.move(list[next])
    if (e.cancelable) e.preventDefault()
  }

  // Scroll active element into view on selection change
  watch(
      () => keys.current(),
      () => {
        void nextTick(() => {
          document.querySelector<HTMLElement>(keys.selected)?.scrollIntoView({ block: 'nearest' })
        })
      }
  )

  useEventListener(window, 'keydown', onKeydown)
}

/** Calculates the adjacent target index in the list, or -1 if boundary is reached. */
function neighbour(list: string[], at: number, step: number): number {
  if (at < 0) return step > 0 ? 0 : list.length - 1
  const next = at + step
  return next < 0 || next >= list.length ? -1 : next
}

/** Checks whether the event originated from an active text input element. */
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el?.tagName) return false
  return el.isContentEditable || el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT'
}

/** Checks whether an overlay component (dialog, menu, popover) is currently open over the list. */
function isCovered(): boolean {
  return !!document.querySelector('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]')
}
