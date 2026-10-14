import { onBeforeUnmount } from 'vue'
import { useEventListener, watchDebounced } from '@vueuse/core'
import { useEnvironmentsStore } from '../stores/environments'
import { useRecordsStore } from '../stores/records'
import { useTabsStore } from '../stores/tabs'
import { useWorkspacesStore } from '../stores/workspaces'
import { useSettings } from './useSettings'

const SAVE_DEBOUNCE_MS = 300

/** Loads what the main window starts from, and keeps what the user types and lays out from being lost. */
export function useSessionPersistence(
    store: ReturnType<typeof useRecordsStore>,
    envStore: ReturnType<typeof useEnvironmentsStore>
): () => Promise<void> {
  const tabs = useTabsStore()
  const { settings, loadSettings, setLayout, setOnboarding } = useSettings()

  // What is still being typed is handed to Go whenever the window may go away: blurred, hidden, closed.
  const handOver = () => {
    void store.flush()
  }
  useEventListener(window, 'blur', handOver)
  useEventListener(document, 'visibilitychange', handOver)
  onBeforeUnmount(handOver)

  // The inspector's layout outlives the session; a drag of its edge is saved once, when it settles.
  // What is watched is the tab on screen: each tab carries its own inspector, and the layout is the
  // one the person left their window in.
  watchDebounced(
    () => tabs.pane?.inspector,
    (inspector) => {
      if (inspector) setLayout({ inspectorOpen: inspector.open, inspectorWidth: inspector.width })
    },
    { debounce: SAVE_DEBOUNCE_MS }
  )

  return async () => {
    await Promise.all([
      loadSettings(),
      useWorkspacesStore().load(),
      envStore.load().then(() => envStore.ensureDefaults()),
    ])
    await tabs.load()
    const stored = settings.value
    if (stored) tabs.pane?.setInspector({ open: stored.inspectorOpen, width: stored.inspectorWidth })
    if (stored && !stored.onboarding.seen) {
      await setOnboarding({ seen: true })
      await tabs.openWelcome()
    }
    await store.load()
  }
}
