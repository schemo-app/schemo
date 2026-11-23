<script setup lang="ts">
import Icon, { type IconName } from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'

// Without `dropIcon` the cell stays, so the buttons keep their column.
const { quiet = false, dropIcon, dropTitle, disabled = false } = defineProps<{
  quiet?: boolean
  dropIcon?: IconName
  dropTitle?: string
  disabled?: boolean
}>()

defineEmits<{ (e: 'drop'): void }>()
</script>

<template>
  <div class="member">
    <span class="member-who">
      <Icon name="User" :size="12" class="member-icon" :class="{ quiet }" />
      <slot />
    </span>

    <span class="role">
      <slot name="role" />
    </span>

    <IconButton
      v-if="dropIcon"
      variant="danger"
      size="xl"
      :hint="dropTitle"
      :aria-label="dropTitle"
      :disabled
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
  grid-template-columns: minmax(0, 1fr) 140px 26px;
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
  @apply inline-flex items-center gap-1.5 min-w-0 h-7;
}

.role :deep(.select),
.role :deep(select) {
  width: 100%;
  min-width: 0;
}

</style>
