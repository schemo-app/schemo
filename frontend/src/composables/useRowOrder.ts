import { computed, type Ref } from 'vue'
import { useSlotDrag } from './useSlotDrag'

/** What a row wears while a drag is on: the class the stylesheet dresses, and how far off its slot
 *  it stands — the one number that dress reads. */
export interface RowState {
  class: Record<string, boolean>
  style: Record<string, string>
}

const STILL: RowState = { class: {}, style: {} }

interface RowOrderOptions {
  /** The rows of one table, in the order they stand; an id is all a drag needs of a row. */
  rows: () => { id: string }[]
  table: Ref<HTMLElement | null>
  move: (id: string, position: number) => Promise<unknown>
}

/**
 * The order of a kv table, dragged: the gesture comes from the handle a row shows on hover, and the
 * table draws which row the handle belongs to and what each row wears on the way. The parameters,
 * the headers and the fields of a form body all stand on this.
 */
export function useRowOrder({ rows, table, move }: RowOrderOptions) {
  const { drag, settle, noAnim, press, offsetOf } = useSlotDrag({
    axis: 'y',
    cursor: 'grabbing',
    ids: () => rows().map((row) => row.id),
    size: (id) => table.value?.querySelector<HTMLElement>(`[data-kv="${id}"]`)?.offsetHeight ?? 0,
    drop: move,
  })

  const states = computed<RowState[]>(() =>
    rows().map((row, index) => {
      const settling = settle.value?.id === row.id
      const carried = !settling && drag.value?.idx === index
      const off = offsetOf(index, row.id)
      if (!settling && !carried && off === 0) return STILL
      return {
        class: {
          'row-settling': settling,
          'no-anim': settling && noAnim.value,
          'row-carried': carried,
          'row-shifted': !settling && !carried,
        },
        style: { '--row-off': `${off}px` },
      }
    })
  )

  return { press, states }
}
