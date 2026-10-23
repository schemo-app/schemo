import { onBeforeUnmount, ref } from 'vue'
import { useTimeoutFn } from '@vueuse/core'

/** How long typing pauses before what was typed is written. */
export const FLUSH_MS = 400

/**
 * An edit that is written after a pause, and at once when the box is left or goes away. `touch` says
 * something was typed, `flush` writes it now if anything is waiting, and `cancel` drops it — the
 * Escape of a box that keeps its own text.
 */
export function useDeferredWrite(write: () => Promise<unknown>, delay = FLUSH_MS) {
  const dirty = ref(false)
  const timer = useTimeoutFn(() => void flush(), delay, { immediate: false })

  /** Marks the text as changed; with `later` false the write waits for a `flush` of its own. */
  function touch(later = true) {
    dirty.value = true
    if (later) timer.start()
  }

  async function flush() {
    timer.stop()
    if (!dirty.value) return
    // Cleared before the write and not after it: a keystroke that arrives while the save travels is
    // an edit the next pause has to write again.
    dirty.value = false
    await write()
  }

  function cancel() {
    timer.stop()
    dirty.value = false
  }

  onBeforeUnmount(() => void flush())

  return { dirty, touch, flush, cancel }
}
