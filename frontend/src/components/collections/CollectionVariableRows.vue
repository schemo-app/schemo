<script setup lang="ts">
import { ref, watch } from 'vue'
import Checkbox from '../ui/Checkbox.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import TokenField from '../ui/TokenField.vue'
import { useCollectionsStore } from '../../stores/collections'
import { useWorkspacesStore } from '../../stores/workspaces'
import { t } from '../../i18n'
import { VariableKind, type Collection, type Variable } from '../../../bindings/schemo/internal/domain'

const { level, embedded = false } = defineProps<{ level: Collection; embedded?: boolean }>()
const collections = useCollectionsStore()
const workspaces = useWorkspacesStore()

type EditorRow = { key: number; variable: Variable }

const rows = ref<EditorRow[]>([])
let nextKey = 1

function fromStore(): EditorRow[] {
  return (level.variables ?? []).map((variable) => ({
    key: nextKey++,
    variable: { ...variable },
  }))
}

watch(
  () => level.variables,
  () => {
    rows.value = fromStore()
  },
  { immediate: true, deep: true }
)

async function commit() {
  if (!workspaces.canWrite) return
  await collections.saveVariables(
    level.id,
    rows.value.map((row) => row.variable)
  )
}

function patch(row: EditorRow, change: Partial<Variable>) {
  rows.value = rows.value.map((it) =>
    it.key === row.key ? { ...it, variable: { ...it.variable, ...change } } : it
  )
}

function toggle(row: EditorRow) {
  patch(row, { enabled: !row.variable.enabled })
  void commit()
}

function remove(row: EditorRow) {
  rows.value = rows.value.filter((it) => it.key !== row.key)
  void commit()
}

function addVariable() {
  rows.value = [
    ...rows.value,
    {
      key: nextKey++,
      variable: {
        id: '',
        name: '',
        value: '',
        kind: VariableKind.VariableText,
        enabled: true,
        position: rows.value.length + 1,
        hasValue: false,
      },
    },
  ]
}
</script>

<template>
  <div>
    <div v-if="!embedded && rows.length" class="head">
      <span></span>
      <span>{{ t('environments.variable') }}</span>
      <span>{{ t('environments.value') }}</span>
      <span></span>
    </div>

    <div v-for="row in rows" :key="row.key" class="row" :class="{ off: row.variable.enabled === false }">
      <Checkbox
        :model-value="row.variable.enabled !== false"
        :disabled="!workspaces.canWrite"
        :aria-label="row.variable.name"
        @update:model-value="toggle(row)"
      />

      <input
        :value="row.variable.name"
        class="kv-input name"
        :readonly="!workspaces.canWrite"
        spellcheck="false"
        @input="patch(row, { name: ($event.target as HTMLInputElement).value })"
        @blur="commit"
      />
      <TokenField
        variant="cell"
        :value="row.variable.value ?? ''"
        :readonly="!workspaces.canWrite"
        @change="patch(row, { value: $event })"
        @focusout="commit"
      />
      <span class="tail">
        <IconButton v-if="workspaces.canWrite" variant="danger" size="md" class="drop" :hint="t('common.delete')" @click="remove(row)">
          <Icon name="X" :size="14" :stroke-width="2.2" />
        </IconButton>
      </span>
    </div>

    <button v-if="!embedded && workspaces.canWrite" type="button" class="collection-variable-add" @click="addVariable">
      <Icon name="Plus" :size="14" :stroke-width="2.2" />
      <span class="cap-line">{{ t('collections.addVariable') }}</span>
    </button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.head,
.row {
  display: grid;
  grid-template-columns: var(--collection-variable-columns);
  align-items: center;
  padding-inline: var(--collection-variable-gutter);
}

.head {
  flex: none;
  height: 1.875rem;
  font-size: var(--text-12);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.head > span {
  padding-inline-start: var(--collection-variable-label-inset);
}

.row {
  flex: none;
  height: 2.375rem;
  border-bottom: 1px solid var(--line2);
  font-family: var(--mono);
  font-size: var(--text-13);
}

.row.off {
  opacity: 0.45;
}

.row:hover {
  background: var(--bg-hover);
}

.name {
  color: var(--accent);
}

.tail {
  @apply flex items-center justify-center;
  grid-column: -2;
  color: var(--text-tertiary);
}

.row :deep(.check) {
  width: 0.875rem;
  height: 0.875rem;
}

.row :deep(.drop) {
  opacity: 0;
  transition: opacity 0.12s ease;
}

.row:hover :deep(.drop),
.row:focus-within :deep(.drop) {
  opacity: 1;
}

</style>
