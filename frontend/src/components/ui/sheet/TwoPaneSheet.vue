<script setup lang="ts">
import { DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import Icon from '../Icon.vue'
import { useMessages } from '../../../i18n'

const { width = 940, title, cancelTop } = defineProps<{
  title: string
  width?: number
  cancelTop?: () => boolean
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

function onOpenChange(value: boolean) {
  if (!value) emit('close')
}

function onEscape(event: KeyboardEvent) {
  if (cancelTop?.()) event.preventDefault()
}

const { t } = useMessages()
</script>

<template>
  <DialogRoot :open="true" @update:open="onOpenChange">
    <DialogPortal>
      <DialogOverlay class="sheet-overlay" />
      <DialogContent
        class="sheet"
        :style="{ maxWidth: `${width}px` }"
        :aria-describedby="undefined"
        @escape-key-down="onEscape"
        @pointer-down-outside.prevent
      >
        <div class="sheet-head">
          <DialogTitle class="sheet-title">{{ title }}</DialogTitle>
          <button
            type="button"
            class="sheet-close"
            :title="`${t('common.close')} · Esc`"
            @click="emit('close')"
          >
            <Icon name="X" :size="14" :stroke-width="2.5" />
          </button>
        </div>

        <div class="sheet-body">
          <slot />
        </div>

        <slot name="over" />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
@reference "../../../style.css";

.sheet-overlay {
  @apply fixed inset-0 z-1500;
  background: rgba(0, 0, 0, 0.28);
}

.sheet {
  @apply fixed left-1/2 top-1/2 z-1500 flex flex-col overflow-hidden;
  transform: translate(-50%, -50%);
  width: calc(100vw - 3.5rem);
  height: min(39rem, calc(100vh - 3.5rem));
  border-radius: 0.875rem;
  background: var(--bg);
  box-shadow: var(--popshadow);
}

.sheet-head {
  @apply flex-none flex items-center border-b;
  gap: 0.625rem;
  height: 3.5rem;
  padding: 0 0.75rem 0 1.25rem;
  border-color: var(--border);
}

.sheet-title {
  @apply flex-1 min-w-0 font-semibold overflow-hidden text-ellipsis whitespace-nowrap;
  font-size: 0.9375rem;
}

.sheet-close {
  @apply flex-none w-7 h-7 border-0 cursor-pointer
         flex items-center justify-center;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-tertiary);
}

.sheet-close:hover {
  @apply bg-bg-hover text-text;
}

.sheet-body {
  @apply flex flex-1 min-h-0;
}
</style>
