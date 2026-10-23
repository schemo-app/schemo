<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import Icon from '../ui/Icon.vue'
import WorkspaceAvatar from './WorkspaceAvatar.vue'
import { useWorkspacesStore, workspaceName } from '../../stores/workspaces'
import { useWorkspaceCounts } from '../../composables/useWorkspaceCounts'
import { t } from '../../i18n'
import { switchWorkspace } from '../../lib/switchWorkspace'

// The panel under the titlebar's chip: every workspace, in the order they were made, with what each
// holds as the row's own tooltip, so a row is one line. Choosing one moves the whole window; the
// two rows at the foot are the ways out of the panel: a new workspace, and the window that manages the
// existing ones.
defineProps<{ side?: 'right' | 'bottom' }>()

const workspaces = useWorkspacesStore()
const { metaOf } = useWorkspaceCounts()

async function choose(id: string) {
  await switchWorkspace(id)
}
</script>

<template>
  <DropdownMenuContent class="ws-menu" :class="{ 'ws-menu-wide': side !== 'right' }" align="start" :side="side">
    <DropdownMenuItem
      v-for="workspace in workspaces.list"
      :key="workspace.id"
      class="ws-row"
      :class="{ active: workspace.id === workspaces.activeId }"
      @select="choose(workspace.id)"
    >
      <span class="ws-mark">
        <WorkspaceAvatar :workspace size="sm" />
      </span>
      <span class="ws-name cap-line" :title="metaOf(workspace)">{{ workspaceName(workspace) }}</span>
      <Icon
        v-if="workspace.id === workspaces.activeId"
        name="Check"
        :size="12"
        :stroke-width="2.6"
        class="ws-check"
      />
    </DropdownMenuItem>

    <!-- The two ways out are menu items and not plain buttons: a panel that stayed open behind the
    window it just opened would be a second thing on screen, and closing on the way out is what an
    item does. `as-child` so the rows keep their own look rather than the menu item's. -->
    <div class="ws-separator"></div>

    <DropdownMenuItem as-child @select="workspaces.openCreate()">
      <button type="button" class="ws-row">
        <span class="ws-mark"><Icon name="Plus" :size="14" :stroke-width="1.9" /></span>
        <span class="ws-name cap-line">{{ t('workspaces.createMenu') }}</span>
      </button>
    </DropdownMenuItem>
    <DropdownMenuItem as-child @select="workspaces.openSheet()">
      <button type="button" class="ws-row">
        <span class="ws-mark"><Icon name="SlidersHorizontal" :size="14" :stroke-width="1.8" /></span>
        <span class="ws-name cap-line">{{ t('workspaces.configure') }}</span>
      </button>
    </DropdownMenuItem>
  </DropdownMenuContent>
</template>

<style scoped>
@reference "../../style.css";

.ws-row {
  @apply flex items-center w-full border-none bg-transparent text-text text-left cursor-pointer;
  gap: 0.5625rem;
  height: 1.875rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  font: inherit;
  --wails-draggable: no-drag;
}

.ws-row:hover,
.ws-row[data-highlighted] {
  @apply bg-bg-hover;
  outline: none;
}

.ws-row.active {
  @apply bg-bg-hover;
}

.ws-mark {
  @apply flex items-center justify-center flex-none;
  width: 1.25rem;
  color: var(--text-secondary);
}

.ws-name {
  @apply flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-13;
}

.ws-row.active .ws-name {
  @apply font-medium;
}

.ws-check {
  @apply flex-none text-text-secondary;
}

.ws-separator {
  height: 1px;
  margin: 0.25rem 0.3rem;
  background: var(--border);
}
</style>
