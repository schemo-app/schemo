import { ref } from 'vue'
import { clamp, tryOnScopeDispose, useEventListener, useTimeoutFn } from '@vueuse/core'

/** A slot carried across its neighbours: how far the pointer has taken it, and where it would land. */
export interface SlotDrag {
  id: string
  idx: number
  target: number
  size: number
  offset: number
  moved: boolean
}

const THRESHOLD = 5
const SETTLE_QUIET = 20
const SETTLE_MS = 45

interface SlotDragOptions {
  axis: 'x' | 'y'
  ids: () => string[]
  /** How far one slot is along the axis: what a step of the carry is worth. */
  size: (id: string) => number
  /** The slot let go at an index of the list, which is not the one it was lifted from. */
  drop: (id: string, to: number) => Promise<unknown>
  cursor?: string
}

/**
 * The gesture of an ordered row of equal slots — the tab strip's tabs, a table's rows: a slot is
 * pressed, carried along the axis while the ones it passes slide out of the way, and let go at a
 * place in the list. The order itself is Go's: this only says where the slot landed.
 */
export function useSlotDrag({ axis, ids, size, drop, cursor = '' }: SlotDragOptions) {
  const drag = ref<SlotDrag | null>(null)
  const settle = ref<{ id: string; off: number } | null>(null)
  const noAnim = ref(false)
  const quiet = useTimeoutFn(() => (noAnim.value = false), SETTLE_QUIET, { immediate: false })
  const fade = useTimeoutFn(() => (settle.value = null), SETTLE_MS, { immediate: false })

  let held: SlotDrag | null = null
  let start = 0
  let count = 0

  const at = (e: PointerEvent) => (axis === 'x' ? e.clientX : e.clientY)

  // The selection and the cursor are the window's while a carry is on, and are put back by the
  // same function that ends it.
  function release(): void {
    held = null
    drag.value = null
    document.body.style.userSelect = ''
    document.body.style.cursor = ''
  }

  /** Starts the gesture under the pointer; the caller has already picked the slot out. */
  function press(e: PointerEvent, id: string, idx: number): void {
    if (e.button !== 0) return
    start = at(e)
    count = ids().length
    held = { id, idx, target: idx, size: size(id), offset: 0, moved: false }
  }

  function onMove(e: PointerEvent): void {
    if (!held) return
    const raw = at(e) - start
    if (!held.moved && Math.abs(raw) < THRESHOLD) return
    if (!held.moved) {
      held.moved = true
      document.body.style.userSelect = 'none'
      document.body.style.cursor = cursor
    }
    held.offset = clamp(raw, -held.idx * held.size, (count - 1 - held.idx) * held.size)
    held.target = clamp(Math.round(held.idx + held.offset / held.size), 0, count - 1)
    drag.value = { ...held }
  }

  async function onUp(): Promise<void> {
    const carried = held
    if (!carried?.moved) {
      release()
      return
    }
    held = null
    document.body.style.userSelect = ''
    document.body.style.cursor = ''
    // Where the slot still stands, past its new place: the settle is what closes that distance.
    const off = carried.offset - (carried.target - carried.idx) * carried.size
    noAnim.value = true
    try {
      if (carried.target !== carried.idx) await drop(carried.id, carried.target)
    } finally {
      // The gesture is released only now, not on the pointerup above: the picture it drew is exactly
      // where Go's order puts the slots, so holding it through the answer leaves nothing to jump.
      drag.value = null
      settle.value = { id: carried.id, off }
      quiet.start()
      fade.start()
    }
  }

  useEventListener(window, 'pointermove', onMove)
  useEventListener(window, 'pointerup', onUp)
  useEventListener(window, 'pointercancel', release)
  useEventListener(window, 'keydown', (e: KeyboardEvent) => {
    if (e.key === 'Escape' && held) release()
  })
  tryOnScopeDispose(release)

  /**
   * How far the slot at an index stands from its place: the carried one by the pointer, the ones it
   * passes a slot aside on the side it came from, and the one just let go by what is left to close.
   */
  function offsetOf(index: number, id: string): number {
    if (settle.value?.id === id) return settle.value.off
    const carried = drag.value
    if (!carried) return 0
    if (index === carried.idx) return carried.offset
    if (carried.idx < index && index <= carried.target) return -carried.size
    if (carried.target <= index && index < carried.idx) return carried.size
    return 0
  }

  return { drag, settle, noAnim, press, offsetOf }
}
