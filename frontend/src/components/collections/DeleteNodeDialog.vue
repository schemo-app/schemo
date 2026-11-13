<script setup lang="ts">
import { computed } from 'vue'
import Modal from '../ui/Modal.vue'
import DialogButton from '../ui/DialogButton.vue'
import { useShownWhileOpen } from '../../composables/useShownWhileOpen'
import { t } from '../../i18n'
import type { TreeRow } from '../../lib/treeRows'

const props = defineProps<{ row: TreeRow | null; open: boolean }>()

const emit = defineEmits<{ (e: 'cancel'): void; (e: 'confirm'): void }>()

const shown = useShownWhileOpen(() => props.open, () => props.row)

const isCollection = computed(() => shown.value?.kind === 'collection')
const count = computed(() => shown.value?.count ?? 0)
const name = computed(() => shown.value?.name || t('collections.unnamedRequest'))

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
  <Modal :open :title variant="ask" @close="emit('cancel')">
    <div class="body">{{ body }}</div>
    <template #foot>
      <DialogButton @click="emit('cancel')">{{ t('common.cancel') }}</DialogButton>
      <DialogButton danger @click="emit('confirm')">{{ t('common.delete') }}</DialogButton>
    </template>
  </Modal>
</template>

<style scoped>
@reference "../../style.css";

.body {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}
</style>
