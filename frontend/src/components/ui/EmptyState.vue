<script setup lang="ts">
import Icon, { type IconName } from './Icon.vue'

// What a section says in the middle of the room it would have filled, when there is nothing in it yet:
// a title, a sentence, and the one thing to do about it — the caller's, as the slot.
defineProps<{ title: string; text: string; icon?: IconName }>()
</script>

<template>
  <div class="empty">
    <div class="box">
      <Icon v-if="icon" :name="icon" :size="28" :stroke-width="1.6" class="mark" />
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

.mark {
  @apply text-text-tertiary;
  margin-bottom: 0.25rem;
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
