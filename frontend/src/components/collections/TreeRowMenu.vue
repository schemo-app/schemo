<script setup lang="ts">
import { computed, ref } from 'vue'
import { ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger } from 'reka-ui'
import ContextMenuContent from '../ui/ContextMenuContent.vue'
import ContextMenuItem from '../ui/ContextMenuItem.vue'
import ContextMenuSeparator from '../ui/ContextMenuSeparator.vue'
import Icon from '../ui/Icon.vue'
import { usePlatform } from '../../composables/usePlatform'
import { useCommands } from '../../composables/useCommands'
import { methodInkClass } from '../../lib/format'
import { removeChord, renameChord } from '../../lib/treeKeys'
import { t } from '../../i18n'
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

const { isMac, keyLabel } = usePlatform()
const { hint, offered } = useCommands()

// A workspace this person may only read keeps the rows that read and send, and turns off the ones that
// change what it holds.
const writable = computed(() => useWorkspacesStore().canWrite)

// The row that has a page says so rather than asking: the same answer the head's button is drawn
// from, and a collection nobody has published reads as one still to be shared. A folder inside a
// published one has a page too, the one that holds it.
const publications = usePublicationsStore()
const shared = computed(() => publications.markOf(row.id)?.page != null)

const mac = computed(() => isMac.value)
const removeKey = computed(() => removeChord(mac.value))

const isCollection = computed(() => row.kind === 'collection')
const otherCollections = computed(() => targets.filter((target) => target.id !== row.id))

// The closing menu keeps focus until its animation ends; opening the popover sooner dismisses it.
const sharing = ref(false)

function afterClose() {
  if (!sharing.value) return
  sharing.value = false
  emit('share')
}

// The menu only points at the removal: the dialog says what goes and asks for the answer.
const removeLabel = computed(() =>
  isCollection.value ? t('collections.deleteCollection') : t('common.delete')
)

const head = computed(() =>
  isCollection.value ? t('collections.headCollection', { name: row.name, n: row.count }) : row.name
)
</script>

<template>
  <ContextMenuContent class="tree-menu" @close-auto-focus="afterClose">
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
      <ContextMenuItem @select="sharing = true">
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
      :disabled="!writable"
      @select="emit('remove')"
    >
      <span class="menu-mark"><Icon name="Trash2" :size="14" :stroke-width="1.8" /></span>
      <span class="menu-label-text cap-line">{{ removeLabel }}</span>
      <span class="menu-key">{{ removeKey }}</span>
    </ContextMenuItem>
  </ContextMenuContent>
</template>
