import { onBeforeUnmount } from 'vue'
import { useEventListener } from '@vueuse/core'
import { useEnvironmentsStore } from '../stores/environments'
import { useRecordsStore } from '../stores/records'
import { useTabsStore } from '../stores/tabs'
import { useWorkspacesStore } from '../stores/workspaces'
import { useSettings } from './useSettings'

/** Loads the main window's session and hands over pending request edits when the window goes away. */
export function useSessionPersistence(
    records: ReturnType<typeof useRecordsStore>,
    environments: ReturnType<typeof useEnvironmentsStore>
): () => Promise<void> {
  const tabs = useTabsStore()
  const { settings, loadSettings, saveOrToast } = useSettings()

  // What is still being typed is handed to Go whenever the window may go away: blurred, hidden, closed.
  const handOver = () => {
    void records.flush()
  }
  useEventListener(window, 'blur', handOver)
  useEventListener(document, 'visibilitychange', handOver)
  onBeforeUnmount(handOver)

  return async () => {
    await Promise.all([
      loadSettings(),
      useWorkspacesStore().load(),
      environments.load().then(() => environments.ensureDefaults()),
    ])
    await tabs.load()
    const stored = settings.value
    if (stored && !stored.onboarding.seen) {
      await saveOrToast({ onboarding: { seen: true } })
      await tabs.openWelcome()
    }
    await records.load()
  }
}
