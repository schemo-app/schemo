<script setup lang="ts">
import { computed } from 'vue'
import { Sheet } from '../ui/sheet'
import { useCookiesStore } from '../../stores/cookies'
import { useMessages } from '../../i18n'

// What is about to go, before it goes. A domain takes its cookies and the block on its writes with
// it, and leaves every other domain of the jar where it was — which the list says as well.
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const jar = useCookiesStore()
const { t } = useMessages()

const rows = computed(() => [
  {
    label: t('counts.cookies', jar.rows.length),
    note: t('cookies.deleteSheetCookiesNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  ...(jar.blocked
    ? [
        {
          label: t('cookies.deleteSheetBlock'),
          note: t('cookies.deleteSheetBlockNote'),
          tag: t('common.deleted'),
          tone: 'drop' as const,
        },
      ]
    : []),
  {
    label: t('cookies.deleteSheetOthers'),
    note: t('cookies.deleteSheetOthersNote'),
    tag: t('common.kept'),
    tone: 'keep' as const,
  },
])
</script>

<template>
  <Sheet
    :title="t('cookies.deleteDomain')"
    :sub="t('cookies.deleteSheetSub')"
    :rows
    :cancel="t('common.cancel')"
    :action="t('common.delete')"
    @close="emit('close')"
    @cancel="emit('close')"
    @action="emit('confirm')"
  />
</template>
