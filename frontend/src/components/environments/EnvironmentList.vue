<script setup lang="ts">
import { computed } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import ManagerRail from '../ui/ManagerRail.vue'
import Icon from '../ui/Icon.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCollectionsStore } from '../../stores/collections'
import { useWorkspacesStore } from '../../stores/workspaces'
import { dotOf } from '../../lib/environmentPalette'
import { t } from '../../i18n'

// The globals on top, then the environments in the order the header lists them.
const emit = defineEmits<{ (e: 'close'): void }>()

const environments = useEnvironmentsStore()
const collections = useCollectionsStore()
const workspaces = useWorkspacesStore()

const rows = computed(() => [
  {
    id: null,
    name: t('environments.globals'),
    meta: '',
    icon: 'Braces' as const,
    active: environments.editedEnvId === null,
    fixed: true,
  },
  ...environments.environments.map((env) => ({
    id: env.id as string | null,
    name: env.name,
    meta: '',
    dot: dotOf(env.color ?? ''),
    active: env.id === environments.editedEnvId,
    readonly: env.readonly,
    checked: env.id === environments.activeId,
  })),
])

function move(id: string, position: number) {
  const ids = environments.environments.map((env) => env.id).filter((one) => one !== id)
  ids.splice(position, 0, id)
  void environments.reorder(ids)
}

// The blocks are edited on the collection's own page, filtered by the environment open here.
async function openCollection(id: string) {
  emit('close')
  await collections.openVariables(id, environments.editedEnvId ?? '')
}
</script>

<template>
  <ManagerRail
    class="environment-rail"
    :rows
    :new-label="t('environments.new')"
    :title="t('environments.title')"
    :can-create="workspaces.canWrite"
    :reorderable="workspaces.canWrite"
    @select="environments.editEnv($event)"
    @create="environments.createEnv()"
    @move="move"
  >
    <template v-if="collections.tree.length" #foot>
      <DropdownMenuRoot>
        <DropdownMenuTrigger as-child>
          <button type="button" class="collection-vars">
            <Icon name="Folder" :size="13" />
            <span class="cap-line">{{ t('environments.collectionVariables') }}</span>
            <Icon name="ChevronRight" :size="10" :stroke-width="2.6" class="caret" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="start">
          <div class="menu-options">
            <DropdownMenuItem v-for="collection in collections.tree" :key="collection.id" @select="openCollection(collection.id)">
              <Icon name="Folder" :size="13" class="menu-mark" />
              <span class="cap-line">{{ collection.name }}</span>
            </DropdownMenuItem>
          </div>
        </DropdownMenuContent>
      </DropdownMenuRoot>
    </template>
  </ManagerRail>
</template>

<style scoped>
@reference "../../style.css";

.environment-rail :deep(.side-group:first-child) {
  padding-bottom: 6px;
  margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.collection-vars {
  @apply flex items-center w-full gap-2 h-7.5 px-2 border-0 rounded-md cursor-pointer bg-transparent
         text-text-secondary text-12;
  font-family: inherit;
}

.collection-vars:hover {
  @apply bg-bg-hover text-text;
}

.caret {
  @apply ml-auto text-text-tertiary;
}
</style>
