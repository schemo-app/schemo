
import { useEventListener } from '@vueuse/core'
import { useCommands } from '../useCommands'
import { useCollectionsStore } from '../../stores/collections'
import { usePlatform } from '../usePlatform'
import { isRemoveEvent, isRenameEvent } from '../../lib/treeKeys'

/** Checks whether the keyboard event originated from an active editable text input. */
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el?.tagName) return false
  return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName)
}

/** Composable managing keyboard shortcuts for selected items within the collections panel. */
export function useCollectionKeys(): void {
  const collections = useCollectionsStore()
  const { matches, offered } = useCommands()
  const { isMac } = usePlatform()

  function onKeydown(e: KeyboardEvent) {
    if (!collections.inCollections || isTyping(e.target)) return
    if (!collections.selectedId) return

    if (matches('newRequest', e)) {
      e.preventDefault()
      collections.pendingCreate = collections.selectedLevel
      return
    }
    if (matches('duplicate', e)) {
      e.preventDefault()
      void collections.duplicate(collections.selectedId)
      return
    }
    if (offered('run') && matches('run', e)) {
      e.preventDefault()
      void collections.run(collections.runNodeId, collections.levelName)
      return
    }
    if (isRenameEvent(e)) {
      e.preventDefault()
      collections.pendingRename = collections.selectedId
      return
    }
    if (isRemoveEvent(e, isMac.value)) {
      e.preventDefault()
      collections.pendingRemove = collections.selectedId
    }
  }

  useEventListener(window, 'keydown', onKeydown)
}