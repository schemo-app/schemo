<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import Icon from '../ui/Icon.vue'
import { IconButton } from '../ui/button'
import { ContextMenu, ContextMenuTrigger } from '../ui/context-menu'
import DeleteNodeDialog from './DeleteNodeDialog.vue'
import TreeRowMenu from './TreeRowMenu.vue'
import { methodInkClass, shortMethod } from '../../lib/format'
import { useTreeDrag, type DropTarget } from '../../composables/tree/useTreeDrag'
import {
  acceptsDrop,
  dropPlace,
  flattenTree,
  levelOf,
  type TreeRow as Row,
} from '../../lib/treeRows'
import { useCollectionsStore } from '../../stores/collections'
import { usePublicationsStore } from '../../stores/publications'
import { useTransferStore } from '../../stores/transfer'
import { useWorkspacesStore } from '../../stores/workspaces'
import { filterTree } from '../../lib/collectionTree'
import { rem } from '../../lib/units'
import { NEW_REQUEST_METHOD, useTreeEdits } from '../../composables/tree/useTreeEdits'
import { useTreeNavigation } from '../../composables/tree/useTreeNavigation'
import { useMessages } from '../../i18n'
import { useToast } from '../../composables/useToast'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { sendRequest } from '../../composables/intents'
import { contentOf } from '../../stores/content'
import { Format } from '../../../bindings/schemo/internal/command'
import type { CollectionSection } from '../../stores/collections'

const store = useCollectionsStore()
const publications = usePublicationsStore()
const transfer = useTransferStore()
const workspaces = useWorkspacesStore()
const { t } = useMessages()
const toast = useToast()
const { copy } = useCopyFeedback()

const INDENT = 8
const STEP = 22

const shown = computed(() => filterTree(store.tree, store.filter))
const visible = computed(() => flattenTree(shown.value, (id) => !!store.expanded[id]))

const walkable = computed(() => flattenTree(shown.value, () => true))

function indentDepth(depth: number): string {
  return rem(INDENT + depth * STEP)
}

// A plain click lands in the preview slot; ⌘/Ctrl-click keeps a tab of its own.
async function pick(row: Row, e?: MouseEvent) {
  await store.select(row.id, !!e && (e.metaKey || e.ctrlKey))
}

const treeEl = useTemplateRef<HTMLElement>('treeEl')

function elementOf(id: string): HTMLElement | null {
  return treeEl.value?.querySelector<HTMLElement>(`[data-id="${id}"]`) ?? null
}

function dropOn(dragged: Row, target: DropTarget) {
  const place = dropPlace(store.tree, visible.value, dragged, target)
  if (!place) return
  if (dragged.kind === 'collection') void store.moveCollection(dragged.id, place.collectionId, place.position)
  else void store.moveNode(dragged.id, place.collectionId, place.position)
}

// The same landing the drag makes, asked for by name: a menu row drops the node into the end of the
// collection it names, and the place is read off the tree the drag reads it off.
function moveTo(row: Row, targetId: string) {
  dropOn(row, { id: targetId, zone: 'inside' })
}

// Every collection a row can be moved into, in the order the tree draws them.
const moveTargets = computed(() => {
  const out: { id: string; name: string }[] = []
  const walk = (collections: typeof store.tree) => {
    for (const collection of collections) {
      out.push({ id: collection.id, name: collection.name })
      walk(collection.children ?? [])
    }
  }
  walk(store.tree)
  return out
})

async function openSection(row: Row, section: CollectionSection) {
  await store.select(row.id)
  store.pendingSection = section
}

function importInto(row: Row) {
  transfer.askImport(row.id)
}

// Sharing is the collection's panel, and the panel stands on the collection's own page: the row is
// shown first, and the panel is left open for the page that arrives to draw.
async function share(row: Row) {
  await store.select(row.id)
  publications.openShare(row.id)
}

// Sending is the tab's: the row is shown first, and the request on screen sends itself.
async function send(row: Row) {
  await store.select(row.id)
  void sendRequest.trigger()
}

// A copy reads the node's draft, and a node that was never opened has none: it is staged in the
// background so the work area stays where it was while the request is read out.
async function copyAs(row: Row, format: Format) {
  await store.select(row.id, false, true)
  const instance = contentOf(row.id)
  if (!instance || !('draft' in instance)) return
  const answer = await instance.draft.copyAs(format)
  if (!answer) return
  if (!(await copy(answer.text))) return
  toast.show(t('common.copied'))
}

const { drag, press, swallowClick } = useTreeDrag({
  rows: () => visible.value,
  elementOf,
  accepts: (dragged, collectionId) => acceptsDrop(store.tree, dragged, collectionId),
  open: (id) => {
    store.expanded[id] = true
  },
  move: dropOn,
})

function onClickCapture(e: MouseEvent) {
  if (!swallowClick()) return
  e.stopPropagation()
  e.preventDefault()
}

function dropClass(row: Row): Record<string, boolean> {
  const target = drag.value?.target
  return {
    'drop-into': target?.id === row.id && target.zone === 'inside',
    'drop-before': target?.id === row.id && target.zone === 'before',
    'drop-after': target?.id === row.id && target.zone === 'after',
  }
}

useTreeNavigation(store, {
  rows: () => visible.value,
  walkable: () => walkable.value,
  elementOf,
  pick,
})

const {
  renamingId,
  nameDraft,
  renameInvalid,
  setNameInput,
  onRenameKeydown,
  startRename,
  commitRenameFrom,
  creatingName,
  creatingInvalid,
  setCreatingInput,
  onCreatingKeydown,
  commitCreating,
  creatingAt,
  startCreating,
  addCollection,
  runNode,
  exportNode,
  remove,
  confirming,
  askRemove,
  cancelTop,
} = useTreeEdits(store, { rows: () => visible.value })

// The method column is as wide as the longest label in the tree, or the one a request being named
// will carry: the names keep one line, no further from the label than they must, and neither a
// filter, a fold nor the new row moves them.
const methodStyle = computed(() => ({
  '--method-chars': flattenTree(store.tree, () => true).reduce(
    (chars, row) => (row.kind === 'request' ? Math.max(chars, shortMethod(row.method).length) : chars),
    creatingAt.value ? shortMethod(NEW_REQUEST_METHOD).length : 0
  ),
}))

// The row whose menu is open: it keeps its «…» and its ring while the menu stands, whichever way
// the menu was asked for.
const menuRow = ref('')

function onMenuOpen(row: Row, open: boolean) {
  if (open) {
    menuRow.value = row.id
    // What the row says about sharing is read while the menu is being drawn: the answer is local,
    // and reading it for every collection would be a question nobody asked.
    if (row.kind === 'collection') void publications.mark(row.id)
  } else if (menuRow.value === row.id) menuRow.value = ''
}

// The button opens the row's own menu, the one the right button asks for: reka anchors it to the
// last context-menu point, so the press is handed on as one, at the button's own corner.
function openMenu(event: MouseEvent) {
  const button = event.currentTarget as HTMLElement
  const box = button.getBoundingClientRect()
  button.closest('.panel-row')?.dispatchEvent(
    new MouseEvent('contextmenu', {
      bubbles: true,
      clientX: box.left,
      clientY: box.bottom + 4,
    })
  )
}

defineExpose({ cancelTop })
</script>

<template>
  <div class="tree-panel">
    <div class="panel-head">
      <span class="panel-title">{{ t('collections.title') }}</span>
      <IconButton
        variant="bare"
        class="panel-add"
        :hint="t('collections.newCollection')"
        :disabled="!workspaces.canWrite"
        @click="addCollection()"
      >
        <Icon name="Plus" :size="16" />
      </IconButton>
    </div>

    <div ref="treeEl" class="tree-scroll" :style="methodStyle" @click.capture="onClickCapture">
      <div v-for="row in visible" :key="row.id" class="tree-node">
        <ContextMenu @update:open="(open: boolean) => onMenuOpen(row, open)">
          <ContextMenuTrigger as-child>
            <div
              class="panel-row"
              :data-id="row.id"
              :class="{
                'row-collection': row.kind === 'collection',
                active: row.id === store.selectedId,
                'row-carried': drag?.row.id === row.id,
                'row-menu-open': menuRow === row.id,
                ...dropClass(row),
              }"
              :style="{ paddingLeft: indentDepth(row.depth) }"
              role="button"
              tabindex="0"
              @pointerdown="workspaces.canWrite && press($event, row)"
              @click="pick(row, $event)"
              @keydown.enter="pick(row)"
              @dblclick="startRename(row)"
            >
              <span
                v-if="row.expandable"
                class="caret"
                :class="{ open: row.expanded }"
                @pointerdown.stop
                @click.stop="store.toggleExpand(row.id)"
              >
                <Icon name="ChevronRight" :size="10" :stroke-width="2.8" />
              </span>
              <span v-else-if="row.kind === 'collection'" class="caret-space"></span>

              <span
                v-if="row.kind === 'collection'"
                class="row-icon"
              >
                <Icon name="Folder" :size="14" />
              </span>

              <span
                v-if="row.kind === 'request'"
                class="panel-method"
                :class="methodInkClass(row.method)"
              >{{ shortMethod(row.method) }}</span>

              <input
                v-if="renamingId === row.id"
                :ref="setNameInput"
                v-model="nameDraft"
                class="row-rename"
                :class="{ invalid: renameInvalid }"
                spellcheck="false"
                @pointerdown.stop
                @click.stop
                @dblclick.stop
                @keydown="onRenameKeydown"
                @blur="commitRenameFrom(row.id)"
              />
              <span v-else class="panel-address" :title="row.name">{{ row.name }}</span>

              <button
                v-if="renamingId !== row.id"
                type="button"
                class="row-more"
                :title="t('collections.moreActions')"
                @pointerdown.stop
                @click.stop="openMenu($event)"
              >
                <Icon name="Ellipsis" :size="14" />
              </button>
            </div>
          </ContextMenuTrigger>

          <TreeRowMenu
            :row
            :targets="moveTargets"
            @new-request="startCreating(levelOf(row))"
            @new-folder="addCollection(row.id)"
            @run-all="runNode(row)"
            @open="pick(row)"
            @send="send(row)"
            @section="openSection(row, $event)"
            @move-to="moveTo(row, $event)"
            @copy-curl="copyAs(row, Format.FormatCurl)"
            @copy-address="copyAs(row, Format.FormatURL)"
            @rename="startRename(row)"
            @duplicate="store.duplicate(row.id)"
            @export="exportNode(row)"
            @share="share(row)"
            @import="importInto(row)"
            @remove="askRemove(row)"
          />
        </ContextMenu>

        <div
          v-if="creatingAt && creatingAt.after === row.id"
          class="panel-row row-creating"
          :style="{ paddingLeft: indentDepth(creatingAt.depth) }"
        >
          <span class="panel-method"></span>
          <input
            :ref="setCreatingInput"
            v-model="creatingName"
            class="row-rename"
            :class="{ invalid: creatingInvalid }"
            spellcheck="false"
            @keydown="onCreatingKeydown"
            @blur="commitCreating"
          />
        </div>

      </div>

      <div v-if="visible.length === 0 && store.tree.length > 0" class="no-results">{{ t('common.nothingFound') }}</div>
    </div>

    <Teleport to="body">
      <div
        v-if="drag"
        class="drag-ghost"
        :style="{ ...methodStyle, left: `${drag.x + 12}px`, top: `${drag.y + 8}px` }"
      >
        <span
          v-if="drag.row.kind === 'request'"
          class="panel-method"
          :class="methodInkClass(drag.row.method)"
        >{{ shortMethod(drag.row.method) }}</span>
        <span class="panel-address">{{ drag.row.name }}</span>
      </div>
    </Teleport>


    <DeleteNodeDialog
      v-if="confirming"
      :open="true"
      :count="confirming?.count ?? 0"
      @cancel="confirming = null"
      @confirm="confirming && remove(confirming)"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.tree-panel {
  @apply relative flex flex-col h-full min-h-0;
}

.panel-head :deep(.panel-add) {
  @apply w-[1.375rem] h-[1.375rem] rounded-[0.375rem] text-text-tertiary;
}

.panel-head :deep(.panel-add:hover:not(:disabled)) {
  @apply bg-bg-hover text-text;
}

.tree-scroll {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col gap-px;
  padding: 0 0.5rem 0.5rem;
}

.panel-row {
  @apply relative;
}

.row-icon {
  @apply flex-none text-text-secondary;
}

.caret {
  @apply flex-none inline-flex items-center justify-center text-text-tertiary transition-transform duration-150;
  width: 0.875rem;
  height: 1.125rem;
  margin-left: -0.25rem;
}

.caret.open {
  transform: rotate(90deg);
}

.caret-space {
  @apply flex-none;
  width: 0.875rem;
  height: 1.125rem;
  margin-left: -0.25rem;
}

.row-rename {
  @apply flex-1 min-w-0 h-6 box-border text-13 outline-none;
  margin-left: -0.375rem;
  padding: 0 0.3125rem;
  border-radius: 0.25rem;
  border: 1px solid var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
  background: var(--bg-panel);
  color: var(--text);
}

.row-rename.invalid {
  border-color: color-mix(in srgb, var(--red) 45%, transparent);
  box-shadow: 0 0 0 3px var(--red-soft);
}

.row-creating {
  @apply cursor-default;
}

.no-results {
  @apply pt-4 px-4 text-center text-text-tertiary text-12;
}

.row-carried {
  @apply opacity-40;
}

.drop-before::after,
.drop-after::after {
  content: '';
  @apply absolute left-0 right-0 h-0.5 rounded-full;
  background: var(--accent);
}

.drop-before::after {
  top: -1px;
}

.drop-after::after {
  bottom: -1px;
}

.drop-into {
  @apply bg-accent-soft;
  box-shadow: inset 0 0 0 1px var(--accent);
}

.drag-ghost {
  @apply fixed z-50 flex items-center gap-2 py-1.5 px-2.5 rounded-lg text-text text-13 pointer-events-none;
  background: var(--glass-overlay);
  backdrop-filter: var(--blur-overlay);
  box-shadow: 0 8px 24px rgb(0 0 0 / 0.18);
}

</style>
