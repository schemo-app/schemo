import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import type { StreamMessage } from '../../bindings/schemo/internal/domain'
import { History, type StreamSnapshot } from '../../bindings/schemo/internal/request'
import type { RecordView } from '../lib/requestRecord'
import { protocolOf } from '../lib/protocol'

export function useStreamTranscript(
  activeId: Readonly<Ref<string | null>>,
  record: Readonly<Ref<RecordView | null>>,
) {
  const snapshot = ref<StreamSnapshot | null>(null)
  const now = ref(Date.now())
  let pending: AbortController | null = null

  async function poll() {
    const id = activeId.value
    if (!id || pending) return
    const request = new AbortController()
    pending = request
    now.value = Date.now()
    try {
      const previous = snapshot.value
      const state = await History.Live(id, previous?.count ?? 0).cancelOn(request.signal)
      if (request.signal.aborted || id !== activeId.value || !state) return
      if (previous?.count === state.count && previous.open === state.open) return
      const kept = previous?.messages?.filter(message => message.sequence >= state.first) ?? []
      snapshot.value = { ...state, messages: [...kept, ...(state.messages ?? [])] }
    } catch {
      // A disappearing connection is resolved by its final record.
    } finally {
      if (pending === request) pending = null
    }
  }

  function stopPolling() {
    pending?.abort()
    pending = null
    ticker.pause()
  }

  const ticker = useIntervalFn(poll, 250, { immediate: false })
  watch(activeId, id => {
    stopPolling()
    snapshot.value = null
    if (id) {
      void poll()
      ticker.resume()
    }
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(stopPolling)

  const transcript = computed(() => readTranscript(record.value))
  const connected = computed(() => snapshot.value?.open === true)
  const messages = computed(() => activeId.value ? snapshot.value?.messages ?? [] : transcript.value)
  const count = computed(() => activeId.value ? snapshot.value?.count ?? 0 : record.value?.messageCount ?? 0)
  return { connected, messages, count, now }
}

function readTranscript(record: RecordView | null): StreamMessage[] {
  if (!record?.responseBody || record.active) return []
  if (!protocolOf(record.protocol).stream?.accepts(record)) return []
  return record.responseBody.trimEnd().split('\n').flatMap(line => {
    try {
      const message = JSON.parse(line) as StreamMessage | null
      return typeof message?.sequence === 'number' && typeof message.data === 'string' ? [message] : []
    } catch {
      return []
    }
  })
}
