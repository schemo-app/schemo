import { computed, inject, provide, type ComputedRef, type InjectionKey } from 'vue'
import { useCollectionsStore } from '../stores/collections'
import { trailOf } from '../lib/collectionTree'
import { NO_VAR_CONTEXT, type VarContext } from '../lib/vars'

const VAR_CONTEXT: InjectionKey<ComputedRef<VarContext>> = Symbol('varContext')

/** Gives request and collection fields their variable scope, outer level first. */
export function provideVarContext(id: () => string | null): ComputedRef<VarContext> {
  const collections = useCollectionsStore()
  const context = computed<VarContext>(() => {
    const trail = trailOf(collections.tree, id())
    if (!trail) return NO_VAR_CONTEXT
    const levels = trail.collection ? [...trail.ancestors, trail.collection] : trail.ancestors
    return {
      collection: levels[0]?.id ?? '',
      variables: levels.flatMap((level) => level.variables ?? []),
    }
  })
  provide(VAR_CONTEXT, context)
  return context
}

export function useVarContext(): ComputedRef<VarContext> {
  return inject(VAR_CONTEXT, computed(() => NO_VAR_CONTEXT))
}
