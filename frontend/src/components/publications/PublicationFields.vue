<script setup lang="ts">
import { computed } from 'vue'
import type { Choice } from '../../../bindings/schemo/internal/usecase/publication'
import type { Visibility } from '../../stores/publications'
import { visibilityOptions } from '../../lib/publications'
import { useMessages } from '../../i18n'
import Icon from '../ui/Icon.vue'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '../ui/dropdown-menu'

const { visibility, environment, environmentLabel, environments, disabled = false, accessOnly = false } = defineProps<{
  visibility: Visibility
  environment: string
  environmentLabel: string
  environments: Choice[]
  disabled?: boolean
  accessOnly?: boolean
}>()
const emit = defineEmits<{
  visibility: [value: Visibility]
  environment: [value: string]
}>()
const { t } = useMessages()
const fields = computed(() => [
  {
    key: 'visibility', label: t('publications.visibility'), value: visibility,
    labelValue: visibilityOptions().find(option => option.value === visibility)?.label,
    options: visibilityOptions(), disabled,
  },
  {
    key: 'environment', label: t('publications.environment'), value: environment,
    labelValue: environmentLabel,
    options: environments.map(choice => ({ value: choice.id, label: choice.name, hint: choice.baseUrl })),
    disabled: disabled || !environments.length,
  },
].filter(field => !accessOnly || field.key === 'visibility'))

function pick(key: string, value: string) {
  if (key === 'visibility') emit('visibility', value as Visibility)
  else emit('environment', value)
}
</script>

<template>
  <div v-for="field in fields" :key="field.key" class="publication-field" :class="{ quick: accessOnly }">
    <span v-if="accessOnly" class="quick-icon"><Icon name="Globe" :size="15" /></span>
    <span class="field-label">{{ field.label }}</span>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" class="field-value" :disabled="field.disabled">
          <span class="field-display">{{ field.labelValue }}</span>
          <Icon name="ChevronDown" :size="9" :stroke-width="3" class="text-text-tertiary" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent class="publication-field-menu" align="end" :side-offset="5">
        <DropdownMenuItem
          v-for="option in field.options"
          :key="option.value"
          class="field-option"
          @select="pick(field.key, option.value)"
        >
          <span class="option-mark">
            <Icon v-if="field.value === option.value" name="Check" :size="12" :stroke-width="2.8" />
          </span>
          <span class="option-text">
            <span>{{ option.label }}</span>
            <span class="option-hint">{{ option.hint }}</span>
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.publication-field {
  @apply flex items-center;
  height: 2.125rem;
  gap: 0.625rem;
}

.field-label {
  @apply flex-1 min-w-0 text-text-secondary;
}

.publication-field.quick {
  padding: 0 0.25rem 0 0.625rem;
}

.quick-icon {
  @apply flex justify-center flex-none text-text-secondary;
  width: 1.125rem;
}

.field-value {
  @apply flex items-center flex-none min-w-0 border-0 bg-transparent text-text cursor-pointer whitespace-nowrap;
  max-width: 75%;
  height: 1.75rem;
  padding: 0 0.5rem 0 0.625rem;
  gap: 0.4375rem;
  border-radius: 0.4375rem;
  font: inherit;
}

.field-value:hover:not(:disabled),
.field-value[data-state='open'] {
  background: var(--bg-hover);
}

.field-display {
  @apply min-w-0 truncate;
}

.field-value:disabled {
  @apply cursor-default;
}

:global(.menu.publication-field-menu) {
  width: 250px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
  font-size: var(--text-13);
  line-height: normal;
  padding: 0.3125rem;
  border-radius: 0.625rem;
}

.field-option {
  @apply flex items-center;
  height: auto;
  gap: 0.5rem;
  padding: 0.375rem 0.5rem;
}

.option-mark {
  @apply flex flex-none text-accent;
  width: 0.875rem;
}

.option-text {
  @apply flex flex-col flex-1 min-w-0;
  gap: 1px;
}

.option-hint {
  @apply truncate text-text-tertiary text-11-5;
}
</style>
