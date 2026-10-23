<script setup lang="ts">
import { computed } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useCookiesStore } from '../../stores/cookies'
import { t } from '../../i18n'

// What is about to go, before it goes. A domain takes its cookies and the block on its writes with
// it, and leaves every other cookie domain where it was — which the list says as well.
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const cookies = useCookiesStore()
const { gone, kept } = useSheetLines()

const rows = computed(() => [
  gone(t('counts.cookies', cookies.rows.length), t('cookies.deleteSheetCookiesNote')),
  ...(cookies.blocked
    ? [gone(t('cookies.deleteSheetBlock'), t('cookies.deleteSheetBlockNote'))]
    : []),
  kept(t('cookies.deleteSheetOthers'), t('cookies.deleteSheetOthersNote')),
])
</script>

<template>
  <ConfirmSheet
    :title="t('cookies.deleteDomain')"
    :sub="t('cookies.deleteSheetSub')"
    :rows
    :action="t('common.delete')"
    @close="emit('close')"
    @confirm="emit('confirm')"
  />
</template>
