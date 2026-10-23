<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'

// The choice at the head of a section of the request pane: which scheme authorizes it, which format
// its body is in. Drawing only; `cell` is the same choice inside a row of a table, with no frame.
export type PaneOption = { key: string; label: string; hint?: string }

const { label, options, current, variant = 'field' } = defineProps<{
  label: string
  variant?: 'field' | 'cell'
  // A group is a run of options between two separators, the way the menu draws them.
  options: PaneOption[][]
  current: string
}>()

const emit = defineEmits<{ select: [key: string] }>()
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <button type="button" class="pane-select" :class="variant">
        <span class="pane-select-name">{{ label }}</span>
        <Icon name="ChevronDown" :size="10" :stroke-width="2.45" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="pane-select-menu" align="start" :side-offset="6">
      <template v-for="(group, gi) in options" :key="gi">
        <DropdownMenuSeparator v-if="gi > 0" />
        <DropdownMenuItem
          v-for="option in group"
          :key="option.key"
          :class="{ current: option.key === current }"
          @select="emit('select', option.key)"
        >
          <span class="pane-select-check">
            <Icon v-if="option.key === current" name="Check" :size="14" :stroke-width="1.75" />
          </span>
          <span class="pane-select-label cap-line">{{ option.label }}</span>
          <span v-if="option.hint" class="pane-select-hint">{{ option.hint }}</span>
        </DropdownMenuItem>
      </template>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<style scoped>
@reference "../../style.css";

.pane-select {
  @apply flex flex-none items-center gap-2 cursor-pointer;
  height: 1.75rem;
  padding: 0 0.5rem 0 0.75rem;
  border: 1px solid var(--border-strong);
  border-radius: 0.5rem;
  background: var(--bg-inset);
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-12-5);
  font-weight: 500;
  --wails-draggable: no-drag;
}

.pane-select:hover,
.pane-select[data-state='open'] {
  background: var(--bg-hover);
}

.pane-select svg {
  @apply flex-none text-text-tertiary;
}

.pane-select-name {
  @apply whitespace-nowrap;
}

.pane-select.cell {
  @apply gap-2.5;
  height: 2rem;
  padding: 0 0.75rem;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  box-shadow: inset 0 0 0 1px var(--border-strong);
  font-weight: 400;
}

.pane-select.cell:hover {
  background: var(--bg-hover);
}

.pane-select.cell[data-state='open'] {
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--accent);
}
</style>
