import { onMounted, onScopeDispose } from 'vue'
import type { CaptureState } from '../../../bindings/schemo/internal/transport/extension'
import { BridgeService } from '../../../bindings/schemo/internal/transport/wails'
import { useSettings } from '../useSettings'
import { useRecordsStore } from '../../stores/records'
import { useWailsEvent } from './useWailsEvent'

/** Subscribes to browser extension capture events and synchronizes state with the requests store. */
export function useCaptureEvents(store: ReturnType<typeof useRecordsStore>): void {
  let changed = false
  onScopeDispose(() => { changed = true })
  onMounted(async () => {
    try {
      const status = await BridgeService.Status()
      if (!changed && status.connected) applyState(status.state)
    } catch {
      // The next bridge event will supply the state if the initial read fails.
    }
  })

  useWailsEvent('capture-state', (ev) => {
    changed = true
    applyState(ev.data)
  })

  function applyState(state: CaptureState): void {
    store.setCaptureState({
      connected: true,
      recording: state.recording,
      paused: state.paused,
      tabs: state.tabs,
      browser: state.browser,
      version: state.version,
      tabList: state.tabList ?? [],
    })
    // A newly connected browser needs the current filters and settings.
    void store.applyCaptureFilters()
    const extension = useSettings().settings.value?.extension
    if (extension) {
      void BridgeService.ApplyExtensionSettings(extension).catch(() => {
        // The bridge may disconnect during propagation; the next state retries it.
      })
    }
  }

  useWailsEvent('capture-disconnected', () => {
    changed = true
    store.setCaptureState({
      connected: false,
      recording: false,
      paused: false,
      tabs: 0,
      browser: '',
      version: '',
      tabList: [],
    })
  })

  useWailsEvent('open-tab', (ev) => {
    store.openSession(String(ev.data))
  })
}
