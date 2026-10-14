import { nextTick, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useResizeObserver } from '@vueuse/core'

// A box as wide as its text is a fraction of a pixel wide, and what is laid out after it stands on
// that fraction, which blurs a glyph. The box is held to the next whole pixel through
// `--whole-width`, which its stylesheet reads as a minimum; `redo` is whatever changes its text.
export function useWholeWidth(target: MaybeRefOrGetter<HTMLElement | null | undefined>, redo: () => unknown) {
  function hold() {
    const box = toValue(target)
    if (!box) return
    box.style.setProperty('--whole-width', '0px')
    const natural = box.getBoundingClientRect().width
    box.style.setProperty('--whole-width', `${Math.ceil(natural - 0.01)}px`)
  }

  useResizeObserver(target, hold)
  watch(redo, () => void nextTick(hold))
}
