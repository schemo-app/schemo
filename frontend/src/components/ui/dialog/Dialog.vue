<script setup lang="ts">
import { DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle, type DialogRootProps } from 'reka-ui'

defineOptions({ inheritAttrs: false })

const { title, placement, tone = 'glass' } = defineProps<
  Omit<DialogRootProps, 'open'> & {
    title?: string
    placement?: 'center' | 'top'
    tone?: 'glass' | 'system' | 'cover'
  }
>()

const open = defineModel<boolean>('open')
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay :class="['dialog-overlay', tone]" />
      <DialogContent
        :aria-describedby="undefined"
        v-bind="$attrs"
        :class="['dialog', tone, { 'dialog-top': placement === 'top' }]"
      >
        <DialogTitle v-if="title" class="dialog-title">{{ title }}</DialogTitle>
        <slot />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
@reference "../../../style.css";

.dialog-overlay {
  @apply fixed inset-0 z-1500;
  background: rgba(0, 0, 0, 0.22);
  animation: dialog-overlay-in 0.15s ease;
}

.dialog-overlay.cover {
  background: rgba(0, 0, 0, 0.28);
}

.dialog-overlay[data-state='closed'] {
  animation: dialog-overlay-out 0.12s ease-in forwards;
}

.dialog {
  @apply fixed left-1/2 top-1/2 max-w-[90vw] z-1500;
  transform: translate(-50%, -50%);
  background: var(--glass-overlay);
  backdrop-filter: var(--blur-overlay);
  border: 1px solid var(--glass-overlay-border);
  border-radius: 0.75rem;
  box-shadow: var(--glass-overlay-shadow);
  animation: dialog-in 0.16s cubic-bezier(0.16, 1, 0.3, 1);
}

.dialog.system {
  background: var(--bg-panel);
  backdrop-filter: none;
  border: 1px solid var(--border);
  box-shadow: var(--popshadow);
}

.dialog.cover {
  background: var(--bg);
  backdrop-filter: none;
  border: 0;
  border-radius: 0.875rem;
  box-shadow: var(--popshadow);
}

.dialog[data-state='closed'] {
  animation: dialog-out 0.12s ease-in forwards;
}

.dialog-top {
  top: 5.75rem;
  transform: translateX(-50%);
  animation: palette-in 0.16s cubic-bezier(0.16, 1, 0.3, 1);
}

.dialog-top[data-state='closed'] {
  animation: palette-out 0.12s ease-in forwards;
}

@keyframes palette-in {
  from {
    opacity: 0;
    transform: translateX(-50%) translateY(-0.5rem) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateX(-50%) translateY(0) scale(1);
  }
}

@keyframes palette-out {
  from {
    opacity: 1;
    transform: translateX(-50%) translateY(0) scale(1);
  }
  to {
    opacity: 0;
    transform: translateX(-50%) translateY(-0.375rem) scale(0.99);
  }
}

@keyframes dialog-overlay-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes dialog-overlay-out {
  from {
    opacity: 1;
  }
  to {
    opacity: 0;
  }
}

@keyframes dialog-in {
  from {
    opacity: 0;
    transform: translate(-50%, -50%) scale(0.96);
  }
  to {
    opacity: 1;
    transform: translate(-50%, -50%) scale(1);
  }
}

@keyframes dialog-out {
  from {
    opacity: 1;
    transform: translate(-50%, -50%) scale(1);
  }
  to {
    opacity: 0;
    transform: translate(-50%, -50%) scale(0.97);
  }
}

.dialog-title {
  @apply text-14 font-semibold;
}
</style>
