<script setup lang="ts">
import { computed } from 'vue'
import Dialog from '../ui/Dialog.vue'
import Button from '../ui/Button.vue'
import { t } from '../../i18n'
import type { TreeRow } from '../../lib/treeRows'

const props = defineProps<{ row: TreeRow | null; open: boolean }>()

const emit = defineEmits<{ (e: 'cancel'): void; (e: 'confirm'): void }>()

const isCollection = computed(() => props.row?.kind === 'collection')
const count = computed(() => props.row?.count ?? 0)
const name = computed(() => props.row?.name || t('collections.unnamedRequest'))

const title = computed(() => {
  if (!isCollection.value) return t('collections.deleteRequest')
  return count.value > 0 ? t('collections.deleteWithContents') : t('collections.deleteCollection')
})

const body = computed(() => {
  if (!isCollection.value || count.value === 0) {
    return t('collections.deleteNodeBodyPlain', { name: name.value })
  }
  return t('collections.deleteNodeBody', {
    count: count.value,
    requests: t('counts.requests', count.value),
  })
})
</script>

<template>
  <Dialog
    :open
    :title
    class="w-110 p-4.5"
    @escape-key-down.prevent
    @update:open="emit('cancel')"
  >
    <div class="body">
      {{ body }}
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
