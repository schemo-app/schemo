<script setup lang="ts">
import { DialogTitle } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import { Dialog } from '../ui/dialog'
import { useMessages } from '../../i18n'

const { open, title } = defineProps<{ open: boolean; title: string }>()

const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useMessages()
</script>

<template>
  <Dialog
    :open
    tone="cover"
    class="w-115 max-h-[calc(100vh-3rem)] flex flex-col overflow-hidden"
    @update:open="(value) => !value && emit('close')"
  >
    <div class="head">
      <slot name="lead" />
      <DialogTitle class="title">{{ title }}</DialogTitle>
      <button type="button" class="close" :title="t('transfer.close')" @click="emit('close')">
        <Icon name="X" :size="12" :stroke-width="2.4" />
      </button>
    </div>

    <div class="body">
      <slot />
    </div>

    <div v-if="$slots.foot" class="foot">
      <slot name="foot" />
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

.head {
  @apply flex items-center flex-none;
  gap: 0.375rem;
  padding: 0.875rem 0.75rem 0.375rem 1.25rem;
}

.title {
  @apply flex-1 min-w-0 font-semibold truncate;
  font-size: var(--text-15);
}

.close {
  @apply flex items-center justify-center flex-none border-0 cursor-pointer;
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-tertiary);
}

.close:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.body {
  @apply flex flex-col flex-1 min-h-0 overflow-auto;
  gap: 1rem;
  padding: 0.375rem 1.25rem 1.25rem;
}

.foot {
  @apply flex justify-end flex-none;
  gap: 0.5rem;
  padding: 0 1.25rem 1.125rem;
}
</style>
