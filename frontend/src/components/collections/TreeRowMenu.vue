<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { injectContextMenuRootContext } from 'reka-ui'
import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from '../ui/context-menu'
import Icon from '../ui/Icon.vue'
import { usePlatform } from '../../composables/usePlatform'
import { useCommands } from '../../composables/useCommands'
import { methodInkClass } from '../../lib/format'
import { removeChord, renameChord } from '../../lib/treeKeys'
import { useMessages } from '../../i18n'
import { usePublicationsStore } from '../../stores/publications'
import { useWorkspacesStore } from '../../stores/workspaces'
import type { CollectionSection } from '../../stores/collections'
import type { TreeRow } from '../../lib/treeRows'

const { row, targets } = defineProps<{
  row: TreeRow
  targets: { id: string; name: string }[]
}>()

const emit = defineEmits<{
  newRequest: []
  newFolder: []
  open: []
  runAll: []
  section: [CollectionSection]
  rename: []
  duplicate: []
  export: []
  share: []
  import: []
  remove: []
  send: []
  moveTo: [string]
  copyCurl: []
  copyAddress: []
}>()

const { t } = useMessages()
const { isMac, keyLabel } = usePlatform()
const { hint, offered } = useCommands()

// A space this person may only read keeps the rows that read and send, and turns off the ones that
// change what it holds.
const writable = computed(() => useWorkspacesStore().canWrite)

// The row that has a page says so rather than asking: the same answer the head's button is drawn
// from, and a collection nobody has published reads as one still to be shared. A folder inside a
// published one has a page as well — the page that holds it — and the row says so.
const publications = usePublicationsStore()
const shared = computed(() => publications.markOf(row.id)?.page != null)

const mac = computed(() => isMac.value)
const removeKey = computed(() => removeChord(mac.value))

const isCollection = computed(() => row.kind === 'collection')
const otherCollections = computed(() => targets.filter((target) => target.id !== row.id))

// Removing a collection takes its contents with it, so the menu asks twice: the second click is the
// only one that removes, and the row says in as many words what goes.
const armed = ref(false)

// A menu that closes forgets the question it asked: a reopened row must not remove on its first click.
const menu = injectContextMenuRootContext()
watch(menu.open, (open) => {
  if (!open) armed.value = false
})

function remove(event: Event) {
  // A request goes on its own; a collection takes whatever is inside it, so only it asks twice.
  if (!isCollection.value) {
    emit('remove')
    return
  }
  if (!armed.value) {
    armed.value = true
    // The first click only asks; reka closes the menu on any select it is not told to keep open.
    event.preventDefault()
    return
  }
  emit('remove')
}

const removeLabel = computed(() => {
  if (!armed.value) return isCollection.value ? t('collections.deleteCollection') : t('common.delete')
  return t('collections.deleteConfirm', { n: row.count })
})

const head = computed(() =>
  isCollection.value ? t('collections.headCollection', { name: row.name, n: row.count }) : row.name
)
</script>

<template>
  <ContextMenuContent class="tree-menu">
    <div class="menu-head">
      <span v-if="row.kind === 'request'" class="menu-head-method" :class="methodInkClass(row.method)">
        {{ row.method }}
      </span>
      <span class="menu-head-title">{{ head }}</span>
    </div>

    <template v-if="isCollection">
      <ContextMenuItem :disabled="!writable" @select="emit('newRequest')">
        <span class="menu-mark"><Icon name="Plus" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.newRequest') }}</span>
        <span v-if="hint('newRequest')" class="menu-key">{{ hint('newRequest') }}</span>
      </ContextMenuItem>
      <ContextMenuItem :disabled="!writable" @select="emit('newFolder')">
        <span class="menu-mark"><Icon name="Folder" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.newFolder') }}</span>
      </ContextMenuItem>
      <ContextMenuItem @select="emit('open')">
        <span class="menu-mark"><Icon name="ExternalLink" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.openCollection') }}</span>
      </ContextMenuItem>
      <ContextMenuItem :disabled="row.count === 0" @select="emit('runAll')">
        <span class="menu-mark"><Icon name="Play" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.runAll') }}</span>
        <span v-if="offered('run')" class="menu-key">{{ hint('run') }}</span>
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem @select="emit('section', 'auth')">
        <span class="menu-mark"><Icon name="Lock" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.subAuth') }}</span>
      </ContextMenuItem>
      <ContextMenuItem @select="emit('section', 'vars')">
        <span class="menu-mark"><Icon name="Braces" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.subVars') }}</span>
      </ContextMenuItem>
      <ContextMenuSeparator />
    </template>

    <template v-else>
      <ContextMenuItem @select="emit('open')">
        <span class="menu-mark"><Icon name="ExternalLink" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.openRequest') }}</span>
        <span class="menu-key">{{ keyLabel('enter') }}</span>
      </ContextMenuItem>
      <ContextMenuItem @select="emit('send')">
        <span class="menu-mark"><Icon name="Send" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.sendRequest') }}</span>
        <span v-if="hint('send')" class="menu-key">{{ hint('send') }}</span>
      </ContextMenuItem>
      <ContextMenuSeparator />
    </template>

    <ContextMenuItem :disabled="!writable" @select="emit('rename')">
      <span class="menu-mark"><Icon name="Pencil" :size="14" :stroke-width="1.8" /></span>
      <span class="menu-label-text cap-line">{{ t('collections.rename') }}</span>
      <span class="menu-key">{{ renameChord }}</span>
    </ContextMenuItem>
    <ContextMenuItem :disabled="!writable" @select="emit('duplicate')">
      <span class="menu-mark"><Icon name="Copy" :size="14" :stroke-width="1.8" /></span>
      <span class="menu-label-text cap-line">{{ t('collections.duplicate') }}</span>
      <span v-if="hint('duplicate')" class="menu-key">{{ hint('duplicate') }}</span>
    </ContextMenuItem>

    <ContextMenuSub v-if="!isCollection">
      <ContextMenuSubTrigger class="menu-item" :disabled="!writable">
        <span class="menu-mark"><Icon name="FolderInput" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.moveTo') }}</span>
        <Icon name="ChevronRight" :size="8" :stroke-width="2.8" class="menu-caret" />
      </ContextMenuSubTrigger>
      <ContextMenuSubContent class="menu tree-submenu">
        <ContextMenuItem
          v-for="target in otherCollections"
          :key="target.id"
          @select="emit('moveTo', target.id)"
        >
          <span class="menu-mark"><Icon name="Folder" :size="14" :stroke-width="1.8" /></span>
          <span class="menu-label-text cap-line">{{ target.name }}</span>
        </ContextMenuItem>
        <ContextMenuItem v-if="otherCollections.length === 0" disabled>
          <span class="menu-mark"></span>
          <span class="menu-label-text cap-line">{{ t('collections.noOtherCollections') }}</span>
        </ContextMenuItem>
      </ContextMenuSubContent>
    </ContextMenuSub>

    <template v-if="!isCollection">
      <ContextMenuSeparator />
      <ContextMenuItem @select="emit('copyCurl')">
        <span class="menu-mark"><Icon name="CodeXml" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.copyCurl') }}</span>
      </ContextMenuItem>
      <ContextMenuItem @select="emit('copyAddress')">
        <span class="menu-mark"><Icon name="Link" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.copyAddress') }}</span>
      </ContextMenuItem>
    </template>

    <template v-else>
      <ContextMenuItem @select="emit('export')">
        <span class="menu-mark"><Icon name="Upload" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.export') }}</span>
      </ContextMenuItem>
      <ContextMenuItem @select="emit('share')">
        <span class="menu-mark"><Icon name="Radio" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">
          {{ t(shared ? 'publications.publicationMenu' : 'publications.shareMenu') }}
        </span>
      </ContextMenuItem>
      <ContextMenuItem :disabled="!writable" @select="emit('import')">
        <span class="menu-mark"><Icon name="Download" :size="14" :stroke-width="1.8" /></span>
        <span class="menu-label-text cap-line">{{ t('collections.importInto') }}</span>
      </ContextMenuItem>
    </template>

    <ContextMenuSeparator />
    <ContextMenuItem
      class="danger"
      :class="{ armed }"
      :disabled="!writable"
      @select="remove"
    >
      <span class="menu-mark"><Icon name="Trash2" :size="14" :stroke-width="1.8" /></span>
      <span class="menu-label-text cap-line">{{ removeLabel }}</span>
      <span v-if="!armed" class="menu-key">{{ removeKey }}</span>
    </ContextMenuItem>
  </ContextMenuContent>
</template>
