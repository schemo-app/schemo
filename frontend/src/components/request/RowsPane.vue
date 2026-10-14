<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from 'vue'
import Icon from '../ui/Icon.vue'
import KvRow from './KvRow.vue'
import TokenField from '../ui/TokenField.vue'
import type { RequestSource } from '../../lib/requestSource'
import { RowKind, type ProjectedRow } from '../../../bindings/schemo/internal/domain'
import { useSetting } from '../../composables/useSettings'
import { useRowOrder } from '../../composables/request/useRowOrder'
import { useMessages } from '../../i18n'

// The parameters or the headers, as the pane draws them: a table with an empty row at the end, and —
// for the headers — the rows an authorization adds, folded under a line of their own. A request
// whose address has `:name` segments draws their values above the query, in a group of their own.
const { source, kind } = defineProps<{ source: RequestSource; kind: 'params' | 'headers' }>()

const emit = defineEmits<{ openAuth: [] }>()

const { t } = useMessages()

const rowKind = computed(() => (kind === 'params' ? RowKind.RowParams : RowKind.RowHeaders))
const rows = computed(() => (kind === 'params' ? source.params : source.headers))

const pathRows = computed(() => (kind === 'params' ? source.pathParams : []))

const derived = computed(() => source.projected.filter((row) => row.target === rowKind.value))
const requests = useSetting('requests')
const shownAuto = computed(() =>
  kind === 'headers' && requests.value.showAuto ? source.auto : []
)

const folded = computed<ProjectedRow[]>(() => [...derived.value, ...shownAuto.value])

const summary = computed(() =>
  t('request.pane.headersSummary', { n: source.enabledHeadersCount, auto: folded.value.length })
)

const autoOpen = ref(false)

const table = useTemplateRef<HTMLElement>('table')

const { press, states } = useRowOrder({
  rows: () => rows.value,
  table,
  move: (id, position) => source.moveRow(rowKind.value, id, position),
})

async function add() {
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

function patchDescription(id: string, description: string) {
  void source.patchRow(rowKind.value, id, { description })
}

function patchPath(id: string, patch: { value?: string; description?: string }) {
  void source.patchRow(RowKind.RowPathParams, id, patch)
}
</script>

<template>
  <div class="rows-pane" :class="kind">
    <div v-if="kind === 'headers'" class="rows-head">
      <span class="rows-summary mono" :title="summary">{{ summary }}</span>
    </div>

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
            @change="patchPath(row.id, { value: $event })"
          />
          <template #desc>
            <input
              :value="row.description ?? ''"
              class="kv-input kv-desc"
              :placeholder="t('request.placeholderDescription')"
              spellcheck="false"
              @input="patchPath(row.id, { description: ($event.target as HTMLInputElement).value })"
            />
          </template>
        </KvRow>
      </div>
      <div class="kv-group section-label">{{ t('request.pane.queryTitle') }}</div>
    </template>

    <div ref="table" class="kv">
      <KvRow
        v-for="(row, index) in rows"
        :key="row.id"
        :enabled="row.enabled"
        :name="row.name"
        :row-id="row.id"
        :class="states[index].class"
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
        <template #desc>
          <input
            :value="row.description ?? ''"
            class="kv-input kv-desc"
            :placeholder="t('request.placeholderDescription')"
            spellcheck="false"
            @input="patchDescription(row.id, ($event.target as HTMLInputElement).value)"
          />
        </template>
      </KvRow>

      <button type="button" class="kv-row kv-add" @click="add">
        <span></span>
        <span class="kv-check ghost"></span>
        <span class="kv-ghost mono">
          {{ kind === 'headers' ? t('request.pane.addHeader') : t('request.pane.addParameter') }}
        </span>
        <span class="kv-ghost mono">{{ t('request.placeholderValue') }}</span>
        <span class="kv-ghost">{{ t('request.placeholderDescription') }}</span>
        <span></span>
      </button>
    </div>

    <div v-if="folded.length" class="auto">
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
          <span></span>
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
  @apply flex flex-col;
  padding: 0.125rem 1.25rem 0.875rem;
}

.rows-pane.params {
  padding-top: 0.625rem;
}

.rows-head {
  @apply flex items-center gap-3;
  height: 2.375rem;
}

.rows-summary {
  @apply flex-1 min-w-0 truncate text-12 text-text-tertiary;
}

.kv-row {
  grid-template-columns: 1rem 1.5rem minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr) 1.75rem;
}

.kv-path .kv-row {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr) 1.75rem;
  padding-left: 3rem;
}

.kv-group {
  @apply flex items-center;
  height: 1.75rem;
}

.kv-desc {
  font-family: inherit;
}

.auto {
  @apply flex flex-col;
  margin-top: 0.625rem;
}

.auto-toggle {
  @apply flex items-center self-start cursor-pointer border-0 text-text-secondary;
  gap: 0.375rem;
  height: 1.75rem;
  padding: 0 0.5rem 0 0.25rem;
  margin-left: -0.25rem;
  border-radius: 0.375rem;
  background: transparent;
  font-family: inherit;
  font-size: var(--text-12-5);
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
  font-size: var(--text-12-5);
}

.auto-value {
  @apply flex items-center gap-2 min-w-0;
  padding: 0 0.5rem;
}

.auto-text {
  @apply min-w-0 truncate text-text-secondary;
  font-size: var(--text-12-5);
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
