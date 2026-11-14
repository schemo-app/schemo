import { computed, ref } from 'vue'
import { MessageFormat } from '../../bindings/schemo/internal/domain'
import { Drafts } from '../../bindings/schemo/internal/request'
import type { RequestSource } from '../lib/requestSource'
import { callOrToast } from '../lib/calls'
import { isBrokenJson } from '../lib/json'

// The message a WebSocket request composes in its chip and the Send button sends once connected.
export function useLiveMessage(source: RequestSource) {
  const sending = ref(false)
  const format = computed(() => source.options?.messageFormat || MessageFormat.MessageJSON)
  const invalid = computed(() => format.value === MessageFormat.MessageJSON && isBrokenJson(source.body))
  const connected = computed(() => source.loading && source.record?.active === true)
  const ready = computed(() => connected.value && !!source.attemptId && !!source.body.length && !invalid.value && !sending.value)

  async function send() {
    const id = source.attemptId
    if (!ready.value || !id) return
    sending.value = true
    try {
      await callOrToast(Drafts.SendMessage(id, { data: source.body, format: format.value }), 'request.sendFailed')
    } finally {
      sending.value = false
    }
  }

  return { format, invalid, connected, ready, send }
}
