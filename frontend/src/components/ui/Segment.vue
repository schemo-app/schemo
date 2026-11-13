<script setup lang="ts">
import { useTemplateRef } from 'vue'
import { useSlidingPill } from '../../composables/useSlidingPill'

const { value, options, size = 'md', grow = true, soonHint = '', disabled = false, variant = 'default' } = defineProps<{
  options: { value: string; label: string; title?: string; soon?: boolean; count?: string | number }[]
  value: string
  size?: 'md' | 'sm' | 'xs'
  grow?: boolean
  soonHint?: string
  disabled?: boolean
  variant?: 'default' | 'toolbar'
}>()

const emit = defineEmits<{ (e: 'pick', value: string): void }>()

const rootEl = useTemplateRef<HTMLElement>('rootEl')
const { style: pillStyle, ready: pillReady } = useSlidingPill(rootEl, '.seg.active', () => [
  value,
  options.map((option) => `${option.label}${option.count ?? ''}`).join('|'),
])
</script>

<template>
  <div ref="rootEl" class="segment" :class="[`segment--${size}`, `segment--${variant}`, { grow, disabled }]">
    <span class="seg-pill slide-mark" :class="{ ready: pillReady }" :style="pillStyle"></span>
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      class="seg"
      :class="{ active: option.value === value, soon: option.soon }"
      :disabled="disabled || option.soon"
      :title="option.soon ? soonHint : option.title"
      :aria-pressed="option.value === value"
      @click="emit('pick', option.value)"
    >
      {{ option.label }}
      <span v-if="option.count" class="count">{{ option.count }}</span>
    </button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.segment {
  @apply relative flex gap-0.5 p-0.5 rounded-lg bg-bg-hover;
  --wails-draggable: no-drag;
}

.segment .seg-pill {
  border-radius: 0.3125rem;
}

.seg {
  @apply relative inline-flex items-center justify-center min-w-0 text-center border-none rounded-md
         cursor-pointer bg-transparent text-text-secondary;
  gap: 0.3125rem;
  font: inherit;
  font-size: var(--text-13);
  font-weight: 500;
  padding: 0 0.6875rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: background 0.15s ease, color 0.15s ease;
}

.segment.grow .seg {
  @apply flex-1;
}

.seg.active {
  @apply text-text;
}

.segment--md .seg {
  height: 1.75rem;
}

.segment--sm .seg {
  height: 1.625rem;
}

.segment--xs .seg {
  height: 1.5rem;
}

.count {
  @apply font-normal text-text-tertiary;
}

.segment.disabled {
  @apply opacity-50;
}

.seg:disabled {
  @apply cursor-default;
}

.seg.soon {
  @apply text-text-tertiary;
}

.segment--toolbar {
  gap: 0;
  background: var(--chip);
}

.segment--toolbar .seg-pill {
  border-radius: 0.375rem;
  background: var(--bg);
}

.segment--toolbar .seg {
  padding: 0 0.625rem;
  font-weight: 400;
}

.segment--toolbar .seg.active {
  font-weight: 500;
}
</style>
