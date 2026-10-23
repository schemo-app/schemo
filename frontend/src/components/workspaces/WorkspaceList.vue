<script setup lang="ts">
import { computed } from 'vue'
import ManagerRail from '../ui/ManagerRail.vue'
import { tintOf } from '../../lib/workspacePalette'
import { useWorkspacesStore, workspaceInitial, workspaceName } from '../../stores/workspaces'
import { useWorkspaceCounts } from '../../composables/useWorkspaceCounts'
import { t } from '../../i18n'

// The rail of the manager window: every workspace as a row, and the one way to make another. What a
// workspace holds is the second line — the same reading the window's Contents block draws, one line long.
const emit = defineEmits<{ (e: 'select', id: string): void; (e: 'create'): void }>()

const workspaces = useWorkspacesStore()
const { metaOf } = useWorkspaceCounts()

const rows = computed(() =>
  workspaces.list.map((workspace) => ({
    id: workspace.id as string | null,
    name: workspaceName(workspace),
    meta: metaOf(workspace),
    initial: workspaceInitial(workspace),
    tint: tintOf(workspace.color),
    active: workspace.id === workspaces.editedId,
  }))
)

// Every row of this rail is a workspace, so the rail's own "no scope" case never comes up here.
function select(id: string | null) {
  if (id !== null) emit('select', id)
}
</script>

<template>
  <ManagerRail :rows :new-label="t('workspaces.create')" @select="select" @create="emit('create')" />
</template>
