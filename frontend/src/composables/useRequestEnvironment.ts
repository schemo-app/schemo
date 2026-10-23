import { computed } from 'vue'
import type { RequestSource } from '../lib/requestSource'
import { useEnvironmentsStore } from '../stores/environments'
import { formatNumber, t } from '../i18n'

/** Composable for resolving and overriding the active environment scope for a request. */
export function useRequestEnvironment(source: RequestSource) {
  const environments = useEnvironmentsStore()

  /** Display name of the globally active window environment. */
  const windowName = computed(() => environments.activeEnvironment?.name ?? t('titlebar.noEnvironment'))

  /** True if the request has a pinned environment override ID. */
  const pinned = computed(() => Boolean(source.environmentId))

  /** Pinned environment object or null if unset/unmatched. */
  const pinnedEnv = computed(
      () => environments.environments.find((environment) => environment.id === source.environmentId) ?? null
  )

  /** True if the request points to a pinned environment ID that no longer exists. */
  const stale = computed(() => pinned.value && pinnedEnv.value === null)

  /** Display label for the environment selector button. */
  const label = computed(() => (pinned.value ? (pinnedEnv.value?.name ?? windowName.value) : windowName.value))

  /** Active environment name used for variable resolution diagnostics. */
  const answersIn = computed(() =>
      pinned.value ? pinnedEnv.value?.name : environments.activeEnvironment?.name
  )

  /** Target environment ID for reading/writing request variables. */
  const scopeId = computed(() => (pinned.value ? (pinnedEnv.value?.id ?? null) : environments.activeId))

  /** True if the effective request environment scope is read-only. */
  const scopeReadonly = computed(() =>
      pinned.value ? Boolean(pinnedEnv.value?.readonly) : Boolean(environments.activeEnvironment?.readonly)
  )

  /**
   * Formatted options list for the environment selector dropdown. The colour and the lock are the
   * row's own, drawn as the window's environment popover draws them. The row that follows the window
   * says so in its name: the word for it stands where the count stands.
   */
  const options = computed(() => [
    {
      value: '',
      name: `${t('request.envFollow')} · ${windowName.value}`,
      vars: '',
      color: '',
      readonly: false,
    },
    ...environments.environments.map((environment) => ({
      value: environment.id,
      name: environment.name,
      vars: formatNumber(environment.vars.length),
      color: environment.color ?? '',
      readonly: Boolean(environment.readonly),
    })),
  ])

  /** Selects a new environment override ID for the request. */
  function choose(id: string): void {
    if (id === source.environmentId) return
    void source.setEnvironmentOverride(id)
  }

  return { windowName, pinned, stale, label, answersIn, scopeId, scopeReadonly, options, choose }
}