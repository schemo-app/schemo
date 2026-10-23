import { describeFailure, t, type MessageKey } from '../i18n'
import { useToast } from '../composables/useToast'
import { CancelError } from '@wailsio/runtime/cancellable'

const toast = useToast()

/**
 * Wraps an async call expecting a result.
 * Displays an error toast on rejection and returns `undefined`.
 */
export async function callOrToast<T>(call: Promise<T>, key: MessageKey): Promise<T | undefined> {
  try {
    return await call
  } catch (error) {
    if (error instanceof CancelError) return undefined
    toast.show(t(key, { error: describeFailure(error) }), 'error')
    return undefined
  }
}

/**
 * Wraps an async action expecting no return value.
 * Displays an error toast on rejection and returns `true` on success or `false` on failure.
 */
export async function runOrToast(call: Promise<unknown>, key: MessageKey): Promise<boolean> {
  return await callOrToast(call.then(() => true), key) === true
}
