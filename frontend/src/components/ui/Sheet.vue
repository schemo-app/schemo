<script setup lang="ts">
import { computed } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import Icon from './Icon.vue'
import SheetRow from './SheetRow.vue'
import type { SheetLine } from '../../composables/useSheetLines'
import { t } from '../../i18n'

const {
  open = true,
  title,
  sub = '',
  cancel = '',
  action = '',
  actionDisabled = false,
  actionTone = 'accent',
  rows = [],
} = defineProps<{
  open?: boolean
  title: string
  sub?: string
  cancel?: string
  action?: string
  actionDisabled?: boolean
  actionTone?: 'accent' | 'danger'
  rows?: SheetLine[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'cancel'): void
  (e: 'action'): void
}>()

function onOpenChange(value: boolean) {
  if (!value) emit('close')
}


// A subtitle is the sheet's description, and a screen reader is handed it as one. A sheet without
// one has nothing to point at, and reka-ui asks for that to be said; otherwise the content is aimed
// at an id no element carries.
const describedBy = computed(() => (sub ? {} : { 'aria-describedby': undefined }))
</script>

<template>
  <DialogRoot :open="open" @update:open="onOpenChange">
    <DialogPortal>
      <DialogOverlay class="sheet-overlay" />
      <DialogContent class="sheet" v-bind="describedBy">
        <div class="sheet-head">
          <div class="sheet-titles">
            <DialogTitle class="sheet-title">{{ title }}</DialogTitle>
            <DialogDescription v-if="sub" class="sheet-sub">{{ sub }}</DialogDescription>
          </div>
          <button class="sheet-close" :title="t('common.close')" @click="emit('close')">
            <Icon name="X" :size="14" :stroke-width="2.2" />
          </button>
        </div>

        <div class="sheet-body">
          <div v-if="rows.length" class="sheet-rows">
            <SheetRow v-for="row in rows" :key="row.label" v-bind="row" />
          </div>
          <slot />
        </div>

        <div v-if="cancel || action" class="sheet-foot">
          <button v-if="cancel" class="sheet-cancel" @click="emit('cancel')">
            {{ cancel }}
          </button>
          <button
            v-if="action"
            class="sheet-action"
            :class="{ danger: actionTone === 'danger' }"
            :disabled="actionDisabled"
            @click="emit('action')"
          >
            {{ action }}
          </button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
@reference "../../style.css";

.sheet-overlay {
  @apply fixed inset-0 z-1500;
  background: rgba(0, 0, 0, 0.22);
}

.sheet {
  @apply fixed left-1/2 top-1/2 max-w-[92vw] w-140 z-1500 flex flex-col rounded-xl;
  transform: translate(-50%, -50%);
  max-height: calc(100vh - 6rem);
  padding: 1.125rem;
  gap: 1rem;
  background: var(--glass-sheet);
  backdrop-filter: var(--blur-sheet);
  box-shadow: var(--glass-sheet-shadow), 0 0 0 1px var(--glass-overlay-border);
}

.sheet-head {
  @apply flex-none flex items-start gap-3;
}

.sheet-titles {
  @apply flex flex-col flex-1 gap-1 min-w-0;
}

.sheet-title {
  @apply text-16 font-semibold;
}

.sheet-sub {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
}

.sheet-close {
  @apply flex-none inline-flex items-center justify-center w-7 h-7 rounded-[0.3rem] border-0
         bg-transparent text-text-tertiary cursor-pointer;
  --wails-draggable: no-drag;
}

.sheet-close:hover {
  @apply bg-bg-active text-text;
}

.sheet-body {
  @apply flex-1 min-h-0 overflow-y-auto;
}

.sheet-rows {
  @apply flex flex-col gap-0.5;
}

.sheet-foot {
  @apply flex-none flex justify-end gap-2.5 pt-4 border-t border-border;
}

.sheet-cancel {
  @apply h-8.5 px-3.5 rounded-lg border border-border-strong bg-bg-inset text-text
         text-13 font-medium cursor-pointer;
  font-family: inherit;
}

.sheet-cancel:hover {
  @apply bg-bg-hover;
}

.sheet-action {
  @apply h-8.5 px-4 rounded-lg border-0 bg-accent text-accent-text text-13 font-semibold
         cursor-pointer;
  font-family: inherit;
}

.sheet-action:hover:not(:disabled) {
  @apply brightness-110;
}

.sheet-action:disabled {
  @apply opacity-50 cursor-default;
}

.sheet-action.danger {
  background: var(--red-text);
  color: #fff;
}

:root.dark .sheet-action.danger {
  color: var(--bg-panel);
}

.sheet-action.danger:hover:not(:disabled) {
  background: color-mix(in srgb, var(--red-text) 85%, #000);
}
</style>
