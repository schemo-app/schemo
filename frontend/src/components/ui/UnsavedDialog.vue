<script setup lang="ts">
import { nextTick, useTemplateRef, watch } from 'vue'
import Modal from './Modal.vue'
import DialogButton from './DialogButton.vue'
import { useShownWhileOpen } from '../../composables/useShownWhileOpen'
import { t } from '../../i18n'

const { open, title, hint, canSave = false, saveLabel = '' } = defineProps<{
  open: boolean
  title: string
  hint: string
  canSave?: boolean
  saveLabel?: string
}>()

const shown = useShownWhileOpen(() => open, () => ({ title, hint, saveLabel }))

const emit = defineEmits<{
  (e: 'discard'): void
  (e: 'cancel'): void
  (e: 'save'): void
}>()

const save = useTemplateRef<{ focus(): void }>('save')

watch(
  () => open,
  async (value) => {
    if (!value) return
    await nextTick()
    save.value?.focus()
  }
)
</script>

<template>
  <Modal :open :title="shown.title" variant="ask" @close="emit('cancel')">
    <span class="hint">{{ shown.hint }}</span>
    <template #foot>
      <button type="button" class="discard" @click="emit('discard')">
        {{ t('common.discard') }}
      </button>
      <span class="flex-1" />
      <DialogButton @click="emit('cancel')">{{ t('common.cancel') }}</DialogButton>
      <DialogButton v-if="canSave" ref="save" primary @click="emit('save')">
        {{ shown.saveLabel || t('common.save') }}
      </DialogButton>
    </template>
  </Modal>
</template>

<style scoped>
@reference "../../style.css";

.hint {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}

.discard {
  @apply border-0 cursor-pointer;
  height: 2rem;
  padding: 0 0.75rem;
  margin-left: -0.75rem;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
}

.discard:hover {
  background: var(--chip);
  color: var(--text);
}
</style>
