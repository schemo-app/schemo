import { BridgeService } from '../../../bindings/schemo/internal/transport/wails'
import { useSettings } from '../useSettings'
import { useRecordsStore } from '../../stores/records'
import { useWailsEvent } from './useWailsEvent'

/** Subscribes to browser extension capture events and synchronizes state with the requests store. */
export function useCaptureEvents(store: ReturnType<typeof useRecordsStore>): void {
  useWailsEvent('capture-state', (ev) => {
    store.setCaptureState({
      connected: true,
      recording: ev.data.recording,
      paused: ev.data.paused,
      tabs: ev.data.tabs,
      browser: ev.data.browser ?? '',
      version: ev.data.version ?? '',
      tabList: ev.data.tabList ?? [],
    })
    // Re-apply what the extension is told on reconnect or state updates: the filters, and the two
    // switches — an extension that just connected knows nothing of either yet.
    void store.applyCaptureFilters()
    const extension = useSettings().settings.value?.extension
    if (extension) void BridgeService.ApplyExtensionSettings(extension)
  })

  useWailsEvent('capture-disconnected', () => {
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
    // The extension asks the window to look at a tab under capture: the history opens on it.
    store.openSession(String(ev.data))
  })
}