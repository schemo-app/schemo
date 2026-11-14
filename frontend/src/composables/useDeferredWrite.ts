import { onBeforeUnmount, ref } from 'vue'
import { useTimeoutFn } from '@vueuse/core'

/** How long typing pauses before what was typed is written. */
export const FLUSH_MS = 400

/** Writes after a pause, or at once on `flush` and unmount; `cancel` drops a pending write. */
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
