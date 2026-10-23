<script setup lang="ts">
import { computed, ref } from 'vue'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { useTabsStore } from '../../stores/tabs'
import { useRecordsStore } from '../../stores/records'
import { useCollectionsStore } from '../../stores/collections'
import { contentOf, nodeOf } from '../../stores/content'
import { methodInkClass } from '../../lib/format'
import { tabView } from '../../lib/tabView'
import { findCollection } from '../../lib/collectionTree'
import { useCommands } from '../../composables/useCommands'
import { useSlotDrag } from '../../composables/useSlotDrag'
import { useTabRow } from '../../composables/useTabRow'
import { saveRequest } from '../../lib/intents'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import AppTabSwitcher from './AppTabSwitcher.vue'
import ContextMenuContent from '../ui/ContextMenuContent.vue'
import ContextMenuItem from '../ui/ContextMenuItem.vue'
import ContextMenuSeparator from '../ui/ContextMenuSeparator.vue'
import { ContextMenuRoot as ContextMenu, ContextMenuTrigger } from 'reka-ui'
import type { Tab } from '../../../bindings/schemo/internal/domain'

const tabs = useTabsStore()
const records = useRecordsStore()
const collections = useCollectionsStore()
const { hint } = useCommands()

const hovered = ref('')
const isCompactTab = (tab: (typeof tabs.tabs)[number]) => tab.pinned || tab.kind === TabKind.TabKindCollections
const ordered = computed(() => [
  ...tabs.tabs.filter(isCompactTab),
  ...tabs.tabs.filter((tab) => !isCompactTab(tab)),
])
const { stripEl, scrollEl, tabW, tiny, compact, visibleIds, hiddenCount, hold, release } = useTabRow({
  tabs: () => ordered.value.map((tab) => ({ id: tab.id, compact: isCompactTab(tab) })),
  activeId: () => tabs.activeId,
})
const movable = computed(() => ordered.value.filter((tab) => !isCompactTab(tab) && visibleIds.value.has(tab.id)))

const { drag, settle, noAnim, press, offsetOf } = useSlotDrag({
  axis: 'x',
  ids: () => movable.value.map((tab) => tab.id),
  size: () => tabW.value + 2,
  drop: async (id, to) => {
    const onto = movable.value[to]?.id
    if (onto) await tabs.move(id, onto)
  },
})

const {
  renaming,
  draft: renameDraft,
  start: startRename,
  commit: commitRename,
  cancel: cancelRename,
  focusOnce: renameRef,
} = useTabRename((nodeId, name) => collections.rename(nodeId, name))

// Where a tab stands while another is carried: slid out of the way, lifted under the pointer, or
// settling into the slot it was let go at.
function liftOf(index: number, id: string) {
  const off = offsetOf(index, id)
  const settling = settle.value?.id === id
  const lifted = drag.value?.idx === index && !settling
  return {
    shift: off !== 0 ? `translateX(${off}px)` : 'none',
    z: lifted || settling ? 5 : 1,
    shadow: lifted ? '0 6px 18px rgba(0, 0, 0, 0.16)' : 'none',
    transition:
      lifted || noAnim.value
        ? 'none'
        : 'transform 0.2s cubic-bezier(.2,.8,.2,1), width 0.2s cubic-bezier(.2,.8,.2,1)',
  }
}

const allViews = computed(() =>
  ordered.value.map((tab, position) => {
    const instance = contentOf(tab.id)
    const draft =
      instance && instance.kind === TabKind.TabKindRequest
        ? { method: instance.draft.method.value, url: instance.draft.url.value }
        : null
    const view = tabView(tab, {
      tree: collections.tree,
      records: records.records,
      // What the tab is being edited into is a fact the draft holds; a tab that was never shown
      // has no draft yet, and its record (or its kind) says what it is meanwhile.
      draft,
    })
    // What the menu can make of the tab: a node to rename, a request to save into a collection.
    const node = nodeOf(tab.id)
    const active = tab.id === tabs.activeId
    const carried = drag.value
    const over = hovered.value === tab.id && !carried
    const dirty = instance?.dirty.value ?? false
    const closeOk = !carried && !tab.pinned && (active || (over && !compact.value))
    const fixed = isCompactTab(tab)
    const i = movable.value.findIndex((one) => one.id === tab.id)
    const { shift, z, shadow, transition } = fixed
      ? { shift: 'none', z: 1, shadow: 'none', transition: 'none' }
      : liftOf(i, tab.id)
    return {
      tab,
      fixed,
      active,
      dirty,
      draft,
      node: node?.node ?? null,
      ...view,
      pinEnd: fixed && !isCompactTab(ordered.value[position + 1] ?? { pinned: false, kind: TabKind.TabKindRequest }),
      pill: over && !active,
      showIcon: fixed || !(tiny.value && active),
      showTitle: !fixed && !tiny.value,
      showX: !fixed && closeOk && !(dirty && !over),
      showDot: !fixed && dirty && !(over && closeOk) && !(tiny.value && !active),
      style: {
        width: fixed ? '32px' : `${tabW.value}px`,
        justifyContent: fixed || tiny.value ? 'center' : 'flex-start',
        padding: fixed || tiny.value ? '0' : '0 6px 0 10px',
        transform: shift,
        zIndex: String(z),
        boxShadow: shadow === 'none' && active ? '0 0.5px 1.5px rgba(0, 0, 0, 0.12), 0 0 0 0.5px var(--border)' : shadow,
        transition,
      },
    }
  })
)

const views = computed(() => allViews.value.filter((entry) => visibleIds.value.has(entry.tab.id)))
const switcherEntries = computed(() => allViews.value.map((entry) => ({
  id: entry.tab.id,
  title: entry.title,
  method: entry.method,
  icon: entry.icon,
  preview: !!entry.tab.preview,
  dirty: entry.dirty,
  pinned: entry.tab.pinned,
  sub: entry.node ? findCollection(collections.tree, entry.node.collectionId)?.name ?? '' : '',
})))

// Pressing a tab shows it: the gesture and the choice are one move, and the drag that may follow
// starts from the same place.
function onTabDown(event: PointerEvent, id: string) {
  if (event.button !== 0) return
  void tabs.activate(id)
  const at = movable.value.findIndex((tab) => tab.id === id)
  if (at >= 0) press(event, id, at)
}

// Closing one tab must not re-flow the rest under the pointer: the width is held until the pointer
// leaves the strip, and a tab that opens is what releases it.
function closeTab(id: string) {
  hold()
  void tabs.close(id)
}

function openTab() {
  release()
  void tabs.newTab()
}

function leaveStrip() {
  if (!drag.value) release()
  hovered.value = ''
}

// Saving is the tab's own panel: the tab is shown, and the request on screen opens it over the
// bookmark — the panel belongs to whoever is on screen, and that is the tab being saved.
async function saveToCollection(id: string) {
  await tabs.activate(id)
  void saveRequest.trigger()
}

// Where the tab lives in the structure: its node in the tree, its record in the history.
function reveal(entry: (typeof views.value)[number]) {
  if (entry.tab.kind === TabKind.TabKindCollections || entry.tab.ref) {
    collections.reveal(entry.tab.ref ?? '')
    return
  }
  if (entry.tab.record) void tabs.openHistory()
}

// A double click keeps a preview; for a saved request it starts its rename. The window's header
// underneath takes a double click as "zoom", so the tab's own meaning stays on the tab.
function onTabDbl(entry: (typeof views.value)[number], event: MouseEvent) {
  event.stopPropagation()
  if (entry.tab.preview) void tabs.keep(entry.tab.id)
  else startRename(entry)
}

const closeHint = computed(() =>
  hint('closeTab') ? `${t('tabs.closeTab')} · ${hint('closeTab')}` : t('tabs.closeTab')
)

/** What renaming needs to know about a tab on screen. */
interface Renamable {
  tab: Pick<Tab, 'id' | 'ref'>
  /** The node the tab edits; a tab without one has nothing to rename. */
  node: unknown
  title: string
}

// A saved request is named by its node, so renaming the tab renames the node — the tree and the tab
// then say the same thing.
function useTabRename(rename: (nodeId: string, name: string) => Promise<unknown>) {
  const renaming = ref('')
  const draft = ref('')

  function start(entry: Renamable) {
    if (!entry.node) return
    renaming.value = entry.tab.id
    draft.value = entry.title
  }

  async function commit(entry: Renamable) {
    if (renaming.value !== entry.tab.id) return
    renaming.value = ''
    const name = draft.value.trim()
    if (name && name !== entry.title && entry.tab.ref) await rename(entry.tab.ref, name)
  }

  function cancel() {
    renaming.value = ''
  }

  // The field takes the caret when it first appears and keeps whatever the person does with it.
  const focusOnce = (el: unknown) => {
    const input = el as (HTMLInputElement & { _focused?: boolean }) | null
    if (input && !input._focused) {
      input._focused = true
      setTimeout(() => {
        input.focus()
        input.select()
      }, 0)
    }
  }

  return { renaming, draft, start, commit, cancel, focusOnce }
}
</script>

<template>
  <div ref="stripEl" class="tab-strip" @mouseleave="leaveStrip">
    <div ref="scrollEl" class="tab-scroll">
      <ContextMenu v-for="entry in views" :key="entry.tab.id">
        <ContextMenuTrigger as-child>
          <div
            class="tab"
            :class="{ active: entry.active, preview: entry.tab.preview, fixed: entry.fixed, 'pin-end': entry.pinEnd }"
            :style="entry.style"
            :title="entry.title"
            @pointerdown="onTabDown($event, entry.tab.id)"
            @dblclick="onTabDbl(entry, $event)"
            @mouseenter="hovered = entry.tab.id"
            @mouseleave="hovered = ''"
            @auxclick="($event.button === 1) && closeTab(entry.tab.id)"
          >
            <span v-if="entry.pill" class="tab-pill"></span>
            <span v-if="entry.showIcon" class="tab-kind-slot">
              <Icon v-if="entry.icon" :name="entry.icon" :size="14" class="tab-kind" />
              <span v-if="entry.method" class="tab-method cap-line" :class="methodInkClass(entry.method)">
                {{ entry.method }}
              </span>
            </span>
            <input
              v-if="renaming === entry.tab.id"
              :ref="renameRef"
              v-model="renameDraft"
              class="tab-rename"
              spellcheck="false"
              @pointerdown.stop
              @click.stop
              @keydown.enter.prevent="commitRename(entry)"
              @keydown.esc="cancelRename()"
              @blur="commitRename(entry)"
            />
            <span v-else-if="entry.showTitle" class="tab-title cap-line">{{ entry.title }}</span>
            <button
              v-if="entry.showDot"
              class="tab-slot"
              :title="t('tabs.unsavedClose')"
              @pointerdown.stop
              @click.stop="closeTab(entry.tab.id)"
            >
              <span class="tab-dot"></span>
            </button>
            <button
              v-else-if="entry.showX"
              class="tab-slot"
              :title="closeHint"
              @pointerdown.stop
              @click.stop="closeTab(entry.tab.id)"
            >
              <Icon name="X" :size="12" :stroke-width="2.8" />
            </button>
          </div>
        </ContextMenuTrigger>

        <ContextMenuContent class="tab-menu">
          <ContextMenuItem v-if="entry.tab.preview" @select="tabs.keep(entry.tab.id)">
            <span class="menu-mark"><Icon name="Bookmark" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.keep') }}</span>
          </ContextMenuItem>
          <ContextMenuItem @select="tabs.pin(entry.tab.id, !entry.tab.pinned)">
            <span class="menu-mark"><Icon name="Pin" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ entry.tab.pinned ? t('tabs.unpin') : t('tabs.pin') }}</span>
          </ContextMenuItem>
          <ContextMenuItem v-if="entry.tab.kind === TabKind.TabKindRequest" @select="tabs.duplicate(entry.tab.id)">
            <span class="menu-mark"><Icon name="Copy" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.duplicate') }}</span>
          </ContextMenuItem>
          <ContextMenuItem v-if="entry.node" @select="startRename(entry)">
            <span class="menu-mark"><Icon name="Pencil" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.rename') }}</span>
          </ContextMenuItem>
          <ContextMenuItem
            v-if="entry.tab.kind === TabKind.TabKindRequest && entry.draft?.url"
            @select="saveToCollection(entry.tab.id)"
          >
            <span class="menu-mark"><Icon name="Download" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.saveToCollection') }}</span>
            <span v-if="hint('save')" class="menu-key">{{ hint('save') }}</span>
          </ContextMenuItem>
          <ContextMenuItem v-if="entry.tab.record || entry.tab.ref" @select="reveal(entry)">
            <span class="menu-mark">
              <Icon :name="entry.tab.record ? 'Clock' : 'PanelLeft'" :size="14" :stroke-width="1.8" />
            </span>
            <span class="menu-label-text cap-line">
              {{ entry.tab.record ? t('tabs.showInHistory') : t('tabs.revealInSide') }}
            </span>
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem @select="tabs.close(entry.tab.id)">
            <span class="menu-mark"><Icon name="X" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.closeTab') }}</span>
            <span v-if="hint('closeTab')" class="menu-key">{{ hint('closeTab') }}</span>
          </ContextMenuItem>
          <ContextMenuItem @select="tabs.closeOthers(entry.tab.id)">
            <span class="menu-mark"><Icon name="X" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.closeOthers') }}</span>
            <span v-if="hint('closeOthers')" class="menu-key">{{ hint('closeOthers') }}</span>
          </ContextMenuItem>
          <ContextMenuItem @select="tabs.closeRight(entry.tab.id)">
            <span class="menu-mark"><Icon name="X" :size="14" :stroke-width="1.8" /></span>
            <span class="menu-label-text cap-line">{{ t('tabs.closeRight') }}</span>
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    </div>
    <AppTabSwitcher :entries="switcherEntries" :hidden-count="hiddenCount" />
    <button class="tab-new" :title="hint('newTab')" @click="openTab">
      <Icon name="Plus" :size="14" />
    </button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.tab-strip {
  @apply flex-1 min-w-0 flex items-center;
  padding-left: 0.3rem;
}

.tab-scroll {
  @apply flex items-center;
  gap: 0.125rem;
  flex: 0 1 auto;
  min-width: 0;
  height: 100%;
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
  padding: 0 0.5625rem;
  margin: 0 -0.5625rem;
}

.tab-scroll::-webkit-scrollbar {
  display: none;
}

.tab {
  @apply relative flex items-center flex-none cursor-default;
  height: 1.875rem;
  gap: 0.4375rem;
  border-radius: 0.4375rem;
  color: var(--text-secondary);
  font-size: var(--text-13);
  user-select: none;
  --wails-draggable: no-drag;
}

.tab:hover {
  color: var(--text);
}

.tab.active {
  background: var(--bg);
  color: var(--text);
  font-weight: 600;
}

.tab.preview .tab-title {
  font-style: italic;
}

.tab.pin-end {
  margin-right: 1.1875rem;
}

.tab.pin-end::after {
  content: '';
  position: absolute;
  right: -0.625rem;
  top: 50%;
  width: 1px;
  height: 1rem;
  margin-top: -0.5rem;
  background: var(--border);
  pointer-events: none;
}

.tab-pill {
  position: absolute;
  left: 0;
  right: 0;
  top: 0;
  bottom: 0;
  border-radius: 0.5rem;
  background: var(--bg-hover);
  box-shadow:
    inset 0 0 0 0.5px var(--border),
    0 1px 2px rgba(0, 0, 0, 0.06);
  z-index: -1;
  pointer-events: none;
}

.tab-kind-slot {
  @apply flex items-center;
  flex: 0 1 auto;
  gap: 0.4375rem;
  min-width: 0;
  padding-block: 0.5rem;
  margin-block: -0.5rem;
  overflow: hidden;
}

.tab-kind {
  @apply flex-none;
}

.tab.active .tab-kind {
  color: var(--accent);
}

.tab:not(.active) .tab-kind {
  color: var(--text-tertiary);
}

.tab-method {
  @apply flex-none;
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 600;
}

.tab-title {
  @apply flex-1 min-w-0;
  overflow: hidden;
  white-space: nowrap;
  -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 18px), transparent);
  mask-image: linear-gradient(to right, #000 calc(100% - 18px), transparent);
}

.tab-rename {
  @apply flex-1 min-w-0;
  height: 1.375rem;
  border-radius: 0.25rem;
  border: 1px solid var(--accent);
  background: var(--bg-panel);
  color: var(--text);
  font-size: var(--text-13);
  padding: 0 0.25rem;
  outline: none;
}

.tab-slot {
  @apply flex-none flex items-center justify-center;
  width: 1.25rem;
  height: 1.25rem;
  margin-left: auto;
  padding: 0;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: var(--text-tertiary);
  cursor: default;
}

.tab-slot:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.tab-dot {
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
  background: var(--text-tertiary);
}

.tab-new {
  @apply flex items-center justify-center flex-none;
  align-self: center;
  width: 1.75rem;
  height: 1.75rem;
  margin: 0 0 0 0.3rem;
  border: 0;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-secondary);
  cursor: default;
  --wails-draggable: no-drag;
}

.tab-new:hover {
  background: var(--bg-hover);
  color: var(--text);
}
</style>
