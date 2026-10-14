<script setup lang="ts">
import { computed } from 'vue'
import ManagerRail from '../ui/ManagerRail.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useWorkspacesStore } from '../../stores/workspaces'
import { GLOBALS_COLOR, dotOf } from './palette'
import { useMessages } from '../../i18n'

// Which set of variables the pane beside it is about: the workspace's environments and, last, the
// globals — the scope that applies everywhere, and a row of the same list rather than a section.
const emit = defineEmits<{ (e: 'select', id: string | null): void; (e: 'create'): void }>()

const envStore = useEnvironmentsStore()
const workspaces = useWorkspacesStore()
const { t } = useMessages()

const rows = computed(() => [
  ...envStore.environments.map((env) => ({
    id: env.id as string | null,
    name: env.name,
    meta: '',
    dot: dotOf(env.color ?? ''),
    count: env.vars.length,
    active: env.id === envStore.editedEnvId,
    readonly: env.readonly,
  })),
  {
    id: null,
    name: t('environments.globals'),
    meta: '',
    dot: dotOf(GLOBALS_COLOR),
    count: envStore.globals.length,
    active: false,
  },
])
</script>

<template>
  <ManagerRail
    :rows
    :new-label="t('environments.new')"
    :can-create="workspaces.canWrite"
    @select="emit('select', $event)"
    @create="emit('create')"
  />
</template>
