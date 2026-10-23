import { getCurrentScope } from 'vue'
import { tryOnScopeDispose } from '@vueuse/core'
import { Events } from '@wailsio/runtime'

export function useWailsEvent<E extends Events.WailsEventName>(
  name: E,
  handler: Events.WailsEventCallback<E>
): () => void {
  const off = Events.On(name, handler)
  // A composable's subscription dies with its scope; one made at a module's top level has none and
  // lives as long as the window, which is what a state every window shares wants.
  if (getCurrentScope()) tryOnScopeDispose(off)
  return off
}
