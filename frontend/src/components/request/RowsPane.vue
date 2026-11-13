<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { refAutoReset } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import KvRow from './KvRow.vue'
import BulkRowsEditor from './BulkRowsEditor.vue'
import TokenField from '../ui/TokenField.vue'
import type { RequestSource } from '../../lib/requestSource'
import { RowKind, type ProjectedRow, type Row } from '../../../bindings/schemo/internal/domain'
import { useSetting } from '../../composables/useSettings'
import { useRowOrder } from '../../composables/useRowOrder'
import { t } from '../../i18n'
import { parsePastedRows, insertRows } from '../../lib/bulkRows'
import { paramsFromUrl } from '../../lib/draftRows'
import { Drafts } from '../../../bindings/schemo/internal/request'
import { Kind } from '../../../bindings/schemo/internal/convert/command'
import { useToast } from '../../composables/useToast'
import { usePlatform } from '../../composables/usePlatform'

// Headers share one table with their automatic rows. Parameters keep path values above the query.
const { source, kind, toolbar = null } = defineProps<{
  source: RequestSource
  kind: 'params' | 'headers'
  toolbar?: HTMLElement | null
}>()

const emit = defineEmits<{ openAuth: [] }>()


const rowKind = computed(() => (kind === 'params' ? RowKind.RowParams : RowKind.RowHeaders))
const rows = computed(() => (kind === 'params' ? source.params : source.headers))

const pathRows = computed(() => (kind === 'params' ? source.pathParams : []))

const derived = computed(() => source.projected.filter((row) => row.target === rowKind.value))
const requests = useSetting('requests')
const shownAuto = computed(() =>
  kind === 'headers' && requests.value.showAuto ? source.auto : []
)

const folded = computed<ProjectedRow[]>(() => [...derived.value, ...shownAuto.value])

const autoOpen = ref(false)
const bulkEditing = defineModel<boolean>('textMode', { default: false })
const editor = useTemplateRef<InstanceType<typeof BulkRowsEditor>>('editor')
const toast = useToast()
const { shortcut } = usePlatform()
const pastedIds = refAutoReset<string[]>([], 2600)
let pasteVersion = 0
onBeforeUnmount(() => pasteVersion++)

async function onPaste(event: ClipboardEvent) {
  if (bulkEditing.value) return
  const text = event.clipboardData?.getData('text/plain') ?? ''
  if (!text.trim()) return
  const command = /^curl(?:\.exe)?\b/i.test(text.trimStart())
  const target = event.target as HTMLElement
  if (target.closest?.('input, textarea, [contenteditable="true"]') && !command && !/[\r\n]/.test(text)) return
  event.preventDefault()
  const version = ++pasteVersion
  const current = source
  const section = kind
  const before = rows.value

  try {
    let pasted: Row[]
    if (command) {
      const result = await Drafts.Paste(text)
      if (version !== pasteVersion || source !== current || kind !== section || rows.value !== before) return
      if (result.reading.kind !== Kind.KindOK || !result.draft) {
        toast.show(t('request.pane.pasteFailed'), 'error')
        return
      }
      pasted = section === 'headers' ? result.draft.headers ?? [] : paramsFromUrl(result.draft.url)
    } else {
      const result = parsePastedRows(text, section)
      if (result.errorLine !== null) {
        toast.show(t('request.pane.bulkError', {line: result.errorLine}), 'error')
        return
      }
      pasted = result.rows
    }
    pasted = pasted.filter(row => row.name.trim())
    if (!pasted.length) return
    const updated = insertRows(rows.value, pasted, section)
    pastedIds.value = updated.slice(-pasted.length).map(row => row.id)
    source.replaceRows(rowKind.value, updated)
    toast.show(t(section === 'headers' ? 'request.pane.pastedHeaders' : 'request.pane.pastedParams', pasted.length))
  } catch {
    toast.show(t('request.pane.pasteFailed'), 'error')
  }
}

function pickMode(value: string) {
  if (value === 'table' && editor.value && !editor.value.apply()) return
  bulkEditing.value = value === 'text'
}

function applyBulk(rows: Row[]) {
  source.replaceRows(rowKind.value, rows)
}

const table = useTemplateRef<HTMLElement>('table')

const { press, states } = useRowOrder({
  rows: () => rows.value,
  table,
  move: (id, position) => source.moveRow(rowKind.value, id, position),
})

async function addRow() {
  const id = await source.addRow(rowKind.value)
  await nextTick()
  table.value?.querySelector<HTMLInputElement>(`[data-row="${id}"]`)?.focus()
}

function patchName(id: string, name: string) {
  void source.patchRow(rowKind.value, id, { name })
}

function patchValue(id: string, value: string) {
  void source.patchRow(rowKind.value, id, { value })
}

</script>

<template>
  <div class="rows-pane" :class="kind">
    <Teleport :to="toolbar" :disabled="!toolbar">
      <button
        type="button"
        class="pane-toggle rows-mode"
        :class="{ active: bulkEditing }"
        :aria-pressed="bulkEditing"
        :title="t(bulkEditing ? 'request.pane.backToTable' : 'request.pane.bulkEdit')"
        :aria-label="t(bulkEditing ? 'request.pane.backToTable' : 'request.pane.bulkEdit')"
        @click="pickMode(bulkEditing ? 'table' : 'text')"
      ><Icon name="AlignLeft" :size="15" :stroke-width="1.9" /></button>
    </Teleport>
    <template v-if="pathRows.length">
      <div class="kv-group section-label">{{ t('request.pane.pathTitle') }}</div>
      <div class="kv kv-path">
        <KvRow
          v-for="row in pathRows"
          :key="row.id"
          :enabled="true"
          :name="row.name"
          :row-id="row.id"
          :name-placeholder="t('request.placeholderKey')"
          :checkable="false"
          :removable="false"
          name-readonly
        >
          <TokenField
            variant="cell"
            :value="row.value"
            :placeholder="t('request.placeholderValue')"
            @change="source.patchRow(RowKind.RowPathParams, row.id, { value: $event })"
          />
        </KvRow>
      </div>
    </template>

    <BulkRowsEditor v-if="bulkEditing" ref="editor" :rows :env-id="source.environmentId" @apply="applyBulk" />
    <div ref="table" class="kv" :class="{ 'kv-text-auto': bulkEditing }" :tabindex="bulkEditing ? undefined : 0" :aria-label="t(`request.chips.${kind}`)" @paste="onPaste">
      <KvRow
        v-for="(row, index) in bulkEditing ? [] : rows"
        :key="row.id"
        :enabled="row.enabled"
        :name="row.name"
        :row-id="row.id"
        :class="[states[index].class, { pasted: pastedIds.includes(row.id) }]"
        :style="states[index].style"
        grabbable
        :name-placeholder="kind === 'headers' ? t('request.pane.headerName') : t('request.placeholderKey')"
        @toggle="source.toggleRow(rowKind, row.id, !row.enabled)"
        @rename="patchName(row.id, $event)"
        @remove="source.removeRow(rowKind, row.id)"
        @grab="press($event, row.id, index)"
      >
        <TokenField
          variant="cell"
          :value="row.value"
          :placeholder="t('request.placeholderValue')"
          @change="patchValue(row.id, $event)"
        />
      </KvRow>

      <button v-if="!bulkEditing" type="button" class="kv-row kv-add" @click="addRow">
        <span></span>
        <span class="kv-ghost mono">{{ t('request.placeholderKey') }}</span>
        <span class="kv-add-value">
          <span class="kv-ghost mono">{{ t('request.placeholderValue') }}</span>
          <span class="paste-hint">{{ t('request.pane.pasteHint', {shortcut: shortcut('V')}) }}</span>
        </span>
        <span></span>
      </button>

      <template v-if="kind === 'headers'">
        <div
          v-for="(row, index) in folded"
          :key="`auto-${row.name}-${index}`"
          class="kv-row header-auto-row"
          :title="t('request.pane.autoTitle')"
        >
          <span class="header-auto-check">
            <Icon name="Check" :size="11" :stroke-width="3.2" />
          </span>
          <span class="header-auto-name mono">
            <span class="truncate">{{ row.name }}</span>
            <button
              v-if="row.from"
              type="button"
              class="header-auto-badge"
              :title="t('request.pane.fromAuth')"
              @click="emit('openAuth')"
            >{{ t('request.pane.autoBadge') }}</button>
            <span v-else class="header-auto-badge">{{ t('request.pane.autoBadge') }}</span>
          </span>
          <span class="header-auto-value mono">{{ row.value || '—' }}</span>
          <span></span>
        </div>
      </template>

    </div>

    <div v-if="kind === 'params' && folded.length" class="auto">
      <button type="button" class="auto-toggle" @click="autoOpen = !autoOpen">
        <Icon name="ChevronRight" :size="10" :stroke-width="2.8" class="auto-chevron" :class="{ open: autoOpen }" />
        <span class="cap-line">{{ t('request.pane.auto') }}</span>
        <span class="auto-count">{{ folded.length }}</span>
      </button>
      <div v-if="autoOpen" class="auto-rows">
        <div
          v-for="(row, index) in folded"
          :key="`${row.name}-${index}`"
          class="kv-row auto-row"
          :title="t('request.pane.autoTitle')"
        >
          <Icon name="Lock" :size="12" :stroke-width="1.9" class="auto-lock" />
          <span class="auto-name mono">{{ row.name }}</span>
          <span class="auto-value">
            <span class="auto-text mono">{{ row.value }}</span>
            <button v-if="row.from" type="button" class="auto-link" @click="emit('openAuth')">
              {{ t('request.pane.fromAuth') }}
            </button>
          </span>
          <span></span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.rows-pane {
  @apply relative flex flex-col;
  padding: 0.5rem 1.125rem 0.5rem 1.125rem;
  --key-width: min(13.75rem, 40%);
}

.kv {
  @apply relative;
  outline: none;
}

.kv-add-value {
  @apply flex items-center min-w-0;
}

.paste-hint {
  @apply truncate text-text-tertiary;
  margin-left: auto;
  padding-left: 0.75rem;
  font-family: inherit;
  font-size: var(--text-12);
  opacity: 0;
  transition: opacity 0.15s ease;
}

.kv:focus-within .paste-hint {
  opacity: 1;
}

.kv-row.pasted {
  border-radius: 0.375rem;
  background: var(--accent-soft);
  transition: background 0.6s ease;
}

.kv-text-auto {
  margin-top: 0.5rem;
}

.kv-row {
  grid-template-columns: 1.375rem var(--key-width) minmax(0, 1fr) 1.75rem;
  gap: 0;
  height: 2rem;
  border-bottom: 0;
}

.kv-path .kv-row {
  grid-template-columns: var(--key-width) minmax(0, 1fr) 1.75rem;
  padding-left: 1.375rem;
}

.kv-path {
  margin-bottom: 0.5rem;
}

.kv-group {
  @apply flex items-center;
  height: 1.75rem;
}

.rows-pane :deep(.kv-anchor) {
  position: absolute;
  left: -0.75rem;
  width: 0.75rem;
}

.rows-pane :deep(.check) {
  width: 0.875rem;
  height: 0.875rem;
}

.header-auto-check {
  @apply flex items-center justify-center justify-self-center text-text-tertiary;
  width: 0.875rem;
  height: 0.875rem;
}

.header-auto-name {
  @apply flex items-center min-w-0 text-text-secondary;
  gap: 0.5rem;
  padding: 0 0.5rem;
  font-size: var(--text-13);
}

.header-auto-badge {
  @apply flex-none border-0 text-text-tertiary;
  padding: 0.125rem 0.3125rem;
  border-radius: 0.25rem;
  background: var(--chip);
  font: inherit;
  font-size: var(--text-11);
  line-height: 1;
}

button.header-auto-badge {
  cursor: pointer;
}

button.header-auto-badge:hover {
  color: var(--text);
}

.header-auto-value {
  @apply truncate text-text-tertiary;
  padding: 0 0.5rem;
  font-size: var(--text-13);
}

.auto {
  @apply flex flex-col;
  margin-top: 0.625rem;
}

.auto-toggle {
  @apply flex items-center self-start cursor-pointer border-0 text-text-secondary;
  gap: 0.3125rem;
  height: 1.75rem;
  padding: 0 0.5rem 0 0.25rem;
  margin-left: -0.25rem;
  border-radius: 0.3125rem;
  background: transparent;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.auto-toggle:hover {
  @apply bg-bg-hover text-text;
}

.auto-chevron {
  @apply text-text-tertiary;
  transition: transform 0.15s ease;
}

.auto-chevron.open {
  transform: rotate(90deg);
}

.auto-count {
  @apply text-11 font-semibold text-text-tertiary;
}

.auto-rows {
  @apply flex flex-col;
  margin-top: 0.25rem;
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.auto-row {
  height: 2rem;
}

.auto-lock {
  @apply justify-self-center text-text-tertiary;
}

.auto-name {
  @apply truncate text-text-secondary;
  padding: 0 0.5rem;
  font-size: var(--text-13);
}

.auto-value {
  @apply flex items-center gap-2 min-w-0;
  padding: 0 0.5rem;
}

.auto-text {
  @apply min-w-0 truncate text-text-secondary;
  font-size: var(--text-13);
}

.auto-link {
  @apply flex-none cursor-pointer border-0 text-accent;
  height: 1.25rem;
  padding: 0 0.4375rem;
  border-radius: 0.25rem;
  background: var(--accent-soft);
  font-family: inherit;
  font-size: var(--text-11);
  font-weight: 500;
}

.auto-link:hover {
  filter: brightness(1.05);
}
</style>
