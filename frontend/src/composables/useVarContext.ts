import { computed, inject, provide, type ComputedRef, type InjectionKey } from 'vue'
import { useCollectionsStore } from '../stores/collections'
import { trailOf } from '../lib/collectionTree'
import { NO_VAR_CONTEXT, type VarContext } from '../lib/vars'

const VAR_CONTEXT: InjectionKey<ComputedRef<VarContext>> = Symbol('varContext')

/** Gives the fields below a request what its levels add to resolution, outer level first. */
export function provideVarContext(nodeId: () => string | null): ComputedRef<VarContext> {
  const collections = useCollectionsStore()
  const context = computed<VarContext>(() => {
    const trail = trailOf(collections.tree, nodeId())
    if (!trail?.node) return NO_VAR_CONTEXT
    return {
      collection: trail.ancestors[0]?.id ?? '',
      variables: trail.ancestors.flatMap((level) => level.variables ?? []),
    }
  })
  provide(VAR_CONTEXT, context)
  return context
}

export function useVarContext(): ComputedRef<VarContext> {
  return inject(VAR_CONTEXT, computed(() => NO_VAR_CONTEXT))
}
