import { ref } from 'vue'
import { CancelError } from '@wailsio/runtime/cancellable'
import { describeFailure } from '../i18n'

// Cleared when the next call starts; a call the window cancelled stays silent.
export function useFailure() {
  const failure = ref('')

  /** Runs a call and answers what it answered; refused, the answer is undefined and `failure` says why. */
  async function attempt<T>(call: () => Promise<T>): Promise<T | undefined> {
    failure.value = ''
    try {
      return await call()
    } catch (error) {
      if (!(error instanceof CancelError)) failure.value = describeFailure(error)
      return undefined
    }
  }

  /** Runs a call that answers nothing, and says whether it went through. */
  async function run(call: () => Promise<unknown>): Promise<boolean> {
    return (await attempt(() => call().then(() => true))) === true
  }

  return { failure, attempt, run }
}
