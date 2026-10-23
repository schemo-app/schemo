import { computed, effectScope, reactive, ref, shallowReactive, type ComputedRef, type Ref, watch } from 'vue'
import { TabKind, type Draft, type DraftID, type Record as RequestRecord, type ResponseExample, type Tab, BodySide } from '../../bindings/schemo/internal/domain'
import { Drafts, type NodeEditor, type Seed, History } from '../../bindings/schemo/internal/request'
import type { RecordView } from '../lib/requestRecord'
import { freshInspector, type InspectorState } from '../lib/inspector'
import { freshResponseView, type ResponseViewState } from '../lib/responseView'
import { t } from '../i18n'
import { callOrToast } from '../lib/calls'
import { useDraft } from '../composables/useDraft'
import { useDebounceFn } from '@vueuse/core'
import { Collections } from '../../bindings/schemo/internal/workspace'
import { FLUSH_MS } from '../composables/useDeferredWrite'
import { bodyMissing, recordView, type RecordBodies } from '../lib/requestRecord'

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
  sentIds: Ref<string[]>
  pendingSendId: Ref<string | null>
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

/** The draft a tab is editing under that id, as it stands in the window. */
export function draftById(id: DraftID): Draft | null {
  for (const instance of instances.values()) {
    if (instance.kind !== TabKind.TabKindRequest) continue
    if (instance.draft.current.value?.id === id) return instance.draft.current.value
  }
  return null
}

/** The instance that owns a send — the one that claimed it, and the one the answer belongs to. */
export function ownerOf(sendId: string): RequestInstance | null {
  for (const instance of instances.values()) {
    if (instance.kind !== TabKind.TabKindRequest) continue
    if (instance.sentIds.value.includes(sendId)) return instance
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

/** Writes every draft's owed keystrokes home — the window is hiding, or the workspace is changing. */
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
    // "Don't save" throws the edit away: the node is opened anew from what is in the tree.
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
  const { load, abandon, sentIds, pendingSendId, claim, finishSend, chain, current, ...draft } =
    instance.draft
  const draftId = computed(() => current.value?.id ?? null)

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
    inheritedAuth: draft.inherited,
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

function requestInstance(tab: Tab): RequestInstance {
  let closed = false
  const node = ref<NodeEditor | null>(null)

  const shown = useShownRecord(() => closed)
  // A request tab is opened for something (a node, a draft, a record) and every one of them is a
  // read: until it lands the pane has nothing of its own to draw. One Go has just made blank is the
  // exception: its draft is empty, so there is nothing to wait for.
  const reading = ref(!bornEmpty.delete(tab.id))
  const responseView = ref<ResponseViewState>(freshResponseView())
  const bound = () => Boolean(tab.ref)

  const draft = useDraft({
    persisted: () => !bound(),
    filled: () => markClean(),
    sent: () => markClean(),
    finished: shown.show,
    scriptsLevel: () => (bound() ? (tab.ref ?? null) : String(tab.draftId)),
  })

  const { responses, saveExamples, flushExamples } = useNodeExamples(node)

  // Where the draft stands now is the state nothing has changed from: what a save, a send or a
  // landing in a collection asks for.
  function markClean() {
    draft.remember()
  }

  // The node the tab edits, as the tree has it now, and the draft it opens with.
  function adopt(opened: NodeEditor) {
    node.value = opened
    draft.load(opened.draft)
  }

  // One reading even under two askers — the strip warms the tab and the pane opens it in the same
  // breath, and a second snapshot could land over a draft the first has already shown.
  let opening: Promise<void> | null = null

  function open(): Promise<void> {
    if (closed) return Promise.resolve()
    if (draft.current.value) return Promise.resolve()
    if (opening) return opening
    opening = (async () => {
      try {
        if (bound()) {
          const opened = await callOrToast(Drafts.OpenNode(tab.ref ?? ''), 'collections.readFailed')
          if (opened && !closed) adopt(opened)
          return
        }
        const id = tab.draftId
        if (!id) return
        const loaded = await callOrToast(Drafts.Open(id), 'request.readFailed')
        if (loaded && !closed) draft.load(loaded)
      } finally {
        opening = null
      }
    })()
    return opening
  }

  // Saving is its own gesture: sending a bound request does not write it into the collection.
  async function saveNode() {
    const edited = draft.current.value
    if (!node.value || !edited || closed) return false
    const saved = await callOrToast(Drafts.SaveNode(edited), 'collections.saveFailed')
    if (!saved || closed) return false
    adopt(saved)
    return true
  }

  async function discardNode() {
    if (closed) return false
    const opened = await callOrToast(Drafts.OpenNode(tab.ref ?? ''), 'collections.readFailed')
    if (!opened || closed) return false
    adopt(opened)
    return true
  }

  // The dot: a bound tab's unwritten edit, an address typed and not yet written home, or keystrokes
  // still on their way. The draft answers for both: a saved request is dirty when its fields differ
  // from the row, a draft when its text differs from what it was opened with. Reverting every edit
  // takes the mark back, since there is nothing unsaved to save.
  const dirty = computed(() => draft.changed.value)
  const instance: RequestInstance = {
    kind: TabKind.TabKindRequest,
    node,
    draft,
    responseView,
    markClean,
    saveNode,
    discardNode,
    record: shown.record,
    dirty,
    warm: open,
    async ensure() {
      // Taken before the tab opens, so that a record shown meanwhile is not overwritten by this one.
      const turn = shown.ask()
      try {
        await open()
        if (tab.record && !closed) await shown.load(tab.record, turn)
      } finally {
        reading.value = false
      }
    },
    response: shown.response,
    reading,
    showRecord: shown.show,
    sentIds: draft.sentIds,
    pendingSendId: draft.pendingSendId,
    loading: draft.loading,
    async sendSpec(seed: Seed) {
      draft.loading.value = true
      const id = await callOrToast(Drafts.SendSeed(seed), 'request.sendFailed')
      if (!id) {
        draft.failSend()
        return
      }
      draft.claim(id)
      if (draft.loading.value) draft.pendingSendId.value = id
    },
    claim: draft.claim,
    finishSend: draft.finishSend,
    failSend: draft.failSend,
    responses,
    saveExamples,
    flushExamples,
    close: () => {
      closed = true
      shown.cancel()
      void flushExamples()
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

// The examples the pane edits. They are the node's own and saved on their own, a pause after the
// typing stops, the way a description is, so the list is held here rather than in the pane: the
// response's "to examples" writes into the same list. The composable the descriptions use is not
// it, since that one flushes on a component's unmount and a tab is not a component.
function useNodeExamples(node: Ref<NodeEditor | null>) {
  const responses = ref<ResponseExample[]>([])
  let pending = false
  // A write that fires after the list was written finds nothing pending and does nothing.
  const writeLater = useDebounceFn(() => void flushExamples(), FLUSH_MS)

  async function flushExamples() {
    if (!pending) return
    pending = false
    const id = node.value?.node.id
    if (!id) return
    const saved = await callOrToast(
      Collections.SaveExamples(id, responses.value),
      'collections.saveFailed'
    )
    if (saved && node.value) node.value = { ...node.value, node: saved }
  }

  function saveExamples(list: ResponseExample[]) {
    responses.value = list
    pending = true
    void writeLater()
  }

  // What the tree answers while nobody is typing: the node is re-read on every open and save, and a
  // list mid-edit is the pane's own until it has been written.
  watch(
    node,
    (now) => {
      if (pending) return
      responses.value = now?.node.responses ?? []
    },
    { immediate: true }
  )

  return { responses, saveExamples, flushExamples }
}

// The record and its bodies, read by name from Go. Every instance keeps its own reading; the
// panel's own list is a different question with a different answer. The record failing is the
// caller's to answer; a body failing is only a body.
async function readRecord(id: string): Promise<{ record: RequestRecord; bodies: RecordBodies }> {
  const record = await History.Record(id)
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
      bodies[key] = await History.Body(id, side)
    } catch {
      // A body pruned or withheld is not a record lost: it opens with what is left of it.
    }
  }
  return { record, bodies }
}

// The answer a request tab shows. Several things ask to show one — a send finishing, a row of the
// history, the tab opening — and the last to ask wins: a read that lands after a newer ask is dropped.
function useShownRecord(isClosed: () => boolean) {
  const record = ref<RequestRecord | null>(null)
  const bodies = ref<RecordBodies>({})
  let latest = 0

  const response = computed(() => (record.value ? recordView(record.value, bodies.value) : null))

  /** Takes a place in the line of askers; a read started under it lands only if nobody asks after. */
  const ask = () => ++latest

  /** Reads a record by name and shows it, unless a newer ask or the tab's closing came first. */
  async function load(id: string, turn: number) {
    const read = await callOrToast(readRecord(id), 'response.openFailed')
    if (!read || isClosed() || turn !== latest) return
    record.value = read.record
    bodies.value = read.bodies
  }

  /** Shows a record: one in hand at once, its bodies as soon as they are read. */
  async function show(shown: RequestRecord | string) {
    const turn = ask()
    if (isClosed()) return
    if (typeof shown !== 'string') {
      bodies.value = {}
      record.value = shown
    }
    await load(typeof shown === 'string' ? shown : shown.id, turn)
  }

  /** Drops whatever is still being read. */
  const cancel = () => void ask()

  return { record, response, show, ask, load, cancel }
}
