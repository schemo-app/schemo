<script setup lang="ts">
import { SwitchRoot, SwitchThumb, type SwitchRootProps } from 'reka-ui'

const props = defineProps<SwitchRootProps>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <SwitchRoot v-bind="{ ...props, ...$attrs }" class="switch">
    <SwitchThumb class="thumb" />
  </SwitchRoot>
</template>

<style scoped>
@reference "../../../style.css";

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
  left: 0.125rem;
  width: 1.125rem;
  height: 1.125rem;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25);
  transition: transform 0.15s ease;
}

.switch[data-state='checked'] .thumb {
  transform: translateX(1rem);
}

@media (prefers-reduced-motion: reduce) {
  .switch,
  .thumb {
    transition: none;
  }
}
</style>
