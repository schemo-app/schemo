<script setup lang="ts">
import Icon, { type IconName } from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'

// Without `dropIcon` the cell stays, so the buttons keep their column.
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

    <IconButton
      v-if="dropIcon"
      variant="danger"
      size="xl"
      :hint="dropTitle"
      :aria-label="dropTitle"
      @click="$emit('drop')"
    >
      <Icon :name="dropIcon" :size="14" :stroke-width="1.8" />
    </IconButton>
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
         text-12 text-text-secondary;
}

.role-dot {
  @apply w-1.5 h-1.5 rounded-full flex-none;
}
</style>
