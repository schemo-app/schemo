<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { clamp, useResizeObserver } from '@vueuse/core'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { useTabsStore } from '../../stores/tabs'
import { useRecordsStore } from '../../stores/records'
import { useCollectionsStore } from '../../stores/collections'
import { contentOf, nodeOf } from '../../stores/content'
import { methodInkClass } from '../../lib/format'
import { tabView } from '../../lib/tabView'
import { useCommands } from '../../composables/useCommands'
import { useSlotDrag } from '../../composables/useSlotDrag'
import { saveRequest } from '../../composables/intents'
import { useMessages } from '../../i18n'
import Icon from '../ui/Icon.vue'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '../ui/context-menu'

// The strip: what is open, in the mockup's own values (Schemo App) — 36px tabs standing in the
// window's 44px header, the active one on the content's own fill with its corners cut into it.
const tabs = useTabsStore()
const records = useRecordsStore()
const collections = useCollectionsStore()
const { hint } = useCommands()
const { t } = useMessages()

const MIN_W = 28
const MAX_W = 220
const STRIP_PAD = 10
const NEW_TAB_ROOM = 40
const TINY = 60
const COMPACT = 96
const SCROLL_PAD = 9
const PILL_NEAR = 7
const PILL_FAR = 2

const stripEl = ref<HTMLElement | null>(null)
const scrollEl = ref<HTMLElement | null>(null)
const stripW = ref(0)
const frozenW = ref(0)
const hovered = ref('')

// Every tab is the same width, so the row is a multiplication: how much room is left after the
// gutter and the «+» is split evenly, and a tab never grows past its own maximum.
const tabW = computed(() => {
  const count = tabs.tabs.length
  if (!count) return 0
  if (frozenW.value) return frozenW.value
  const room = (stripW.value || 900) - STRIP_PAD - NEW_TAB_ROOM
  return clamp(Math.floor(room / count), MIN_W, MAX_W)
})

const tiny = computed(() => tabW.value < TINY)
const compact = computed(() => tabW.value < COMPACT)

const { drag, settle, noAnim, press, offsetOf } = useSlotDrag({
  axis: 'x',
  ids: () => tabs.tabs.map((tab) => tab.id),
  size: () => tabW.value,
  drop: async (id, to) => {
    const onto = tabs.tabs[to]?.id
    if (onto) await tabs.move(id, onto)
  },
})

// The chrome moves under the pointer, so a width read once would go stale; the deadband is what
// stops a strip whose width the tools group changes from measuring itself in a loop.
useResizeObserver(stripEl, () => {
  const width = stripEl.value?.clientWidth ?? 0
  if (Math.abs(width - stripW.value) > 2) stripW.value = width
})

// The row scrolls only as far as the tab on screen needs: the point of a hidden scrollbar is that
// the tab you turned to is never the one you cannot see.
watch(
  [() => tabs.activeId, () => tabs.tabs.length, () => tabW.value],
  () => {
    const box = scrollEl.value
    if (!box || box.scrollWidth <= box.clientWidth) return
    const at = tabs.tabs.findIndex((tab) => tab.id === tabs.activeId)
    if (at < 0) return
    const left = SCROLL_PAD + at * tabW.value
    const right = left + tabW.value
    if (left < box.scrollLeft) box.scrollLeft = left - SCROLL_PAD
    else if (right > box.scrollLeft + box.clientWidth) {
      box.scrollLeft = right - box.clientWidth + SCROLL_PAD
    }
  },
  { flush: 'post' }
)

const views = computed(() =>
  tabs.tabs.map((tab, i) => {
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
    const before = tabs.tabs[i - 1]
    const after = tabs.tabs[i + 1]
    const active = tab.id === tabs.activeId
    const carried = drag.value
    const over = hovered.value === tab.id && !carried
    const dirty = instance?.dirty.value ?? false
    const closeOk = !carried && !tab.pinned && (active || (over && !compact.value))
    // The pill is the tab's own hover: it reaches closer to an active neighbour, which already has
    // the content's fill and would otherwise be crowded by the pill's rounded corner.
    const pillL = before?.id === tabs.activeId ? PILL_NEAR : PILL_FAR
    const pillR = after?.id === tabs.activeId ? PILL_NEAR : PILL_FAR
    let shift = 'none'
    let z = 1
    let shadow = 'none'
    let transition = noAnim.value
      ? 'none'
      : 'transform 0.2s cubic-bezier(.2,.8,.2,1), width 0.2s cubic-bezier(.2,.8,.2,1)'
    const off = offsetOf(i, tab.id)
    if (off !== 0) shift = `translateX(${off}px)`
    const settling = settle.value?.id === tab.id
    if (carried?.idx === i && !settling) {
      z = 5
      shadow = '0 6px 18px rgba(0, 0, 0, 0.16)'
      transition = 'none'
    } else if (settling) {
      z = 5
    }
    // The divider on a tab's left edge stands only between two standing-apart tabs: never on the
    // active one, never beside it, and never under a pill that would cut across it.
    const sep =
      !carried && i > 0 && !active && before?.id !== tabs.activeId && !over && hovered.value !== before?.id
    return {
      tab,
      active,
      dirty,
      draft,
      node: node?.node ?? null,
      ...view,
      sep: sep ? 1 : 0,
      pill: over && !active,
      pillL,
      pillR,
      showIcon: !(tiny.value && active),
      showTitle: !tiny.value,
      showX: closeOk && !(dirty && !over),
      showDot: dirty && !(over && closeOk) && !(tiny.value && !active),
      style: {
        width: `${tabW.value}px`,
        justifyContent: tiny.value ? 'center' : 'flex-start',
        padding: tiny.value ? '0' : active ? '0 6px 0 12px' : `0 ${pillR + 5}px 0 ${pillL + 10}px`,
        transform: shift,
        zIndex: String(z),
        boxShadow: shadow,
        transition,
        '--tab-sep': `${sep ? 1 : 0}`,
      },
    }
  })
)

// Pressing a tab shows it, exactly as the mockup does: the gesture and the choice are one move, and
// the drag that may follow starts from the same place.
function onTabDown(event: PointerEvent, id: string, index: number) {
  if (event.button !== 0) return
  void tabs.activate(id)
  press(event, id, index)
}

// Closing one tab must not re-flow the rest under the pointer: the width is held until the pointer
// leaves the strip, and a tab that opens is what releases it.
function closeTab(id: string) {
  frozenW.value = tabW.value
  void tabs.close(id)
}

function openTab() {
  frozenW.value = 0
  void tabs.newTab()
}

function leaveStrip() {
  if (!drag.value) frozenW.value = 0
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

// A saved request is named by its node, so renaming the tab renames the node — the tree and the
// tab then say the same thing.
const renaming = ref('')
const renameDraft = ref('')

function startRename(entry: (typeof views.value)[number]) {
  if (!entry.node) return
  renaming.value = entry.tab.id
  renameDraft.value = entry.title
}

async function commitRename(entry: (typeof views.value)[number]) {
  if (renaming.value !== entry.tab.id) return
  renaming.value = ''
  const name = renameDraft.value.trim()
  if (name && name !== entry.title && entry.tab.ref) await collections.rename(entry.tab.ref, name)
}

// A double click keeps a preview; for a saved request it starts its rename. The window's header
// underneath takes a double click as «zoom», so the tab's own meaning stays on the tab.
function onTabDbl(entry: (typeof views.value)[number], event: MouseEvent) {
  event.stopPropagation()
  if (entry.tab.preview) void tabs.keep(entry.tab.id)
  else startRename(entry)
}

const closeHint = computed(() =>
  hint('closeTab') ? `${t('tabs.closeTab')} · ${hint('closeTab')}` : t('tabs.closeTab')
)

const renameRef = (el: unknown) => {
  const input = el as (HTMLInputElement & { _focused?: boolean }) | null
  if (input && !input._focused) {
    input._focused = true
    setTimeout(() => {
      input.focus()
      input.select()
    }, 0)
  }
}
</script>

<template>
  <div ref="stripEl" class="tab-strip" @mouseleave="leaveStrip">
    <div ref="scrollEl" class="tab-scroll">
      <ContextMenu v-for="(entry, i) in views" :key="entry.tab.id">
        <ContextMenuTrigger as-child>
          <div
            class="tab"
            :class="{ active: entry.active, preview: entry.tab.preview }"
            :style="entry.style"
            :title="entry.title"
            @pointerdown="onTabDown($event, entry.tab.id, i)"
            @dblclick="onTabDbl(entry, $event)"
            @mouseenter="hovered = entry.tab.id"
            @mouseleave="hovered = ''"
            @auxclick="($event.button === 1) && closeTab(entry.tab.id)"
          >
            <span class="tab-sep"></span>
            <span v-if="entry.pill" class="tab-pill" :style="{ left: `${entry.pillL}px`, right: `${entry.pillR}px` }"></span>
            <template v-if="entry.active">
              <span class="tab-corner tab-corner-left"></span>
              <span class="tab-corner tab-corner-right"></span>
            </template>
            <Icon v-if="entry.tab.pinned" name="Pin" :size="10" class="tab-pin" />
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
              @keydown.esc="renaming = ''"
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
    <button class="tab-new" :title="hint('newTab')" @click="openTab">
      <Icon name="Plus" :size="14" />
    </button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.tab-strip {
  @apply flex-1 min-w-0 flex items-end pl-2.5;
}

.tab-scroll {
  @apply flex items-end;
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
  height: 2.25rem;
  gap: 0.4375rem;
  border-radius: 0.5625rem 0.5625rem 0 0;
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
  font-weight: 500;
}

.tab.preview .tab-title {
  font-style: italic;
}

.tab-sep {
  position: absolute;
  left: 0;
  top: 0.625rem;
  width: 1px;
  height: 1rem;
  background: var(--border);
  opacity: calc(var(--tab-sep, 0) * 1);
}

.tab-pill {
  position: absolute;
  top: 2px;
  bottom: 3px;
  border-radius: 0.5rem;
  background: var(--tabhover);
  box-shadow:
    inset 0 0 0 0.5px var(--border),
    0 1px 2px rgba(0, 0, 0, 0.06);
  z-index: -1;
  pointer-events: none;
}

.tab-corner {
  position: absolute;
  bottom: 0;
  width: 0.5625rem;
  height: 0.5625rem;
}

.tab-corner-left {
  left: -0.5625rem;
  background: radial-gradient(circle at 0 0, transparent 0.53125rem, var(--bg) 0.5625rem);
}

.tab-corner-right {
  right: -0.5625rem;
  background: radial-gradient(circle at 100% 0, transparent 0.53125rem, var(--bg) 0.5625rem);
}

.tab-pin {
  @apply flex-none;
  color: var(--text-tertiary);
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
  font-size: var(--text-10);
  font-weight: 700;
  letter-spacing: 0.01em;
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
  margin: 0.375rem 0 0 0.375rem;
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
