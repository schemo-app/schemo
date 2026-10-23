import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { type CancellablePromise } from '@wailsio/runtime/cancellable'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'
import { findCollection, holderOf, trailOf } from '../lib/collectionTree'
import { t } from '../i18n'
import {
  TabKind,
  type Auth,
  type Collection,
  type CollectionRun,
  type CollectionRunResult,
  type DraftID,
  type Variable,
} from '../../bindings/schemo/internal/domain'
import { Events } from '@wailsio/runtime'
import { Collections, type CollectionPatch } from '../../bindings/schemo/internal/workspace'
import { Drafts, Runs, type RunProgress } from '../../bindings/schemo/internal/request'
import { callOrToast, runOrToast } from '../lib/calls'
import { markStep } from '../composables/useGettingStarted'
import { useTabsStore } from './tabs'
import {
  askUnsaved as askCardUnsaved,
  draftById,
  recordInto,
  type PaneSource,
  type RequestInstance,
} from './content'

/** Which of a collection's page's sections is on screen. The page owns the choice and the tree asks
 *  for one, so the words live here where both can name them. */
export type CollectionSection = 'overview' | 'docs' | 'auth' | 'vars' | 'scripts' | 'runs'

/** The tree, the runs, and the card the strip is showing: the card itself lives in the tab it
 *  belongs to, and what is read here is that tab's answer. */
export const useCollectionsStore = defineStore('collections', () => {
  const tabs = useTabsStore()

  const snapshot = useWorkspaceSnapshot<Collection[]>([])
  const tree = snapshot.state

  const expanded = ref<{ [id: string]: boolean }>({})
  const filter = ref('')
  // Where the next ⌘S puts the next request: the save sheet promises it in as many words.
  const lastSaveTarget = ref('')
  // A collection made by a button has no row to type its name in yet; the tree opens one as soon as
  // the row exists. `pendingCreate` is the same from the other side: the level a new request goes in.
  const pendingRename = ref<string | null>(null)
  const pendingCreate = ref<string | null>(null)
  // The same from the other side again: the row a key or a menu asked to remove, and the section of
  // the page a menu asked to open — the page holds its section itself, so it answers when told.
  const pendingRemove = ref<string | null>(null)
  const pendingSection = ref<CollectionSection | null>(null)

  // What the tree highlights is what the active tab is about: the strip drives the sidebar, and the
  // sidebar never drives the strip. A request tab names its node and a collections tab its
  // collection; the highlight and the tree's own keys are about either one.
  const collectionTabId = computed<string | null>(() =>
    tabs.activeKind === TabKind.TabKindCollections ? tabs.activeTab?.ref || null : null
  )
  const selectedId = computed<string | null>(() =>
    tabs.activeKind === TabKind.TabKindCollections ? collectionTabId.value : tabs.activeTab?.ref || null
  )

  const trail = computed(() => trailOf(tree.value, selectedId.value))
  const selected = computed(() => trail.value?.node ?? null)
  // A request opens as a tab of its node; a collection opens as its own page.
  const cardOpen = computed(() => selected.value !== null)
  const collectionId = computed(
    () => collectionTabId.value ?? holderOf(trail.value)?.id ?? null
  )
  // Whether the work area is in the collections at all — the tree's own keys ask.
  const inCollections = computed(
    () => tabs.activeKind === TabKind.TabKindCollections || selectedId.value !== null
  )
  // The level a new row would join: a folder's row names the folder, a request's names its collection.
  const selectedLevel = computed(() => (selected.value ? collectionId.value : selectedId.value) ?? '')
  // What the open level is called: the status bar names it, or the path to a card inside it.
  const levelName = computed(() => trail.value?.node?.name ?? trail.value?.collection?.name ?? '')
  // A collection runs everything inside it and answers with the empty id; a request is itself.
  const runNodeId = computed(() => selected.value?.id ?? '')

  /** The node-bound request tab on screen, when there is one. */
  const card = computed<RequestInstance | null>(() => {
    const instance = tabs.activeInstance
    return instance && instance.kind === TabKind.TabKindRequest && instance.node.value
      ? instance
      : null
  })
  const pane = computed<PaneSource | null>(() => (cardOpen.value ? tabs.pane : null))

  const editor = computed(() => card.value?.node.value ?? null)
  const dirty = computed(() => card.value?.dirty.value ?? false)
  const response = computed(() => card.value?.response.value ?? null)
  const missing = computed(() => pane.value?.missing ?? [])

  /** Saves the bound request into its collection — the status bar's Save, and the dialog's. */
  async function saveNode() {
    await card.value?.saveNode()
  }

  /** Whether leaving the tab may go ahead unanswered; the dialog is the tabs' own. */
  function askUnsaved(): Promise<boolean> {
    return tabs.activeId ? askCardUnsaved(tabs.activeId) : Promise.resolve(true)
  }

  // The tree is Go's: it is read whole after every change, ours or anyone's, and the strip closes
  // the tabs whose content was removed.
  async function load() {
    const signal = snapshot.signal()
    const loaded = await callOrToast(
      snapshot.take(Collections.Tree(), (tree) => tree ?? []),
      'collections.readFailed'
    )
    if (loaded !== undefined && !signal.aborted) tabs.reconcile(tree.value)
  }

  // A write answers with nothing but what it made; the tree is read again once Go has it.
  async function write<T>(call: Promise<T>): Promise<{ value: T } | undefined> {
    const signal = snapshot.signal()
    let value: T | undefined
    const ok = await runOrToast(call.then((answer) => { value = answer }), 'collections.saveFailed')
    if (!ok || signal.aborted) return undefined
    await load()
    return signal.aborted ? undefined : { value: value as T }
  }
  const change = async (call: Promise<unknown>) => { await write(call) }

  // Another window, an agent, sync or an import changed the tree: it is read again.
  let pending: ReturnType<typeof setTimeout> | null = null
  const offChanged = Events.On('changed', (event) => {
    const kind = event.data?.kind
    if ((kind !== 'collection' && kind !== 'request') || pending) return
    pending = setTimeout(() => {
      pending = null
      void load()
    }, 50)
  })
  onScopeDispose(() => offChanged?.())

  const applyTree = <T extends { tree: Collection[] | null }>(call: CancellablePromise<T>) =>
    snapshot.take(call, (answer) => answer.tree ?? tree.value)

  // What a workspace switch leaves behind: everything that names something of the workspace being left.
  function reset() {
    snapshot.reset()
    runSnapshot.reset()
    expanded.value = {}
    running.value = null
    filter.value = ''
    lastSaveTarget.value = ''
    pendingRename.value = null
    pendingCreate.value = null
    pendingRemove.value = null
    pendingSection.value = null
  }

  // A folder is a collection with a parent; empty is the top of the tree, where the panel's "+" puts it.
  async function createCollection(name: string, parentId = '') {
    const signal = snapshot.signal()
    const created = await write(Collections.CreateCollection(name, '', parentId))
    if (!created || signal.aborted) return
    // The new row comes back by id, so the selection and the rename box land on it.
    await select(created.value.id)
    if (!signal.aborted) pendingRename.value = created.value.id
  }

  async function createNode(collection: string, name: string, method = 'GET') {
    const signal = snapshot.signal()
    const created = await write(Collections.CreateNode({ collectionId: collection, name, method }))
    if (!created || signal.aborted) return
    expanded.value[collection] = true
    // A new request opens straight away: it was made to be filled in.
    await select(created.value.id)
  }

  // A collection or request dropped somewhere; an empty parent is the top level.
  const move = (id: string, parentId: string, position: number) =>
    change(Collections.Move(id, parentId, position))
  const update = (id: string, patch: CollectionPatch) => change(Collections.Update(id, patch))
  const rename = (id: string, name: string) => update(id, { name })
  const setDescription = (id: string, description: string) => update(id, { description })
  // Go refuses a secret: a collection is what gets exported and handed on.
  const saveVariables = (id: string, variables: Variable[]) => update(id, { variables })
  // The level's own auth, which everything inside inherits; "None" is an empty auth.
  const saveAuth = (id: string, auth: Auth) => update(id, { auth })
  const duplicate = (id: string) =>
    change(Collections.Duplicate(id, t('collections.copySuffix')))
  const remove = (id: string) => change(Collections.Delete(id))

  // Saving a request tab makes the tab the node's own: it stays where it was, pin and position
  // kept, and what stands under it becomes the card of the collection it was just saved to.
  function bindTabToNode(nodeId: string, draftID: DraftID) {
    markStep('save')
    const tab = tabs.tabs.find((tab) => tab.kind === TabKind.TabKindRequest && tab.draftId === draftID)
    if (tab) void tabs.adoptNode(tab.id, nodeId)
  }

  // Saving copies the named draft into a collection; the draft is not touched. Answers whether it
  // landed: the sheet has two things to do after it, neither right on a refusal.
  async function saveDraft(collection: string, name: string, draftID: DraftID): Promise<boolean> {
    const signal = snapshot.signal()
    const edited = draftById(draftID)
    if (!edited) return false
    const created = await write(Drafts.SaveAsNode(collection, name, edited))
    if (!created || signal.aborted) return false
    expanded.value[collection] = true
    lastSaveTarget.value = collection
    bindTabToNode(created.value.id, draftID)
    return true
  }

  // A collection nobody has yet, with this request as its first — one call, so a failure cannot leave
  // an empty collection behind.
  async function saveDraftToNew(collection: string, name: string, draftID: DraftID): Promise<boolean> {
    const signal = snapshot.signal()
    const edited = draftById(draftID)
    if (!edited) return false
    const created = await write(Drafts.SaveAsNewCollection(collection, name, edited))
    if (!created || signal.aborted) return false
    expanded.value[created.value.collectionId] = true
    lastSaveTarget.value = created.value.collectionId
    bindTabToNode(created.value.id, draftID)
    return true
  }

  // A row that closes takes what is inside it with it, so reopening it later looks like the first time.
  function toggleExpand(id: string) {
    const open = !expanded.value[id]
    expanded.value[id] = open
    if (open) return
    const collection = findCollection(tree.value, id)
    if (collection) collapseWithChildren(collection)
  }

  function collapseWithChildren(collection: Collection) {
    expanded.value[collection.id] = false
    for (const child of collection.children ?? []) collapseWithChildren(child)
  }

  // Opens the collections above a node, so that the node can be seen in the tree.
  function reveal(id: string) {
    const above = trailOf(tree.value, id)
    if (above) for (const ancestor of above.ancestors) expanded.value[ancestor.id] = true
  }

  // Anything a list opens (the tree, the palette, a row of a run) opens as the tab of its node or
  // the page of its collection, or shows the tab already open for it. A plain click lands in the
  // preview slot and a ⌘-click keeps a tab of its own; the palette's "in the background" stages one
  // without taking the work area. Replacing a dirty preview asks first, in tabs.open.
  async function select(id: string, keep = false, background = false): Promise<boolean> {
    if (id === selectedId.value && !keep && !background) return true
    if (findCollection(tree.value, id)) {
      void loadLastRun(id)
      await tabs.openCollection(id, keep, background)
    } else {
      await tabs.openNode(id, keep, background)
    }
    return true
  }

  const running = ref<{ done: number; total: number; name: string } | null>(null)
  // The last run of a level, live while it goes: the page draws its rows under the requests it ran.
  const runSnapshot = useWorkspaceSnapshot<CollectionRun | null>(null)
  const lastRun = runSnapshot.state

  async function loadLastRun(id: string) {
    await callOrToast(runSnapshot.take(Runs.Last(id, '')), 'collections.readFailed')
  }

  async function run(nodeId: string, name: string) {
    const signal = snapshot.signal()
    const collection = collectionId.value
    if (!collection) return
    // The first progress event says how many requests there are; a guess would show a wrong total.
    running.value = { done: 0, total: 0, name }
    try {
      await Runs.Run(collection, nodeId).cancelOn(signal)
    } catch (error) {
      if (!signal.aborted) running.value = null
      throw error
    }
  }

  async function stop() {
    await callOrToast(Runs.Stop().cancelOn(snapshot.signal()), 'collections.saveFailed')
  }

  // One request of a run has come back: the counters feed the status bar, and the rows fill the
  // level's page while the run goes.
  function applyRunProgress(progress: RunProgress) {
    if (!running.value || !findCollection(tree.value, progress.collectionId)) return
    running.value = { ...running.value, done: progress.done, total: progress.total }

    const current = lastRun.value
    if (current?.id === progress.runId) {
      runSnapshot.reset({ ...current, results: [...(current.results ?? []), progress.result] })
      return
    }
    runSnapshot.reset({
      id: progress.runId,
      collectionId: progress.collectionId,
      nodeId: progress.nodeId,
      environment: progress.environment ?? '',
      startedAt: Date.now(),
      finishedAt: 0,
      durationUs: 0,
      passed: 0,
      failed: 0,
      results: [progress.result],
    })
  }

  function applyRunFinished(finished: CollectionRun) {
    if (!findCollection(tree.value, finished.collectionId)) return
    running.value = null
    runSnapshot.reset(finished)
  }

  // A row of a run, or of a request's history, opens the request with the answer it got; one that
  // never went out opens as a plain card.
  const openRunResult = (result: CollectionRunResult) =>
    openRecordOf(result.nodeId, result.recordId ?? '')

  async function openRecordOf(nodeId: string, recordId: string) {
    const signal = snapshot.signal()
    await select(nodeId)
    if (!recordId || signal.aborted || tabs.activeTab?.ref !== nodeId) return
    // The record is read by the tab it lands in: losing it — pruned history, a cleared database —
    // is not losing the row, and the request is still worth opening.
    await recordInto(tabs.activeId, recordId)
  }

  onScopeDispose(reset)

  return {
    tree,
    reveal,
    selectedId,
    expanded,
    filter,
    lastSaveTarget,
    pendingRename,
    pendingCreate,
    pendingRemove,
    pendingSection,
    trail,
    selected,
    collectionId,
    selectedLevel,
    levelName,
    runNodeId,
    load,
    applyTree,
    reset,
    createCollection,
    createNode,
    move,
    rename,
    setDescription,
    saveVariables,
    saveAuth,
    duplicate,
    remove,
    saveDraft,
    saveDraftToNew,
    toggleExpand,
    // The card the strip shows.
    editor,
    dirty,
    response,
    missing,
    inCollections,
    select,
    saveNode,
    askUnsaved,
    // Runs.
    running,
    lastRun,
    loadLastRun,
    run,
    stop,
    applyRunProgress,
    applyRunFinished,
    openRunResult,
  }
})
