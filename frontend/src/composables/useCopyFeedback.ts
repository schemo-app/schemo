import { refAutoReset } from '@vueuse/core'
import { copyToClipboard } from '../lib/clipboard'

/** A copy action that marks what it copied and forgets the mark a moment later. A list that copies
 *  one cell of many passes that cell's key and marks only it; `true` is the plain, unkeyed answer. */
export function useCopyFeedback<K extends string = string>(ms = 1500) {
  const copied = refAutoReset<K | true | null>(null, ms)

  async function copy(text: string, key: K | true = true): Promise<boolean> {
    const ok = await copyToClipboard(text)
    if (ok) copied.value = key
    return ok
  }

  return { copied, copy }
}
