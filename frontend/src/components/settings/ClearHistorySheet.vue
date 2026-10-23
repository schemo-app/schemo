<script setup lang="ts">
import { computed } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { History } from '../../../bindings/schemo/internal/request'
import { formatBytes, t } from '../../i18n'

// What is about to be thrown away, in the two numbers that say how much of it there is.
const { count, bytes } = defineProps<{ count: number; bytes: number }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'cleared'): void }>()

const { gone } = useSheetLines()

const rows = computed(() => [
  gone(
    t('counts.requests', count),
    t('settings.clearSize', { size: formatBytes(bytes) }),
    t('settings.clearTag')
  ),
])

async function clear() {
  try {
    await History.ClearAll()
  } finally {
    // Either way the panel reads its numbers again: a call that failed leaves the history as it
    // was, and one that worked leaves none of it.
    emit('cleared')
  }
}
</script>

<template>
  <ConfirmSheet
    :title="t('settings.clearHistory')"
    :sub="t('settings.clearHistorySub')"
    :rows
    :action="t('settings.clearAction')"
    @close="emit('close')"
    @confirm="clear"
  />
</template>
