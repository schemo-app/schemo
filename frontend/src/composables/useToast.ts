import { ref, shallowRef } from 'vue'
import { refAutoReset } from '@vueuse/core'

/** A button in the toast, such as "Undo". */
export interface ToastAction {
  label: string
  run: () => void
}

const message = refAutoReset('', 5000)
const kind = ref<'info' | 'error'>('info')
const action = shallowRef<ToastAction | null>(null)

/** The one toast the window shows: a new one replaces the last and starts its own countdown. */
export function useToast() {
  function show(text: string, severity: 'info' | 'error' = 'info', act: ToastAction | null = null): void {
    message.value = text
    kind.value = severity
    action.value = act
  }

  function clear(): void {
    message.value = ''
    action.value = null
  }

  function runAction(): void {
    const act = action.value
    clear()
    act?.run()
  }

  return { message, kind, action, show, clear, runAction }
}
