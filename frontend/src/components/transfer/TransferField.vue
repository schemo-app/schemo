<script setup lang="ts">
import Icon from '../ui/Icon.vue'

const { label = '', options = [] } = defineProps<{
  label?: string
  options?: { id: string; label: string }[]
}>()

const model = defineModel<string>()
</script>

<template>
  <div class="field">
    <span v-if="label" class="label">{{ label }}</span>
    <slot>
      <div class="pick">
        <select v-model="model">
          <option v-for="option in options" :key="option.id" :value="option.id">
            {{ option.label }}
          </option>
        </select>
        <Icon name="ChevronsUpDown" :size="10" :stroke-width="2.6" class="pick-arrow" />
      </div>
    </slot>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.field {
  @apply flex flex-col;
  gap: 0.3rem;
}

.label {
  color: var(--text-secondary);
  font-size: var(--text-12);
}

.pick {
  @apply relative flex;
}

.pick select {
  @apply flex-1 min-w-0 border-0 outline-none appearance-none;
  height: 2rem;
  border-radius: 0.5rem;
  background: var(--bg-panel);
  box-shadow: inset 0 0 0 1px var(--border);
  padding: 0 1.875rem 0 0.625rem;
  font: inherit;
  color: var(--text);
  cursor: default;
}

.pick-arrow {
  @apply absolute pointer-events-none;
  right: 0.625rem;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-tertiary);
}
</style>
