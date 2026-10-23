<script setup lang="ts">
import Sheet from './Sheet.vue'
import type { SheetLine } from '../../composables/useSheetLines'
import { t } from '../../i18n'

// A sheet that asks before it acts: what is about to happen, the way back, and the one way forward.
// Cancelling and closing are the same answer.
defineProps<{
  title: string
  sub?: string
  rows?: SheetLine[]
  action: string
  actionTone?: 'accent' | 'danger'
  actionDisabled?: boolean
}>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

</script>

<template>
  <Sheet
    :title
    :sub
    :rows
    :cancel="t('common.cancel')"
    :action
    :action-tone
    :action-disabled
    @close="emit('close')"
    @cancel="emit('close')"
    @action="emit('confirm')"
  >
    <slot />
  </Sheet>
</template>
