<script setup lang="ts">
import Icon from './Icon.vue'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { useMessages } from '../../i18n'

const { text, label = '', mono = true } = defineProps<{ text: string; label?: string; mono?: boolean }>()

const { t } = useMessages()
const { copied, copy } = useCopyFeedback()
</script>

<template>
  <button type="button" class="copy-row" :title="t('common.copy')" @click="copy(text)">
    <span v-if="label" class="row-label">{{ label }}</span>
    <span class="row-text" :class="{ mono }"><slot>{{ text }}</slot></span>
    <Icon class="row-mark" :name="copied ? 'Check' : 'Copy'" :size="13" :stroke-width="1.8" />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.copy-row {
  @apply flex w-full items-baseline gap-2 text-left cursor-pointer border-0 bg-transparent;
  padding: 0.125rem 0.375rem;
  margin: 0 -0.375rem;
  width: calc(100% + 0.75rem);
  border-radius: 0.375rem;
  color: var(--text);
  --wails-draggable: no-drag;
}

.copy-row:hover {
  background: var(--bg-hover);
}

.row-label {
  @apply flex-none text-12 text-text-tertiary;
  min-width: 4.875rem;
}

.row-text {
  @apply flex-1 min-w-0 text-13 break-all;
  line-height: 1.5;
}

.row-text.mono {
  font-family: var(--mono);
}

.row-mark {
  @apply flex-none text-text-tertiary;
  opacity: 0;
  transition: opacity 0.12s ease;
}

.copy-row:hover .row-mark,
.copy-row:focus-visible .row-mark {
  opacity: 1;
}
</style>
