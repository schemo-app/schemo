<script setup lang="ts">
// A line of a transfer's list: a tile with the format's mark, its name, and what it says of itself.
// What stands at the end — the tick of the chosen one — is the caller's.
defineProps<{ mono: string; name: string; hint: string; active?: boolean }>()
</script>

<template>
  <div class="row" :class="{ active }">
    <span class="tile">{{ mono }}</span>
    <span class="name">{{ name }}</span>
    <span class="hint">{{ hint }}</span>
    <slot />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.row {
  @apply flex items-center;
  height: 2.5rem;
  gap: 0.6875rem;
  padding: 0 0.6rem;
  border-radius: 0.5rem;
  background: transparent;
  cursor: default;
}

.row:hover,
.row.active {
  background: var(--bg-hover);
}

.tile {
  @apply flex items-center justify-center flex-none;
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.3rem;
  background: var(--chip);
  color: var(--text-secondary);
  font: 700 0.59375rem var(--mono);
}

.name {
  @apply flex-1 min-w-0 font-medium;
}

.hint {
  @apply flex-none;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}
</style>
