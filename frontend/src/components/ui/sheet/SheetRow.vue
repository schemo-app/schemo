<script setup lang="ts">

const { label, note = '', tag = '', tone = 'drop' } = defineProps<{
  label: string
  note?: string
  tag?: string
  tone?: 'keep' | 'drop'
}>()
</script>

<template>
  <div class="sheet-row">
    <span class="row-text">
      <span class="row-label">{{ label }}</span>
      <span v-if="note" class="row-note">{{ note }}</span>
    </span>
    <span v-if="tag" class="row-tag" :class="tone">{{ tag }}</span>
    <slot v-else />
  </div>
</template>

<style scoped>
@reference "../../../style.css";

.sheet-row {
  @apply flex items-center gap-3 min-h-11 px-2.5 rounded-lg;
}

.sheet-row:hover {
  @apply bg-bg-hover;
}

.row-text {
  @apply flex-1 min-w-0 flex flex-col gap-0.5;
}

.row-label {
  @apply text-13;
}

.row-note {
  @apply text-12 text-text-tertiary;
}

.row-tag {
  @apply flex-none text-13 font-semibold px-2 py-1 rounded-md
         bg-bg-hover text-text-tertiary;
}

.row-tag.keep {
  @apply bg-green-soft;
  color: var(--green-text);
}
</style>
