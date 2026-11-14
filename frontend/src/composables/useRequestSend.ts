import { computed, onScopeDispose, ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import type { RequestSource } from '../lib/requestSource'
import { useCommands } from './useCommands'
import { useToast } from './useToast'
import { useLiveMessage } from './useLiveMessage'
import { pickOperation, sendRequest } from '../lib/intents'
import { describeFailure, formatNumber, t } from '../i18n'
import { protocolOf } from '../lib/protocol'

export function useRequestSend(source: RequestSource) {
  const toast = useToast()
  const { hint } = useCommands()

  const elapsed = ref(0)
  let startedAt = 0
  const ticker = useIntervalFn(() => (elapsed.value = Date.now() - startedAt), 100, { immediate: false })

  watch(
    () => source.loading,
    (loading) => {
      ticker.pause()
      if (!loading) return
      elapsed.value = 0
      startedAt = Date.now()
      ticker.resume()
    }
  )

  const protocol = computed(() => protocolOf(source.protocol))
  const message = useLiveMessage(source)
  // A connected WebSocket sends the composed message from the button that connected it; the log
  // has its own button to disconnect.
  const messaging = computed(() => !!protocol.value.stream?.outgoing && message.connected.value)
  const label = computed(() => {
    const { stream, send } = protocol.value
    if (source.stopping && stream) return t('request.protocol.stopping')
    if (!source.loading) return t(send)
    if (messaging.value) return t('request.send')
    return t(stream?.stop ?? 'common.cancel')
  })

  const key = computed(() =>
    source.loading && !messaging.value ? `${formatNumber(elapsed.value, 0)} ${t('units.ms')}` : hint('send')
  )

  const disabled = computed(() => {
    if (source.stopping) return true
    if (messaging.value) return !message.ready.value
    return source.loading ? !source.attemptId : !source.url.trim()
  })

  async function send() {
    if (disabled.value || source.loading) return
    const missingOperation = protocol.value.operationRequired?.(source.options)
    if (missingOperation) {
      void pickOperation.trigger()
      toast.show(t(missingOperation))
      return
    }
    try {
      await source.send()
    } catch (error) {
      source.failSend()
      toast.show(t('request.sendFailed', { error: describeFailure(error) }), 'error')
    }
  }

  function onSend() {
    if (disabled.value) return
    if (messaging.value) void message.send()
    else if (source.loading) void source.cancel()
    else void send()
  }

  onScopeDispose(sendRequest.on(send).off)

  return { label, key, disabled, messaging, onSend }
}
