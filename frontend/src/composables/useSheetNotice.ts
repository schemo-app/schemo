import { ref } from 'vue'
import { describeFailure } from '../i18n'
import { CancelError } from '@wailsio/runtime'

// Shared reactive state for environment sheet notification messages
const notice = ref('')

/** Composable providing global notification state management for environment sheets. */
export function useSheetNotice() {
  /** Sets the active notice message. */
  function setNotice(message: string): void {
    notice.value = message
  }

  /** Clears the active notice message. */
  function clearNotice(): void {
    notice.value = ''
  }

  /**
   * Runs a call and words its refusal in the notice. The sheets' side of `callOrToast` and
   * `runOrToast`: a control that quietly does nothing looks the same as one that is broken.
   */
  async function withNotice(call: () => Promise<unknown>): Promise<boolean> {
    try {
      await call()
      clearNotice()
      return true
    } catch (error) {
      if (error instanceof CancelError) return false
      setNotice(describeFailure(error))
      return false
    }
  }

  return { notice, setNotice, clearNotice, withNotice }
}
