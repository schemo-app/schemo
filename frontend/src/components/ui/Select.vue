<script setup lang="ts">
import Icon from './Icon.vue'

const {
  options,
  disabled = false,
  roomy = false,
} = defineProps<{
  options: { value: string; label: string }[]
  disabled?: boolean
  roomy?: boolean
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <span class="select" :class="{ disabled, roomy }">
    <select v-model="model" :disabled>
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
    <Icon name="ChevronDown" :size="10" :stroke-width="2.6" class="chevron" />
  </span>
</template>

<style scoped>
@reference "../../style.css";

.select {
  @apply relative inline-flex flex-none items-center;
}

select {
  @apply h-7 rounded-[0.3rem] text-13 text-text cursor-pointer appearance-none;
  font: inherit;
  font-size: var(--text-13);
  min-width: 8.75rem;
  padding: 0 1.875rem 0 0.625rem;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.select.roomy {
  @apply flex-1 min-w-0 w-full;
}

.select.roomy select {
  height: 2.125rem;
  width: 100%;
  padding: 0 2rem 0 0.625rem;
  font-family: var(--mono);
}

select:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.select.disabled select,
select:disabled {
  @apply opacity-50 cursor-default;
}

.chevron {
  @apply absolute right-2 text-text-tertiary pointer-events-none;
}

.select.roomy .chevron {
  right: 0.625rem;
}
</style>
