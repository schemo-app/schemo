<script setup lang="ts">
import { CheckboxIndicator, CheckboxRoot, type CheckboxRootProps } from 'reka-ui'
import Icon from './Icon.vue'

const { tone = 'accent', ...rootProps } = defineProps<CheckboxRootProps & { tone?: 'accent' | 'secret' }>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <CheckboxRoot v-bind="{ ...rootProps, ...$attrs }" class="check" :class="{ secret: tone === 'secret' }">

    <CheckboxIndicator>
      <Icon name="Check" :size="12" :stroke-width="3" />
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

.check.secret[data-state='checked'] {
  border-color: var(--orange);
  background: var(--orange);
}
</style>
