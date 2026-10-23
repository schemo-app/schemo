import { ref, toValue, type MaybeRefOrGetter, type Ref } from 'vue'
import { clamp, tryOnScopeDispose, useEventListener } from '@vueuse/core'
import { unitScale } from '../lib/units'

type Axis = 'x' | 'y'
type Side = 'left' | 'right' | 'top' | 'bottom'

interface ResizeOptions {
  // Which way the size runs, and the edge the handle closes on: a panel standing on the right of the
  // window grows as its inner edge is dragged left, and a pane at the top grows downward.
  axis: Axis
  side: MaybeRefOrGetter<Side>
  min: number
  // The cap, as a function when it follows the window: a taller window may allow a taller pane.
  max: MaybeRefOrGetter<number>
  // The size to count from, when it is not the ref's own: a pane measures itself as the drag starts,
  // and the ref is where the drag is written.
  from?: () => number
  // What the drag leaves behind: the layout remembers the size it was let go at.
  onEnd?: () => void
}

/** Dragging an edge to size a panel, along either axis: the size follows the pointer in `size`. */
export function useResizeDrag(
  size: Ref<number>,
  { axis, side, min, max, from, onEnd }: ResizeOptions
) {
  const dragging = ref(false)
  // `scale` is how many of the layout's own units one pixel of the screen is worth: a pointer that
  // crossed ten pixels of a large interface moved fewer of them, and the size is counted in them.
  let start: { at: number; size: number; scale: number } | null = null

  function onMove(e: MouseEvent) {
    if (!start) return
    const edge = toValue(side)
    const grows = edge === 'left' || edge === 'top' ? 1 : -1
    const moved = ((axis === 'x' ? e.clientX : e.clientY) - start.at) / start.scale
    size.value = clamp(start.size + moved * grows, min, toValue(max))
  }

  // The cursor and the selection are the window's while a drag is on, so they are put back by the
  // same function that ends it — by the release, and by the window going away with a drag open.
  function release() {
    start = null
    dragging.value = false
    document.body.style.cursor = ''
    document.body.style.userSelect = ''
  }

  function startDrag(e: MouseEvent) {
    start = {
      at: axis === 'x' ? e.clientX : e.clientY,
      size: from ? from() : size.value,
      scale: unitScale(),
    }
    dragging.value = true
    document.body.style.cursor = axis === 'x' ? 'col-resize' : 'row-resize'
    document.body.style.userSelect = 'none'
  }

  useEventListener(window, 'mousemove', onMove)
  useEventListener(window, 'mouseup', () => {
    if (!start) return
    release()
    onEnd?.()
  })
  // A window that goes away mid-drag: the cursor is put back, and the layout is not written — the
  // drag never ended, so there is no size it was let go at.
  tryOnScopeDispose(release)

  return { startDrag, dragging }
}
