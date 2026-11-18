import { computed } from 'vue'
import type { RequestSource } from '../lib/requestSource'
import { useEnvironmentsStore } from '../stores/environments'
import { useCollectionsStore } from '../stores/collections'
import { environmentOwnerOf, trailOf } from '../lib/collectionTree'
import { callOrToast } from '../lib/calls'
import { t } from '../i18n'

export function useRequestEnvironment(source: RequestSource) {
  const environments = useEnvironmentsStore()
  const collections = useCollectionsStore()

  const owner = computed(() => environmentOwnerOf(
    trailOf(collections.tree, source.nodeId), environments.envState?.collectionActive
  ))
  const owned = computed(() => environments.collectionEnvironments.filter(
    env => !env.folder && env.collectionId === owner.value?.id
  ))
  const base = computed(() => owned.value.find(env => env.base))
  const variants = computed(() => owned.value.filter(env => !env.base))
  const defaultUsesBase = computed(() => !!(owner.value?.environmentRoot && owner.value.variables?.length))
  const stored = computed(() => owner.value ? environments.envState?.collectionActive?.[owner.value.id] : undefined)
  const collectionSelection = computed(() => {
    if (stored.value) return stored.value === base.value?.id ? 'base' : stored.value
    return defaultUsesBase.value ? 'base' : ''
  })
  const pinned = computed(() => Boolean(source.environmentId))
  const selection = computed(() => source.environmentId || collectionSelection.value)
  const active = computed(() => {
    if (selection.value === 'base') return base.value
    if (!selection.value || selection.value === 'window') return environments.activeEnvironment
    return [...environments.collectionEnvironments, ...environments.environments].find(env => env.id === selection.value)
  })
  const scopeId = computed(() => active.value?.id ?? null)
  const hasPreviousSelection = computed(() => {
    if (!stored.value) return false
    if (stored.value === 'base') return !owned.value.length && !defaultUsesBase.value
    return stored.value === 'window' || environments.environments.some(env => env.id === stored.value)
  })
  const label = computed(() => {
    if (selection.value === 'base') return t('request.envBaseShort')
    if (pinned.value && selection.value !== 'window' && !active.value) return t('request.envRemoved')
    return active.value?.name ?? t('titlebar.noEnvironment')
  })

  async function choose(id: string) {
    if (pinned.value) {
      await source.setEnvironmentOverride('')
      return
    }
    if (owner.value) await callOrToast(environments.selectCollection(owner.value.id, id), 'environments.readFailed')
  }

  return { pinned, selection, active, scopeId, hasPreviousSelection, label, variants, choose }
}
