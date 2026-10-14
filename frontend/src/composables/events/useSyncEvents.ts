import { toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useEventListener } from '@vueuse/core'
import { useSyncStore } from '../../stores/sync'
import { useCollectionsStore } from '../../stores/collections'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCookiesStore } from '../../stores/cookies'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useRecordsStore } from '../../stores/records'
import { useWailsEvent } from './useWailsEvent'

function followSync(arrived?: () => void, historyArrived?: () => void, enabled: MaybeRefOrGetter<boolean> = true) {
  const sync = useSyncStore()

  watch(() => toValue(enabled), (ready) => {
    if (ready) void sync.load()
  }, { immediate: true })

  useWailsEvent('sync:changed', (ev) => {
    if (!toValue(enabled) || !ev.data) return

    const newer = (ev.data.contentAt ?? 0) > sync.contentAt
    const records = (ev.data.historyAt ?? 0) > sync.historyAt

    sync.set(ev.data)

    if (newer) arrived?.()
    if (records) historyArrived?.()
  })

  useEventListener(window, 'focus', () => { if (toValue(enabled)) sync.wake() })
  useEventListener(document, 'visibilitychange', () => {
    if (toValue(enabled) && !document.hidden) sync.wake()
  })
}

/** useSyncStatus follows sync status without loading workspace data. */
export function useSyncStatus(): void {
  followSync()
}

/** useSyncEvents follows sync status and reloads stores when remote changes arrive. */
export function useSyncEvents(enabled: MaybeRefOrGetter<boolean>): void {
  const stores = [
    useCollectionsStore(),
    useEnvironmentsStore(),
    useCookiesStore(),
    useWorkspacesStore(),
  ]
  const history = useRecordsStore()

  followSync(
    () => stores.forEach((store) => void store.load()),
    () => void history.load(),
    enabled,
  )
}