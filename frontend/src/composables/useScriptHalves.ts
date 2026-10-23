import { ref } from 'vue'
import type { Scripts } from '../../bindings/schemo/internal/domain'

/**
 * The two halves of code a level runs around its requests, as the boxes hold them. A half can be
 * switched off without being emptied: the code stays, and what changes is whether it runs. `hold`
 * puts a level's scripts into the boxes, and nothing at all is the empty ones.
 */
export function useScriptHalves() {
  const pre = ref('')
  const post = ref('')
  const off = ref({ pre: false, post: false })

  function hold(scripts?: Scripts | null) {
    pre.value = scripts?.pre ?? ''
    post.value = scripts?.post ?? ''
    off.value = { pre: scripts?.preOff ?? false, post: scripts?.postOff ?? false }
  }

  return { pre, post, off, hold }
}
