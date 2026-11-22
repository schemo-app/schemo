<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import Checkbox from '../ui/Checkbox.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import type { useVariableCells } from '../../composables/useVariableCells'
import { VariableKind, type Variable } from '../../../bindings/schemo/internal/domain'
import { SECRET_MASK } from '../../lib/layout'
import { t } from '../../i18n'
import { useSheetNotice } from '../../composables/useSheetNotice'

const { variable, scope, editor, locked = false, allowSecret = true } = defineProps<{
  variable: Variable
  scope: string | null
  editor: ReturnType<typeof useVariableCells>
  locked?: boolean
  allowSecret?: boolean
}>()

const emit = defineEmits<{
  patch: [patch: Partial<Variable>]
  remove: []
}>()

const environments = useEnvironmentsStore()
const { notice, withNotice } = useSheetNotice()
const lifetime = new AbortController()
const secret = computed(() => variable.kind === VariableKind.VariableSecret)
const revealed = computed(() => environments.isRevealed(variable.id))
const value = computed(() => {
  if (!secret.value || revealed.value) return environments.effectiveValue(variable)
  return variable.hasValue ? SECRET_MASK : ''
})

async function toggleReveal() {
  if (revealed.value) environments.hide(variable.id)
  else await withNotice(() => environments.reveal(variable.id, lifetime.signal))
}

onBeforeUnmount(() => {
  lifetime.abort()
  environments.hide(variable.id)
})
</script>

<template>
  <div class="variable-row" :class="{ off: !variable.enabled }">
    <Checkbox
      size="sm"
      :model-value="variable.enabled"
      :disabled="locked"
      :aria-label="t('environments.nameColumn') + ': ' + variable.name"
      @update:model-value="emit('patch', { enabled: !variable.enabled })"
    />
    <div
      v-for="field in (['name', 'value'] as const)"
      :key="field"
      class="cell"
      :tabindex="locked ? -1 : 0"
      @focus.self="editor.start(variable, field, scope)"
      @click="editor.start(variable, field, scope)"
    >
      <input
        v-if="editor.isEditingCell(variable, field, scope)"
        :ref="editor.setCellInput"
        v-model="editor.draft.value"
        class="cell-input"
        :type="field === 'value' && secret && !revealed ? 'password' : 'text'"
        :aria-label="t(field === 'name' ? 'environments.nameColumn' : 'environments.value')"
        :aria-invalid="Boolean(notice)"
        spellcheck="false"
        @keydown="editor.onCellKeydown($event, variable, field, scope)"
        @blur="editor.commitFrom(variable, field)"
      />
      <span v-else class="cell-text" :class="{ masked: field === 'value' && secret && !revealed }">
        {{ field === 'name' ? variable.name : value }}
      </span>
    </div>
    <slot />
    <span class="actions">
      <IconButton v-if="secret" size="md" class="hover-only" :hint="t(revealed ? 'environments.hideValue' : 'environments.showValue')" :aria-label="t(revealed ? 'environments.hideValue' : 'environments.showValue')" @click="toggleReveal">
        <Icon :name="revealed ? 'EyeOff' : 'Eye'" :size="14" :stroke-width="1.8" />
      </IconButton>
      <IconButton
        v-if="allowSecret"
        size="md"
        :class="{ 'hover-only': !secret, 'lock-on': secret }"
        :disabled="locked"
        :hint="t(secret ? 'environments.showAsText' : 'environments.makeSecret')"
        :aria-label="t(secret ? 'environments.showAsText' : 'environments.makeSecret')"
        @click="emit('patch', { kind: secret ? VariableKind.VariableText : VariableKind.VariableSecret })"
      >
        <Icon name="Lock" :size="14" :stroke-width="1.8" />
      </IconButton>
      <IconButton v-if="!locked" size="md" class="hover-only" :hint="t('common.delete')" :aria-label="t('common.delete')" @click="emit('remove')">
        <Icon name="X" :size="14" :stroke-width="1.8" />
      </IconButton>
    </span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.variable-row {
  display: grid;
  grid-template-columns: var(--row-columns, 28px minmax(0, 1fr) minmax(0, 1.4fr) 80px);
  align-items: center;
  height: 32px;
  margin: 0 -8px;
  padding: 0 8px;
  border-radius: 6px;
}

.variable-row:hover,
.variable-row:focus-within {
  background: var(--bg-hover);
}

.variable-row.off {
  opacity: 0.45;
}

.cell {
  display: flex;
  align-items: center;
  min-width: 0;
  height: 100%;
  padding-right: 12px;
  cursor: text;
}

.cell-text,
.cell-input {
  min-width: 0;
  font-family: var(--mono);
  font-size: var(--text-13);
  color: var(--text);
}

.cell-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cell-input {
  width: 100%;
  height: 28px;
  padding: 0;
  border: 0;
  outline: none;
  background: transparent;
  caret-color: var(--accent);
}

.cell-text.masked {
  color: var(--text-tertiary);
}

.cell-input[aria-invalid='true'] {
  box-shadow: inset 0 -1px 0 var(--red);
}

.actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
  color: var(--text-tertiary);
}

.actions :deep(.lock-on) {
  color: var(--text-secondary);
}

.actions :deep(.hover-only) {
  opacity: 0;
}

.variable-row:hover :deep(.hover-only),
.variable-row:focus-within :deep(.hover-only) {
  opacity: 1;
}
</style>
