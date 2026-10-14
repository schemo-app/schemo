import { ref, watch, type Ref } from 'vue'
import { createSharedComposable, useEventListener } from '@vueuse/core'

// One listener for the whole window: every guard asks the same question.
const pointer = { x: -1, y: -1 }

function track(e: PointerEvent) {
  pointer.x = e.clientX
  pointer.y = e.clientY
}

// One listener for however many hints are mounted, and only while any of them is.
const trackPointer = createSharedComposable(() =>
  useEventListener(window, 'pointermove', track, { capture: true, passive: true })
)

function pointerInside(node: HTMLElement): boolean {
  const r = node.getBoundingClientRect()
  return pointer.x >= r.left && pointer.x <= r.right && pointer.y >= r.top && pointer.y <= r.bottom
}

// Whether the pointer arrived at `el` by moving rather than `el` appearing under it (a popover
// opening where the user clicked). Starts unarmed then, and arms on pointerleave.
export function useHoverArrival(el: Readonly<Ref<HTMLElement | null>>) {
  const isArmed = ref(false)

  trackPointer()

  // Post-flush: a box means nothing before layout.
  watch(
      el,
      (node) => {
        isArmed.value = node !== null && !pointerInside(node)
      },
      { flush: 'post' }
  )

  return isArmed
}
