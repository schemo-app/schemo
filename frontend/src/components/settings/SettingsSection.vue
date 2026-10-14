<script setup lang="ts">
// A group of lines under a heading: the group named in a line of its own, a card drawn around the
// lines, and a sentence under it when the group needs one. `lead` stands between the two.
defineProps<{ label: string; note?: string }>()
</script>

<template>
  <div class="section">
    <span class="label">{{ label }}</span>
    <slot name="lead" />
    <div class="card"><slot /></div>
    <span v-if="note" class="note">{{ note }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.section {
  @apply flex flex-col gap-2.5;
}

.label {
  @apply text-12 font-semibold text-text-tertiary;
}

.card {
  @apply border border-border rounded-[0.625rem] overflow-hidden;
}

.card :deep(.row + .row),
.card :deep(.row + .card-block),
.card :deep(.card-block + .row),
.card :deep(.card-block + .card-block) {
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.note {
  @apply text-12 text-text-tertiary;
  line-height: 1.5;
}
</style>
