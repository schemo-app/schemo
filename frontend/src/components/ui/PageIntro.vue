<script setup lang="ts">
import Icon, { type IconName } from './Icon.vue'

// The head of a page that has nothing to list yet: an icon tile, a title and what to do about it.
defineProps<{ icon: IconName; title: string; text: string }>()
</script>

<template>
  <div class="intro">
    <span class="tile"><Icon :name="icon" :size="24" :stroke-width="1.5" /></span>
    <span class="text">
      <span class="title">{{ title }}</span>
      <span class="note">{{ text }}</span>
    </span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.intro {
  @apply flex items-start;
  max-width: 67.5rem;
  gap: 1rem;
}

.tile {
  @apply flex items-center justify-center flex-none;
  width: 3rem;
  height: 3rem;
  border-radius: 0.75rem;
  background: var(--chip);
  color: var(--text-secondary);
}

.text {
  @apply flex flex-col flex-1 min-w-0;
  max-width: 38.75rem;
  gap: 0.1875rem;
}

.title {
  @apply font-semibold;
  font-size: var(--text-20);
  letter-spacing: -0.015em;
}

.note {
  color: var(--text-secondary);
  font-size: var(--text-14);
  line-height: 1.5;
  text-wrap: pretty;
}
</style>
