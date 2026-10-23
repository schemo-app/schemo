<script setup lang="ts">
// One line of a section: what the row is, what it is about, and the one control that decides it.
// `soon` marks a control that is drawn and does nothing; `alarm` paints the note as a warning.
defineProps<{ title: string; note?: string; soon?: string; alarm?: boolean }>()
</script>

<template>
  <div class="row">
    <span class="text">
      <span class="head">
        <span class="title">{{ title }}</span>
        <span v-if="soon" class="soon">{{ soon }}</span>
      </span>
      <span v-if="note" class="note" :class="{ alarm }">{{ note }}</span>
    </span>
    <span class="control"><slot /></span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.row {
  @apply flex items-center gap-3.5;
  min-height: 2.75rem;
  padding: 0.75rem 1rem;
}

.text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.125rem;
}

.head {
  @apply flex items-center gap-2 min-w-0;
}

.title {
  @apply text-14;
}

.soon {
  @apply flex-none inline-flex items-center h-4.5 px-1.5 rounded-[0.25rem]
         text-11 text-text-tertiary;
  background: var(--bg-hover);
}

.note {
  @apply text-12 text-text-tertiary;
  line-height: 1.5;
}

.note.alarm {
  color: var(--red-text);
}

.control {
  @apply flex-none flex items-center gap-2;
}
</style>
