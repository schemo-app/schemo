import { ref } from 'vue'
import { CancelError } from '@wailsio/runtime/cancellable'
import { describeFailure } from '../i18n'

// The line of text under the rows a refusal is about. It is cleared when the next call starts, and
// a refusal is worded by the catalogue and put there. A call the window itself cancelled has
// nothing to say.
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
