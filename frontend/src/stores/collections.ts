import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { type CancellablePromise } from '@wailsio/runtime'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'
import { findCollection, holderOf, requestCount, trailOf } from '../lib/collectionTree'
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
import type { RunProgress } from '../../bindings/schemo/internal/usecase/collection'
import { CollectionsService } from '../../bindings/schemo/internal/transport/wails'
import { callOrToast } from './calls'
import { markStep } from '../composables/useGettingStarted'
import { useTabsStore } from './tabs'
import {
  askUnsaved as askCardUnsaved,
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

  // ---- the tree ----------------------------------------------------------------

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
  // sidebar never drives the strip. A request tab names its node; a collections tab names its
  // collection — the highlight and the tree's own keys are about either one.
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
  // What running the selection would send, which is the number the overview counts.
  const selectedRequestCount = computed(() => {
    const at = trail.value
    if (!at) return 0
    if (at.node) return 1
    return at.collection ? requestCount(at.collection) : 0
  })
  // A collection runs everything inside it and answers with the empty id; a request is itself.
  const runNodeId = computed(() => selected.value?.id ?? '')

  // ---- the request the strip shows -------------------------------------------------

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

  async function load() {
    await callOrToast(snapshot.take(CollectionsService.Tree(), (tree) => tree ?? []), 'collections.readFailed')
  }

  // Every change answers with the whole tree, so the mirror is replaced rather than patched: the
  // order rows are drawn in is Go's. The selection is the strip's, and a tab whose node the tree no
  // longer has keeps its tab: what it shows is the node's own empty state.
  async function changeTree(call: CancellablePromise<Collection[] | null>) {
    await callOrToast(snapshot.take(call, (tree) => tree ?? []), 'collections.saveFailed')
  }

  const takeTree = <T extends { tree: Collection[] | null }>(call: CancellablePromise<T>) =>
    snapshot.take(call, (answer) => answer.tree ?? tree.value)

  // What a workspace switch leaves behind: everything that names something of the space being left.
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

  // A folder is a collection with a parent; empty is the top of the tree, where the panel's «+» puts it.
  async function createCollection(name: string, parentId = '') {
    const signal = snapshot.signal()
    const created = await callOrToast(
      takeTree(CollectionsService.CreateCollection(name, '', parentId)),
      'collections.saveFailed'
    )
    if (!created || signal.aborted) return
    // The new row comes back by id, so the selection and the rename box land on it.
    await select(created.collection.id)
    if (!signal.aborted) pendingRename.value = created.collection.id
  }

  async function createNode(collection: string, name: string, method = 'GET') {
    const signal = snapshot.signal()
    const created = await callOrToast(
      takeTree(CollectionsService.CreateNode({ collectionId: collection, name, method })),
      'collections.saveFailed'
    )
    if (!created || signal.aborted) return
    expanded.value[collection] = true
    // A new request opens straight away: it was made to be filled in.
    await select(created.node.id)
  }

  const moveNode = (id: string, collection: string, position: number) =>
    changeTree(CollectionsService.MoveNode(id, collection, position))
  // A collection dropped into another one, or back at the top level when the parent is empty.
  const moveCollection = (id: string, parentId: string, position: number) =>
    changeTree(CollectionsService.MoveCollection(id, parentId, position))
  const rename = (id: string, name: string) => changeTree(CollectionsService.Rename(id, name))
  const describe = (id: string, text: string) => changeTree(CollectionsService.Describe(id, text))
  // What the collection says on the page it is published as, beside the line above it. The whole
  // set arrives as the tab holds it: the three notes belong to one document, and one save is one
  // answer about all of them.
  // The `{{tokens}}` a level answers for everything inside it; Go refuses a secret, since a collection
  // is what gets exported and handed on.
  const saveVariables = (id: string, variables: Variable[]) =>
    changeTree(CollectionsService.SaveVariables(id, variables))
  // The level's own auth, which everything inside inherits; "None" is an empty auth.
  const saveAuth = (id: string, auth: Auth) => changeTree(CollectionsService.SaveAuth(id, auth))
  const duplicate = (id: string) =>
    changeTree(CollectionsService.Duplicate(id, t('collections.copySuffix')))
  const remove = (id: string) => changeTree(CollectionsService.Delete(id))

  // Saving a request tab makes the tab the node's own: it stays where it was, pin and position
  // kept, and what stands under it becomes the card of the collection it was just saved to.
  function adopted(nodeId: string, draftID: DraftID) {
    markStep('save')
    const tab = tabs.tabs.find((tab) => tab.kind === TabKind.TabKindRequest && tab.draftId === draftID)
    if (tab) void tabs.adoptNode(tab.id, nodeId)
  }

  // Saving copies the named draft into a collection; the draft is not touched. Answers whether it
  // landed: the sheet has two things to do after it, neither right on a refusal.
  async function saveDraft(collection: string, name: string, draftID: DraftID): Promise<boolean> {
    const signal = snapshot.signal()
    const created = await callOrToast(
      takeTree(CollectionsService.SaveDraft(collection, name, draftID)),
      'collections.saveFailed'
    )
    if (!created || signal.aborted) return false
    expanded.value[collection] = true
    lastSaveTarget.value = collection
    adopted(created.node.id, draftID)
    return true
  }

  // A collection nobody has yet, with this request as its first — one call, so a failure cannot leave
  // an empty collection behind.
  async function saveDraftToNew(collection: string, name: string, draftID: DraftID): Promise<boolean> {
    const signal = snapshot.signal()
    const created = await callOrToast(
      takeTree(CollectionsService.SaveDraftToNew(collection, name, draftID)),
      'collections.saveFailed'
    )
    if (!created || signal.aborted) return false
    expanded.value[created.node.collectionId] = true
    lastSaveTarget.value = created.node.collectionId
    adopted(created.node.id, draftID)
    return true
  }

  // A row that closes takes what is inside it with it, so reopening it later looks like the first time.
  function toggleExpand(id: string) {
    const open = !expanded.value[id]
    expanded.value[id] = open
    if (open) return
    const collection = findCollection(tree.value, id)
    if (collection) closeBelow(collection)
  }

  function closeBelow(collection: Collection) {
    expanded.value[collection.id] = false
    for (const child of collection.children ?? []) closeBelow(child)
  }

  function setFilter(text: string) {
    filter.value = text
  }

  // Opens the collections above a node, so that the node can be seen in the tree.
  function reveal(id: string) {
    const above = trailOf(tree.value, id)
    if (above) for (const ancestor of above.ancestors) expanded.value[ancestor.id] = true
  }

  // Anything a list opens — the tree, the palette, a row of a run — opens as the tab of its node
  // or the page of its collection, or shows the tab already open for it. A plain click lands in the
  // preview slot and a ⌘-click keeps a tab of its own; the palette's «in the background» stages
  // one without taking the work area. Replacing a dirty preview asks first, in tabs.open.
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

  // ---- runs ------------------------------------------------------------------------

  const running = ref<{ done: number; total: number; name: string } | null>(null)
  // The last run of a level, live while it goes: the page draws its rows under the requests it ran.
  const runSnapshot = useWorkspaceSnapshot<CollectionRun | null>(null)
  const lastRun = runSnapshot.state

  async function loadLastRun(id: string) {
    await callOrToast(runSnapshot.take(CollectionsService.LastRun(id, '')), 'collections.readFailed')
  }

  async function run(nodeId: string, name: string) {
    const signal = snapshot.signal()
    const collection = collectionId.value
    if (!collection) return
    // The first progress event says how many requests there are; a guess would show a wrong total.
    running.value = { done: 0, total: 0, name }
    try {
      await CollectionsService.Run(collection, nodeId).cancelOn(signal)
    } catch (error) {
      if (!signal.aborted) running.value = null
      throw error
    }
  }

  async function stop() {
    await callOrToast(CollectionsService.Stop().cancelOn(snapshot.signal()), 'collections.saveFailed')
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
  const openSend = (nodeId: string, recordId: string) => openRecordOf(nodeId, recordId)

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
    cardOpen,
    collectionId,
    selectedLevel,
    levelName,
    selectedRequestCount,
    runNodeId,
    load,
    takeTree,
    reset,
    createCollection,
    createNode,
    moveNode,
    moveCollection,
    rename,
    describe,
    saveVariables,
    saveAuth,
    duplicate,
    remove,
    saveDraft,
    saveDraftToNew,
    toggleExpand,
    setFilter,
    // The card the strip shows.
    editor,
    dirty,
    response,
    missing,
    inCollections,
    collectionTabId,
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
    openSend,
  }
})
