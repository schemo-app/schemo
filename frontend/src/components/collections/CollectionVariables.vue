<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../ui/Icon.vue'
import TokenField from '../ui/TokenField.vue'
import { IconButton } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import { useCollectionsStore } from '../../stores/collections'
import { useEnvironmentsStore } from '../../stores/environments'
import { findCollection } from '../../lib/collectionTree'
import { SECRET_MASK } from '../../lib/layout'
import { useMessages } from '../../i18n'
import { VariableKind, type Variable } from '../../../bindings/schemo/internal/domain'

// What the collection keeps between its requests, and what the environment says about the same name
// — a value the environment overrides is shown beside the one written here.
const store = useCollectionsStore()
const environments = useEnvironmentsStore()
const { t } = useMessages()

type EditorRow = { key: number; variable: Variable }

// The level the page is about: the same answer the header names and the tree highlights by.
const level = computed(() => findCollection(store.tree, store.collectionId))

const rows = ref<EditorRow[]>([])
let nextKey = 1

function fromStore(): EditorRow[] {
  return (level.value?.variables ?? []).map((variable) => ({
    key: nextKey++,
    variable: { ...variable },
  }))
}

watch(
  () => level.value?.variables,
  () => {
    rows.value = fromStore()
  },
  { immediate: true, deep: true }
)

const envName = computed(() => environments.activeEnvironment?.name ?? '')

/** What the environment holds under the same name, if anything: the value that would go out. */
const envByName = computed(() => {
  const map = new Map<string, Variable>()
  for (const variable of environments.activeEnvironment?.vars ?? []) {
    if (variable.enabled === false) continue
    map.set(variable.name, variable)
  }
  return map
})

function currentOf(row: EditorRow): { text: string; overridden: boolean } {
  const fromEnv = envByName.value.get(row.variable.name)
  if (fromEnv) return { text: shown(fromEnv), overridden: true }
  return { text: shown(row.variable), overridden: false }
}

function shown(variable: Variable): string {
  if (variable.value) return variable.value
  return variable.hasValue ? SECRET_MASK : ''
}

function isSecret(variable: Variable): boolean {
  return variable.kind === VariableKind.VariableSecret
}

async function commit() {
  if (!level.value) return
  await store.saveVariables(
    level.value.id,
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

function add() {
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
  <div class="vars">
    <!-- A collection with no variables of its own is the button alone, the way an environment's
         empty set is: a table nobody has filled yet says nothing with its headings. -->
    <div v-if="rows.length" class="head">
      <span></span>
      <span>{{ t('collections.varName') }}</span>
      <span>{{ t('collections.inCollection') }}</span>
      <span>{{ envName ? t('collections.varsCurrent', { env: envName }) : t('collections.varsCurrentBare') }}</span>
      <span></span>
    </div>

    <div v-for="row in rows" :key="row.key" class="row" :class="{ off: row.variable.enabled === false }">
      <Checkbox
        :model-value="row.variable.enabled !== false"
        @update:model-value="toggle(row)"
      />

      <input
        :value="row.variable.name"
        class="kv-input name"
        spellcheck="false"
        @input="patch(row, { name: ($event.target as HTMLInputElement).value })"
        @blur="commit"
      />
      <TokenField
        variant="cell"
        :value="row.variable.value ?? ''"
        @change="patch(row, { value: $event })"
        @focusout="commit"
      />
      <span class="cur-cell">
        <span class="cur" :class="{ over: currentOf(row).overridden }">{{ currentOf(row).text }}</span>
      </span>
      <span class="tail">
        <Icon v-if="isSecret(row.variable)" name="Lock" :size="14" :stroke-width="1.8" class="lock" />
        <IconButton variant="danger" size="md" class="drop" :hint="t('common.delete')" @click="remove(row)">
          <Icon name="X" :size="14" :stroke-width="2.2" />
        </IconButton>
      </span>
    </div>

    <button type="button" class="add" @click="add">
      <Icon name="Plus" :size="14" :stroke-width="2.2" />
      <span class="cap-line">{{ t('collections.addVariable') }}</span>
    </button>

    <p class="foot">
      {{ t('collections.varsFoot') }}
    </p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.vars {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 1rem 1.5rem;
}

.head,
.row {
  display: grid;
  grid-template-columns: 1.5rem minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr) 1.75rem;
  align-items: center;
  padding: 0 0.625rem;
}

.head {
  height: 1.875rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.head > span {
  padding: 0 0.5rem;
}

.row {
  height: 2.5rem;
  border-bottom: 1px solid var(--line2);
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.row.off {
  opacity: 0.45;
}

.name {
  color: var(--accent);
}

.cur-cell {
  @apply min-w-0 truncate;
  padding: 0 1rem 0 0.5rem;
}

.cur {
  color: var(--text-secondary);
}

.cur.over {
  padding: 0.125rem 0.375rem;
  border-radius: 0.3125rem;
  background: var(--accent-soft);
  color: var(--accent);
}

.tail {
  @apply flex items-center justify-end;
  color: var(--text-tertiary);
}

.lock {
  @apply flex-none;
}

.row :deep(.drop) {
  opacity: 0;
  transition: opacity 0.12s ease;
}

.row:hover :deep(.drop) {
  opacity: 1;
}

.add {
  @apply flex items-center gap-1.5 self-start cursor-pointer border-0 bg-transparent text-accent;
  margin-top: 0.75rem;
  padding: 0 0.625rem;
  height: 1.875rem;
  border-radius: 0.375rem;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.add:hover {
  background: var(--accent-soft);
}

.foot {
  @apply text-text-secondary;
  margin-top: 0.875rem;
  padding: 0 0.625rem;
  max-width: 42.5rem;
  line-height: 1.55;
}
</style>
