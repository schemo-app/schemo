import { computed, ref, watch } from 'vue'
import type { RequestSource } from '../../lib/requestSource'
import { missingPlace } from '../../lib/missingPlace'
import { useEnvironmentsStore } from '../../stores/environments'
import { useRequestEnvironment } from './useRequestEnvironment'
import { useToast } from '../useToast'
import { describeFailure, useMessages } from '../../i18n'

export function useMissingVars(source: RequestSource) {
  const envStore = useEnvironmentsStore()
  const toast = useToast()
  const { t } = useMessages()

  const open = ref(false)
  const missing = computed(() => source.missing)
  const marked = computed(() => missing.value.length > 0)
  const rows = computed(() =>
    missing.value.map((name) => ({
      name,
      label: `{{${name}}}`,
      place: missingPlace(source, name, t),
    }))
  )

  watch(
    () => [envStore.envState, source.environmentId],
    () => void source.refreshPreview()
  )

  const { stale, scopeId, scopeReadonly } = useRequestEnvironment(source)

  const title = computed(() => {
    if (stale.value) return t('request.pinGone')
    if (scopeId.value === null) return t('request.chooseEnvFirst')
    if (scopeReadonly.value) return t('request.envReadOnly')
    return undefined
  })

  const mayAdd = computed(() => scopeId.value !== null && !scopeReadonly.value && !stale.value)

  function add(names: string[]) {
    const envId = scopeId.value
    if (envId === null) return
    if (!scopeReadonly.value) {
      for (const name of names) {
        void envStore.addVar(envId, { name }).catch((error) => {
          toast.show(t('request.createFailed', { error: describeFailure(error) }), 'error')
        })
      }
    }
    open.value = false
    envStore.openSheet({ envId, varName: names[0] ?? '' })
  }

  return { open, missing, marked, rows, title, mayAdd, add }
}
