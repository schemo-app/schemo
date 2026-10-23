<script setup lang="ts">
import { useTemplateRef } from 'vue'

// The two buttons a modal's foot holds: the plain chip, and the accent one that commits.
const { primary = false } = defineProps<{ primary?: boolean }>()

// A foot may want the focus on one of them when it opens — Enter on a question about unsaved work
// should keep it, not throw it away.
const el = useTemplateRef<HTMLButtonElement>('el')
defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <button ref="el" type="button" :class="primary ? 'primary' : 'chip'">
    <slot />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.chip,
.primary {
  @apply border-0 cursor-pointer;
  height: 2rem;
  border-radius: 0.5rem;
  font: inherit;
}

.chip {
  padding: 0 0.875rem;
  background: var(--chip);
  color: var(--text);
}

.chip:hover {
  background: var(--bg-hover);
}

.primary {
  padding: 0 1rem;
  background: var(--accent);
  color: #fff;
  font-weight: 600;
}

.primary:disabled {
  opacity: 0.4;
  cursor: default;
}

.primary:not(:disabled):hover {
  filter: brightness(1.06);
}

.primary:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-soft);
}
</style>
