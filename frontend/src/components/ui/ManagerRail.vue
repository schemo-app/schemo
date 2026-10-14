<script setup lang="ts">
import Icon from './Icon.vue'

interface RailRow {
  id: string | null
  name: string
  meta: string
  active: boolean
  readonly?: boolean
  initial?: string
  tint?: string
  // A list whose rows are one line marks its own by a dot and counts beside the name; one whose rows
  // carry a second line marks them by the square and puts that line under the name.
  dot?: string
  count?: number
  hint?: string
}

const { rows, newLabel, canCreate = true } = defineProps<{
  rows: RailRow[]
  newLabel: string
  canCreate?: boolean
}>()

const emit = defineEmits<{ (e: 'select', id: string | null): void; (e: 'create'): void }>()
</script>

<template>
  <div class="sheet-side">
    <div class="side-rows">
      <button
        v-for="row in rows"
        :key="row.id ?? '_'"
        type="button"
        class="side-row"
        :class="{ active: row.active, stacked: !row.dot }"
        :title="row.hint"
        @click="emit('select', row.id)"
      >
        <span v-if="row.dot" class="side-dot" :style="{ background: row.dot }"></span>
        <span v-else class="avatar" :style="{ background: row.tint }">{{ row.initial }}</span>
        <span class="side-text">
          <span class="side-name">{{ row.name }}</span>
          <span v-if="row.meta" class="side-meta">{{ row.meta }}</span>
        </span>
        <Icon v-if="row.readonly" name="Lock" :size="12" class="side-lock" />
        <span v-if="row.count !== undefined" class="side-count">{{ row.count }}</span>
      </button>
    </div>

    <div v-if="canCreate" class="side-foot">
      <button type="button" class="side-new" @click="emit('create')">
        <Icon name="Plus" :size="14" :stroke-width="2.2" />
        <span class="cap-line">{{ newLabel }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.sheet-side {
  @apply flex-none flex flex-col border-r;
  width: 13.125rem;
  padding: 0.875rem 0.5rem 0.5rem;
  background: var(--bg-sidebar);
  border-color: var(--border);
}

.side-rows {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col;
  gap: 1px;
}

.side-row {
  @apply flex items-center w-full text-left border-0 bg-transparent cursor-pointer text-text;
  gap: 0.5625rem;
  min-height: 1.875rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  font-family: inherit;
  --wails-draggable: no-drag;
}

.side-row.stacked {
  min-height: 0;
  padding: 0.4375rem 0.5rem;
}

.side-row:hover {
  @apply bg-bg-hover;
}

.side-row.active {
  @apply bg-bg-hover;
}

.side-dot {
  @apply flex-none;
  width: 0.4375rem;
  height: 0.4375rem;
  margin: 0 0.25rem;
  border-radius: 999px;
}

.avatar {
  @apply letter-square w-5.5 h-5.5 rounded-md;
}

.side-text {
  @apply flex-1 min-w-0 flex flex-col gap-px;
}

.side-name {
  @apply text-13 overflow-hidden text-ellipsis whitespace-nowrap;
}

.side-row.active .side-name {
  @apply font-medium;
}

.side-meta {
  @apply text-11 text-text-tertiary overflow-hidden text-ellipsis whitespace-nowrap;
}

.side-lock {
  @apply flex-none text-text-tertiary;
}

.side-count {
  @apply flex-none text-text-tertiary;
  font-size: var(--text-11-5);
}

.side-foot {
  @apply flex-none;
  padding: 0.2rem 0;
}

.side-new {
  @apply flex items-center w-full gap-1.5 h-7 px-2 border-0 rounded-md cursor-pointer
         bg-transparent text-accent;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.side-new:hover {
  @apply bg-accent-soft;
}
</style>
