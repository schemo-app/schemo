import { onMounted, ref, watch, type Ref } from 'vue'
import { unrefElement, useMutationObserver, useResizeObserver } from '@vueuse/core'

/**
 * Composable for positioning a sliding selector indicator ("pill") behind the active option in a segmented control.
 * Measures DOM offsets to dynamically adjust transform coordinates and element dimensions.
 */
export function useSlidingPill(root: Readonly<Ref<HTMLElement | null>>, chosen: string, follow: () => unknown) {
  const style = ref<Record<string, string>>({})
  const ready = ref(false)

  // The root may be a component (reka's TabsList): its element is what gets measured.
  const element = () => unrefElement(root) ?? null

  /** Recalculates position and dimensions of the sliding pill relative to the active option. */
  function place(): void {
    const option = element()?.querySelector<HTMLElement>(chosen)
    if (!option) {
      style.value = { ...style.value, opacity: '0' }
      if (!ready.value) ready.value = true
      return
    }

    style.value = {
      transform: `translate(${option.offsetLeft}px, ${option.offsetTop}px)`,
      width: `${option.offsetWidth}px`,
      height: `${option.offsetHeight}px`,
      opacity: '1',
    }
    if (!ready.value) ready.value = true
  }

  watch([follow, root], place, { flush: 'post', immediate: true })

  useResizeObserver(element, place)
  // A choice made elsewhere (reka flips data-state) moves the pill; the pill's own restyling does not.
  useMutationObserver(
    element,
    (records) => {
      const pill = element()?.querySelector('.slide-mark')
      if (!records.every((r) => r.target === pill)) place()
    },
    { attributes: true, subtree: true, attributeFilter: ['class', 'data-state'] }
  )
  onMounted(place)

  return { style, ready }
}