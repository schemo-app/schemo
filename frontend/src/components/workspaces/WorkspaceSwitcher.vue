<script setup lang="ts">
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'
import { useWorkspacesStore, workspaceName } from '../../stores/workspaces'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import WorkspaceAvatar from './WorkspaceAvatar.vue'
import WorkspaceMenu from './WorkspaceMenu.vue'

defineProps<{ compact?: boolean }>()

const workspaces = useWorkspacesStore()

</script>

<template>
  <!-- The counts under the names are a reading of what other features hold, and nothing tells this
       panel when one of them changes: it reads them again every time it is opened. -->
  <DropdownMenu v-if="workspaces.active" @update:open="(open) => open && workspaces.load()">
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="workspace-pill"
        :class="{ compact }"
        :title="t('workspaces.switcher', { name: workspaceName(workspaces.active) })"
      >
        <WorkspaceAvatar :workspace="workspaces.active" size="sm" />
        <span v-if="!compact" class="workspace-name">{{ workspaceName(workspaces.active) }}</span>
        <Icon v-if="!compact" name="ChevronsUpDown" :size="12" :stroke-width="2.4" class="workspace-chevron" />
      </button>
    </DropdownMenuTrigger>
    <WorkspaceMenu :side="compact ? 'right' : 'bottom'" />
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

.workspace-pill.compact {
  width: 2.375rem;
  height: 2.125rem;
  justify-content: center;
  padding: 0;
}

.workspace-pill:hover,
.workspace-pill[data-state='open'] {
  background: var(--bg-hover);
}

.workspace-name {
  @apply flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap;
}

.workspace-chevron {
  @apply text-text-tertiary;
}
</style>
