<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from 'reka-ui'

const { modelValue = false, disabled = false } = defineProps<{
  modelValue?: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <SwitchRoot
    v-bind="$attrs"
    :model-value
    :disabled
    class="switch"
    @update:model-value="(value) => emit('update:modelValue', value)"
  >
    <SwitchThumb class="thumb" />
  </SwitchRoot>
</template>

<style scoped>
@reference "../../style.css";

.switch {
  @apply relative flex-none rounded-full cursor-pointer;
  width: 2.375rem;
  height: 1.375rem;
  background: var(--bg-hover);
  transition: background 0.15s ease;
}

.switch[data-state='checked'] {
  background: var(--accent);
}

.switch:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.switch:disabled {
  @apply opacity-50 cursor-default;
}

.thumb {
  @apply block rounded-full;
  position: absolute;
  top: 0.125rem;
  inset-inline-start: 0.125rem;
  width: 1.125rem;
  height: 1.125rem;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25);
  transition: inset-inline-start 0.15s ease;
}

.switch[data-state='checked'] .thumb {
  inset-inline-start: calc(100% - 1.125rem - 0.125rem);
}

@media (prefers-reduced-motion: reduce) {
  .switch,
  .thumb {
    transition: none;
  }
}
</style>
