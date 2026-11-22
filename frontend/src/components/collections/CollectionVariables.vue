<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import Input from '../ui/Input.vue'
import Modal from '../ui/Modal.vue'
import DialogButton from '../ui/DialogButton.vue'
import VariableRow from '../environments/VariableRow.vue'
import RowCreate from '../ui/RowCreate.vue'
import EnvironmentMenu from '../environments/EnvironmentMenu.vue'
import { useCollectionsStore } from '../../stores/collections'
import { useEnvironmentsStore } from '../../stores/environments'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useVariableCells } from '../../composables/useVariableCells'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { useToast } from '../../composables/useToast'
import { findCollection } from '../../lib/collectionTree'
import { dotOf } from '../../lib/environmentPalette'
import { isVariableName } from '../../lib/vars'
import { t } from '../../i18n'
import { VariableKind, type Variable } from '../../../bindings/schemo/internal/domain'

const collections = useCollectionsStore()
const environments = useEnvironmentsStore()
const workspaces = useWorkspacesStore()
const toast = useToast()
const { notice, setNotice, clearNotice, withNotice } = useSheetNotice()
const level = computed(() => findCollection(collections.tree, collections.collectionId))
const hasBlocks = computed(() => Boolean(level.value && !level.value.parentId))
const query = ref('')
const filter = ref(collections.varsFilter)
const replacement = ref<{ set: string; variable: Variable; target: string | null } | null>(null)
const filterEnv = computed(() => environments.envOf(filter.value))
const locked = (set: string | null) => !workspaces.canWrite || Boolean(set && environments.envOf(set)?.readonly)
const varsOf = (set: string | null, collection = level.value?.id ?? null) => set
  ? environments.envOf(set)?.collections[collection ?? ''] ?? []
  : findCollection(collections.tree, collection)?.variables ?? []
const rowOrder = new Map<string, number>()
const rows = computed(() => {
  const entries = [
    ...varsOf('').map(variable => ({ set: '', variable })),
    ...(hasBlocks.value ? environments.environments.flatMap(env =>
      varsOf(env.id).map(variable => ({ set: env.id, variable }))
    ) : []),
  ]
  // Keep new rows at the end while this collection is open, including after a reload.
  for (const { variable } of entries) {
    if (!rowOrder.has(variable.id)) rowOrder.set(variable.id, rowOrder.size)
  }
  return entries.sort((a, b) => rowOrder.get(a.variable.id)! - rowOrder.get(b.variable.id)!)
})
const shown = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return rows.value.filter(({ set, variable }) =>
    (!filterEnv.value || !set || set === filterEnv.value.id) &&
    (!needle || variable.name.toLowerCase().includes(needle) ||
      (variable.kind !== VariableKind.VariableSecret && (variable.value ?? '').toLowerCase().includes(needle)))
  )
})

let writing: Promise<unknown> = Promise.resolve()
let closed = false

function write(change: (id: string) => Promise<unknown>) {
  const id = level.value?.id
  const workspace = workspaces.activeId
  const run = () => id && workspaces.activeId === workspace && findCollection(collections.tree, id)
    ? change(id)
    : Promise.resolve()
  const saved = writing.then(run, run)
  writing = saved
  return saved
}

function save(set: string | null, change: (vars: Variable[]) => Variable[]) {
  return write(async id => {
    if (locked(set)) return
    const vars = change(varsOf(set, id))
    if (set) await environments.saveScope(set, vars, id)
    else await collections.saveVariables(id, vars)
  })
}

function patch(set: string | null, id: string, change: Partial<Variable>) {
  if (set) return write(collection => locked(set) ? Promise.resolve() : environments.updateVar(set, id, change, collection))
  return save(set, vars => vars.map(variable => variable.id === id ? { ...variable, ...change } : variable))
}

const editor = useVariableCells({
  rows: () => shown.value.map(({ set, variable }) => ({ scope: set, variable })),
  vars: varsOf,
  locked,
  save: patch,
})

watch(() => collections.varsFilter, value => { filter.value = value })
watch(() => level.value?.id, () => {
  replacement.value = null
  rowOrder.clear()
  query.value = ''
  filter.value = collections.varsFilter
  editor.cancel()
}, { flush: 'sync' })
watch(filter, () => editor.cancel())
onBeforeUnmount(() => {
  closed = true
  clearNotice()
})

async function add(name: string, value: string): Promise<boolean> {
  const id = level.value?.id
  const set = filterEnv.value?.id ?? ''
  if (locked(set) || !name.trim()) return false
  const variable = { ...environments.blankVar(name), value }
  const saved = await withNotice(() => save(set, vars => [...vars, variable]))
  if (saved && !closed && level.value?.id === id) {
    await nextTick()
    void editor.start(variable, 'value', set)
  }
  return saved
}

async function move(set: string, variable: Variable, target: string | null, replace = false) {
  const id = level.value?.id
  if (!id || set === (target ?? '') || locked(set) || locked(target)) return
  await withNotice(() => write(async collection => {
    const current = varsOf(set, collection).find(item => item.id === variable.id)
    if (!current) return
    if (!replace && current.name && varsOf(target, collection).some(item => item.name === current.name)) {
      replacement.value = { set, variable: current, target }
      return
    }
    const replaced = await environments.moveVariable(collection, current.id, set, target ?? '')
    await collections.load()
    if (replaced) toast.show(t('collections.varReplaced', {
      name: current.name,
      where: environments.envOf(target)?.name ?? t('collections.varsAll'),
    }))
  }))
}

function confirmMove() {
  const pending = replacement.value
  replacement.value = null
  if (pending) void move(pending.set, pending.variable, pending.target, true)
}
</script>

<template>
  <div class="vars">
    <div class="tools">
      <div class="search">
        <Icon name="Search" :size="13" />
        <Input v-model="query" variant="bare" size="sm" :placeholder="t('collections.varsSearch')" />
      </div>
      <DropdownMenuRoot v-if="hasBlocks">
        <DropdownMenuTrigger as-child>
          <button type="button" class="filter">
            <span v-if="filterEnv" class="dot" :style="{ background: dotOf(filterEnv.color ?? '') }"></span>
            <span class="cap-line">{{ filterEnv?.name ?? t('collections.varsAllEnvironments') }}</span>
            <Icon name="ChevronDown" :size="9" :stroke-width="2.8" class="caret" />
          </button>
        </DropdownMenuTrigger>
        <EnvironmentMenu :selected="filterEnv?.id ?? null" :none-label="t('collections.varsAllEnvironments')" align="start" @select="filter = $event ?? ''" />
      </DropdownMenuRoot>
    </div>
    <div class="variable-table" :class="{ blocks: hasBlocks }">
      <div class="grid head">
        <span></span><span>{{ t('collections.varName') }}</span><span>{{ t('collections.varValue') }}</span>
        <span v-if="hasBlocks">{{ t('collections.varEnvironment') }}</span><span></span>
      </div>
      <VariableRow
        v-for="{ set, variable } in shown"
        :key="`${set}/${variable.id}`"
        :variable
        :scope="set"
        :editor
        :locked="locked(set)"
        :allow-secret="Boolean(set)"
        @patch="withNotice(() => patch(set, variable.id, $event))"
        @remove="withNotice(() => save(set, vars => vars.filter(item => item.id !== variable.id)))"
      >
        <span v-if="hasBlocks" class="scope-cell">
          <DropdownMenuRoot>
            <DropdownMenuTrigger as-child :disabled="locked(set)">
              <button type="button" class="scope" :class="{ all: !set }">
                <span v-if="set" class="dot" :style="{ background: dotOf(environments.envOf(set)?.color ?? '') }"></span>
                <span class="cap-line">{{ environments.envOf(set)?.name ?? t('collections.varsAll') }}</span>
                <Icon v-if="locked(set)" name="Lock" :size="11" class="caret" />
              </button>
            </DropdownMenuTrigger>
            <EnvironmentMenu :selected="set" :none-label="t('collections.varsAllEnvironments')" writable-only :manage="false" align="start" @select="move(set, variable, $event)" />
          </DropdownMenuRoot>
        </span>
      </VariableRow>
      <RowCreate
        v-if="!query && !locked(filterEnv?.id ?? '')"
        :key="`${level?.id}/${filterEnv?.id ?? ''}`"
        :create="add"
        :name-placeholder="t('collections.varName')"
        :value-placeholder="t('collections.varValue')"
        :validate-name="name => isVariableName(name) ? '' : t('environments.nameInvalid')"
        @invalid="setNotice"
      >
        <span v-if="hasBlocks">{{ filterEnv?.name ?? t('collections.varsAll') }}</span>
      </RowCreate>
      <span v-if="query && !shown.length" class="note">{{ t('common.nothingFound') }}</span>
      <span v-if="notice" class="note bad">{{ notice }}</span>
    </div>
    <Modal
      v-if="replacement"
      :open="true"
      :title="t('collections.varReplaceTitle', { name: replacement.variable.name, where: environments.envOf(replacement.target)?.name ?? t('collections.varsAll') })"
      variant="ask"
      @close="replacement = null"
    >
      <p class="replacement-note">{{ t('collections.varReplaceSub') }}</p>
      <template #foot>
        <DialogButton @click="replacement = null">{{ t('common.cancel') }}</DialogButton>
        <DialogButton danger @click="confirmMove">{{ t('common.replace') }}</DialogButton>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.vars {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 12px 24px 24px;
}

.replacement-note {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}

.tools {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  margin-bottom: 4px;
}

.search {
  display: flex;
  align-items: center;
  flex: 0 1 240px;
  gap: 7px;
  min-width: 120px;
  height: 28px;
  padding: 0 10px;
  border-radius: 7px;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text-tertiary);
}

.search :deep(input) {
  width: 100%;
  min-width: 0;
  font-size: var(--text-13);
}

.filter,
.scope {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  max-width: 100%;
  padding: 0 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
  cursor: default;
}

.filter {
  flex: none;
  height: 28px;
  padding-left: 10px;
  border-radius: 7px;
  box-shadow: inset 0 0 0 1px var(--border);
}

.scope {
  height: 26px;
  margin-left: -8px;
}

.filter:hover,
.filter[data-state='open'],
.scope:hover:not(:disabled),
.scope[data-state='open'] {
  background: var(--bg-hover);
}

.scope.all,
.caret {
  color: var(--text-tertiary);
}

.scope-cell {
  min-width: 0;
}

.dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.variable-table {
  --row-columns: 28px minmax(120px, 200px) minmax(160px, 1fr) 80px;
  min-width: 430px;
}

.variable-table.blocks {
  --row-columns: 28px minmax(120px, 200px) minmax(160px, 1fr) minmax(130px, 190px) 80px;
  min-width: 600px;
}

.grid {
  display: grid;
  grid-template-columns: var(--row-columns);
  align-items: center;
}

.head {
  height: 28px;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

.note {
  display: block;
  padding: 14px 0;
  font-size: var(--text-13);
  color: var(--text-tertiary);
}

.note.bad {
  color: var(--red);
}
</style>
