<script setup lang="ts">
import { computed } from 'vue'
import Segment from '../ui/Segment.vue'
import { useMessages } from '../../i18n'

// The room an answer is read or an example written in: a title row, the bar with the switch between
// the body and the headers, and what the switch shows. What stands in the title row and at the end
// of the bar is the caller's.
const { live = false, count = 0 } = defineProps<{ live?: boolean; count?: number }>()

const pane = defineModel<'body' | 'headers'>('pane', { required: true })

const { t } = useMessages()

const panes = computed(() => [
  { value: 'body', label: t('request.chips.body') },
  { value: 'headers', label: t('request.chips.headers'), count: count || undefined },
])
</script>

<template>
  <div class="view">
    <div class="title-row" :class="{ live }">
      <slot name="title" />
    </div>

    <div class="bar">
      <Segment
        class="flex-none"
        size="xs"
        :grow="false"
        :options="panes"
        :value="pane"
        @pick="pane = $event as 'body' | 'headers'"
      />
      <slot name="bar" />
    </div>

    <slot />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.view {
  @apply flex flex-col flex-1 min-h-0;
}

.title-row {
  @apply flex items-center flex-none;
  height: 3.25rem;
  gap: 0.625rem;
  padding: 0 0.75rem 0 0.875rem;
}

.title-row.live {
  padding-left: 1.375rem;
}

.bar {
  @apply flex items-center flex-none;
  height: 2.25rem;
  gap: 0.625rem;
  padding: 0 1.25rem 0.25rem;
}
</style>
