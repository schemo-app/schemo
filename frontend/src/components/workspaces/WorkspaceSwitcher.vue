<script setup lang="ts">
import { DropdownMenu, DropdownMenuTrigger } from '../ui/dropdown-menu'
import { useWorkspacesStore, workspaceName } from '../../stores/workspaces'
import { useMessages } from '../../i18n'
import Icon from '../ui/Icon.vue'
import WorkspaceAvatar from './WorkspaceAvatar.vue'
import WorkspaceMenu from './WorkspaceMenu.vue'

const store = useWorkspacesStore()

const { t } = useMessages()
</script>

<template>
  <!-- The counts under the names are a reading of what other features hold, and nothing tells this
       panel when one of them changes: it reads them again every time it is opened. -->
  <DropdownMenu v-if="store.active" @update:open="(open) => open && store.load()">
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="workspace-pill"
        :title="t('workspaces.switcher', { name: workspaceName(store.active) })"
      >
        <WorkspaceAvatar :workspace="store.active" size="sm" />
        <span class="workspace-name">{{ workspaceName(store.active) }}</span>
        <Icon name="ChevronsUpDown" :size="12" :stroke-width="2.4" class="workspace-chevron" />
      </button>
    </DropdownMenuTrigger>
    <WorkspaceMenu />
  </DropdownMenu>
</template>

<style scoped>
@reference "../../style.css";

.workspace-pill {
  @apply flex items-center w-full flex-none cursor-pointer border-0 text-left;
  height: 2rem;
  gap: 0.5625rem;
  padding: 0 0.5rem;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.workspace-pill:hover {
  background: var(--bg-hover);
}

.workspace-name {
  @apply flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap;
}

.workspace-chevron {
  @apply text-text-tertiary;
}
</style>
