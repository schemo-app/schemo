<script setup lang="ts">
import { useTemplateRef } from 'vue'

const { primary = false, danger = false } = defineProps<{ primary?: boolean; danger?: boolean }>()
const el = useTemplateRef<HTMLButtonElement>('el')
defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <button ref="el" type="button" :class="danger ? 'danger' : primary ? 'primary' : 'chip'">
    <slot />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.chip,
.primary,
.danger {
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

.primary,
.danger {
  padding: 0 1rem;
  color: #fff;
  font-weight: 600;
}

.primary {
  background: var(--accent);
}

.danger {
  background: var(--red);
}

.primary:disabled,
.danger:disabled {
  opacity: 0.4;
  cursor: default;
}

.primary:not(:disabled):hover,
.danger:not(:disabled):hover {
  filter: brightness(1.06);
}

.primary:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.danger:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--red-soft);
}
</style>
