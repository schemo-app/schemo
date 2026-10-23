<script setup lang="ts">
import { computed } from 'vue'
import type { PublicationOptions } from '../../../bindings/schemo/internal/domain'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'

defineProps<{ options: PublicationOptions; disabled?: boolean }>()
defineEmits<{ choose: [key: keyof PublicationOptions] }>()
const choices = computed(() => [
  { key: 'docs' as const, label: t('publications.showDocs') },
  { key: 'examples' as const, label: t('publications.showExamples') },
])
</script>

<template>
  <div class="publication-options">
    <span class="text-text-secondary">{{ t('publications.showOnPage') }}</span>
    <div class="option-chips">
      <button v-for="choice in choices" :key="choice.key" type="button" class="option-chip"
        :class="{ selected: options[choice.key] }" :aria-pressed="options[choice.key]"
        :disabled="disabled" @click="$emit('choose', choice.key)">
        <Icon v-if="options[choice.key]" name="Check" :size="11" :stroke-width="2.8" />
        {{ choice.label }}
      </button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.publication-options {
  @apply flex items-center flex-wrap;
  min-height: 2.5rem;
  gap: 0.625rem;
  padding: 0.3rem 0;
  border-top: 1px solid var(--line2);
}

.publication-options > span {
  @apply flex-1 whitespace-nowrap;
}

.option-chips {
  @apply flex flex-wrap justify-end;
  gap: 0.3rem;
}

.option-chip {
  @apply flex items-center border-0 bg-transparent text-text-secondary cursor-pointer;
  height: 1.75rem;
  padding: 0 0.6rem;
  border-radius: 0.4375rem;
  gap: 0.3125rem;
  box-shadow: inset 0 0 0 1px var(--border);
  font: inherit;
  font-size: var(--text-12-5);
}

.option-chip.selected {
  background: var(--accent-soft);
  color: var(--accent);
  box-shadow: none;
}

.option-chip:disabled {
  @apply cursor-default opacity-45;
}
</style>
