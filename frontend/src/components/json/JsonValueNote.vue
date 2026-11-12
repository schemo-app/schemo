<script setup lang="ts">
import { computed } from 'vue'
import type { ReadingRow } from '../../../bindings/schemo/internal/domain'
import { formatReadingTime } from '../../lib/reading'

const { reading } = defineProps<{ reading?: ReadingRow }>()
const note = computed(() => reading && formatReadingTime(reading))
</script>

<template>
  <span v-if="note" class="json-value-note" :title="note">{{ note }}</span>
</template>

<style scoped>
@reference "../../style.css";

.json-value-note {
  @apply min-w-0 flex-none truncate text-12 text-text-tertiary;
  max-width: 55%;
  margin-left: 0.75rem;
  font-family: system-ui, sans-serif;
}
</style>
