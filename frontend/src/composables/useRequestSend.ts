import { computed, ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import type { RequestSource } from '../lib/requestSource'
import { useCommands } from './useCommands'
import { useToast } from './useToast'
import { sendRequest } from '../lib/intents'
import { describeFailure, formatNumber, t } from '../i18n'

export function useRequestSend(source: RequestSource) {
  const toast = useToast()
  const { hint } = useCommands()

  const elapsed = ref(0)
  const ticker = useIntervalFn(() => (elapsed.value += 1), 100, { immediate: false })

  watch(
    () => source.loading,
    (loading) => {
      ticker.pause()
      if (!loading) return
      elapsed.value = 0
      ticker.resume()
    }
  )

  const label = computed(() => (source.loading ? t('common.cancel') : t('request.send')))

  const key = computed(() =>
    source.loading ? `${formatNumber(elapsed.value * 100, 0)} ${t('units.ms')}` : hint('send')
  )

  const disabled = computed(() => !source.loading && !source.url.trim())

  async function send() {
    try {
      await source.send()
    } catch (error) {
      source.failSend()
      toast.show(t('request.sendFailed', { error: describeFailure(error) }), 'error')
    }
  }

  function onSend() {
    if (source.loading) void source.cancel()
    else void send()
  }

  sendRequest.on(() => void send())

  return { label, key, disabled, send, onSend }
}
