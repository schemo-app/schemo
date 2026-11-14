<script setup lang="ts">
defineProps<{ info?: string }>()
</script>

<template>
  <div class="body-bar">
    <slot />
    <span v-if="info" class="body-info" :title="info">{{ info }}</span>
    <slot name="actions" />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.body-bar {
  @apply flex items-center flex-none gap-2.5;
  height: 2.5rem;
  padding: 0 0.8125rem;
  overflow: hidden;
}

.body-info {
  @apply flex-1 min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12);
}
</style>
