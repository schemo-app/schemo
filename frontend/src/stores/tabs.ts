import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { TabKind, type Collection, type DraftID } from '../../bindings/schemo/internal/domain'
import { Workspaces } from '../../bindings/schemo/internal/workspace'
import { Drafts } from '../../bindings/schemo/internal/request'
import { callOrToast, runOrToast } from '../lib/calls'
import { markStep } from '../composables/useGettingStarted'
import { useSettings } from '../composables/useSettings'
import * as strip from '../lib/strip'
import {
  askUnsaved,
  contentOf,
  dropContent,
  ensureContent,
  keepOnly,
  markBornEmpty,
  paneSource,
  warmContent,
  type AnyInstance,
  type PaneSource,
} from './content'

/** The strip: what is open, in what order, and which tab the work area shows. The window owns its
 *  rules (lib/strip); Go keeps the strip of each workspace and the drafts its request tabs edit. */
export const useTabsStore = defineStore('tabs', () => {
  const state = ref<strip.StripState>({ tabs: [], active: '' })
  const tabs = computed(() => state.value.tabs)
  const activeId = computed(() => state.value.active)

  // The workspace the strip on screen belongs to; empty until it is read and after a reset.
  let workspace = ''
  // Bumped by a reset, so work that awaited across a workspace switch does not land in the next one.
  let generation = 0
  // The last tree the collections reported: a strip read after it is checked against it.
  let tree: Collection[] | null = null
  // The workspaces whose strip this run has read. "Restore tabs" off starts each one fresh once.
  const seen = new Set<string>()
  // Saves and draft deletions go out one after another, so the last strip written is the last one made.
  let writes: Promise<unknown> = Promise.resolve()

  const activeTab = computed(() => tabs.value.find((tab) => tab.id === activeId.value) ?? null)
  const activeKind = computed<TabKind | null>(() => activeTab.value?.kind ?? null)
  const activeInstance = computed<AnyInstance | null>(() =>
    activeId.value ? contentOf(activeId.value) : null
  )

  /** The pane's view of the active tab, for a request: a page draws the window's own stores. */
  const pane = computed<PaneSource | null>(() => {
    const instance = activeInstance.value
    if (!instance || instance.kind !== TabKind.TabKindRequest) return null
    return paneSource(instance)
  })

  function apply(change: strip.Change, beforeContent?: () => void) {
    const unchanged = change.tabs === state.value.tabs && change.active === state.value.active
    state.value = { tabs: change.tabs, active: change.active }
    beforeContent?.()
    keepOnly(tabs.value.map((tab) => tab.id))
    for (const tab of tabs.value) void warmContent(tab)
    const shown = activeTab.value
    if (shown) void ensureContent(shown)
    if (!workspace || (unchanged && change.dropped.length === 0)) return
    const saved = { workspace, tabs: change.tabs, active: change.active }
    writes = writes
      .then(() => runOrToast(Workspaces.SaveTabs(saved), 'errors.tabsFailed'))
      .then(() => Promise.all(change.dropped.map((id) => Drafts.Discard(id).catch(() => {}))))
  }

  async function load() {
    const at = generation
    const read = await callOrToast(Workspaces.Tabs(), 'errors.tabsFailed')
    if (!read || at !== generation) return
    workspace = read.workspace
    let change = strip.restored(read.tabs ?? [], read.active)
    if (!seen.has(workspace)) {
      seen.add(workspace)
      if (useSettings().settings.value?.restoreTabs === false) {
        const left = change.tabs.map((tab) => tab.draftId).filter(strip.isTabDraft) as DraftID[]
        change = { ...strip.restored([], ''), dropped: [...change.dropped, ...left] }
      }
    }
    if (tree) {
      const checked = strip.reconcile(change, tree)
      change = { ...checked, dropped: [...change.dropped, ...checked.dropped] }
    }
    apply(change)
  }

  /** Closes the tabs of collections and requests that are no longer in the tree. */
  function reconcile(next: Collection[]) {
    tree = next
    if (!workspace) return
    const change = strip.reconcile(state.value, next)
    if (change.tabs !== state.value.tabs) apply(change)
  }

  /** Opens the thing named, or shows its tab when it is already open. */
  async function open(input: strip.OpenInput) {
    const at = generation
    const already = strip.indexOfSameThing(tabs.value, input) >= 0
    // A plain open lands in the preview slot, so the preview there now steps aside — and a dirty
    // collection card leaves only after its own dialog.
    if (!input.newTab && input.ref && !already) {
      const preview = tabs.value.find((tab) => tab.preview)
      if (preview && !(await askUnsaved(preview.id))) return
    }
    let draftId = input.draftId
    if (input.kind === TabKind.TabKindRequest && input.record && !draftId && !already) {
      // A record opens as a request laid out from it: the draft is written before the tab exists.
      draftId = strip.mintDraftId()
      if (!(await runOrToast(Drafts.FromRecord(draftId, input.record), 'errors.tabsFailed'))) return
    }
    if (at !== generation) return
    apply(strip.open(state.value, { ...input, draftId }))
  }

  /** A blank request tab at the end — the strip's "+". */
  function newTab() {
    const change = strip.append(state.value)
    apply(change, () => markBornEmpty(change.active))
  }

  /** Opens the overview page — one of it, as with the history. */
  async function openWelcome(background = false) {
    await open({ kind: TabKind.TabKindWelcome, background })
  }

  /** Opens the history page — the strip keeps one, wherever it was opened from. */
  async function openHistory(background = false) {
    await open({ kind: TabKind.TabKindHistory, background })
  }

  /** Opens the capture page — one of it, as with the history. */
  async function openCapture(background = false) {
    markStep('capture')
    await open({ kind: TabKind.TabKindCapture, background })
  }

  /** Opens the pages this workspace has published — one of it, as with the history. */
  async function openPublications(background = false) {
    await open({ kind: TabKind.TabKindPubs, background })
  }

  /** Opens a collection's page. */
  async function openCollection(id: string, keep = false, background = false) {
    await open({ kind: TabKind.TabKindCollections, ref: id, newTab: keep, background })
  }

  /** Opens a saved request to edit: the node's draft is the thing, so a plain click is a preview. */
  async function openNode(id: string, keep = false, background = false) {
    await open({ kind: TabKind.TabKindRequest, ref: id, draftId: id as DraftID, newTab: keep, background })
  }

  /** Opens a history record — sent by hand or captured: both are requests laid out from it. */
  async function openRecord(id: string, keep = false, background = false) {
    await open({ kind: TabKind.TabKindRequest, record: id, newTab: keep, background })
  }

  /** Opens an independent editable copy of a history request. */
  async function editRecord(id: string) {
    const at = generation
    const draftId = strip.mintDraftId()
    if (!(await runOrToast(Drafts.FromRecord(draftId, id), 'errors.tabsFailed')) || at !== generation) return
    apply(strip.open(state.value, { kind: TabKind.TabKindRequest, draftId, newTab: true }))
  }

  // How many times a tab has been turned to. The same tab clicked again is still a turn to it —
  // the work area may be showing a section's own page, and this is how it steps aside.
  const activationTick = ref(0)

  function activate(id: string) {
    activationTick.value++
    apply(strip.activate(state.value, id))
  }

  // What leaving a set of tabs takes: a bound request with unsaved edits answers first — and a "no"
  // leaves every tab where it is — and a keystroke still owed is written home before its tab is gone.
  async function closeTabs(ids: string[], change: () => strip.Change) {
    const at = generation
    for (const id of ids) {
      if (!(await askUnsaved(id)) || at !== generation) return
    }
    for (const id of ids) {
      const content = contentOf(id)
      if (content && 'draft' in content) {
        if (!(await content.draft.flush()) || at !== generation) return
      }
    }
    apply(change())
  }

  async function close(id: string) {
    if (!tabs.value.some((tab) => tab.id === id)) return
    await closeTabs([id], () => strip.closeOne(state.value, id))
  }

  async function closeOthers(id: string) {
    if (!tabs.value.some((tab) => tab.id === id)) return
    const closing = tabs.value.filter((tab) => tab.id !== id && !tab.pinned).map((closed) => closed.id)
    await closeTabs(closing, () => strip.closeOthers(state.value, id))
  }

  async function closeRight(id: string) {
    const at = tabs.value.findIndex((tab) => tab.id === id)
    if (at < 0) return
    const closing = tabs.value.slice(at + 1).filter((one) => !one.pinned).map((closed) => closed.id)
    await closeTabs(closing, () => strip.closeRight(state.value, id))
  }

  function pin(id: string, pinned: boolean) {
    apply(strip.pin(state.value, id, pinned))
  }

  function keep(id: string) {
    apply(strip.keep(state.value, id))
  }

  function move(id: string, to: string) {
    apply(strip.move(state.value, id, to))
  }

  /** Copies a request tab right after itself, onto a copy of its draft. */
  async function duplicate(id: string) {
    const at = generation
    const source = tabs.value.find((tab) => tab.id === id)
    if (!source?.draftId || strip.isPage(source.kind)) return
    const content = contentOf(id)
    if (content && 'draft' in content && !(await content.draft.flush())) return
    const draftId = strip.mintDraftId()
    if (!(await runOrToast(Drafts.Copy(source.draftId, draftId), 'errors.tabsFailed')) || at !== generation) return
    apply(strip.duplicate(state.value, id, draftId))
  }

  /** A request tab is pointed at the node its draft has just been saved as. */
  function adoptNode(id: string, nodeId: string) {
    apply(strip.adopt(state.value, id, nodeId), () => dropContent(id))
  }

  /** What a workspace switch leaves behind: the strip belongs to the workspace being left. */
  function reset() {
    generation++
    workspace = ''
    tree = null
    state.value = { tabs: [], active: '' }
    keepOnly([])
  }

  onScopeDispose(reset)

  return {
    tabs,
    activeId,
    activeTab,
    activeKind,
    activeInstance,
    pane,
    activationTick,
    load,
    reconcile,
    open,
    newTab,
    openWelcome,
    openHistory,
    openCapture,
    openPublications,
    openCollection,
    openNode,
    openRecord,
    editRecord,
    activate,
    close,
    closeOthers,
    closeRight,
    pin,
    keep,
    move,
    duplicate,
    adoptNode,
    reset,
  }
})
