<script setup lang="ts">
import { CheckboxIndicator, CheckboxRoot } from 'reka-ui'
import Icon from './Icon.vue'

// Only the props in use: spreading all of reka's would cast an absent trueValue to false.
const { tone = 'accent', modelValue = false, disabled = false, size = 'md' } = defineProps<{
  modelValue?: boolean
  disabled?: boolean
  tone?: 'accent' | 'secret'
  size?: 'sm' | 'md'
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <CheckboxRoot
    v-bind="$attrs"
    :model-value
    :disabled
    class="check"
    :class="{ secret: tone === 'secret', small: size === 'sm' }"
    @update:model-value="(value) => emit('update:modelValue', value === true)"
  >
    <CheckboxIndicator>
      <Icon name="Check" :size="size === 'sm' ? 9 : 12" :stroke-width="size === 'sm' ? 4 : 3" />
    </CheckboxIndicator>
  </CheckboxRoot>
</template>

<style scoped>
@reference "../../style.css";

.check {
  @apply flex-none flex items-center justify-center cursor-pointer text-white;
  width: 1.125rem;
  height: 1.125rem;
  border-radius: 0.25rem;
  border: 1px solid var(--border-strong);
  background: var(--bg-panel);
  transition: background-color 0.15s ease, border-color 0.15s ease;
}

.check[data-state='checked'] {
  border-color: var(--accent);
  background: var(--accent);
}

.check.small {
  width: 14px;
  height: 14px;
  border-width: 1.5px;
}

.check.secret[data-state='checked'] {
  border-color: var(--orange);
  background: var(--orange);
}
</style>
