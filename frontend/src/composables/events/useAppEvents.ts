import type { MaybeRefOrGetter } from 'vue'
import { useCaptureEvents } from './useCaptureEvents'
import { useRecordEvents } from './useRecordEvents'
import { useSyncEvents } from './useSyncEvents'
import { useTransferEvents } from './useTransferEvents'
import { useWorkspaceEvents } from './useWorkspaceEvents'
import type { useCollectionsStore } from '../../stores/collections'
import type { useRecordsStore } from '../../stores/records'

/**
 * Everything the main window listens for from Go, in one call: a reader asking what this window
 * reacts to reads one line, and each subscription is still a file of its own. The settings window
 * has its own (`useSettingsEvents`), because what it reacts to is that window's business.
 */
export function useAppEvents(
  store: ReturnType<typeof useRecordsStore>,
  collections: ReturnType<typeof useCollectionsStore>,
  ready: MaybeRefOrGetter<boolean>
): void {
  useWorkspaceEvents()
  useSyncEvents(ready)
  useCaptureEvents(store)
  useRecordEvents(store, collections)
  useTransferEvents()
}
