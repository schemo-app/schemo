<script setup lang="ts">
import { computed } from 'vue'
import type { Choice } from '../../../bindings/schemo/internal/cloud'
import type { Visibility } from '../../stores/publications'
import { visibilityOptions } from '../../lib/publicationText'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger, DropdownMenuCheckboxItem } from 'reka-ui'

const { visibility, environmentIds, environments, disabled = false, accessOnly = false } = defineProps<{
  visibility: Visibility
  environmentIds: string[] | null
  environments: Choice[]
  disabled?: boolean
  accessOnly?: boolean
}>()
const emit = defineEmits<{
  visibility: [value: Visibility]
  environment: [value: string[]]
}>()
const accessChoices = computed(visibilityOptions)
const visibilityLabel = computed(() => accessChoices.value.find(option => option.value === visibility)?.label)
const environmentLabel = computed(() => (environmentIds ?? [])
  .map(id => environments.find(choice => choice.id === id)?.name).filter(Boolean)
  .join(', ') || t('publications.envNone'))

function toggleEnvironment(id: string) {
  const selected = environmentIds ?? []
  if (selected.includes(id)) {
    if (selected.length > 1) emit('environment', selected.filter(value => value !== id))
  } else {
    emit('environment', [...selected, id])
  }
}
</script>

<template>
  <div class="publication-field" :class="{ quick: accessOnly }">
    <span v-if="accessOnly" class="quick-icon"><Icon name="Globe" :size="15" /></span>
    <span class="field-label">{{ t('publications.visibility') }}</span>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" class="field-value" :disabled="disabled">
          <span class="field-display">{{ visibilityLabel }}</span>
          <Icon name="ChevronDown" :size="9" :stroke-width="3" class="text-text-tertiary" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent class="publication-field-menu" align="end" :side-offset="5">
        <DropdownMenuItem
          v-for="option in accessChoices"
          :key="option.value"
          class="field-option"
          @select="emit('visibility', option.value as Visibility)"
        >
          <span class="option-mark">
            <Icon v-if="visibility === option.value" name="Check" :size="12" :stroke-width="2.8" />
          </span>
          <span class="option-text">
            <span>{{ option.label }}</span>
            <span class="option-hint">{{ option.hint }}</span>
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
  <div v-if="!accessOnly" class="publication-field">
    <span class="field-label">{{ t('publications.environment') }}</span>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" class="field-value" :disabled="disabled || !environments.length">
          <span class="field-display">{{ environmentLabel }}</span>
          <Icon name="ChevronDown" :size="9" :stroke-width="3" class="text-text-tertiary" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent class="publication-field-menu" align="end" :side-offset="5">
        <DropdownMenuCheckboxItem v-for="choice in environments" :key="choice.id"
          class="menu-item field-option" :model-value="environmentIds?.includes(choice.id) ?? false"
          :disabled="disabled || (environmentIds?.length === 1 && environmentIds.includes(choice.id))"
          @select.prevent="toggleEnvironment(choice.id)">
          <span class="option-mark">
            <Icon v-if="environmentIds?.includes(choice.id)" name="Check" :size="12" :stroke-width="2.8" />
          </span>
          <span class="option-text">
            <span>{{ choice.name }}</span>
            <span class="option-hint">{{ choice.baseUrl }}</span>
          </span>
        </DropdownMenuCheckboxItem>
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
  padding: 0.3125rem 0.5rem;
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
  @apply truncate text-text-tertiary text-12;
}
</style>
