<script setup lang="ts">
import Icon, { type IconName } from '../ui/Icon.vue'

// One line of a space's people: who, the role they hold, and a button to take them out. A role that
// is only read is a word in a box; one that may be handed out is the application's own select, which
// wears a frame of its own. Without `dropIcon` the cell stays, so that the buttons stay a column.
const { tint, live = false, quiet = false, dropIcon, dropTitle } = defineProps<{
  tint?: string
  live?: boolean
  quiet?: boolean
  dropIcon?: IconName
  dropTitle?: string
}>()

defineEmits<{ (e: 'drop'): void }>()
</script>

<template>
  <div class="member">
    <span class="member-who">
      <Icon name="User" :size="12" class="member-icon" :class="{ quiet }" />
      <slot />
    </span>

    <span class="role" :class="{ boxed: !live }">
      <span v-if="!live" class="role-dot" :style="{ background: tint }"></span>
      <slot name="role" />
    </span>

    <button v-if="dropIcon" class="member-drop" type="button" :title="dropTitle" @click="$emit('drop')">
      <Icon :name="dropIcon" :size="10" />
    </button>
    <span v-else></span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.member {
  @apply grid items-center gap-2 py-1.5 px-1.5 min-h-10;
  grid-template-columns: 1fr auto 1.625rem;
}

.member-who {
  @apply flex items-center gap-2 min-w-0;
}

.member-icon {
  @apply flex-none text-text-secondary;
}

.member-icon.quiet {
  @apply text-text-tertiary;
}

.role {
  @apply inline-flex items-center gap-1.5 min-w-0;
}

.role.boxed {
  @apply min-w-24 h-6 px-2 rounded-md border border-border bg-bg-hover
         text-11-5 text-text-secondary;
}

.role-dot {
  @apply w-1.5 h-1.5 rounded-full flex-none;
}

.member-drop {
  @apply inline-flex items-center justify-center justify-self-end w-5 h-5 rounded-md
         cursor-pointer text-text-tertiary;
  background: transparent;
  border: 0;
}

.member-drop:hover {
  @apply text-red bg-red-soft;
}
</style>
