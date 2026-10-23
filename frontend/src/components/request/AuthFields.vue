<script setup lang="ts">
import { ref } from 'vue'
import Icon from '../ui/Icon.vue'
import VarHighlight from '../ui/VarHighlight.vue'
import { vVarPeek } from '../../lib/varPeek'
import PaneSelect from './PaneSelect.vue'
import Select from '../ui/Select.vue'
import { textOf, t } from '../../i18n'
import { parseTokens } from '../../lib/vars'
import type { Auth, Field, Scheme } from '../../../bindings/schemo/internal/domain'

const { scheme, auth } = defineProps<{
  scheme: Scheme | null
  auth: Auth
  scale?: 'compact' | 'roomy' | 'rows'
}>()

const emit = defineEmits<{
  update: [key: string, value: string]
  commit: []
}>()


function tokensIn(value: string): boolean {
  return value.trim() !== '' && parseTokens(value).length > 0
}

function covered(value: string): boolean {
  return !value.includes('\n') && tokensIn(value)
}

const revealed = ref<Record<string, boolean>>({})

function valueOf(field: Field): string {
  const given = auth.fields?.[field.key]
  if (given !== undefined && given !== null) return given
  return scheme?.fields?.find((schemeField) => schemeField.key === field.key)?.default ?? ''
}

const fields = () => (scheme?.fields ?? []).filter(shown)

function shown(field: Field): boolean {
  if (!field.when) return true
  const held = auth.fields?.[field.when.key]
  const answer = held !== undefined && held !== null ? held : (defaultOf(field.when.key) ?? '')
  return (field.when.values ?? []).includes(answer)
}

function defaultOf(key: string): string {
  return scheme?.fields?.find((field) => field.key === key)?.default ?? ''
}

function choiceLabel(field: Field): string {
  const option = field.options?.find((option) => option.value === valueOf(field))
  return option ? textOf(option.label) : valueOf(field)
}

function choose(field: Field, value: string) {
  emit('update', field.key, value)
  emit('commit')
}

function onInput(field: Field, event: Event) {
  emit('update', field.key, (event.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value)
}
</script>

<template>
  <div class="fields" :class="scale ?? 'compact'">
    <label v-for="field in fields()" :key="field.key" class="field" :class="{ full: field.full }">
      <span class="label">{{ textOf(field.label) }}</span>

      <div v-if="field.kind === 'password'" class="veiled">
        <input
          :type="revealed[field.key] ? 'text' : 'password'"
          :value="valueOf(field)"
          :placeholder="field.placeholder ? textOf(field.placeholder) : undefined"
          :class="{ mono: field.mono }"
          spellcheck="false"
          autocomplete="off"
          @input="onInput(field, $event)"
          @blur="emit('commit')"
          @keydown.enter.prevent="emit('commit')"
        />
        <button
          type="button"
          class="eye"
          :aria-label="t('request.auth.reveal')"
          @click="revealed[field.key] = !revealed[field.key]"
        >
          <Icon name="Eye" :size="12" />
        </button>
      </div>

      <div v-else-if="field.kind === 'textarea'" class="covered" :class="{ mono: field.mono }">
        <textarea
          v-var-peek
          :value="valueOf(field)"
          :placeholder="field.placeholder ? textOf(field.placeholder) : undefined"
          :class="{ veiled: covered(valueOf(field)) }"
          :style="{ height: field.height && scale !== 'rows' ? `${field.height}px` : undefined }"
          spellcheck="false"
          @input="onInput(field, $event)"
          @blur="emit('commit')"
          @keydown.enter.meta.prevent="emit('commit')"
        />
        <VarHighlight v-if="covered(valueOf(field))" :value="valueOf(field)" multiline />
      </div>

      <span v-else-if="field.kind === 'select' && scale === 'rows'" class="choice">
        <PaneSelect
          variant="cell"
          :label="choiceLabel(field)"
          :options="[(field.options ?? []).map((option) => ({ key: option.value, label: textOf(option.label) }))]"
          :current="valueOf(field)"
          @select="choose(field, $event)"
        />
      </span>

      <Select
        v-else-if="field.kind === 'select'"
        roomy
        :options="(field.options ?? []).map((option) => ({ value: option.value, label: textOf(option.label) }))"
        :model-value="valueOf(field)"
        @update:model-value="choose(field, $event)"
      />

      <div v-else class="covered" :class="{ mono: field.mono }">
        <input
          v-var-peek
          :type="field.kind === 'number' ? 'number' : 'text'"
          :value="valueOf(field)"
          :placeholder="field.placeholder ? textOf(field.placeholder) : undefined"
          :class="{ veiled: covered(valueOf(field)) }"
          spellcheck="false"
          autocomplete="off"
          @input="onInput(field, $event)"
          @blur="emit('commit')"
          @keydown.enter.prevent="emit('commit')"
        />
        <VarHighlight v-if="covered(valueOf(field))" :value="valueOf(field)" />
      </div>

      <span v-if="field.hint" class="hint">{{ textOf(field.hint) }}</span>
    </label>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.fields {
  @apply grid grid-cols-2 items-start;
}

.field {
  @apply flex flex-col min-w-0;
}

.field.full {
  @apply col-span-2;
}

.fields.compact {
  --field-pad: 0 0.75rem;
  gap: 0.75rem;
}

.fields.roomy {
  --field-pad: 0 0.625rem;
  gap: 0.625rem;
}

.fields.compact .field {
  gap: 0.3rem;
}

.fields.roomy .field {
  gap: 0.25rem;
}

.label {
  color: var(--text-secondary);
  font-size: var(--text-12);
  font-weight: 500;
}

.roomy .label {
  @apply text-text-tertiary;
  font-size: var(--text-11);
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
}

.fields input,
.fields textarea,
.fields select {
  @apply w-full border bg-bg-inset text-text box-border;
  font: inherit;
  outline: none;
}

.fields input,
.fields select {
  height: 2.125rem;
}

.fields input,
.fields select,
.fields .highlight {
  padding: 0 0.75rem;
  font-size: var(--text-13);
}

.fields textarea,
.fields .highlight.area {
  padding: 0.625rem 0.75rem;
  font-size: var(--text-13);
  line-height: 1.6;
}

.fields input,
.fields textarea,
.fields select {
  border-color: var(--border-strong);
  border-radius: 0.5rem;
}

.fields textarea {
  resize: none;
}

.fields select {
  cursor: pointer;
}

.roomy input,
.roomy select {
  height: 2.125rem;
}

.roomy input,
.roomy select,
.roomy textarea,
.roomy .highlight,
.roomy .highlight.area {
  font-family: var(--mono);
  font-size: var(--text-13);
}

.roomy input,
.roomy select,
.roomy .highlight {
  padding: 0 0.6rem;
}

.roomy textarea,
.roomy .highlight.area {
  padding: 0.5rem 0.625rem;
}

.roomy input,
.roomy textarea,
.roomy select {
  border-color: var(--border-strong);
  border-radius: 0.5rem;
}

.fields input:focus,
.fields textarea:focus,
.fields select:focus {
  border-color: var(--accent);
}

.fields .mono {
  font-family: var(--mono);
}

.fields input::placeholder,
.fields textarea::placeholder {
  color: var(--text-tertiary);
}

.veiled {
  @apply relative flex min-w-0;
}

.covered {
  @apply relative flex min-w-0;
}

.covered input.veiled,
.covered textarea.veiled {
  color: transparent;
  caret-color: var(--text);
}

.veiled input {
  padding-right: 1.75rem;
}

.veiled input[type='password']::-ms-reveal,
.veiled input[type='password']::-ms-clear {
  display: none;
}

.eye {
  @apply absolute flex items-center justify-center border-none bg-transparent cursor-pointer text-text-tertiary;
  right: 0.25rem;
  top: 50%;
  transform: translateY(-50%);
  width: 1.25rem;
  height: 1.25rem;
  border-radius: 0.25rem;
  padding: 0;
}

.eye:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.hint {
  color: var(--text-tertiary);
  font-size: var(--text-11);
  line-height: 1.4;
}

.fields.rows {
  @apply flex flex-col items-stretch;
  --field-pad: 0 0.625rem;
}

.fields.rows .field {
  @apply grid items-center;
  grid-template-columns: 11.25rem minmax(0, 1fr);
  min-height: 3.25rem;
  border-bottom: 1px solid var(--line2);
}

.fields.rows .field > :not(.label):not(.hint):not(.choice) {
  margin: 0.25rem 0;
  width: calc(100% + 1rem);
}

.fields.rows .choice {
  @apply flex items-center;
  margin: 0.25rem 0;
  width: calc(100% + 1rem);
}

.fields.rows .label {
  @apply self-start;
  grid-row: span 2;
  padding: 0.5rem 0.5rem 0.5625rem 0;
  font-size: var(--text-12-5);
  font-weight: 400;
}

.fields.rows .hint {
  grid-column: 2;
  padding: 0 0.5rem 0.25rem;
}

.fields.rows input,
.fields.rows select,
.fields.rows input,
.fields.rows select,
.fields.rows textarea {
  border: 0;
  border-radius: 0.5rem;
  background: var(--raise);
  box-shadow: inset 0 0 0 1px var(--line2);
  font-size: var(--text-12-5);
}

.fields.rows input,
.fields.rows select {
  height: 2rem;
}

.fields.rows input,
.fields.rows select,
.fields.rows .highlight {
  padding: 0 0.6rem;
  font-size: var(--text-12-5);
}

.fields.rows textarea,
.fields.rows .highlight.area {
  height: 2rem;
  padding: 0 0.6rem;
  font-size: var(--text-12-5);
  line-height: 1.75rem;
}

.fields.rows input:focus,
.fields.rows select:focus,
.fields.rows textarea:focus {
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}

.fields.rows input:hover:not(:focus),
.fields.rows select:hover:not(:focus),
.fields.rows textarea:hover:not(:focus) {
  background: var(--bg-inset);
}

.fields.rows .veiled input {
  padding-right: 1.75rem;
}
</style>
