<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { useTabsStore } from '../../stores/tabs'
import { useCollectionsStore } from '../../stores/collections'
import type { CollectionSection } from '../../stores/collections'
import { useTransferStore } from '../../stores/transfer'
import { useTreeEdits } from '../../composables/useTreeEdits'
import { flattenTree } from '../../lib/treeRows'
import { useCaptureStore } from '../../stores/capture'
import { usePublicationsStore } from '../../stores/publications'
import { useWorkspacesStore } from '../../stores/workspaces'
import { t } from '../../i18n'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger, ContextMenuRoot as ContextMenu, ContextMenuTrigger } from 'reka-ui'
import Dialog from '../ui/Dialog.vue'
import Button from '../ui/Button.vue'
import Input from '../ui/Input.vue'
import TreeRowMenu from '../collections/TreeRowMenu.vue'
import DeleteNodeDialog from '../collections/DeleteNodeDialog.vue'
import Icon from '../ui/Icon.vue'
import WorkspaceSwitcher from '../workspaces/WorkspaceSwitcher.vue'
import AccountButton from '../cloud/AccountButton.vue'
import CollectionPeek from '../workspaces/CollectionPeek.vue'

const tabs = useTabsStore()
const collections = useCollectionsStore()
const capture = useCaptureStore()
const publications = usePublicationsStore()
const workspaces = useWorkspacesStore()
const transfer = useTransferStore()
const rows = computed(() => flattenTree(collections.tree, () => true))
const roots = computed(() => rows.value.filter((row) => row.depth === 0))
const {
  renamingId,
  nameDraft,
  renameInvalid,
  setNameInput,
  startRename,
  commitRename,
  creatingIn,
  creatingName,
  creatingInvalid,
  setCreatingInput,
  startCreating,
  commitCreating,
  addCollection,
  runNode,
  exportNode,
  askRemove,
  remove,
  confirming,
  cancelTop,
} = useTreeEdits(collections, { rows: () => rows.value })
const renaming = computed((previous = false) => renamingId.value ? true : creatingIn.value ? false : previous)
const editingName = computed({
  get: () => renaming.value ? nameDraft.value : creatingName.value,
  set: (value: string) => {
    if (renamingId.value) nameDraft.value = value
    else creatingName.value = value
  },
})
const editingTitle = computed(() => t(renaming.value ? 'collections.rename' : 'collections.newRequest'))

function saveName() {
  if (renamingId.value) void commitRename()
  else void commitCreating()
}

function menuOpen(id: string, value: boolean) {
  if (!value) return
  closePeek()
  void publications.mark(id)
}

async function openSection(id: string, section: CollectionSection) {
  await collections.select(id)
  collections.pendingSection = section
}

async function share(id: string) {
  await collections.select(id)
  publications.openShare(id)
}
const peekId = ref('')
const colors = ['var(--accent)', 'var(--orange)', 'var(--green)', 'var(--purple)', 'var(--blue)']
const nav = computed(() => [
  { kind: TabKind.TabKindHistory, icon: 'Clock' as const, label: t('sidebar.history'), mark: '', open: () => tabs.openHistory() },
  { kind: TabKind.TabKindCapture, icon: 'Globe' as const, label: t('sidebar.capture'), mark: capture.recording ? 'live' : '', open: () => tabs.openCapture() },
  { kind: TabKind.TabKindPubs, icon: 'Radio' as const, label: t('sidebar.publications'), mark: publications.hasChanges ? 'warn' : '', open: () => tabs.openPublications() },
])
const owner = computed(() => collections.trail?.ancestors[0]?.id ?? collections.trail?.collection?.id ?? '')

function closePeek() {
  peekId.value = ''
}

function open(id: string, event?: MouseEvent) {
  closePeek()
  void collections.select(id, !!(event?.metaKey || event?.ctrlKey))
}

watch(() => workspaces.activeId, () => {
  closePeek()
  cancelTop()
})
watch(() => collections.tree, (tree) => {
  if (!tree.some((collection) => collection.id === peekId.value)) closePeek()
})
</script>

<template>
  <nav class="sidebar-rail" :aria-label="t('sidebar.collections')">
    <WorkspaceSwitcher compact />
    <button
      v-for="item in nav"
      :key="item.kind"
      type="button"
      class="rail-btn"
      :class="{ active: tabs.activeKind === item.kind }"
      :title="item.label"
      :aria-label="item.label"
      @click="closePeek(); item.open()"
    >
      <Icon :name="item.icon" :size="18" :stroke-width="1.7" />
      <span v-if="item.mark" class="rail-dot" :class="item.mark"></span>
    </button>
    <span class="rail-divider"></span>
    <div class="rail-collections">
      <ContextMenu
        v-for="(collection, i) in roots"
        :key="collection.id"
        @update:open="menuOpen(collection.id, $event)"
      >
        <ContextMenuTrigger as="div">
          <Popover
            :open="peekId === collection.id"
            @update:open="(value) => peekId = value ? collection.id : peekId === collection.id ? '' : peekId"
          >
            <PopoverTrigger as-child>
              <button
                type="button"
                class="rail-btn"
                :class="{ active: owner === collection.id, peeking: peekId === collection.id }"
                :title="collection.name"
                :aria-label="collection.name"
                aria-haspopup="dialog"
                :aria-expanded="peekId === collection.id"
                @keydown.down.prevent="peekId = collection.id"
              >
                <span class="rail-letter" :style="{ '--rail-ink': colors[i % colors.length] }">
                  {{ collection.name.trim().charAt(0).toLocaleUpperCase() || '?' }}
                </span>
              </button>
            </PopoverTrigger>
            <PopoverContent
              class="rail-popover"
              side="right"
              align="start"
              :side-offset="4"
              :aria-label="collection.name"
              @open-auto-focus.prevent
              @close-auto-focus.prevent
            >
              <CollectionPeek :key="collection.id" :collection="collections.tree[i]" @open="open" />
            </PopoverContent>
          </Popover>
        </ContextMenuTrigger>
        <TreeRowMenu
          :row="collection"
          :targets="[]"
          @new-request="startCreating(collection.id)"
          @new-folder="addCollection(collection.id)"
          @open="open(collection.id)"
          @run-all="runNode(collection)"
          @section="openSection(collection.id, $event)"
          @rename="startRename(collection)"
          @duplicate="collections.duplicate(collection.id)"
          @export="exportNode(collection)"
          @share="share(collection.id)"
          @import="transfer.askImport(collection.id)"
          @remove="askRemove(collection)"
        />
      </ContextMenu>
    </div>
    <div class="rail-foot" @click="closePeek">
      <AccountButton compact />
    </div>
  </nav>
  <Dialog
    :open="!!renamingId || !!creatingIn"
    :title="editingTitle"
    class="w-90 p-4.5"
    @update:open="(value) => !value && cancelTop()"
  >
    <form class="rail-name-form" @submit.prevent="saveName">
      <Input
        :ref="renaming ? setNameInput : setCreatingInput"
        v-model="editingName"
        :aria-label="editingTitle"
        :aria-invalid="renaming ? renameInvalid : creatingInvalid"
      />
      <div class="rail-name-actions">
        <Button type="button" @click="cancelTop">{{ t('common.cancel') }}</Button>
        <Button type="submit" variant="primary">{{ t('common.save') }}</Button>
      </div>
    </form>
  </Dialog>
  <DeleteNodeDialog
    :open="!!confirming"
    :count="confirming?.count ?? 0"
    @cancel="cancelTop"
    @confirm="confirming && remove(confirming)"
  />
</template>

<style scoped>
@reference "../../style.css";

.sidebar-rail {
  @apply flex flex-col items-center flex-none min-h-0 relative;
  z-index: 26;
  width: 3.375rem;
  gap: 0.1875rem;
  padding: 0.3rem 0 0.5rem;
  background: var(--chrome);
  --wails-draggable: no-drag;
}

.rail-btn {
  @apply flex items-center justify-center flex-none relative border-0 cursor-default;
  width: 2.375rem;
  height: 2.125rem;
  padding: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text-secondary);
}

.rail-btn:hover,
.rail-btn.active,
.rail-btn.peeking {
  background: var(--bg-hover);
}

.rail-btn.active {
  color: var(--accent);
}

.rail-letter {
  @apply flex items-center justify-center;
  width: 1.375rem;
  height: 1.375rem;
  border-radius: 0.3rem;
  background: color-mix(in srgb, var(--rail-ink) 20%, transparent);
  color: var(--rail-ink);
  font-size: var(--text-12);
  font-weight: 600;
}

.rail-divider {
  width: 1.5rem;
  height: 1px;
  flex: none;
  margin: 0.3rem 0;
  background: var(--border);
}

.rail-collections {
  @apply flex flex-col items-center min-h-0 overflow-y-auto;
  gap: 0.1875rem;
  scrollbar-width: none;
}

.rail-foot {
  margin-top: auto;
  padding-top: 0.1875rem;
}

.rail-dot {
  position: absolute;
  top: 0.3rem;
  right: 0.4375rem;
  width: 0.3rem;
  height: 0.3rem;
  border-radius: 50%;
}

.rail-dot.live {
  background: var(--red);
}

.rail-dot.warn {
  background: var(--orange);
}

.rail-name-form {
  @apply flex flex-col gap-4 mt-3;
}

.rail-name-actions {
  @apply flex justify-end gap-2;
}
</style>
