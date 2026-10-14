<script setup lang="ts">
import { nextTick, useTemplateRef, watch } from 'vue'
import Dialog from './dialog/Dialog.vue'
import { DialogButton } from './button'
import { useMessages } from '../../i18n'

// The question asked wherever unsaved work is about to be replaced: discard it, or stay. A place
// whose work can be kept where it stands asks a third answer — saving — and one that cannot says so
// in its hint rather than offering a door that would have to open somewhere else.
const { open, title, hint, canSave = false } = defineProps<{
  open: boolean
  title: string
  hint: string
  canSave?: boolean
}>()

const emit = defineEmits<{
  (e: 'discard'): void
  (e: 'cancel'): void
  (e: 'save'): void
}>()

const { t } = useMessages()

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
  <Dialog :open tone="cover" class="w-120" @update:open="(value) => !value && emit('cancel')">
    <div class="head">
      <span class="title">{{ title }}</span>
      <span class="hint">{{ hint }}</span>
    </div>
    <div class="foot">
      <button type="button" class="discard" @click="emit('discard')">
        {{ t('common.discard') }}
      </button>
      <span class="flex-1" />
      <DialogButton @click="emit('cancel')">{{ t('common.cancel') }}</DialogButton>
      <DialogButton v-if="canSave" ref="save" primary @click="emit('save')">
        {{ t('common.save') }}
      </DialogButton>
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

.head {
  @apply flex flex-col;
  gap: 0.5rem;
  padding: 1.375rem 1.25rem;
}

.title {
  @apply text-15 font-semibold;
  line-height: 1.35;
  text-wrap: pretty;
}

.hint {
  @apply text-text-tertiary;
  line-height: 1.5;
  text-wrap: pretty;
}

.foot {
  @apply flex items-center flex-none;
  gap: 0.5rem;
  padding: 0.875rem 1.25rem;
  border-top: 1px solid var(--border);
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
