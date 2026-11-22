import type { Directive } from 'vue'
import { effectScope, onScopeDispose, shallowRef } from 'vue'
import { useEventListener, useTimeoutFn } from '@vueuse/core'
import { useEnvironmentsStore } from '../stores/environments'
import { useCollectionsStore } from '../stores/collections'
import { parseTokens } from './vars'

export const hoveredVar = shallowRef<HTMLElement | null>(null)

function tokenAt(field: HTMLElement, e: MouseEvent): HTMLElement | null {
  const tokens = Array.from(field.parentElement?.querySelectorAll<HTMLElement>('.var-token') ?? [])
  return (
    tokens.find((token) => {
      const r = token.getBoundingClientRect()
      return e.clientX >= r.left && e.clientX < r.right && e.clientY >= r.top && e.clientY < r.bottom
    }) ?? null
  )
}

// Alt-click opens where the value lives; an unknown name opens the active environment to add it.
function open(token: HTMLElement) {
  const environments = useEnvironmentsStore()
  const varName = token.dataset.var ?? ''
  const collection = token.dataset.collection ?? ''
  switch (token.dataset.source) {
    case 'global':
      environments.openSheet({ envId: null, varName })
      return
    case 'collection':
    case 'block':
      if (collection) {
        const envId = token.dataset.source === 'block' ? (environments.activeId ?? '') : ''
        void useCollectionsStore().openVariables(collection, envId)
        return
      }
  }
  environments.openSheet({ envId: environments.activeId, varName })
}

const detach = new WeakMap<HTMLElement, () => void>()

export const vVarPeek: Directive<HTMLElement> = {
  mounted(field) {
    const lifetime = effectScope()
    lifetime.run(() => {
      let under: HTMLElement | null = null
      let shown: HTMLElement | null = null
      // The card waits for the pointer to rest, so a pass across the field opens none.
      const arrival = useTimeoutFn(() => { hoveredVar.value = shown = under }, 150, { immediate: false })

      function hover(token: HTMLElement | null) {
        if (token === under) return
        under = token
        arrival.start()
      }
      const onDown = (e: MouseEvent) => {
        const token = e.altKey ? tokenAt(field, e) : null
        if (!token) return
        e.preventDefault()
        open(token)
      }

      // A double click takes the name between the braces; the variable is the braces as well.
      const onDouble = () => {
        const input = field as HTMLInputElement
        const [from, to] = [input.selectionStart ?? 0, input.selectionEnd ?? 0]
        const token = parseTokens(input.value).find((one) => from >= one.start && to <= one.end)
        if (token) input.setSelectionRange(token.start, token.end)
      }

      useEventListener(field, 'mousemove', (event) => hover(tokenAt(field, event)))
      useEventListener(field, 'mousedown', onDown)
      useEventListener(field, 'dblclick', onDouble)
      useEventListener(field, 'mouseleave', () => hover(null))
      onScopeDispose(() => {
        if (hoveredVar.value === shown) hoveredVar.value = null
      })
    })
    detach.set(field, () => lifetime.stop())
  },
  unmounted(field) {
    detach.get(field)?.()
    detach.delete(field)
  },
}
