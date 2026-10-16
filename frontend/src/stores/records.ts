import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { CancelError } from '@wailsio/runtime'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'
import { bodyMissing, type RecordBodies, type RecordView } from '../lib/requestRecord'
import {
  BodySide,
  RecordSource,
  TabKind,
  type Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import type { Seed } from '../../bindings/schemo/internal/usecase/draft'
import type { CaptureState as BridgeCaptureState, TabState } from '../../bindings/schemo/internal/transport/extension'
import { BridgeService, RecordsService } from '../../bindings/schemo/internal/transport/wails'
import { useSettings } from '../composables/useSettings'
import { useWorkspacesStore } from './workspaces'
import { focusUrlField } from '../composables/intents'
import { callOrToast, runOrToast } from './calls'
import { useTabsStore } from './tabs'
import { adoptRecord as adoptInTabs, flushAll } from './content'
import { captureSessions as groupCaptureSessions } from '../lib/recordTabs'

type CaptureState = Omit<BridgeCaptureState, 'type' | 'version' | 'tabList'> & {
  connected: boolean
  version: string
  tabList: TabState[]
}

/** Manages the history list, browser capture state, and the session the history page is filtered
 *  by. What a tab shows is the strip's — read through it rather than kept a second time here. */
export const useRecordsStore = defineStore('records', () => {
  const tabs = useTabsStore()

  // ---- The history, and what the strip makes of it -------------------------------------------

  const workspaceId = ref('')
  const snapshot = useWorkspaceSnapshot<RequestRecord[]>([])
  let pagination = new AbortController()
  const records = snapshot.state
  const olderLoading = ref(false)
  const olderExhausted = ref(false)
  const bodies = ref<{ [id: string]: RecordBodies }>({})
  // The captured browser tab the history page is filtered by — set from the capture page, cleared
  // by its chip.
  const sessionFilter = ref('')
  const historySource = ref<'all' | 'manual' | 'browser' | 'mcp'>('all')
  const historyQuery = ref('')
  const capture = ref<CaptureState>({
    connected: false,
    recording: false,
    paused: false,
    tabs: 0,
    browser: '',
    version: '',
    tabList: [],
  })
  const browserRecords = computed(() => records.value.filter(record => record.source === RecordSource.SourceBrowser))
  const captureSessions = computed(() => groupCaptureSessions(browserRecords.value, capture.value.tabList))

  // The line the bookmark saves is the active request tab's draft; the record the work area shows
  // is the active request tab's answer. Both are the strip's answers, read through it.
  const line = computed(() => tabs.pane)

  const url = computed(() => line.value?.url ?? '')

  // Synchronizes the stored line revision with the active draft revision.
  function markClean() {
    line.value?.markClean()
  }

  /** Writes every draft's owed keystrokes home — the window is hiding or the space is changing. */
  async function flush() {
    await flushAll()
  }

  /** The answer the work area shows, whatever tab is showing it. */
  const activeRecord = computed<RecordView | null>(() => tabs.pane?.record ?? null)

  // Returns true if the record belongs to the active workspace.
  function accepts(record: RequestRecord): boolean {
    return !workspaceId.value || !record.workspaceId || record.workspaceId === workspaceId.value
  }

  async function load() {
    cancelOlder()
    const signal = snapshot.signal()
    workspaceId.value = useWorkspacesStore().activeId || workspaceId.value
    const before = new Map(records.value.map((record) => [record.id, record]))
    const call = RecordsService.List(RecordSource.$zero, 0).then((list) => {
      const changed = new Map(records.value.filter((record) => before.get(record.id) !== record)
        .map((record) => [record.id, record]))
      const loaded = new Set((list ?? []).map((record) => record.id))
      return [
        ...[...changed.values()].filter((record) => !loaded.has(record.id)),
        ...(list ?? []).map((record) => changed.get(record.id) ?? record),
      ]
    })
    const list = await callOrToast(snapshot.take(call), 'history.readFailed')
    if (!list || signal.aborted || records.value !== list) return
    olderExhausted.value = false
    const present = new Set(list.map((record) => record.id))
    for (const id of Object.keys(bodies.value)) if (!present.has(id)) delete bodies.value[id]
  }

  const OLDER_PAGE = 50

  function cancelOlder() {
    pagination.abort(new CancelError())
    pagination = new AbortController()
    olderLoading.value = false
  }

  // Fetches older history records before the oldest currently displayed record.
  async function loadOlder() {
    if (olderLoading.value || olderExhausted.value) return
    const signal = AbortSignal.any([snapshot.signal(), pagination.signal])
    olderLoading.value = true
    try {
      const oldest = records.value.reduce((at, r) => Math.min(at, r.startedAt), Infinity)
      const page = await callOrToast(
        RecordsService.Page(Number.isFinite(oldest) ? oldest : 0, OLDER_PAGE).cancelOn(signal),
        'history.readFailed'
      )
      if (page === undefined || signal.aborted) return
      const rows = page ?? []
      const known = new Set(records.value.map((r) => r.id))
      records.value = [...records.value, ...rows.filter((r) => accepts(r) && !known.has(r.id))]
      if (rows.length < OLDER_PAGE) olderExhausted.value = true
    } finally {
      if (!signal.aborted) olderLoading.value = false
    }
  }

  // Resets all workspace history state; the strip and what its tabs hold are forgotten elsewhere.
  function reset() {
    snapshot.reset()
    cancelOlder()
    workspaceId.value = useWorkspacesStore().activeId
    olderExhausted.value = false
    bodies.value = {}
    sessionFilter.value = ''
    historySource.value = 'all'
    historyQuery.value = ''
  }

  // Inserts or updates a record at the head of the list if accepted by workspace filter.
  function prepend(record: RequestRecord) {
    if (!accepts(record)) return
    records.value = [record, ...records.value.filter((r) => r.id !== record.id)]
  }

  function addIngested(record: RequestRecord) {
    prepend(record)
  }

  // ---- What the strip opens for the lists -----------------------------------------------------

  // A history row — sent by hand or captured — opens as a request tab laid out from the record: the
  // same record twice is the same tab. A plain click lands in the preview slot and a ⌘-click keeps
  // a tab of its own; the palette's «in the background» stages it without taking the work area.
  async function selectRecord(id: string, keep = false, background = false) {
    await tabs.openRecord(id, keep, background)
  }

  // A session of the capture page: the history page opens filtered by that browser tab.
  function openSession(key: string) {
    sessionFilter.value = key
    historySource.value = 'browser'
    historyQuery.value = ''
    void tabs.openHistory()
  }

  function clearSession() {
    sessionFilter.value = ''
  }

  // Executes an ad-hoc request seed without touching any draft: the answer is the active tab's.
  async function sendSpec(seed: Seed) {
    const instance = tabs.activeInstance
    if (instance?.kind === TabKind.TabKindRequest) await instance.sendSpec(seed)
  }

  // ---- Capture state --------------------------------------------------------------------------

  function setCaptureState(partial: Partial<CaptureState>) {
    capture.value = { ...capture.value, ...partial }
  }

  // Transmits user settings capture filters to the extension bridge.
  async function applyCaptureFilters() {
    const filters = useSettings().settings.value?.captureFilters
    if (!filters) return
    try {
      await BridgeService.ApplyCaptureFilters(filters)
    } catch {
      // Ignore failures during filter propagation frames.
    }
  }

  async function toggleCapture(key?: string) {
    if (key) {
      const tab = capture.value.tabList.find(tab => String(tab.tabId) === key)
      if (!tab) return
      await callOrToast(
        BridgeService.SetTabCapture(tab.tabId, capture.value.browser, !tab.paused),
        'browser.captureFailed',
      )
      return
    }
    const call = capture.value.recording ? BridgeService.PauseCapture() : BridgeService.ResumeCapture()
    await callOrToast(call, 'browser.captureFailed')
  }

  // Fetches and caches request/response body contents for a given record ID.
  async function loadBodies(id: string): Promise<RecordBodies | null> {
    const signal = snapshot.signal()
    const cached = bodies.value[id]
    if (cached) return cached
    const record = records.value.find((r) => r.id === id)
    if (!record) return null

    const read: RecordBodies = {
      request: record.requestBody?.inline,
      response: record.responseBody?.inline,
    }
    return await callOrToast((async () => {
      if (bodyMissing(record.requestBody)) {
        read.request = await RecordsService.Body(id, BodySide.SideRequest).cancelOn(signal)
        signal.throwIfAborted()
      }
      if (bodyMissing(record.responseBody)) {
        read.response = await RecordsService.Body(id, BodySide.SideResponse).cancelOn(signal)
        signal.throwIfAborted()
      }
      if (records.value.find((row) => row.id === id) !== record) return null
      bodies.value[id] = read
      return read
    })(), 'response.openFailed') ?? null
  }

  async function clearRecords(ids: string[]) {
    if (ids.length === 0) return
    const signal = snapshot.signal()
    if (!(await runOrToast(RecordsService.Clear(ids).cancelOn(signal), 'history.clearFailed')) || signal.aborted) return
    const gone = new Set(ids)
    snapshot.reset(records.value.filter((r) => !gone.has(r.id)))
    cancelOlder()
    for (const id of ids) delete bodies.value[id]
  }

  // Replaces an existing record entry in the local list if present.
  function adoptRecord(record: RequestRecord) {
    if (!accepts(record)) return
    records.value = records.value.map((row) => row.id === record.id ? record : row)
    delete bodies.value[record.id]
  }

  /** Reads a record again and puts the fresh copy where it is shown: the list and its tab. */
  async function refresh(id: string) {
    const signal = snapshot.signal()
    const fresh = await callOrToast(RecordsService.Record(id).cancelOn(signal), 'response.openFailed')
    if (!fresh || signal.aborted || !accepts(fresh)) return
    adoptRecord(fresh)
    adoptInTabs(fresh)
  }

  function focusSearch() {
    focusUrlField()
  }

  onScopeDispose(reset)

  return {
    workspaceId,
    records,
    bodies,
    sessionFilter,
    historySource,
    historyQuery,
    capture,
    browserRecords,
    captureSessions,
    url,
    markClean,
    flush,
    activeRecord,
    accepts,
    load,
    loadOlder,
    olderLoading,
    olderExhausted,
    reset,
    prepend,
    addIngested,
    selectRecord,
    openSession,
    clearSession,
    sendSpec,
    setCaptureState,
    applyCaptureFilters,
    toggleCapture,
    loadBodies,
    clearRecords,
    adoptRecord,
    refresh,
    focusSearch,
  }
})
