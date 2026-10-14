<script setup lang="ts">
import { Dialog } from '../ui/dialog'
import { Button } from '../ui/button'
import { useMessages } from '../../i18n'

const { t } = useMessages()

defineProps<{ count: number; open: boolean }>()

const emit = defineEmits<{ (e: 'cancel'): void; (e: 'confirm'): void }>()
</script>

<template>
  <Dialog
    :open
    :title="t('collections.deleteWithContents')"
    class="w-110 p-4.5"
    @escape-key-down.prevent
    @update:open="emit('cancel')"
  >
    <div class="body">
      {{ t('collections.deleteNodeBody', { count: t('counts.requests', count) }) }}
    </div>
    <div class="actions">
      <Button @click="emit('cancel')">{{ t('common.cancel') }}</Button>
      <Button variant="primary" @click="emit('confirm')">{{ t('common.delete') }}</Button>
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

.body {
  @apply mt-3 mb-4 text-13 text-text-secondary;
}

.actions {
  @apply flex justify-end gap-2;
}
</style>
