<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'
import { RecordSource, type Record as RequestRecord } from '../../../bindings/schemo/internal/domain'
import { History } from '../../../bindings/schemo/internal/request'
import { callOrToast } from '../../lib/calls'
import { useCaptureStore } from '../../stores/capture'
import { useRecordsStore } from '../../stores/records'
import { splitAddress } from '../../lib/address'
import { methodInkClass, statusTone } from '../../lib/format'
import { groupLabel, tabKeyOf } from '../../lib/recordTabs'
import { recordsByDay, responseSize } from '../../lib/requestRecord'
import { daysAgo, formatClock, formatDuration, formatNumber, formatStatus, t } from '../../i18n'
import { useHarExport } from '../../composables/useHarExport'
import { useLoadOlder } from '../../composables/useLoadOlder'
import { useToast } from '../../composables/useToast'
import Icon from '../ui/Icon.vue'
import Segment from '../ui/Segment.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'

const records = useRecordsStore()
const capture = useCaptureStore()
const toast = useToast()
const { exportHar } = useHarExport(() => records.sessionFilter)
const { loadIfNearEnd } = useLoadOlder()

type Source = typeof records.historySource
type ClearMessage = 'clearedShown' | 'clearedOlder'

const clearOpen = ref(false)
const expandedRepeats = ref(new Set<string>())

const scrollEl = useTemplateRef<HTMLElement>('scrollEl')

const sources = computed(() => [
  { value: 'all', label: t('history.filterAll') },
  { value: 'manual', label: t('history.filterManual') },
  { value: 'browser', label: t('history.filterBrowser') },
  { value: 'mcp', label: t('history.filterMcp') },
])

function sourceOf(record: RequestRecord): Exclude<Source, 'all'> {
  if (record.agent) return 'mcp'

  return record.source === RecordSource.SourceBrowser ? 'browser' : 'manual'
}

function matches(record: RequestRecord, query: string): boolean {
  return `${record.method} ${record.url} ${record.status}`.toLowerCase().includes(query) ||
    (record.agent ?? '').toLowerCase().includes(query)
}

function isHttpError(record: RequestRecord): boolean {
  return record.status >= 400 && record.status < 600
}

function isSlow(record: RequestRecord): boolean {
  return record.durationUs >= 1_000_000
}

function duration(record: RequestRecord): string {
  if (!isSlow(record)) return formatDuration(record.durationUs)
  return `${formatNumber(record.durationUs / 1_000_000, 1)} ${t('units.s')}`
}

const matching = computed(() => {
  const query = records.historyQuery.trim().toLowerCase()
  const selectedSource = records.historySource
  const session = records.sessionFilter

  return records.records.filter(
    record =>
      (selectedSource === 'all' || sourceOf(record) === selectedSource) &&
      (!session || tabKeyOf(record) === session) &&
      (!query || matches(record, query)),
  )
})

const errorCount = computed(() => matching.value.filter(isHttpError).length)
const slowCount = computed(() => matching.value.filter(isSlow).length)
const filtered = computed(() => matching.value.filter(record =>
  (!records.historyErrorsOnly || isHttpError(record)) &&
  (!records.historySlowOnly || isSlow(record))
))

function historyEntry(record: RequestRecord) {
  const { host, path } = splitAddress(record.url)
  const normalizedPath = path.split(/[?#]/, 1)[0]
    .replace(/\/(?:\d+|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})(?=\/|$)/gi, '/{id}') || '/'
  const origin = record.url.slice(0, record.url.length - path.length)
  const key = `${record.method} ${origin}${normalizedPath} ${record.status} ${Boolean(record.error)}`
  return { record, host, path, normalizedPath, key }
}

type HistoryEntry = ReturnType<typeof historyEntry>

function groupConsecutive(items: RequestRecord[]): HistoryEntry[][] {
  const groups: HistoryEntry[][] = []
  for (const record of items) {
    const entry = historyEntry(record)
    const last = groups.at(-1)
    if (last?.[0].key === entry.key) last.push(entry)
    else groups.push([entry])
  }
  return groups
}

const groups = computed(() => recordsByDay(filtered.value).map(group => ({
  day: group.day,
  label: group.label,
  count: group.items.length,
  repeats: groupConsecutive(group.items),
})))

const rowsByDay = computed(() => groups.value.map(group => ({
  day: group.day,
  label: group.label,
  count: group.count,
  rows: group.repeats.flatMap(repeat => {
    const first = repeat[0]
    const expanded = expandedRepeats.value.has(first.record.id)
    const repeated = repeat.length >= 3
    const entries = () => repeat.map(entry => ({ ...entry, repeatCount: 0, indented: repeated, expanded: false }))
    if (!repeated) return entries()

    const summary = { ...first, path: first.normalizedPath, repeatCount: repeat.length, indented: false, expanded }
    return expanded ? [summary, ...entries()] : [summary]
  }),
})))

const count = computed(() =>
  filtered.value.length === records.records.length
    ? t('history.requestsN', { n: filtered.value.length })
    : t('history.countOf', {
      shown: filtered.value.length,
      total: records.records.length,
    }),
)

const hidden = computed(() => records.records.length - filtered.value.length)

const older = computed(() =>
    records.records.filter((record) => daysAgo(record.startedAt) > 0),
)

const sessionGroup = computed(() => {
  const session = records.sessionFilter

  return session
      ? capture.sessions.find((group) => group.key === session)
      : undefined
})

const sessionTitle = computed(() =>
    sessionGroup.value ? groupLabel(sessionGroup.value) : '',
)

const selectedTab = computed(() => sessionGroup.value?.tab)
const recording = computed(() => selectedTab.value ? !selectedTab.value.paused : capture.recording)
const showCapture = computed(() =>
  capture.connected && (records.sessionFilter ? !!selectedTab.value : capture.recording),
)
const captureAction = computed(() => {
  if (!records.sessionFilter) return t('capture.pauseAll')
  return recording.value ? t('history.pause') : t('history.resume')
})

const liveLine = computed(() => {
  const parts: string[] = []

  if (sessionGroup.value) parts.push(groupLabel(sessionGroup.value))
  if (capture.browser) parts.push(capture.browser)

  if (records.sessionFilter) {
    parts.push(t('history.requestsN', { n: sessionGroup.value?.items.length ?? 0 }))
  } else {
    parts.push(t('capture.recordingTabs', {
      active: capture.tabList.filter(tab => !tab.paused).length,
      total: capture.tabList.length,
    }))
  }

  return parts.join(' · ')
})

const sourceIcons = { manual: 'MousePointer2', browser: 'Globe', mcp: 'Asterisk' } as const

function sourceText(record: RequestRecord): string {
  if (record.agent) return t('history.sourceAgent', { agent: record.agent })

  return record.source === RecordSource.SourceBrowser
      ? t('history.filterBrowser')
      : t('history.filterManual')
}

// A plain click selects the preview; Cmd/Ctrl-click opens a separate tab.
function select(id: string, event: MouseEvent) {
  void records.selectRecord(id, event.metaKey || event.ctrlKey)
}

function toggleRepeat(id: string) {
  if (expandedRepeats.value.has(id)) expandedRepeats.value.delete(id)
  else expandedRepeats.value.add(id)
}

// The store tells the person when the rows could not be deleted, so only a deletion that happened
// is announced here.
async function clearRecords(ids: string[], message: ClearMessage) {
  if (!ids.length) return

  if (await records.clearRecords(ids)) {
    toast.show(t(`history.${message}`, { n: ids.length }))
  }
}

function clearShown() {
  return clearRecords(
      filtered.value.map((record) => record.id),
      'clearedShown',
  )
}

function clearOlder() {
  return clearRecords(
      older.value.map((record) => record.id),
      'clearedOlder',
  )
}

async function clearAll() {
  const removed = await callOrToast(History.ClearAll(), 'history.clearFailed')

  if (removed !== undefined) toast.show(t('history.clearedAll', { n: removed }))
}

const onScroll = () => loadIfNearEnd(scrollEl.value)

// Filters and collapsed runs can leave no scrollbar even when older records are available.
onMounted(onScroll)
watch(rowsByDay, onScroll, { flush: 'post' })
watch(() => [
  records.historyQuery, records.historySource, records.sessionFilter,
  records.historyErrorsOnly, records.historySlowOnly,
], () => {
  if (scrollEl.value) scrollEl.value.scrollTop = 0
})
watch(() => records.workspaceId, () => expandedRepeats.value.clear())
</script>

<template>
  <div class="history-page">
    <div class="hist-bar">
      <div class="hist-search">
        <Icon name="Search" :size="13" :stroke-width="2" class="shrink-0" />
        <input
            v-model="records.historyQuery"
            class="hist-search-input"
            spellcheck="false"
            :placeholder="t('history.filterPlaceholder')"
            :aria-label="t('history.filterPlaceholder')"
        />
      </div>

      <Segment
          class="hist-seg"
          size="sm"
          variant="toolbar"
          :grow="false"
          :options="sources"
          :value="records.historySource"
          @pick="records.historySource = $event as Source"
      />

      <div class="hist-quick-filters">
        <button
          type="button"
          class="hist-quick"
          :class="{ muted: !errorCount }"
          :aria-pressed="records.historyErrorsOnly"
          :title="t('history.errorsHint')"
          @click="records.historyErrorsOnly = !records.historyErrorsOnly"
        >
          <span class="hist-quick-dot error"></span>
          {{ t('history.errors') }}
          <span class="hist-quick-count">{{ errorCount }}</span>
        </button>
        <button
          type="button"
          class="hist-quick"
          :class="{ muted: !slowCount }"
          :aria-pressed="records.historySlowOnly"
          :title="t('history.slowHint')"
          @click="records.historySlowOnly = !records.historySlowOnly"
        >
          <span class="hist-quick-dot slow"></span>
          {{ t('history.slow') }}
          <span class="hist-quick-count">{{ slowCount }}</span>
        </button>
      </div>

      <div class="hist-actions">
        <span class="hist-count">{{ count }}</span>
        <DropdownMenu v-model:open="clearOpen">
          <DropdownMenuTrigger as-child>
            <button
                type="button"
                class="hist-clear"
                :class="{ open: clearOpen }"
                :disabled="records.records.length === 0"
                :title="t('history.clearTitle')"
                :aria-label="t('history.clearTitle')"
            >
              <Icon name="Trash2" :size="15" :stroke-width="1.7" />
            </button>
          </DropdownMenuTrigger>

          <DropdownMenuContent align="end" :side-offset="6">
            <div class="hist-menu-head">{{ t('history.clearHead') }}</div>

            <DropdownMenuItem
                v-if="filtered.length && hidden"
                class="hist-menu-item"
                @select="clearShown"
            >
              <span>{{ t('history.clearShown') }}</span>
              <span class="hist-menu-count">{{ filtered.length }}</span>
            </DropdownMenuItem>

            <DropdownMenuItem
                v-if="older.length"
                class="hist-menu-item"
                @select="clearOlder"
            >
              <span>{{ t('history.clearOlder') }}</span>
              <span class="hist-menu-count">{{ older.length }}</span>
            </DropdownMenuItem>

            <DropdownMenuSeparator
                v-if="(filtered.length && hidden) || older.length"
                class="hist-menu-sep"
            />

            <DropdownMenuItem class="hist-menu-item danger" @select="clearAll">
              <span>{{ t('history.clearAll') }}</span>
              <span class="hist-menu-count">{{ records.records.length }}</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <span v-if="sessionGroup" class="hist-chip">
          <span class="hist-chip-text">
            {{ t('history.session', { title: sessionTitle }) }}
          </span>
          <button
            type="button"
            class="hist-chip-x"
            :aria-label="t('history.session', { title: sessionTitle })"
            @click="records.clearSession()"
          >
            <Icon name="X" :size="8" :stroke-width="3" />
          </button>
        </span>
      </div>
    </div>

    <div v-if="showCapture" class="hist-rec" :class="{ paused: !recording }">
      <span class="hist-rec-dot"></span>
      <span class="hist-rec-title">{{ recording ? t('history.recording') : t('history.paused') }}</span>
      <span class="hist-rec-line">{{ liveLine }}</span>
      <button
        type="button"
        class="hist-rec-btn"
        @click="capture.toggle(records.sessionFilter || undefined)"
      >
        {{ captureAction }}
      </button>
      <button class="hist-rec-btn" @click="exportHar()">
        {{ t('files.exportHar') }}
      </button>
    </div>

    <div ref="scrollEl" class="hist-scroll" @scroll.passive="onScroll">
      <template v-for="group in rowsByDay" :key="group.day">
        <div class="hist-group">
          <span>{{ group.label }}</span>
          <span>· {{ group.count }}</span>
        </div>

        <button
          v-for="row in group.rows"
          :key="row.repeatCount ? `repeat:${row.record.id}` : row.record.id"
          type="button"
          class="hist-row"
          :class="{ indented: row.indented }"
          :aria-expanded="row.repeatCount ? row.expanded : undefined"
          @click="row.repeatCount ? toggleRepeat(row.record.id) : select(row.record.id, $event)"
        >
          <span class="hist-method" :class="methodInkClass(row.record.method)">
            {{ row.record.method }}
          </span>

          <span class="hist-addr">
            <span class="hist-url" :title="row.repeatCount ? undefined : row.record.url">
              <span class="hist-host">{{ row.host }}</span>{{ row.path === '/' ? '' : row.path }}
            </span>
            <span
              v-if="row.repeatCount"
              class="hist-repeat"
              :title="row.expanded ? t('history.collapseRepeated') : t('history.showRepeated', { n: row.repeatCount })"
            >
              ×{{ row.repeatCount }}
              <Icon name="ChevronRight" :size="9" :stroke-width="3" :class="{ expanded: row.expanded }" />
            </span>
          </span>

          <span class="hist-status">
            <span class="hist-chip-status" :class="statusTone(row.record)">
              {{ formatStatus(row.record.status, row.record.error) }}
            </span>
          </span>

          <span class="hist-metrics">
            <span v-if="!row.expanded" :class="{ slow: !row.repeatCount && isSlow(row.record) }">
              {{ duration(row.record) }}
            </span>
            <span v-if="!row.repeatCount && row.record.responseBody?.size" class="hist-size">
              · {{ responseSize(row.record) }}
            </span>
          </span>
          <span class="hist-source" :title="sourceText(row.record)">
            <Icon :name="sourceIcons[sourceOf(row.record)]" :size="14" />
            <span class="sr-only">{{ sourceText(row.record) }}</span>
          </span>
          <span class="hist-when">{{ formatClock(row.record.startedAt) }}</span>
        </button>
      </template>

      <div v-if="!rowsByDay.length && !records.olderLoading" class="hist-empty">
        {{ records.records.length ? t('common.nothingFound') : t('history.empty') }}
      </div>

      <div v-if="records.olderLoading" class="hist-more">
        {{ t('history.loadingOlder') }}
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.history-page {
  @apply flex-1 min-h-0 flex flex-col;
  --clock-width: 2.875rem;
}

.history-page:lang(en) {
  --clock-width: 4rem;
}

.hist-bar {
  @apply flex flex-wrap items-center flex-none;
  min-height: 3.5rem;
  gap: 0.625rem;
  padding: 0.75rem 1.25rem;
}

.hist-search {
  @apply flex items-center;
  flex: 1 1 0;
  min-width: 8.75rem;
  max-width: 17.5rem;
  height: 1.875rem;
  gap: 0.4375rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  box-shadow: inset 0 0 0 1px var(--border-strong);
  color: var(--text-tertiary);
}

.hist-search:focus-within {
  box-shadow: inset 0 0 0 1px var(--accent);
}

.hist-search-input {
  @apply flex-1 min-w-0;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--text);
  font: inherit;
}

.hist-seg {
  @apply flex-none;
}

.hist-quick-filters {
  @apply flex flex-none items-center;
  gap: 0.625rem;
}

.hist-quick-filters::before {
  content: '';
  width: 1px;
  height: 1rem;
  background: var(--border);
}

.hist-quick {
  @apply flex items-center whitespace-nowrap border-0 bg-transparent cursor-pointer;
  height: 1.75rem;
  padding: 0 0.6875rem;
  gap: 0.375rem;
  border-radius: 0.875rem;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text-secondary);
  font: inherit;
}

.hist-quick.muted:not([aria-pressed='true']) {
  opacity: 0.5;
}

.hist-quick:hover {
  color: var(--text);
  opacity: 1;
}

.hist-quick[aria-pressed='true'] {
  background: var(--bg-hover);
  box-shadow: none;
  color: var(--text);
}

.hist-quick-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
}

.hist-quick-dot.error {
  background: var(--red);
}

.hist-quick-dot.slow {
  background: var(--orange);
}

.hist-quick-count {
  color: var(--text-tertiary);
  font-variant-numeric: tabular-nums;
}

.hist-chip {
  @apply flex items-center min-w-0;
  flex: 0 1 auto;
  height: 1.75rem;
  max-width: 16.25rem;
  gap: 0.3rem;
  padding: 0 0.3rem 0 0.625rem;
  border-radius: 0.4375rem;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: var(--text-12);
}

.hist-chip-text {
  @apply truncate;
}

.hist-chip-x {
  @apply flex items-center justify-center flex-none;
  width: 1.125rem;
  height: 1.125rem;
  border: 0;
  border-radius: 0.3125rem;
  background: transparent;
  color: inherit;
  cursor: default;
}

.hist-chip-x:hover {
  background: var(--accent-soft);
}

.hist-actions {
  @apply flex items-center min-w-0;
  margin-left: auto;
  gap: 0.625rem;
}

.hist-count {
  @apply flex-none whitespace-nowrap;
  color: var(--text-tertiary);
  font-size: var(--text-12);
  font-variant-numeric: tabular-nums;
}

.hist-clear {
  @apply flex items-center justify-center flex-none cursor-pointer;
  width: 1.875rem;
  height: 1.875rem;
  border: 0;
  border-radius: 0.5rem;
  background: var(--chip);
  color: var(--text-secondary);
  font: inherit;
}

.hist-clear:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text);
}

.hist-clear.open {
  background: var(--bg-active);
  color: var(--text);
}

.hist-clear:disabled {
  opacity: 0.35;
  cursor: default;
}

.hist-menu-head {
  @apply flex items-center flex-none text-text-tertiary;
  height: 1.625rem;
  padding: 0 0.5rem;
  font-size: var(--text-12);
}

.hist-menu-item {
  @apply flex items-center justify-between flex-none cursor-default;
  height: 1.75rem;
  gap: 0.625rem;
  padding: 0 0.5rem;
  border-radius: 0.3rem;
  color: var(--text);
  font-size: var(--text-13);
}

.hist-menu-item:hover,
.hist-menu-item[data-highlighted] {
  background: var(--bg-hover);
  outline: none;
}

.hist-menu-item.danger {
  color: var(--red-text);
}

.hist-menu-item.danger:hover,
.hist-menu-item.danger[data-highlighted] {
  background: var(--red-soft);
}

.hist-menu-count {
  @apply flex-none;
  color: var(--text-tertiary);
  font-size: var(--text-12);
  font-variant-numeric: tabular-nums;
}

.hist-menu-sep {
  height: 1px;
  margin: 0.25rem 0.3rem;
  background: var(--border);
}

.hist-rec {
  @apply flex items-center flex-none;
  margin: 0 1.25rem;
  height: 2.875rem;
  gap: 0.75rem;
  padding: 0 0.5rem 0 0.875rem;
  border-radius: 0.625rem;
  background: var(--raise);
  box-shadow: inset 0 0 0 0.5px var(--border);
}

.hist-rec-dot {
  @apply flex-none;
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: var(--red);
  box-shadow: 0 0 0 0.1875rem var(--red-soft);
}

.hist-rec.paused .hist-rec-dot {
  background: var(--text-tertiary);
  box-shadow: none;
}

.hist-rec-title {
  @apply flex-none font-semibold;
}

.hist-rec-line {
  @apply flex-1 min-w-0 truncate text-text-secondary;
}

.hist-rec-btn {
  @apply flex-none;
  height: 1.875rem;
  padding: 0 0.75rem;
  border: 0;
  border-radius: 0.4375rem;
  background: var(--bg-panel);
  color: var(--text);
  font: inherit;
  cursor: default;
  box-shadow: 0 0 0 1px var(--border);
}

.hist-rec-btn:hover {
  background: var(--bg-hover);
}

.hist-scroll {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0 0.5rem 1.25rem;
}

.hist-row {
  @apply grid w-full items-center border-0 bg-transparent text-left cursor-pointer;
  grid-template-columns: 3.25rem minmax(0, 1fr) 3.5rem min(9.375rem, 25%) 1.625rem var(--clock-width);
  column-gap: 0.5rem;
  height: 2.125rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem;
  color: var(--text);
  font: inherit;
}

.hist-row:hover,
.hist-row:focus-visible {
  background: var(--bg-hover);
}

.hist-row.indented {
  padding-left: 1.75rem;
}

.hist-group {
  @apply flex items-baseline;
  gap: 0.375rem;
  font-size: var(--text-12);
  color: var(--text-tertiary);
  padding: 0.875rem 0.75rem 0.375rem;
}

.hist-group > :first-child {
  color: var(--text-secondary);
  font-weight: 600;
}

.hist-method {
  @apply truncate;
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 700;
}

.hist-addr {
  @apply flex items-center min-w-0;
  gap: 0.5rem;
}

.hist-url {
  @apply min-w-0 truncate;
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.hist-host {
  color: var(--text-tertiary);
}

.hist-repeat {
  @apply flex flex-none items-center;
  height: 1.125rem;
  padding: 0 0.3125rem 0 0.4375rem;
  gap: 0.1875rem;
  border-radius: 0.5625rem;
  background: var(--chip);
  color: var(--text-secondary);
  font-size: var(--text-11);
  font-weight: 600;
}

.hist-repeat .expanded {
  transform: rotate(90deg);
}

.hist-status {
  @apply flex justify-end;
}

.hist-chip-status {
  @apply truncate;
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 0.3125rem;
}

.hist-chip-status.is-ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.hist-chip-status.is-bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.hist-metrics,
.hist-when {
  @apply truncate text-right;
  color: var(--text-secondary);
  font-size: var(--text-12);
  font-variant-numeric: tabular-nums;
}

.hist-metrics .slow {
  color: var(--orange-text);
  font-weight: 600;
}

.hist-size,
.hist-when {
  color: var(--text-tertiary);
}

.hist-source {
  @apply flex justify-center;
  color: var(--text-tertiary);
}

.hist-empty {
  padding: 2.5rem 0.5rem;
  text-align: center;
  color: var(--text-tertiary);
}

.hist-more {
  @apply flex items-center justify-center text-text-tertiary;
  height: 3rem;
  font-size: var(--text-12);
}
</style>
