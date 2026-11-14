import { ref } from 'vue'
import type { Scripts } from '../../bindings/schemo/internal/domain'

/** The pre- and post-request script boxes; a half can be switched off without losing its code. */
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
