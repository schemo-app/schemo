<script setup lang="ts">

import { useTemplateRef } from 'vue'

const { variant = 'field', size = 'md', type = 'text' } = defineProps<{
  variant?: 'field' | 'bare'
  size?: 'sm' | 'md'
  type?: string
  mono?: boolean
}>()

const model = defineModel<string>({ default: '' })

const el = useTemplateRef<HTMLInputElement>('el')

defineExpose({ focus: () => el.value?.focus(), select: () => el.value?.select() })
</script>

<template>
  <input
    ref="el"
    v-model="model"
    :type
    :class="['inp', `inp--${variant}`, `inp--${size}`, { 'inp--mono': mono }]"
  />
</template>

<style scoped>
@reference "../../../style.css";

.inp {
  @apply text-text outline-none;
  font-family: inherit;
  font-size: inherit;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
  --wails-draggable: no-drag;
}

.inp--mono {
  font-family: var(--mono);
}

.inp--field {
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.inp--field.inp--md {
  height: 2rem;
  padding: 0 0.5rem;
  border-radius: 0.5rem;
  font-size: var(--text-13);
}

.inp--field.inp--sm {
  height: 1.75rem;
  padding: 0 0.5rem;
  border-radius: 0.375rem;
  font-size: var(--text-12);
}

.inp--field:hover:not(:disabled) {
  border-color: var(--border-strong);
}

.inp--field:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.inp--field:disabled {
  @apply opacity-50 cursor-default;
}

.inp--bare {
  @apply border-0 bg-transparent;
}

.inp--bare.inp--sm {
  font-size: var(--text-12);
}
</style>
