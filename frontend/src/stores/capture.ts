import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { Extension, type CaptureState, type TabState } from '../../bindings/schemo/internal/agent'
import { useSettings } from '../composables/useSettings'
import { captureSessions } from '../lib/recordTabs'
import { callOrToast } from '../lib/calls'
import { useRecordsStore } from './records'

/** What the browser extension is doing: whether it is connected, whether it records, and which tabs
 *  of the browser it follows. What it captured is the history's — the records carry their tab. */
export const useCaptureStore = defineStore('capture', () => {
  const records = useRecordsStore()

  const connected = ref(false)
  const recording = ref(false)
  const browser = ref('')
  const tabList = ref<TabState[]>([])

  // The tabs the extension follows, each with what it captured. A tab that has closed keeps its
  // requests, so the list is the union of the two.
  const sessions = computed(() => captureSessions(records.browserRecords, tabList.value))

  /** Takes what the extension has just said about itself. */
  function adopt(state: CaptureState) {
    connected.value = true
    recording.value = state.recording
    browser.value = state.browser
    tabList.value = state.tabList ?? []
  }

  /** The extension went away, and with it everything it was recording. */
  function disconnect() {
    connected.value = false
    recording.value = false
    browser.value = ''
    tabList.value = []
  }

  /** Hands the capture filters of the settings to the extension. */
  async function applyFilters() {
    const filters = useSettings().settings.value?.captureFilters
    if (!filters) return
    try {
      await Extension.ApplyCaptureFilters(filters)
    } catch {
      // The bridge may be gone by now; the next state the extension sends brings the filters again.
    }
  }

  /** Pauses or resumes one browser tab by its key, or all of them when no key is given. */
  async function toggle(sessionKey?: string) {
    if (sessionKey) {
      const tab = tabList.value.find((one) => String(one.tabId) === sessionKey)
      if (!tab) return
      await callOrToast(
        Extension.SetTabCapture(tab.tabId, browser.value, !tab.paused),
        'browser.captureFailed'
      )
      return
    }
    const call = recording.value ? Extension.PauseCapture() : Extension.ResumeCapture()
    await callOrToast(call, 'browser.captureFailed')
  }

  return { connected, recording, browser, tabList, sessions, adopt, disconnect, applyFilters, toggle }
})
