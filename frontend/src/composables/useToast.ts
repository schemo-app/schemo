import { ref } from 'vue'
import { refAutoReset } from '@vueuse/core'

const VISIBLE_MS = 5000

const message = refAutoReset('', VISIBLE_MS)
const kind = ref<'info' | 'error'>('info')

/** The one toast the window shows: a new one replaces the last and starts its own countdown. */
export function useToast() {
  function show(text: string, severity: 'info' | 'error' = 'info'): void {
    message.value = text
    kind.value = severity
  }

  function clear(): void {
    message.value = ''
  }

  return { message, kind, show, clear }
}
