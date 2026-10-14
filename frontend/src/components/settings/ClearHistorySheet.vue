<script setup lang="ts">
import { computed } from 'vue'
import { Sheet } from '../ui/sheet'
import { RecordsService } from '../../../bindings/schemo/internal/transport/wails'
import { formatBytes, useMessages } from '../../i18n'

// What is about to be thrown away, in the two numbers that say how much of it there is.
const { count, bytes } = defineProps<{ count: number; bytes: number }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'cleared'): void }>()

const { t } = useMessages()

const rows = computed(() => [
  {
    label: t('counts.requests', count),
    note: t('settings.clearSize', { size: formatBytes(bytes) }),
    tag: t('settings.clearTag'),
    tone: 'drop' as const,
  },
])

async function clear() {
  try {
    await RecordsService.ClearAll()
  } finally {
    // Either way the panel reads its numbers again: a call that failed leaves the history as it
    // was, and one that worked leaves none of it.
    emit('cleared')
  }
}
</script>

<template>
  <Sheet
    :title="t('settings.clearHistory')"
    :sub="t('settings.clearHistorySub')"
    :rows
    :cancel="t('common.cancel')"
    :action="t('settings.clearAction')"
    @close="emit('close')"
    @cancel="emit('close')"
    @action="clear"
  />
</template>
