<script setup lang="ts">
// What a section says in the middle of the room it would have filled, when there is nothing in it yet:
// a title, a sentence, and the one thing to do about it — the caller's, as the slot.
defineProps<{ title: string; text: string }>()
</script>

<template>
  <div class="empty">
    <div class="box">
      <span class="title">{{ title }}</span>
      <span class="text">{{ text }}</span>
      <slot />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.empty {
  @apply flex flex-1 min-h-0 items-center justify-center;
  padding: 1.5rem;
}

.box {
  @apply flex flex-col items-center text-center;
  max-width: 23.75rem;
  gap: 0.625rem;
}

.title {
  font-size: var(--text-15);
  font-weight: 600;
}

.text {
  @apply text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}
</style>
