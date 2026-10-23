import { TabKind, type Collection, type DraftID, type Tab } from '../../bindings/schemo/internal/domain'

/**
 * The rules of the tab strip, as pure functions. A plain open lands in the preview slot, a ⌘-open
 * keeps a tab of its own, a close falls to the neighbour, pinned tabs stand first, and a page (the
 * history, the capture…) is one however it was reached. Each change answers with the strip after
 * it and the tab drafts that fell out, which the caller deletes.
 */
export interface StripState {
  tabs: Tab[]
  active: string
}

export interface Change extends StripState {
  dropped: DraftID[]
}

export interface OpenInput {
  kind: TabKind
  ref?: string
  draftId?: DraftID
  record?: string
  /** The ⌘-click: a tab of its own rather than the preview slot. */
  newTab?: boolean
  /** Opens without showing it. */
  background?: boolean
}

const PAGES: ReadonlySet<TabKind> = new Set([
  TabKind.TabKindWelcome,
  TabKind.TabKindHistory,
  TabKind.TabKindCapture,
  TabKind.TabKindPubs,
])
const KINDS: ReadonlySet<string> = new Set([...PAGES, TabKind.TabKindRequest, TabKind.TabKindCollections])

export const isPage = (kind: TabKind) => PAGES.has(kind)
export const isTabDraft = (id: string | null | undefined) => id?.startsWith('tab:') ?? false
export const mintDraftId = () => `tab:${crypto.randomUUID()}` as DraftID
const mintTabId = () => crypto.randomUUID()

const draftsOf = (tabs: Tab[]) => tabs.map((tab) => tab.draftId).filter(isTabDraft) as DraftID[]
const indexOf = (tabs: Tab[], id: string) => tabs.findIndex((tab) => tab.id === id)

function done(tabs: Tab[], active: string, dropped: DraftID[] = []): Change {
  // Pinned tabs stand before the others, each group in its own order.
  const sorted = [...tabs.filter((tab) => tab.pinned), ...tabs.filter((tab) => !tab.pinned)]
  return { tabs: sorted, active, dropped }
}

const same = (s: StripState): Change => ({ tabs: s.tabs, active: s.active, dropped: [] })

function insertAfter(tabs: Tab[], after: string, tab: Tab): Tab[] {
  const at = indexOf(tabs, after)
  const next = [...tabs]
  next.splice(at < 0 ? tabs.length : at + 1, 0, tab)
  return next
}

/** A new request with nothing in it. */
function blankTab(): Tab {
  return { id: mintTabId(), kind: TabKind.TabKindRequest, draftId: mintDraftId(), pinned: false, preview: false }
}

/** The tab already about what an open names, or -1: a record by the record, the rest by what it edits. */
export function indexOfSameThing(tabs: Tab[], input: OpenInput): number {
  if (input.record) return tabs.findIndex((tab) => tab.kind === TabKind.TabKindRequest && tab.record === input.record)
  if (input.ref) return tabs.findIndex((tab) => tab.kind === input.kind && tab.ref === input.ref)
  return -1
}

/** A stored strip as this build can draw it: unknown kinds go, and the shown tab is one that is there. */
export function restored(tabs: Tab[], active: string): Change {
  const kept = tabs.filter((tab) => KINDS.has(tab.kind) && tab.id)
  const dropped = draftsOf(tabs.filter((tab) => !kept.includes(tab)))
  if (kept.length === 0) {
    const blank = blankTab()
    return done([blank], blank.id, dropped)
  }
  return done(kept, indexOf(kept, active) >= 0 ? active : kept[0].id, dropped)
}

/** Shows the thing named, opening a tab for it when none is open. */
export function open(s: StripState, input: OpenInput): Change {
  const show = (id: string) => (input.background ? s.active : id)
  if (isPage(input.kind)) {
    const page = s.tabs.find((tab) => tab.kind === input.kind)
    if (page) return done(s.tabs, show(page.id))
    // A page never takes the preview slot: that slot is for the thing being looked at.
    const tab: Tab = { id: mintTabId(), kind: input.kind, pinned: false, preview: false }
    return done(insertAfter(s.tabs, s.active, tab), show(tab.id))
  }

  const at = indexOfSameThing(s.tabs, input)
  if (at >= 0) {
    const tabs = [...s.tabs]
    if (input.newTab) tabs[at] = { ...tabs[at], preview: false }
    return done(tabs, show(tabs[at].id))
  }

  const tab: Tab = {
    id: mintTabId(),
    kind: input.kind,
    ref: input.ref || undefined,
    record: input.record || undefined,
    draftId: input.draftId ?? (input.kind === TabKind.TabKindRequest ? mintDraftId() : undefined),
    pinned: false,
    preview: !input.newTab,
  }
  const preview = s.tabs.findIndex((one) => one.preview)
  if (input.newTab || preview < 0) return done(insertAfter(s.tabs, s.active, tab), show(tab.id))
  // The old preview steps aside in place, taking its draft with it.
  const tabs = [...s.tabs]
  const dropped = draftsOf([tabs[preview]])
  tabs[preview] = tab
  return done(tabs, show(tab.id), dropped)
}

/** A blank request tab at the end, shown: the strip's "+". */
export function append(s: StripState): Change {
  const tab = blankTab()
  return done([...s.tabs, tab], tab.id)
}

export function activate(s: StripState, id: string): Change {
  return indexOf(s.tabs, id) < 0 ? same(s) : done(s.tabs, id)
}

/**
 * Closes the tabs `closes` names. The shown tab survives when it can; otherwise the named tab is
 * shown when it survived, and the tab that took the old place when it did not. The last tab may go.
 */
export function close(s: StripState, named: string, closes: (tab: Tab, at: number) => boolean): Change {
  if (indexOf(s.tabs, named) < 0) return same(s)
  const kept = s.tabs.filter((tab, at) => !closes(tab, at))
  const dropped = draftsOf(s.tabs.filter((tab) => !kept.includes(tab)))
  if (kept.length === 0) return done([], '', dropped)
  if (indexOf(kept, s.active) >= 0) return done(kept, s.active, dropped)
  const survivor = indexOf(kept, named)
  const at = survivor >= 0 ? survivor : Math.min(Math.max(indexOf(s.tabs, s.active), 0), kept.length - 1)
  return done(kept, kept[at].id, dropped)
}

export const closeOne = (s: StripState, id: string) => close(s, id, (tab) => tab.id === id)

/** Every tab but the named one and the pinned ones. */
export const closeOthers = (s: StripState, id: string) => close(s, id, (tab) => tab.id !== id && !tab.pinned)

/** Every unpinned tab after the named one. */
export function closeRight(s: StripState, id: string): Change {
  const from = indexOf(s.tabs, id)
  return close(s, id, (tab, at) => !tab.pinned && at > from)
}

function update(s: StripState, id: string, patch: Partial<Tab>): Change {
  const at = indexOf(s.tabs, id)
  if (at < 0) return same(s)
  const tabs = [...s.tabs]
  tabs[at] = { ...tabs[at], ...patch }
  return done(tabs, s.active)
}

/** A pin is never a preview. */
export const pin = (s: StripState, id: string, pinned: boolean) =>
  update(s, id, pinned ? { pinned, preview: false } : { pinned })

/** Takes a preview out of the preview slot for good. */
export const keep = (s: StripState, id: string) => update(s, id, { preview: false })

/** Drags a tab onto another's place, taking its pin; a dragged tab is never a preview. */
export function move(s: StripState, id: string, to: string): Change {
  const from = indexOf(s.tabs, id)
  const target = indexOf(s.tabs, to)
  if (from < 0 || target < 0 || id === to) return same(s)
  const tabs = [...s.tabs]
  const [moved] = tabs.splice(from, 1)
  tabs.splice(target, 0, { ...moved, pinned: s.tabs[target].pinned, preview: false })
  return done(tabs, s.active)
}

/**
 * Copies a request tab right after itself onto a draft the caller has copied already. The copy
 * edits no saved request: two editors of one node would be one request in two tabs.
 */
export function duplicate(s: StripState, id: string, draftId: DraftID): Change {
  const source = s.tabs.find((tab) => tab.id === id)
  if (!source || isPage(source.kind)) return same(s)
  const copy: Tab = { ...source, id: mintTabId(), ref: undefined, draftId, pinned: false, preview: false }
  return done(insertAfter(s.tabs, id, copy), copy.id)
}

/** Points a request tab at the node it was just saved as; the draft it composed goes. */
export function adopt(s: StripState, id: string, nodeId: string): Change {
  const source = s.tabs.find((tab) => tab.id === id)
  if (!source || source.kind !== TabKind.TabKindRequest) return same(s)
  const change = update(s, id, { ref: nodeId, draftId: nodeId as DraftID, preview: false })
  return { ...change, dropped: source.draftId !== nodeId ? draftsOf([source]) : [] }
}

/** Closes the tabs whose collection or saved request is gone from the tree. */
export function reconcile(s: StripState, tree: Collection[]): Change {
  const refs = new Map<string, TabKind>()
  const visit = (levels: Collection[] | null) => {
    for (const level of levels ?? []) {
      refs.set(level.id, TabKind.TabKindCollections)
      for (const node of level.items ?? []) refs.set(node.id, TabKind.TabKindRequest)
      visit(level.children)
    }
  }
  visit(tree)
  const gone = (tab: Tab) => !!tab.ref && refs.get(tab.ref) !== tab.kind
  const kept = s.tabs.filter((tab) => !gone(tab))
  if (kept.length === s.tabs.length) return same(s)
  const dropped = draftsOf(s.tabs.filter(gone))
  if (kept.length === 0) return done([], '', dropped)
  if (indexOf(kept, s.active) >= 0) return done(kept, s.active, dropped)
  const at = Math.min(Math.max(indexOf(s.tabs, s.active), 0), kept.length - 1)
  return done(kept, kept[at].id, dropped)
}
