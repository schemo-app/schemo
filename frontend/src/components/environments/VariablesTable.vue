<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { refAutoReset } from '@vueuse/core'
import { SECRET_MASK } from '../../lib/layout'
import Checkbox from '../ui/Checkbox.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { VariableKind } from '../../../bindings/schemo/internal/domain'
import type { Variable } from '../../../bindings/schemo/internal/domain'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { useVariableCells } from '../../composables/useVariableCells'
import { t } from '../../i18n'

const { locked, environmentId, focusName } = defineProps<{
  locked: boolean
  environmentId?: string
  focusName?: string
}>()

const emit = defineEmits<{ (e: 'import'): void }>()

const environments = useEnvironmentsStore()
const { notice, withNotice } = useSheetNotice()

const inline = computed(() => environmentId !== undefined)
const envId = computed(() => environmentId ?? environments.editedEnvId)

const rows = computed(() => environments.rowsFor(envId.value))
const vars = computed(() => rows.value.own)

function isSecret(v: Variable): boolean {
  return v.kind === VariableKind.VariableSecret
}

function isRevealed(v: Variable): boolean {
  return environments.isRevealed(v.id)
}

function toggleReveal(v: Variable) {
  if (environments.isRevealed(v.id)) {
    environments.hide(v.id)
    return
  }
  void withNotice(() => environments.reveal(v.id))
}

function isSecretMasked(v: Variable): boolean {
  return isSecret(v) && !isRevealed(v)
}

function displayValue(v: Variable): string {
  if (!isSecret(v)) return v.value ?? ''
  return isRevealed(v) ? environments.effectiveValue(v) : SECRET_MASK
}

const { draft, setCellInput, isEditingCell, start, commitFrom, onCellKeydown, cancelTop } =
  useVariableCells(environments, {
    vars: () => vars.value,
    scope: () => envId.value,
    locked: () => locked,
  })

const flashId = refAutoReset<string | null>(null, 1200)

function openInherited(g: Variable, edit = false) {
  const scope = rows.value.base?.id ?? null
  environments.editEnv(scope)
  nextTick(() => {
    document.getElementById('var-' + g.id)?.scrollIntoView({ block: 'center' })
    if (edit) {
      void start(g, 'name', scope)
      return
    }
    flashId.value = g.id
  })
}

const pendingName = ref('')

function revealName(name: string) {
  const found = vars.value.find((variable) => variable.name === name)
  if (!found) {
    pendingName.value = name
    return
  }
  pendingName.value = ''
  document.getElementById('var-' + found.id)?.scrollIntoView({ block: 'center' })
  flashId.value = found.id
}

watch(
  () => environments.sheetFocus,
  (focus) => {
    if (inline.value || !focus?.varName) return
    const wanted = environments.takeFocus()
    if (!wanted) return
    if (wanted.envId !== envId.value) environments.editEnv(wanted.envId)
    nextTick(() => revealName(wanted.varName))
  },
  { immediate: true }
)

watch(() => focusName, (name) => {
  if (name && inline.value) void nextTick(() => revealName(name))
}, { immediate: true })

watch(vars, () => {
  if (pendingName.value) revealName(pendingName.value)
})

async function addVar() {
  if (locked) return
  const id = await environments.addVar(envId.value, { name: '', value: '' })
  const created = vars.value.find((variable) => variable.id === id)
  if (created) void start(created, 'name')
}

function removeVar(v: Variable) {
  if (locked) return
  void environments.removeVar(envId.value, v.id)
}

function toggleEnabled(v: Variable) {
  if (locked) return
  void withNotice(() => environments.updateVar(envId.value, v.id, { enabled: !v.enabled }))
}

// There is no type to pick: a value is text, and the lock is what keeps it out of the export, the
// logs and the screen — everywhere a secret is left out.
function toggleSecret(v: Variable) {
  if (locked) return
  const next = isSecret(v) ? VariableKind.VariableText : VariableKind.VariableSecret
  void environments.updateVar(envId.value, v.id, { kind: next })
}

defineExpose({ cancelTop })
</script>

<template>
  <div class="variables" :class="{ compact: inline }">
    <div class="section">
      <div v-if="!inline" class="section-head">
        <span class="section-label">{{ t('environments.variablesLabel') }}</span>
        <span class="section-count mono">{{ vars.length }}</span>
        <span class="head-spacer"></span>
        <button
          type="button"
          class="section-action"
          :disabled="locked"
          :title="locked ? t('environments.locked') : undefined"
          @click="emit('import')"
        >
          {{ t('environments.importEnv') }}
        </button>
      </div>

      <div class="card">
        <div v-if="vars.length || $slots['before-rows']" class="grid card-head">
          <span v-if="inline"></span>
          <div>{{ t('environments.variable') }}</div>
          <div>{{ t('environments.value') }}</div>
          <div></div>
          <div></div>
          <div></div>
        </div>

        <slot name="before-rows" />

        <div
          v-for="v in vars"
          :key="v.id"
          :id="'var-' + v.id"
          class="grid row"
          :class="{ flash: flashId === v.id, off: inline && !v.enabled }"
        >
          <div v-if="inline" class="cell">
            <Checkbox :model-value="v.enabled" :disabled="locked" :aria-label="v.name" @update:model-value="toggleEnabled(v)" />
          </div>
          <div class="cell cell-name" @click="start(v, 'name')">
            <input
              v-if="isEditingCell(v, 'name', envId)"
              :ref="setCellInput"
              v-model="draft"
              class="cell-input mono"
              :class="{ invalid: notice }"
              spellcheck="false"
              @keydown="onCellKeydown($event, v, 'name', envId)"
              @blur="commitFrom(v, 'name')"
            />
            <span v-else class="cell-text mono" :title="v.name">{{ v.name }}</span>
          </div>

          <div class="cell cell-value" @click="start(v, 'value')">
            <input
              v-if="isEditingCell(v, 'value', envId)"
              :ref="setCellInput"
              v-model="draft"
              class="cell-input mono"
              :type="isSecretMasked(v) ? 'password' : 'text'"
              spellcheck="false"
              @keydown="onCellKeydown($event, v, 'value', envId)"
              @blur="commitFrom(v, 'value')"
            />
            <span v-else class="cell-text mono" :class="{ masked: isSecretMasked(v) }">{{
              displayValue(v)
            }}</span>
          </div>

          <!-- The eye stands with the lock, not in the value: a button coming and going inside the
               cell would take the value's width with it and the field would jump as it hides. -->
          <div class="cell cell-icon">
            <IconButton
              v-if="isSecret(v)"
              size="xl"
              :hint="isRevealed(v) ? t('environments.hideValue') : t('environments.showValue')"
              @click="toggleReveal(v)"
            >
              <Icon :name="isRevealed(v) ? 'EyeOff' : 'Eye'" :size="14" :stroke-width="1.9" />
            </IconButton>
          </div>

          <div class="cell cell-icon">
            <IconButton
              size="xl"
              :disabled="locked"
              :hint="
                locked
                  ? t('environments.locked')
                  : isSecret(v)
                    ? t('environments.showAsText')
                    : t('environments.makeSecret')
              "
              @click="toggleSecret(v)"
            >
              <Icon
                :name="isSecret(v) ? 'Lock' : 'LockOpen'"
                :size="14"
                :stroke-width="1.9"
                :class="{ 'lock-on': isSecret(v) }"
              />
            </IconButton>
          </div>

          <div class="cell cell-icon">
            <IconButton
              v-if="!locked"
              variant="danger"
              size="xl"
              :hint="t('common.delete')"
              @click="removeVar(v)"
            >
              <Icon name="X" :size="14" :stroke-width="1.9" />
            </IconButton>
          </div>
        </div>

        <button type="button" :class="inline ? 'collection-variable-add' : 'new-row'" :disabled="locked" @click="addVar">
          <Icon name="Plus" :size="14" :stroke-width="2.2" />
          <span class="cap-line">{{ t('environments.newVariable') }}</span>
        </button>
      </div>

      <span v-if="notice || !inline" class="note" :class="{ bad: !!notice }">{{ notice || t('environments.secretsNote') }}</span>
    </div>

    <div v-if="!inline && rows.inherited.length" class="section">
      <div class="section-head">
        <span class="section-label">{{ rows.base?.name ?? t('environments.inheritedFromGlobals') }}</span>
        <span class="section-count mono">{{ rows.inherited.length }}</span>
        <span class="head-spacer"></span>
        <span v-if="!rows.base" class="section-hint">{{ t('environments.applyEverywhere') }}</span>
      </div>

      <div class="card">
        <button
          v-for="g in rows.inherited"
          :key="g.id"
          type="button"
          :id="'var-' + g.id"
          class="grid row inherited"
          :class="{ overridden: g.overridden, flash: flashId === g.id }"
          :title="rows.base?.name ?? t('environments.goToGlobal')"
          @click="openInherited(g)"
        >
          <div class="cell cell-name">
            <Icon name="CornerDownRight" :size="12" class="inherit-icon" />
            <span
              class="cell-text mono"
              :title="g.overridden ? t('environments.overridden') : g.name"
              >{{ g.name }}</span
            >
          </div>
          <div class="cell cell-value">
            <span class="cell-text mono">{{ displayValue(g) }}</span>
          </div>
          <span></span>
          <div class="cell">
            <span v-if="g.overridden" class="tag tag-overridden">{{
              t('environments.overriddenShort')
            }}</span>
            <span v-else-if="!rows.base" class="tag tag-global">{{ t('environments.global') }}</span>
          </div>
          <div class="cell cell-icon">
            <IconButton
              size="xl"
              :hint="rows.base ? t('environments.editVariables') : t('environments.editGlobal')"
              @click.stop="openInherited(g, true)"
            >
              <Icon name="Pencil" :size="14" />
            </IconButton>
          </div>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.variables {
  @apply flex flex-col gap-4.5;
}

.section {
  @apply flex flex-col gap-2;
}

.section-head {
  @apply flex items-center gap-2.5;
}

.section-label {
  @apply section-label;
}

.section-count {
  @apply text-11 text-text-tertiary;
}

.section-hint {
  @apply text-12 text-text-tertiary;
}

.section-action {
  @apply h-6.5 px-2 border-0 rounded-md cursor-pointer bg-transparent
         text-text-secondary;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.section-action:hover:not(:disabled) {
  @apply bg-bg-hover text-text;
}

.section-action:disabled {
  @apply opacity-50 cursor-default;
}

.card {
  @apply overflow-hidden;
}

.grid {
  @apply grid items-stretch;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 2rem 2rem 2rem;
  padding: 0 0.75rem;
}

.compact .grid:not(.inherited) {
  grid-template-columns: var(--collection-variable-columns);
  padding-inline: var(--collection-variable-gutter);
}

.compact .card-head > :nth-child(2),
.compact .card-head > :nth-child(3),
.compact .cell-text {
  padding-inline-start: var(--collection-variable-label-inset);
}

.compact .cell-input {
  padding-inline-start: calc(var(--collection-variable-label-inset) - 1px);
}

.compact :deep(.check) {
  width: 0.875rem;
  height: 0.875rem;
}

.row.off {
  opacity: 0.45;
}

.card-head {
  @apply items-center text-text-tertiary;
  height: 1.875rem;
  font-size: var(--text-12);
  border-bottom: 1px solid var(--border);
}

.row {
  @apply border-b;
  height: 2.375rem;
  border-color: var(--line2);
}

.row:hover {
  @apply bg-bg-hover;
}

.cell {
  @apply flex items-center min-w-0;
}

.cell-name,
.cell-value {
  padding-right: 0.75rem;
}

.cell-name {
  gap: 0.3125rem;
}

.cell-value {
  gap: 0.3125rem;
}

.cell-text {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-text;
  font-family: var(--mono);
  font-size: var(--text-13);
}

.cell-name .cell-text {
  color: var(--accent);
}

.cell-text.masked {
  @apply text-text-tertiary;
}

.cell-input {
  @apply w-full outline-none;
  box-sizing: border-box;
  height: 1.875rem;
  padding: 0 0.5rem;
  border-radius: 0.3125rem;
  font-family: var(--mono);
  font-size: var(--text-13);
  background: var(--bg-inset);
  border: 1px solid var(--accent);
}

.cell-input.invalid {
  border-color: color-mix(in srgb, var(--red) 45%, transparent);
  box-shadow: 0 0 0 2px var(--red-soft);
}

.cell-icon {
  @apply justify-center;
}

.tag {
  @apply flex-none text-11 font-semibold cursor-pointer;
  padding: 0.125rem 0.4375rem;
  border-radius: 0.25rem;
}

.tag:disabled {
  @apply cursor-default;
}

.lock-on {
  color: var(--accent);
}

.tag-global {
  @apply text-purple;
  background: color-mix(in srgb, var(--purple) 13%, transparent);
}

.tag-overridden {
  @apply text-text-tertiary bg-bg-hover;
}

.new-row {
  @apply flex items-center w-full gap-2 border-0 border-b bg-transparent text-left
         text-accent cursor-pointer;
  font-family: inherit;
  font-size: var(--text-13);
  height: 2.25rem;
  padding: 0.75rem 0.5rem;
  border-color: var(--line2);
}

.new-row:hover:not(:disabled) {
  @apply bg-bg-hover;
}

.new-row:disabled {
  @apply opacity-50 cursor-default;
}

.row.inherited {
  @apply w-full border-0 text-left cursor-pointer;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 2rem 6.25rem 2rem;
  font-family: inherit;
  background: color-mix(in srgb, var(--purple) 3%, transparent);
}

.row.inherited:hover {
  background: color-mix(in srgb, var(--purple) 7%, transparent);
}

.inherit-icon {
  @apply flex-none text-purple;
}

.row.overridden .cell-text {
  @apply text-text-tertiary line-through;
}

.row.overridden .inherit-icon {
  @apply opacity-40;
}

.row.flash {
  box-shadow: inset 0 0 0 2px var(--accent);
}

.note {
  @apply text-12 text-text-tertiary;
}

.note.bad {
  @apply text-red;
}
</style>
