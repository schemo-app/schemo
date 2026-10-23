import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { CancelError } from '@wailsio/runtime/cancellable'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'
import { bodyMissing, type RecordBodies } from '../lib/requestRecord'
import {
  BodySide,
  RecordSource,
  TabKind,
  type Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import type { Seed } from '../../bindings/schemo/internal/request'
import { History } from '../../bindings/schemo/internal/request'
import { useWorkspacesStore } from './workspaces'
import { callOrToast, runOrToast } from '../lib/calls'
import { useTabsStore } from './tabs'
import { adoptRecord as adoptInTabs, flushAll } from './content'

/** Manages the history list and the session the history page is filtered by. What a tab shows is the
 *  strip's — read through it rather than kept a second time here. */
export const useRecordsStore = defineStore('records', () => {
  const tabs = useTabsStore()

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
  const browserRecords = computed(() => records.value.filter(record => record.source === RecordSource.SourceBrowser))

  /** Writes every draft's owed keystrokes home — the window is hiding or the workspace is changing. */
  async function flush() {
    await flushAll()
  }

  // Returns true if the record belongs to the active workspace.
  function accepts(record: RequestRecord): boolean {
    return !workspaceId.value || !record.workspaceId || record.workspaceId === workspaceId.value
  }

  async function load() {
    cancelOlder()
    const signal = snapshot.signal()
    workspaceId.value = useWorkspacesStore().activeId || workspaceId.value
    const before = new Map(records.value.map((record) => [record.id, record]))
    const call = History.List(RecordSource.$zero, 0).then((list) => {
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
        History.Page(Number.isFinite(oldest) ? oldest : 0, OLDER_PAGE).cancelOn(signal),
        'history.readFailed'
      )
      if (page === undefined || signal.aborted) return
      const rows = page ?? []
      const known = new Set(records.value.map((record) => record.id))
      records.value = [...records.value, ...rows.filter((row) => accepts(row) && !known.has(row.id))]
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
    records.value = [record, ...records.value.filter((one) => one.id !== record.id)]
  }

  // A history row (sent by hand or captured) opens as a request tab laid out from the record: the
  // same record twice is the same tab. A plain click lands in the preview slot and a ⌘-click keeps
  // a tab of its own; the palette's "in the background" stages it without taking the work area.
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

  // Fetches and caches request/response body contents for a given record ID.
  async function loadBodies(id: string): Promise<RecordBodies | null> {
    const signal = snapshot.signal()
    const cached = bodies.value[id]
    if (cached) return cached
    const record = records.value.find((record) => record.id === id)
    if (!record) return null

    const read: RecordBodies = {
      request: record.requestBody?.inline,
      response: record.responseBody?.inline,
    }
    return await callOrToast((async () => {
      if (bodyMissing(record.requestBody)) {
        read.request = await History.Body(id, BodySide.SideRequest).cancelOn(signal)
        signal.throwIfAborted()
      }
      if (bodyMissing(record.responseBody)) {
        read.response = await History.Body(id, BodySide.SideResponse).cancelOn(signal)
        signal.throwIfAborted()
      }
      if (records.value.find((row) => row.id === id) !== record) return null
      bodies.value[id] = read
      return read
    })(), 'response.openFailed') ?? null
  }

  /** Deletes records, and answers whether they are gone: a refusal is told to the person here. */
  async function clearRecords(ids: string[]): Promise<boolean> {
    if (ids.length === 0) return false
    const signal = snapshot.signal()
    if (!(await runOrToast(History.Clear(ids).cancelOn(signal), 'history.clearFailed')) || signal.aborted) {
      return false
    }
    const gone = new Set(ids)
    snapshot.reset(records.value.filter((record) => !gone.has(record.id)))
    cancelOlder()
    for (const id of ids) delete bodies.value[id]
    return true
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
    const fresh = await callOrToast(History.Record(id).cancelOn(signal), 'response.openFailed')
    if (!fresh || signal.aborted || !accepts(fresh)) return
    adoptRecord(fresh)
    adoptInTabs(fresh)
  }

  onScopeDispose(reset)

  return {
    workspaceId,
    records,
    bodies,
    sessionFilter,
    historySource,
    historyQuery,
    browserRecords,
    flush,
    load,
    loadOlder,
    olderLoading,
    olderExhausted,
    reset,
    prepend,
    selectRecord,
    openSession,
    clearSession,
    sendSpec,
    loadBodies,
    clearRecords,
    adoptRecord,
    refresh,
  }
})
