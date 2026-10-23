import { onMounted, onScopeDispose, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useEventListener } from '@vueuse/core'
import { Extension, type CaptureState } from '../../bindings/schemo/internal/agent'
import { Transfer } from '../../bindings/schemo/internal/convert'
import { useSyncStore } from '../stores/sync'
import { ownerOf } from '../stores/content'
import { holdAnswer } from '../lib/earlyAnswers'
import { useSettings } from './useSettings'
import { useCollectionsStore } from '../stores/collections'
import { useCookiesStore } from '../stores/cookies'
import { useEnvironmentsStore } from '../stores/environments'
import { useCaptureStore } from '../stores/capture'
import { useRecordsStore } from '../stores/records'
import { useTransferStore } from '../stores/transfer'
import { useWorkspacesStore } from '../stores/workspaces'
import { failureText, t as tr } from '../i18n'
import { useToast } from './useToast'
import { useWailsEvent } from './useWailsEvent'

/** Everything the main window listens for from Go. The settings window listens with
 *  `useWailsEvent` directly. */
export function useAppEvents(
  records: ReturnType<typeof useRecordsStore>,
  collections: ReturnType<typeof useCollectionsStore>,
  ready: MaybeRefOrGetter<boolean>
): void {
  useWorkspaceEvents()
  useSyncEvents(ready)
  useCaptureEvents()
  useRecordEvents(records, collections)
  useTransferEvents()
}

function useWorkspaceEvents(): void {
  useWailsEvent('workspace:changed', () => void useWorkspacesStore().load())
}

function followSync(arrived?: () => void, historyArrived?: () => void, enabled: MaybeRefOrGetter<boolean> = true) {
  const sync = useSyncStore()

  watch(() => toValue(enabled), (ready) => {
    if (ready) void sync.load()
  }, { immediate: true })

  useWailsEvent('sync:changed', (event) => {
    if (!toValue(enabled) || !event.data) return

    const newContent = (event.data.contentAt ?? 0) > sync.contentAt
    const newHistory = (event.data.historyAt ?? 0) > sync.historyAt

    sync.set(event.data)

    if (newContent) arrived?.()
    if (newHistory) historyArrived?.()
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
function useSyncEvents(enabled: MaybeRefOrGetter<boolean>): void {
  const workspaces = useWorkspacesStore()
  const stores = [
    useCollectionsStore(),
    useEnvironmentsStore(),
    useCookiesStore(),
    workspaces,
  ]
  const records = useRecordsStore()

  useWailsEvent('mcp:data-changed', (event) => {
    if (!toValue(enabled)) return
    if (event.data === workspaces.activeId) {
      stores.forEach((store) => void store.load())
    } else {
      void workspaces.load()
    }
  })

  followSync(
    () => stores.forEach((store) => void store.load()),
    () => void records.load(),
    enabled,
  )
}

/** Follows the browser extension: keeps the capture store in step with what it says, and hands it
 *  the settings it works by whenever a browser connects. */
export function useCaptureEvents(): void {
  const capture = useCaptureStore()
  const records = useRecordsStore()

  // An event that arrives before the first status read is newer than the read, and wins.
  let eventSeen = false
  onScopeDispose(() => {
    eventSeen = true
  })

  onMounted(async () => {
    try {
      const status = await Extension.Status()
      if (!eventSeen && status.connected) adopt(status.state)
    } catch {
      // The next bridge event will supply the state if the initial read fails.
    }
  })

  function adopt(state: CaptureState): void {
    capture.adopt(state)
    // A newly connected browser needs the current filters and settings.
    void capture.applyFilters()
    const extension = useSettings().settings.value?.extension
    if (extension) {
      void Extension.ApplyExtensionSettings(extension).catch(() => {
        // The bridge may disconnect during propagation; the next state retries it.
      })
    }
  }

  useWailsEvent('capture-state', (event) => {
    eventSeen = true
    adopt(event.data)
  })

  useWailsEvent('capture-disconnected', () => {
    eventSeen = true
    capture.disconnect()
  })

  useWailsEvent('open-tab', (event) => {
    records.openSession(String(event.data))
  })
}

/** Subscribes to request execution, record ingestion, and collection run events emitted by the backend. */
function useRecordEvents(
    records: ReturnType<typeof useRecordsStore>,
    collections: ReturnType<typeof useCollectionsStore>
): void {
  const toast = useToast()

  // Handles new ingested records from browser capture or manual sampling
  useWailsEvent('record:added', (event) => {
    records.prepend(event.data)
  })

  // Routes completed request execution payloads to the tab that sent it, or to the early answer buffer
  useWailsEvent('request:finished', (event) => {
    const { id, record } = event.data
    const owner = ownerOf(id)
    if (owner) {
      void owner.finishSend(record)
      records.prepend(record)
    } else if (record.agent) {
      // An agent's send belongs to nobody in this window (it asked over the MCP server), so there
      // is no answer to hold for anybody: it goes straight into the history, where the person
      // watches what the agent does.
      records.prepend(record)
    } else holdAnswer(id, { record })
  })

  // Handles request execution failures prior to record creation
  useWailsEvent('request:failed', (event) => {
    const { id } = event.data
    toast.show(tr('errors.requestFailed', { error: failureText(event.data.failure) ?? event.data.error }), 'error')
    const owner = ownerOf(id)
    if (owner) owner.failSend()
    else holdAnswer(id, { failed: true })
  })

  // Reloads history store when history is cleared for the current workspace
  useWailsEvent('history:cleared', (event) => {
    if (records.workspaceId && event.data.workspaceId && event.data.workspaceId !== records.workspaceId) {
      return
    }
    records.reset()
    void records.load()
  })

  // Collection execution progress and completion updates
  useWailsEvent('run:progress', (event) => {
    collections.applyRunProgress(event.data)
  })

  useWailsEvent('run:finished', (event) => {
    collections.applyRunFinished(event.data)
  })
}

/**
 * A link to a published page asks this window for one collection, so the import dialog opens on it.
 * The page is taken from Go rather than carried by the event: a link is usually what started this
 * app, and nothing is listening while the window is still drawing.
 */
function useTransferEvents(): void {
  const transfer = useTransferStore()

  // One caller opens the dialog, whichever of the two gets there first: the take empties the page.
  async function follow(): Promise<void> {
    try {
      const page = await Transfer.TakeImportLink()
      if (page) transfer.askImportPage(page)
    } catch {
      // No backend answer: the window keeps its own import button, which is where a link lands by
      // hand anyway.
    }
  }

  useWailsEvent('import-link', () => void follow())
  onMounted(() => void follow())
}
