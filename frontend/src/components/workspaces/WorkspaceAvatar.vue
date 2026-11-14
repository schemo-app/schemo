<script setup lang="ts">
import { computed } from 'vue'
import type { Workspace } from '../../../bindings/schemo/internal/domain'
import { tintOf } from '../../lib/workspacePalette'
import { workspaceInitial } from '../../stores/workspaces'

const {
  workspace,
  size = 'md',
} = defineProps<{ workspace: Workspace; size?: 'sm' | 'md' }>()

const style = computed(() => ({ background: tintOf(workspace.color) }))
</script>

<template>
  <span class="avatar" :class="`avatar--${size}`" :style>
    {{ workspaceInitial(workspace) }}
  </span>
</template>

<style scoped>
@reference "../../style.css";

.avatar {
  @apply letter-square;
  letter-spacing: 0.01em;
}

.avatar--sm {
  @apply w-5 h-5 rounded-[0.3125rem];
}

.avatar--md {
  @apply w-6.5 h-6.5 rounded-[0.4375rem] text-12;
}
</style>
