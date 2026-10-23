import { computed, ref, watch } from 'vue'
import { clamp, useResizeObserver } from '@vueuse/core'

const MIN_W = 120
const MAX_W = 220
const COMPACT_W = 32
const GAP = 2
const TOOLS_ROOM = 50
const OVERFLOW_ROOM = 56

/** Measures the strip and keeps recently used tabs visible when the row overflows. */
export function useTabRow(opts: {
  tabs: () => { id: string; compact: boolean }[]
  activeId: () => string
}) {
  const stripEl = ref<HTMLElement | null>(null)
  const scrollEl = ref<HTMLElement | null>(null)
  const stripW = ref(0)
  const frozenW = ref(0)
  const recent = ref<string[]>([])

  watch(opts.activeId, (id) => {
    if (id) recent.value = [id, ...recent.value.filter((used) => used !== id)]
  }, { immediate: true })

  const layout = computed(() => {
    const all = opts.tabs()
    const pins = all.filter((tab) => tab.compact)
    const movable = all.filter((tab) => !tab.compact)
    const room = Math.max(0, (stripW.value || 900) - TOOLS_ROOM - pins.length * (COMPACT_W + GAP) - (pins.length ? 19 : 0))
    const overflowing = movable.length * (MIN_W + GAP) > room
    const available = Math.max(0, room - (overflowing ? OVERFLOW_ROOM : 0))
    const capacity = Math.max(1, Math.floor(available / (MIN_W + GAP)))
    const priority = [opts.activeId(), ...recent.value, ...movable.map((tab) => tab.id)]
    const keep = new Set<string>()
    for (const id of priority) {
      if (keep.size >= capacity) break
      if (movable.some((tab) => tab.id === id)) keep.add(id)
    }
    const visible = overflowing ? movable.filter((tab) => keep.has(tab.id)) : movable
    return {
      ids: new Set([...pins, ...visible].map((tab) => tab.id)),
      hidden: movable.length - visible.length,
      width: visible.length ? clamp(Math.floor(available / visible.length) - GAP, 72, MAX_W) : 0,
    }
  })

  const tabW = computed(() => frozenW.value || layout.value.width)
  const tiny = computed(() => tabW.value < 60)
  const compact = computed(() => tabW.value < 96)
  const visibleIds = computed(() => layout.value.ids)
  const hiddenCount = computed(() => layout.value.hidden)

  useResizeObserver(stripEl, () => {
    const width = stripEl.value?.clientWidth ?? 0
    if (Math.abs(width - stripW.value) > 2) stripW.value = width
  })

  watch(() => opts.tabs().length, (count, before) => {
    if (count > before) frozenW.value = 0
  })

  const hold = () => (frozenW.value = tabW.value)
  const release = () => (frozenW.value = 0)

  return { stripEl, scrollEl, tabW, tiny, compact, visibleIds, hiddenCount, hold, release }
}
