<script setup lang="ts">
import { DialogTitle } from 'reka-ui'
import Icon from './Icon.vue'
import Dialog from './Dialog.vue'
import { t } from '../../i18n'

const { open, title, variant = 'panel' } = defineProps<{
  open: boolean
  title: string
  variant?: 'panel' | 'ask'
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

</script>

<template>
  <Dialog
    :open
    tone="cover"
    :class="['max-h-[calc(100vh-3rem)] flex flex-col overflow-hidden', variant === 'ask' ? 'w-130' : 'w-115']"
    @update:open="(value) => !value && emit('close')"
  >
    <div class="head" :class="variant">
      <slot name="lead" />
      <DialogTitle class="title" :class="variant">{{ title }}</DialogTitle>
      <button
        v-if="variant === 'panel'"
        type="button"
        class="close"
        :title="t('common.close')"
        @click="emit('close')"
      >
        <Icon name="X" :size="12" :stroke-width="2.4" />
      </button>
    </div>

    <div class="body" :class="variant">
      <slot />
    </div>

    <div v-if="$slots.foot" class="foot" :class="variant">
      <slot name="foot" />
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

.head {
  @apply flex items-center flex-none;
  gap: 0.3125rem;
  padding: 0.875rem 0.75rem 0.375rem 1.25rem;
}

.head.ask {
  padding: 1.5rem 1.5rem 0;
}

.title {
  @apply flex-1 min-w-0 font-semibold;
  font-size: var(--text-14);
}

.title.panel {
  @apply truncate;
}

.title.ask {
  line-height: 1.35;
  text-wrap: pretty;
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

.body.ask {
  gap: 0.5rem;
  padding: 0.5rem 1.5rem 0.375rem;
}

.foot {
  @apply flex justify-end flex-none;
  gap: 0.5rem;
  padding: 0 1.25rem 1.125rem;
}

.foot.ask {
  padding: 1.125rem 1.5rem 1.375rem;
}
</style>
