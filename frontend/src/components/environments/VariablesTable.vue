<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { refAutoReset, useDebounceFn } from '@vueuse/core'
import { SECRET_MASK } from '../../lib/layout'
import Input from '../ui/Input.vue'
import VariableRow from './VariableRow.vue'
import RowCreate from '../ui/RowCreate.vue'
import BulkRowsEditor from '../request/BulkRowsEditor.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { VariableKind, type Row, type Variable } from '../../../bindings/schemo/internal/domain'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { useVariableCells } from '../../composables/useVariableCells'
import { isVariableName } from '../../lib/vars'
import { t } from '../../i18n'

// One set of variables: an environment's own (for every collection) or the globals.
const { envId, locked, textMode } = defineProps<{ envId: string | null; locked: boolean; textMode: boolean }>()

const environments = useEnvironmentsStore()
let closed = false
const { notice, setNotice, clearNotice, withNotice } = useSheetNotice()

const vars = computed(() => environments.varsOf(envId))

const SEARCH_FROM = 9
const query = ref('')
const searchable = computed(() => vars.value.length >= SEARCH_FROM)
const shown = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!searchable.value || !needle) return vars.value
  return vars.value.filter((v) =>
    v.name.toLowerCase().includes(needle) || (!isSecret(v) && (v.value ?? '').toLowerCase().includes(needle))
  )
})

watch(() => envId, () => {
  query.value = ''
  clearNotice()
})

const isSecret = (v: Variable) => v.kind === VariableKind.VariableSecret
const isRevealed = (v: Variable) => environments.isRevealed(v.id)

function displayValue(v: Variable): string {
  if (!isSecret(v)) return v.value ?? ''
  if (isRevealed(v)) return environments.effectiveValue(v)
  return v.hasValue ? SECRET_MASK : ''
}

const editor = useVariableCells({
  rows: () => shown.value.map(variable => ({ scope: envId, variable })),
  vars: environments.varsOf,
  locked: () => locked,
  save: environments.updateVar,
})

const flashId = refAutoReset<string | null>(null, 1200)

// The sheet may open on one name: scroll to it and mark it.
watch(
  () => environments.sheetFocus,
  (focus) => {
    if (!focus?.varName) return
    const wanted = environments.takeFocus()
    if (!wanted) return
    if (wanted.envId !== envId) environments.editEnv(wanted.envId)
    void nextTick(() => {
      const found = environments.varsOf(wanted.envId).find((v) => v.name === wanted.varName)
      if (!found) return
      document.getElementById('var-' + found.id)?.scrollIntoView({ block: 'center' })
      flashId.value = found.id
    })
  },
  { immediate: true }
)

async function addVar(name: string, value: string): Promise<boolean> {
  if (locked || !name.trim()) return false
  const scope = envId
  query.value = ''
  return withNotice(async () => {
    const id = await environments.addVar(scope, name, value)
    const created = vars.value.find(variable => variable.id === id)
    if (created && !closed && envId === scope) void editor.start(created, 'value', scope)
  })
}

function patchVariable(variable: Variable, patch: Partial<Variable>) {
  if (!locked) void withNotice(() => environments.updateVar(envId, variable.id, patch))
}

function removeVar(variable: Variable) {
  if (!locked) void withNotice(() => environments.removeVar(envId, variable.id))
}

// Text mode: one line per variable. A secret is shown masked and keeps its value while the mask
// stays; secrecy follows the name.
const textRows = computed<Row[]>(() =>
  vars.value.map((v) => ({ id: v.id, name: v.name, value: displayValue(v), enabled: v.enabled }))
)

function fromRows(rows: Row[]): Variable[] | null {
  const byName = new Map(vars.value.map((v) => [v.name, v]))
  const seen = new Set<string>()
  const out: Variable[] = []
  for (const row of rows) {
    const name = row.name.trim()
    if (!isVariableName(name)) {
      setNotice(t('environments.nameInvalidLine', { name }))
      return null
    }
    if (seen.has(name)) {
      setNotice(t('environments.nameRepeated', { name }))
      return null
    }
    seen.add(name)
    const held = byName.get(name)
    const secret = held !== undefined && isSecret(held)
    const kept = secret && row.value === SECRET_MASK
    out.push({
      id: held?.id ?? row.id,
      name,
      kind: secret ? VariableKind.VariableSecret : VariableKind.VariableText,
      value: kept ? '' : row.value,
      hasValue: kept ? held.hasValue : false,
      enabled: row.enabled,
      position: out.length,
    })
  }
  clearNotice()
  return out
}

let pending: { envId: string | null; vars: Variable[] } | null = null
const flush = () => {
  const next = pending
  pending = null
  if (next) void withNotice(() => environments.saveScope(next.envId, next.vars))
}
const saveSoon = useDebounceFn(flush, 400)

function applyText(rows: Row[]) {
  if (locked) return
  const next = fromRows(rows)
  if (!next) return
  pending = { envId, vars: next }
  void saveSoon()
}

watch(() => [textMode, envId], () => {
  flush()
  editor.cancel()
}, { flush: 'sync' })
onBeforeUnmount(() => {
  closed = true
  flush()
})

defineExpose({ cancelTop: editor.cancelTop })
</script>

<template>
  <div class="variables">
    <template v-if="textMode">
      <BulkRowsEditor :key="envId ?? ''" :rows="textRows" :readonly="locked" @apply="applyText" />
    </template>

    <template v-else>
      <Input v-if="searchable" v-model="query" size="sm" class="search" :placeholder="t('environments.search')" />

      <div class="grid head">
        <span></span>
        <span>{{ t('environments.nameColumn') }}</span>
        <span>{{ t('environments.value') }}</span>
        <span></span>
      </div>

      <VariableRow
        v-for="variable in shown"
        :id="'var-' + variable.id"
        :key="variable.id"
        :variable
        :scope="envId"
        :editor
        :locked
        :class="{ flash: flashId === variable.id }"
        @patch="patchVariable(variable, $event)"
        @remove="removeVar(variable)"
      />

      <RowCreate
        v-if="!locked"
        :key="envId ?? ''"
        :create="addVar"
        :name-placeholder="t('environments.nameColumn')"
        :value-placeholder="t('environments.value')"
        :validate-name="name => isVariableName(name) ? '' : t('environments.nameInvalid')"
        @invalid="setNotice"
      />
    </template>

    <span v-if="notice" class="note bad">{{ notice }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.variables {
  display: flex;
  flex-direction: column;
}

.search {
  margin-bottom: 8px;
}

.grid {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr) minmax(0, 1.4fr) 80px;
  align-items: center;
}

.head {
  height: 28px;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

.flash {
  box-shadow: inset 0 0 0 2px var(--accent);
}

.note {
  margin-top: 8px;
  font-size: var(--text-12);
  color: var(--red);
}
</style>
