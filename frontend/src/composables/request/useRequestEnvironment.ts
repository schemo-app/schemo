import { computed } from 'vue'
import type { RequestSource } from '../../lib/requestSource'
import { useEnvironmentsStore } from '../../stores/environments'
import { formatNumber, useMessages } from '../../i18n'

/** Composable for resolving and overriding the active environment scope for a request. */
export function useRequestEnvironment(source: RequestSource) {
  const envStore = useEnvironmentsStore()
  const { t } = useMessages()

  /** Display name of the globally active window environment. */
  const windowName = computed(() => envStore.activeEnvironment?.name ?? t('titlebar.noEnvironment'))

  /** True if the request has a pinned environment override ID. */
  const pinned = computed(() => Boolean(source.environmentId))

  /** Pinned environment object or null if unset/unmatched. */
  const pinnedEnv = computed(
      () => envStore.environments.find((e) => e.id === source.environmentId) ?? null
  )

  /** True if the request points to a pinned environment ID that no longer exists. */
  const stale = computed(() => pinned.value && pinnedEnv.value === null)

  /** Display label for the environment selector button. */
  const label = computed(() => (pinned.value ? (pinnedEnv.value?.name ?? windowName.value) : windowName.value))

  /** Active environment name used for variable resolution diagnostics. */
  const answersIn = computed(() =>
      pinned.value ? pinnedEnv.value?.name : envStore.activeEnvironment?.name
  )

  /** Target environment ID for reading/writing request variables. */
  const scopeId = computed(() => (pinned.value ? (pinnedEnv.value?.id ?? null) : envStore.activeId))

  /** True if the effective request environment scope is read-only. */
  const scopeReadonly = computed(() =>
      pinned.value ? Boolean(pinnedEnv.value?.readonly) : Boolean(envStore.activeEnvironment?.readonly)
  )

  /** Formatted options list for the environment selector dropdown. The colour and the lock are the
   * row's own, drawn as the window's own environment popover draws them. The row that follows the
   * window says so in its name: the word for it stands where the count stands, and the count is what
   * the row it is beside carries. */
  const options = computed(() => [
    {
      value: '',
      name: `${t('request.envFollow')} · ${windowName.value}`,
      vars: '',
      color: '',
      readonly: false,
    },
    ...envStore.environments.map((e) => ({
      value: e.id,
      name: e.name,
      vars: formatNumber(e.vars.length),
      color: e.color ?? '',
      readonly: Boolean(e.readonly),
    })),
  ])

  /** Selects a new environment override ID for the request. */
  function choose(id: string): void {
    if (id === source.environmentId) return
    void source.setEnvironmentOverride(id)
  }

  return { windowName, pinned, stale, label, answersIn, scopeId, scopeReadonly, options, choose }
}