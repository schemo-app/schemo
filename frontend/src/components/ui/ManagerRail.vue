<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'
import { useRowOrder } from '../../composables/useRowOrder'

interface RailRow {
  id: string | null
  name: string
  meta: string
  active: boolean
  readonly?: boolean
  initial?: string
  tint?: string
  dot?: string
  icon?: IconName
  count?: number
  checked?: boolean
  hint?: string
  fixed?: boolean
}

const { rows, newLabel, canCreate = true, reorderable = false } = defineProps<{
  rows: RailRow[]
  newLabel: string
  canCreate?: boolean
  reorderable?: boolean
}>()

const emit = defineEmits<{
  (e: 'select', id: string | null): void
  (e: 'create'): void
  (e: 'move', id: string, position: number): void
}>()

const pinned = computed(() => rows.filter((row) => row.fixed))
const ordered = computed(() => rows.filter((row) => !row.fixed))

const list = useTemplateRef<HTMLElement>('list')
const { press, states } = useRowOrder({
  rows: () => ordered.value.map((row) => ({ id: row.id ?? '' })),
  table: list,
  move: async (id, position) => emit('move', id, position),
})

function grab(event: PointerEvent, row: RailRow, index: number) {
  if (reorderable && row.id) press(event, row.id, index)
}
</script>

<template>
  <div class="sheet-side">
    <slot name="head" />
    <div class="side-rows">
      <template v-for="(group, at) in [pinned, ordered]" :key="at">
        <div :ref="at === 1 ? 'list' : undefined" class="side-group">
          <button
            v-for="(row, index) in group"
            :key="row.id ?? '_'"
            type="button"
            class="side-row"
            :class="[{ active: row.active, stacked: !row.dot && !row.icon }, at === 1 ? states[index]?.class : {}]"
            :style="at === 1 ? states[index]?.style : undefined"
            :data-kv="row.id ?? undefined"
            :title="row.hint"
            @pointerdown="at === 1 && grab($event, row, index)"
            @click="emit('select', row.id)"
          >
            <Icon v-if="row.icon" :name="row.icon" :size="13" class="side-icon" />
            <span v-else-if="row.dot" class="side-dot" :style="{ background: row.dot }"></span>
            <span v-else class="avatar" :style="{ background: row.tint }">{{ row.initial }}</span>
            <span class="side-text">
              <span class="side-name">{{ row.name }}</span>
              <span v-if="row.meta" class="side-meta">{{ row.meta }}</span>
            </span>
            <Icon v-if="row.readonly" name="Lock" :size="12" class="side-lock" />
            <Icon v-if="row.checked" name="Check" :size="12" :stroke-width="2.6" class="side-check" />
            <span v-if="row.count !== undefined" class="side-count">{{ row.count }}</span>
          </button>
        </div>
      </template>
    </div>

    <div v-if="canCreate" class="side-create">
      <button type="button" class="side-new" @click="emit('create')">
        <span class="side-new-mark"><Icon name="Plus" :size="14" :stroke-width="2.2" /></span>
        <span class="cap-line">{{ newLabel }}</span>
      </button>
    </div>

    <div v-if="$slots.foot" class="side-foot">
      <slot name="foot" />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.sheet-side {
  @apply flex-none flex flex-col min-h-0 border-r;
  width: 13.125rem;
  padding: 0.875rem 0.5rem 0.5rem;
  background: var(--bg-sidebar);
  border-color: var(--border);
}

.side-rows {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col;
  gap: 1px;
}

.side-group {
  @apply flex flex-col flex-none;
  gap: 1px;
}

.side-group:first-child:not(:empty) {
  margin-bottom: 0.5rem;
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

.side-row.row-carried {
  opacity: 0.6;
}

.side-dot {
  @apply flex-none;
  width: 0.4375rem;
  height: 0.4375rem;
  margin: 0 0.25rem;
  border-radius: 999px;
}

.side-icon {
  @apply flex-none text-text-tertiary;
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

.side-lock,
.side-check {
  @apply flex-none text-text-tertiary;
}

.side-count {
  @apply flex-none text-text-tertiary;
  font-size: var(--text-12);
}

.side-foot {
  @apply flex-none;
  padding: 0.1875rem 0;
}

.side-create {
  @apply flex-none;
  padding-top: 0.125rem;
}

.side-new {
  @apply flex items-center w-full gap-[0.5625rem] h-7 px-2 border-0 rounded-md cursor-pointer
         bg-transparent text-accent;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.side-new-mark {
  @apply flex items-center justify-center flex-none w-4;
}

.side-new:hover {
  @apply bg-accent-soft;
}
</style>
