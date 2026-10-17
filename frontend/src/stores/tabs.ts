import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import type { CancellablePromise } from '@wailsio/runtime'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'
import { TabKind, type DraftID } from '../../bindings/schemo/internal/domain'
import type { OpenInput, Strip } from '../../bindings/schemo/internal/usecase/tabstrip'
import { TabsService } from '../../bindings/schemo/internal/transport/wails'
import { callOrToast } from './calls'
import { markStep } from '../composables/useGettingStarted'
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

/** The strip: what is open, in what order, and which tab the work area shows. Go owns it — every
 *  call here adopts its whole answer — and what each tab holds is the content module's. */
export const useTabsStore = defineStore('tabs', () => {
  const snapshot = useWorkspaceSnapshot<Strip>({ tabs: [], active: '' })
  const tabs = computed(() => snapshot.state.value.tabs ?? [])
  const activeId = computed(() => snapshot.state.value.active)

  const activeTab = computed(() => tabs.value.find((t) => t.id === activeId.value) ?? null)
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

  async function change(call: CancellablePromise<Strip>, beforeContent?: (strip: Strip) => void) {
    const signal = snapshot.signal()
    const strip = await callOrToast(snapshot.take(call), 'errors.tabsFailed')
    if (!strip || signal.aborted || snapshot.state.value !== strip) return
    beforeContent?.(strip)
    keepOnly(tabs.value.map((tab) => tab.id))
    for (const tab of tabs.value) void warmContent(tab)
    const shown = activeTab.value
    if (shown) void ensureContent(shown)
  }

  async function load() {
    await change(TabsService.Strip())
  }

  /** Opens the thing named, or shows its tab when it is already open. */
  async function open(input: OpenInput) {
    // A plain open lands in the preview slot, so the preview there now steps aside — and a dirty
    // collection card leaves only after its own dialog. What is already open is shown, not replaced.
    const signal = snapshot.signal()
    const ref = input.ref ?? ''
    const replacing = !(input.newTab ?? false)
    if (replacing && ref !== '' && !tabs.value.some((t) => t.kind === input.kind && t.ref === ref)) {
      const preview = tabs.value.find((t) => t.preview)
      if (preview && !(await askUnsaved(preview.id))) return
    }
    if (signal.aborted) return
    await change(TabsService.Open({
      kind: input.kind,
      ref,
      draftId: input.draftId,
      record: input.record ?? '',
      newTab: input.newTab ?? false,
      background: input.background ?? false,
    }))
  }

  /** A blank request tab at the end — the strip's «+». */
  async function newTab() {
    await change(TabsService.New(), (strip) => markBornEmpty(strip.active))
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

  /** Opens the pages this space has published — one of it, as with the history. */
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

  async function editRecord(id: string) {
    await change(TabsService.EditRecord(id))
  }

  // How many times a tab has been turned to. The same tab clicked again is still a turn to it —
  // the work area may be showing a section's own page, and this is how it steps aside.
  const activationTick = ref(0)

  async function activate(id: string) {
    activationTick.value++
    await change(TabsService.Activate(id))
  }

  // What leaving a set of tabs takes: a bound request with unsaved edits answers first — and a «no»
  // leaves every tab where it is — and a keystroke still owed is written home before its tab is gone.
  async function closeTabs(ids: string[], command: () => CancellablePromise<Strip>) {
    const signal = snapshot.signal()
    for (const id of ids) {
      if (!(await askUnsaved(id)) || signal.aborted) return
    }
    for (const id of ids) {
      const content = contentOf(id)
      if (content && 'draft' in content) {
        if (!(await content.draft.flush()) || signal.aborted) return
      }
    }
    if (!signal.aborted) await change(command())
  }

  async function close(id: string) {
    if (!tabs.value.some((t) => t.id === id)) return
    await closeTabs([id], () => TabsService.Close(id))
  }

  async function closeOthers(id: string) {
    if (!tabs.value.some((tab) => tab.id === id)) return
    const closing = tabs.value.filter((t) => t.id !== id && !t.pinned).map((t) => t.id)
    await closeTabs(closing, () => TabsService.CloseOthers(id))
  }

  async function closeRight(id: string) {
    const at = tabs.value.findIndex((t) => t.id === id)
    if (at < 0) return
    const closing = tabs.value.slice(at + 1).filter((t) => !t.pinned).map((t) => t.id)
    await closeTabs(closing, () => TabsService.CloseRight(id))
  }

  async function pin(id: string, pinned: boolean) {
    await change(TabsService.Pin(id, pinned))
  }

  async function keep(id: string) {
    await change(TabsService.Keep(id))
  }

  async function move(id: string, to: string) {
    await change(TabsService.Move(id, to))
  }

  async function duplicate(id: string) {
    const signal = snapshot.signal()
    const content = contentOf(id)
    if (content && 'draft' in content && !(await content.draft.flush())) return
    if (signal.aborted) return
    await change(TabsService.Duplicate(id))
  }

  async function setRecord(id: string, recordId: string) {
    await change(TabsService.SetRecord(id, recordId))
  }

  /** A request tab is pointed at the node its draft has just been saved as. */
  async function adoptNode(id: string, nodeId: string) {
    await change(TabsService.Adopt(id, nodeId), () => dropContent(id))
  }

  /** What a workspace switch leaves behind: the strip is the session's, and the space is another. */
  function reset() {
    snapshot.reset()
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
    setRecord,
    adoptNode,
    reset,
  }
})
