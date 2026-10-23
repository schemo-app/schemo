import { ref, type Ref } from 'vue'
import { tryOnScopeDispose, useEventListener } from '@vueuse/core'

/** Represents a single draggable tree node (collection or request) metadata. */
export interface DraggableRow {
  id: string
  kind: 'collection' | 'request'
  name: string
  method: string
  depth: number
  collectionId: string
  position: number
}

export type DropZone = 'before' | 'after' | 'inside'

export interface DropTarget {
  id: string
  zone: DropZone
}

const THRESHOLD = 4
const OPEN_AFTER = 500

interface TreeDragOptions<R extends DraggableRow> {
  rows: () => R[]
  elementOf: (id: string) => HTMLElement | null
  accepts: (dragged: R, collectionId: string) => boolean
  open: (id: string) => void
  move: (dragged: R, target: DropTarget) => void
}

/** Composable providing custom pointer-based drag-and-drop tree reordering logic. */
export function useTreeDrag<R extends DraggableRow = DraggableRow>(options: TreeDragOptions<R>) {
  const drag = ref(null) as Ref<{ row: R; target: DropTarget | null; x: number; y: number } | null>

  let pending: { row: R; x: number; y: number } | null = null
  let opening: { id: string; timer: number } | null = null

  function clearOpening(): void {
    if (!opening) return
    window.clearTimeout(opening.timer)
    opening = null
  }

  /** Evaluates drop zone ('before', 'after', 'inside') based on vertical cursor offset within element bounds. */
  function zoneOf(rect: DOMRect, y: number, draggable: boolean): DropZone | null {
    const quarter = rect.height / 4
    if (y < rect.top + quarter) return 'before'
    if (y > rect.bottom - quarter) return 'after'
    return draggable ? 'inside' : null
  }

  /** Resolves current drop target and zone under cursor coordinates. */
  function targetAt(x: number, y: number): DropTarget | null {
    const carrying = drag.value
    if (!carrying) return null

    for (const row of options.rows()) {
      if (row.id === carrying.row.id) continue
      const element = options.elementOf(row.id)
      if (!element) continue

      const rect = element.getBoundingClientRect()
      if (y < rect.top || y > rect.bottom) continue
      if (x < rect.left || x > rect.right) continue

      const into = row.kind === 'collection' && options.accepts(carrying.row, row.id)
      const zone = zoneOf(rect, y, into)
      if (!zone) return null

      return { id: row.id, zone }
    }
    return null
  }

  function onMove(e: PointerEvent): void {
    if (pending && !drag.value) {
      const travelled = Math.abs(e.clientX - pending.x) + Math.abs(e.clientY - pending.y)
      if (travelled < THRESHOLD) return
      drag.value = { row: pending.row, target: null, x: e.clientX, y: e.clientY }
      pending = null
      document.body.style.userSelect = 'none'
      document.body.style.cursor = 'grabbing'
    }
    if (!drag.value) return

    drag.value = { ...drag.value, x: e.clientX, y: e.clientY, target: targetAt(e.clientX, e.clientY) }

    // Auto-expand hovered collection when holding drag over it
    const target = drag.value.target
    if (opening && (!target || target.id !== opening.id)) clearOpening()
    if (target && target.zone !== 'inside') clearOpening()
    if (target && target.zone === 'inside' && !opening) {
      const id = target.id
      opening = { id, timer: window.setTimeout(() => options.open(id), OPEN_AFTER) }
    }
  }

  function endDrag(dropped: boolean): void {
    clearOpening()
    pending = null
    document.body.style.userSelect = ''
    document.body.style.cursor = ''

    const carrying = drag.value
    drag.value = null
    if (!dropped || !carrying || !carrying.target) return

    options.move(carrying.row, carrying.target)
  }

  let swallowed = false

  /** Returns true if a click event should be swallowed following a completed drag gesture. */
  function swallowClick(): boolean {
    const was = swallowed
    swallowed = false
    return was
  }

  function onUp(): void {
    swallowed = drag.value !== null
    endDrag(true)
  }

  function onCancel(): void {
    endDrag(false)
  }

  function onKeyDown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && (drag.value || pending)) endDrag(false)
  }

  /** Initiates drag sequence monitoring on pointerdown. */
  function press(e: PointerEvent, row: R): void {
    if (e.button !== 0) return
    pending = { row, x: e.clientX, y: e.clientY }
  }

  useEventListener(window, 'pointermove', onMove)
  useEventListener(window, 'pointerup', onUp)
  useEventListener(window, 'pointercancel', onCancel)
  useEventListener(window, 'keydown', onKeyDown)
  tryOnScopeDispose(() => endDrag(false))

  return { drag, press, endDrag, swallowClick }
}