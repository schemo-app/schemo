import { describeFailure, t, type MessageKey } from '../i18n'
import { useToast } from '../composables/useToast'
import { CancelError } from '@wailsio/runtime/cancellable'

const toast = useToast()

/** Awaits a call and toasts a rejection; resolves to `undefined` on failure. */
export async function callOrToast<T>(call: Promise<T>, key: MessageKey): Promise<T | undefined> {
  try {
    return await call
  } catch (error) {
    if (error instanceof CancelError) return undefined
    toast.show(t(key, { error: describeFailure(error) }), 'error')
    return undefined
  }
}

/** Awaits an action and toasts a rejection; resolves to whether it succeeded. */
export async function runOrToast(call: Promise<unknown>, key: MessageKey): Promise<boolean> {
  return await callOrToast(call.then(() => true), key) === true
}
