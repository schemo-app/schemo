import { computed, effectScope, reactive, ref, shallowReactive, watch, type ComputedRef, type Ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import {
  BodySide,
  TabKind,
  type Record as RequestRecord,
  type ResponseExample,
  type Tab,
} from '../../bindings/schemo/internal/domain'
import type { Seed, State } from '../../bindings/schemo/internal/usecase/draft'
import type { NodeEditor } from '../../bindings/schemo/internal/transport/wails/models'
import {
  CollectionsService,
  DraftService,
  RecordsService,
} from '../../bindings/schemo/internal/transport/wails'
import { bodyMissing, recordView, type RecordBodies, type RecordView } from '../lib/requestRecord'
import { freshInspector, type InspectorState } from '../lib/inspector'
import { freshResponseView, type ResponseViewState } from '../lib/responseView'
import { FLUSH_MS } from '../lib/draftBuffer'
import { t } from '../i18n'
import { callOrToast } from './calls'
import { useDraft } from './draft'

export interface TabInstance {
  kind: TabKind
  dirty: Ref<boolean>
  reading: Ref<boolean>
  warm(): Promise<void>
  ensure(): Promise<void>
  close(): void
}

export interface RequestInstance extends TabInstance {
  kind: typeof TabKind.TabKindRequest
  state: Ref<State | null>
  node: Ref<NodeEditor | null>
  draft: ReturnType<typeof useDraft>
  responseView: Ref<ResponseViewState>
  markClean(): void
  saveNode(): Promise<boolean>
  discardNode(): Promise<boolean>
  responses: Ref<ResponseExample[]>
  saveExamples(list: ResponseExample[]): void
  flushExamples(): Promise<void>
  record: Ref<RequestRecord | null>
  response: ComputedRef<RecordView | null>
  showRecord(record: RequestRecord | string): Promise<void>
  sendSpec(seed: Seed): Promise<void>
  claim(id: string): void
  finishSend(record: RequestRecord): Promise<void>
  failSend(): void
  mine: Ref<string[]>
  pendingId: Ref<string | null>
  loading: Ref<boolean>
}

export interface PageInstance extends TabInstance {
  kind:
    | typeof TabKind.TabKindCollections
    | typeof TabKind.TabKindHistory
    | typeof TabKind.TabKindCapture
    | typeof TabKind.TabKindPubs
}

export type AnyInstance = RequestInstance | PageInstance

export type PaneSource = ReturnType<typeof paneSource>

// Shallow, because an instance is a bag of refs and a deep proxy would unwrap them under the code
// that reads them: a computed that asked for an instance re-asks when the strip moves.
const instances = shallowReactive(new Map<string, AnyInstance>())

// The alert between leaving a dirty node-bound tab and the click that leaves it. One at a time, as
// a dialog is; the tab it belongs to travels with the question.
const pendingLeave = ref<null | { name: string; tabId: string; resolve: (ok: boolean) => void }>(null)

export const leavePending = pendingLeave

/** The instance of a tab, or null when it has never been opened. */
export function contentOf(id: string): AnyInstance | null {
  return instances.get(id) ?? null
}

/** The node a tab edits, read off its own instance — the strip's rename needs the name. */
export function nodeOf(id: string): NodeEditor | null {
  const instance = instances.get(id)
  if (!instance || instance.kind !== TabKind.TabKindRequest) return null
  return instance.node.value
}

/** The instance that owns a send — the one that claimed it, and the one the answer belongs to. */
export function ownerOf(sendId: string): RequestInstance | null {
  for (const instance of instances.values()) {
    if (instance.kind !== TabKind.TabKindRequest) continue
    if (instance.mine.value.includes(sendId)) return instance
  }
  return null
}

/** Forgets a tab's content: its buffers stop, its record and its node go. */
export function dropContent(id: string) {
  const instance = instances.get(id)
  if (!instance) return
  instances.delete(id)
  instance.close()
  const pending = pendingLeave.value
  if (pending?.tabId === id) {
    pendingLeave.value = null
    pending.resolve(false)
  }
}

// Concurrent callers share the complete load; warming reads only the draft for the title.
const ensured = new WeakMap<AnyInstance, Promise<void>>()

// The tabs Go has just made, whose draft holds nothing: there is nothing for the pane to wait for,
// and it draws its own empty page at once. A name is taken where the tab's instance is made.
const bornEmpty = new Set<string>()

/** Names a tab Go has just made blank, before anything is read for it. */
export function markBornEmpty(id: string) {
  bornEmpty.add(id)
}

/** Brings a tab's title up: the strip draws every tab, and a request tab's address is in its draft. */
export async function warmContent(tab: Tab): Promise<AnyInstance> {
  const known = instances.get(tab.id)
  if (known) {
    await known.warm()
    return known
  }
  const scope = effectScope(true)
  const made = scope.run(() =>
    tab.kind === TabKind.TabKindRequest ? requestInstance(tab) : pageInstance(tab)
  )!
  const close = made.close
  made.close = () => {
    scope.stop()
    close()
  }
  instances.set(tab.id, made)
  await made.warm()
  return made
}

/** Makes sure the tab being shown has somewhere for it to be drawn from. */
export async function ensureContent(tab: Tab): Promise<AnyInstance> {
  const made = await warmContent(tab)
  let ready = ensured.get(made)
  if (!ready) {
    ready = made.ensure()
    ensured.set(made, ready)
  }
  await ready
  return made
}

/** Drops every instance a strip no longer holds. */
export function keepOnly(ids: string[]) {
  const alive = new Set(ids)
  for (const id of [...instances.keys()]) {
    if (!alive.has(id)) dropContent(id)
  }
}

/** Writes every draft's owed keystrokes home — the window is hiding, or the space is changing. */
export async function flushAll() {
  for (const instance of instances.values()) {
    if (instance.kind === TabKind.TabKindRequest) await instance.draft.flush()
  }
}

/** Shows a stored record in the tab named — a run row's answer, a history row's. */
export async function recordInto(tabId: string, recordId: string) {
  const instance = instances.get(tabId)
  if (!instance || instance.kind !== TabKind.TabKindRequest) return
  await instance.showRecord(recordId)
}

// The inspector a tab without a draft still has: a captured response is inspected the same way, and
// there is no draft under it to keep the width on.
const bareInspector = ref<InspectorState>(freshInspector())

/** The inspector the viewer draws with — the pane's own, or the window's when the tab has none. */
export function inspectorState(pane: PaneSource | null): InspectorState {
  return pane?.inspector ?? bareInspector.value
}

export function setInspectorState(
  pane: PaneSource | null,
  partial: Partial<InspectorState>
) {
  if (pane) pane.setInspector(partial)
  else bareInspector.value = { ...bareInspector.value, ...partial }
}

const bareResponseView = ref<ResponseViewState>(freshResponseView())

/** Which reading of the answer is on screen — the pane's own, or the window's when it has none. */
export function viewState(pane: PaneSource | null): ResponseViewState {
  return pane?.responseView ?? bareResponseView.value
}

export function setViewState(pane: PaneSource | null, partial: Partial<ResponseViewState>) {
  if (pane) pane.setResponseView(partial)
  else bareResponseView.value = { ...bareResponseView.value, ...partial }
}

/** A record rewritten outside the window — a cookie write undone — reaches the tab showing it. */
export function adoptRecord(fresh: RequestRecord) {
  for (const instance of instances.values()) {
    if (instance.kind !== TabKind.TabKindRequest) continue
    if (instance.response.value?.id === fresh.id) void instance.showRecord(fresh)
  }
}

/** The active tab's draft writes its scripts down and reads the chain back — a workspace switch
 *  asks for the two of them at once, the way it did when there was one draft. */
export async function reloadScripts() {
  for (const instance of instances.values()) {
    if (instance.kind === TabKind.TabKindRequest) await instance.draft.loadScripts()
  }
}

/** A dirty node-bound tab is asked about before it is left; the answer is awaited by whoever asks. */
export function askUnsaved(tabId: string): Promise<boolean> {
  const instance = instances.get(tabId)
  if (
    !instance ||
    instance.kind !== TabKind.TabKindRequest ||
    !instance.node.value ||
    !instance.dirty.value
  ) {
    return Promise.resolve(true)
  }
  const name = instance.node.value.node.name || t('collections.unnamedRequest')
  if (pendingLeave.value) return Promise.resolve(false)
  return new Promise((resolve) => (pendingLeave.value = { name, tabId, resolve }))
}

export async function answerUnsaved(action: 'save' | 'discard' | 'cancel') {
  const pending = pendingLeave.value
  if (!pending) return
  pendingLeave.value = null
  if (action === 'cancel') return pending.resolve(false)
  const instance = instances.get(pending.tabId)
  if (instance && instance.kind === TabKind.TabKindRequest && instance.node.value) {
    // «Don't save» throws the edit away: the node is opened anew from what is in the tree.
    const completed = action === 'save' ? await instance.saveNode() : await instance.discardNode()
    if (!completed) {
      pending.resolve(false)
      return
    }
  }
  pending.resolve(true)
}

/** The pane's view of an instance: the draft's own refs and calls, unwrapped by `reactive`, and the
 *  answers that are the tab's rather than the draft's. */
export function paneSource(instance: RequestInstance) {
  // What the pane has no business with: the buffer's own plumbing and the send's bookkeeping.
  const {
    receive,
    load,
    abandon,
    applyText,
    mine,
    pendingId,
    claim,
    finishSend,
    chain,
    ...draft
  } = instance.draft
  const draftId = computed(
    () => instance.node.value?.state.draft.id ?? instance.state.value?.draft.id ?? null
  )

  return reactive({
    ...draft,
    scriptsLevel: draftId,
    draftId,
    nodeId: computed(() => instance.node.value?.node.id ?? null),
    responses: instance.responses,
    saveExamples: instance.saveExamples,
    flushExamples: instance.flushExamples,
    saveNode: () => instance.saveNode(),
    dirty: computed(() => instance.dirty.value),
    canInherit: computed(() => instance.node.value !== null),
    inheritedAuth: computed(() => instance.node.value?.state.inherited ?? null),
    canSave: computed(() => instance.node.value === null),
    blank: computed(
      () =>
        instance.node.value === null && instance.record.value === null && !draft.url.value.trim()
    ),
    record: instance.response,
    nodeEditor: instance.node,
    responseView: instance.responseView,
    markClean: instance.markClean,
    setResponseView: (partial: Partial<ResponseViewState>) => {
      instance.responseView.value = { ...instance.responseView.value, ...partial }
    },
  })
}

// The record and its bodies, read by name from Go. Every instance keeps its own reading; the
// panel's own list is a different question with a different answer. The record failing is the
// caller's to answer; a body failing is only a body.
async function readRecord(id: string): Promise<{ record: RequestRecord; bodies: RecordBodies }> {
  const record = await RecordsService.Record(id)
  const bodies: RecordBodies = {
    request: record.requestBody?.inline,
    response: record.responseBody?.inline,
  }
  for (const [side, key] of [
    [BodySide.SideRequest, 'request'],
    [BodySide.SideResponse, 'response'],
  ] as const) {
    const held = key === 'request' ? record.requestBody : record.responseBody
    if (!bodyMissing(held)) continue
    try {
      bodies[key] = await RecordsService.Body(id, side)
    } catch {
      // A body pruned or withheld is not a record lost: it opens with what is left of it.
    }
  }
  return { record, bodies }
}

function requestInstance(tab: Tab): RequestInstance {
  let closed = false
  let recordRead = 0
  const state = ref<State | null>(null)
  const node = ref<NodeEditor | null>(null)

  const record = ref<RequestRecord | null>(null)
  const bodies = ref<RecordBodies>({})
  // A request tab is opened for something — a node, a draft, a record — and every one of them is a
  // read: until it lands the pane has nothing of its own to draw. One Go has just made blank is the
  // exception: its draft is empty, so there is nothing to wait for.
  const reading = ref(!bornEmpty.delete(tab.id))
  const responseView = ref<ResponseViewState>(freshResponseView())
  const bound = () => Boolean(tab.ref)

  async function showRecord(shown: RequestRecord | string) {
    const readId = ++recordRead
    if (closed) return
    if (typeof shown !== 'string') {
      bodies.value = {}
      record.value = shown
    }
    const read = await callOrToast(readRecord(typeof shown === 'string' ? shown : shown.id), 'response.openFailed')
    if (read && !closed && readId === recordRead) {
      record.value = read.record
      bodies.value = read.bodies
    }
  }

  const draft = useDraft({
    id: () => node.value?.state.draft.id ?? state.value?.draft.id ?? null,
    state: () => node.value?.state ?? state.value,
    keep: (next) => {
      if (node.value) node.value.state = next
      else state.value = next
    },
    filled: () => markClean(),
    sent: () => markClean(),
    finished: showRecord,
    scriptsLevel: () => (bound() ? (tab.ref ?? null) : String(tab.draftId)),
  })

  // The examples the pane edits. They are the node's own and saved on their own — a pause after
  // the typing stops, the way a description is — so the list is held here rather than in the pane:
  // the response's «to examples» writes into the same list. The composable the descriptions use
  // is not it — that one flushes on a component's unmount, and a tab is not a component.
  const responses = ref<ResponseExample[]>([])
  let responsesPending = false
  // A write that fires after the list was written finds nothing pending and does nothing.
  const writeLater = useDebounceFn(() => void writeResponses(), FLUSH_MS)

  async function writeResponses() {
    if (!responsesPending) return
    responsesPending = false
    const id = node.value?.node.id
    if (!id) return
    const saved = await callOrToast(
      CollectionsService.SaveExamples(id, responses.value),
      'collections.saveFailed'
    )
    if (saved && node.value) node.value = { ...node.value, node: saved }
  }

  function saveExamples(list: ResponseExample[]) {
    responses.value = list
    responsesPending = true
    void writeLater()
  }

  // What the tree answers while nobody is typing: the node is re-read on every open and save, and
  // a list mid-edit is the pane's own until it has been written.
  watch(
    node,
    (now) => {
      if (responsesPending) return
      responses.value = now?.node.responses ?? []
    },
    { immediate: true }
  )

  // Where the draft stands now is the state nothing has changed from: what a save, a send or a
  // landing in a collection asks for.
  function markClean() {
    draft.remember()
  }

  // One reading even under two askers — the strip warms the tab and the pane opens it in the same
  // breath, and a second snapshot could land over a draft the first has already shown.
  let opening: Promise<void> | null = null

  function open(): Promise<void> {
    if (closed) return Promise.resolve()
    if (node.value || state.value) return Promise.resolve()
    if (opening) return opening
    opening = (async () => {
      try {
        if (bound()) {
          const opened = await callOrToast(
            CollectionsService.OpenNode(tab.ref ?? ''),
            'collections.readFailed'
          )
          if (opened && !closed) {
            node.value = opened
            draft.load(opened.state)
          }
          return
        }
        const id = tab.draftId
        if (!id) return
        const loaded = await callOrToast(DraftService.Snapshot(id), 'request.readFailed')
        if (loaded && !closed) {
          draft.load(loaded)
          markClean()
        }
      } finally {
        opening = null
      }
    })()
    return opening
  }

  // Saving is its own gesture: sending a bound request does not write it into the collection.
  async function saveNode() {
    const id = node.value?.node.id
    if (!id || closed || !(await draft.flush())) return false
    if (closed) return false
    const saved = await callOrToast(CollectionsService.SaveNode(id), 'collections.saveFailed')
    if (!saved || closed) return false
    node.value = saved
    draft.load(saved.state)
    return true
  }

  async function discardNode() {
    if (closed) return false
    const opened = await callOrToast(CollectionsService.OpenNode(tab.ref ?? ''), 'collections.readFailed')
    if (!opened || closed) return false
    node.value = opened
    draft.load(opened.state)
    return true
  }

  // The dot: a bound tab's unwritten edit, an address typed and not yet written home, or keystrokes
  // still on their way.
  // The draft answers for both: a saved request is dirty when its fields differ from the row, a
  // draft when its text differs from what it was opened with. Reverting every edit takes the mark
  // back — there is nothing unsaved to save.
  const dirty = computed(() => draft.changed.value)
  const instance: RequestInstance = {
    kind: TabKind.TabKindRequest,
    state,
    node,
    draft,
    responseView,
    markClean,
    saveNode,
    discardNode,
    record,
    dirty,
    warm: open,
    async ensure() {
      const readId = ++recordRead
      try {
        await open()
        if (tab.record && !closed) {
          const read = await callOrToast(readRecord(tab.record), 'response.openFailed')
          if (read && !closed && readId === recordRead) {
            record.value = read.record
            bodies.value = read.bodies
          }
        }
      } finally {
        reading.value = false
      }
    },
    response: computed(() => (record.value ? recordView(record.value, bodies.value) : null)),
    reading,
    showRecord,
    mine: draft.mine,
    pendingId: draft.pendingId,
    loading: draft.loading,
    async sendSpec(seed: Seed) {
      draft.loading.value = true
      const id = await callOrToast(RecordsService.SendSpec(seed), 'request.sendFailed')
      if (!id) {
        draft.failSend()
        return
      }
      draft.claim(id)
      if (draft.loading.value) draft.pendingId.value = id
    },
    claim: draft.claim,
    finishSend: draft.finishSend,
    failSend: draft.failSend,
    responses,
    saveExamples,
    flushExamples: writeResponses,
    close: () => {
      closed = true
      recordRead++
      void writeResponses()
      draft.abandon()
    },
  }
  return instance
}

function pageInstance(tab: Tab): PageInstance {
  return {
    kind: tab.kind as PageInstance['kind'],
    dirty: ref(false),
    reading: ref(false),
    // A page draws the window's own store, already there: it has nothing of its own to open.
    warm: () => Promise.resolve(),
    ensure: () => Promise.resolve(),
    close: () => undefined,
  }
}
