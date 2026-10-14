<script setup lang="ts">
import Icon from '../ui/Icon.vue'

// A row of the panel that does something; `menu` marks one that opens a list.
defineProps<{ menu?: boolean }>()
</script>

<template>
  <button type="button" class="action">
    <slot />
    <Icon v-if="menu" name="ChevronDown" :size="10" :stroke-width="2.6" />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.action {
  @apply flex w-full items-center justify-between gap-2 cursor-pointer rounded-[0.375rem] border-0 bg-transparent px-2 text-left text-13 text-text;
  height: 1.875rem;
  --wails-draggable: no-drag;
}

.action:hover {
  background: var(--bg-hover);
}
</style>
