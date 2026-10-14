<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef, watch } from 'vue'
import { RecordSource, type Record as RequestRecord } from '../../../bindings/schemo/internal/domain'
import { RecordsService } from '../../../bindings/schemo/internal/transport/wails'
import { useRecordsStore } from '../../stores/records'
import { splitAddress } from '../../lib/address'
import { methodInkClass, statusTone } from '../../lib/format'
import { effectiveTab, groupLabel, tabGroups, tabKeyOf } from '../../lib/recordTabs'
import {
  daysAgo,
  describeFailure,
  formatBytes,
  formatClock,
  formatDay,
  formatDuration,
  formatStatus,
  useMessages,
} from '../../i18n'
import { useToast } from '../../composables/useToast'
import Icon from '../ui/Icon.vue'
import Segment from '../ui/Segment.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'

const store = useRecordsStore()
const toast = useToast()
const { t } = useMessages()

type Source = 'all' | 'manual' | 'browser' | 'mcp'
type ClearMessage = 'clearedShown' | 'clearedOlder'

const query = ref('')
const source = ref<Source>('all')
const clearOpen = ref(false)

const scrollEl = useTemplateRef<HTMLElement>('scrollEl')

const sources = computed(() => [
  { value: 'all', label: t('history.filterAll') },
  { value: 'manual', label: t('history.filterManual') },
  { value: 'browser', label: t('history.filterBrowser') },
  { value: 'mcp', label: t('history.filterMcp') },
])

function sourceOf(r: RequestRecord): Source {
  if (r.agent) return 'mcp'

  return r.source === RecordSource.SourceBrowser ? 'browser' : 'manual'
}

function matches(r: RequestRecord, q: string): boolean {
  return (
      r.method.toLowerCase().includes(q) ||
      String(r.status).includes(q) ||
      r.url.toLowerCase().includes(q) ||
      (r.agent ?? '').toLowerCase().includes(q)
  )
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  const selectedSource = source.value
  const session = store.sessionFilter

  return store.records.filter(
      (r) =>
          (selectedSource === 'all' || sourceOf(r) === selectedSource) &&
          (!session || tabKeyOf(r) === session) &&
          (!q || matches(r, q)),
  )
})

const groups = computed(() => {
  const result: { label: string; items: RequestRecord[] }[] = []

  for (const record of filtered.value) {
    const label = formatDay(record.startedAt)
    const last = result[result.length - 1]

    if (last?.label === label) {
      last.items.push(record)
    } else {
      result.push({ label, items: [record] })
    }
  }

  return result
})

const count = computed(() =>
    t('history.countOf', {
      shown: filtered.value.length,
      total: store.records.length,
    }),
)

const hidden = computed(() => store.records.length - filtered.value.length)

const older = computed(() =>
    store.records.filter((r) => daysAgo(r.startedAt) > 0),
)

const browserRecords = computed(() =>
    store.records.filter((r) => r.source === RecordSource.SourceBrowser),
)

const browserTabGroups = computed(() => tabGroups(browserRecords.value))

const sessionGroup = computed(() => {
  const session = store.sessionFilter

  return session
      ? browserTabGroups.value.find((group) => group.key === session)
      : undefined
})

const sessionTitle = computed(() =>
    sessionGroup.value ? groupLabel(sessionGroup.value) : '',
)

const liveGroup = computed(() => effectiveTab(browserTabGroups.value, ''))
const recording = computed(() => store.capture.recording)

const liveLine = computed(() => {
  const parts: string[] = []

  if (liveGroup.value) parts.push(groupLabel(liveGroup.value))
  if (store.capture.browser) parts.push(store.capture.browser)

  parts.push(t('history.requestsN', browserRecords.value.length))

  return parts.join(' · ')
})

function sizeOf(r: RequestRecord): string {
  const size = r.responseBody?.size ?? 0

  return size > 0 ? formatBytes(size) : '—'
}

function sourceText(r: RequestRecord): string {
  if (r.agent) return t('history.sourceAgent', { agent: r.agent })

  return r.source === RecordSource.SourceBrowser
      ? t('history.filterBrowser')
      : t('history.filterManual')
}

// A plain click selects the preview; Cmd/Ctrl-click opens a separate tab.
function select(id: string, e?: MouseEvent) {
  void store.selectRecord(id, !!e && (e.metaKey || e.ctrlKey))
}

async function exportHar() {
  try {
    const written = await RecordsService.ExportHar(
        t('files.exportHar'),
        store.sessionFilter,
    )

    if (written) toast.show(t('browser.saved'))
  } catch (error) {
    toast.show(
        t('history.clearFailed', { error: describeFailure(error) }),
        'error',
    )
  }
}

async function clearRecords(ids: string[], message: ClearMessage) {
  if (!ids.length) return

  try {
    await store.clearRecords(ids)
    toast.show(t(`history.${message}`, { n: ids.length }))
  } catch (error) {
    toast.show(
        t('history.clearFailed', { error: describeFailure(error) }),
        'error',
    )
  }
}

function clearShown() {
  return clearRecords(
      filtered.value.map((r) => r.id),
      'clearedShown',
  )
}

function clearOlder() {
  return clearRecords(
      older.value.map((r) => r.id),
      'clearedOlder',
  )
}

async function clearAll() {
  try {
    const removed = await RecordsService.ClearAll()
    toast.show(t('history.clearedAll', { n: removed }))
  } catch (error) {
    toast.show(
        t('history.clearFailed', { error: describeFailure(error) }),
        'error',
    )
  }
}

function onScroll() {
  const el = scrollEl.value

  if (
      !el ||
      store.olderLoading ||
      store.olderExhausted ||
      store.records.length === 0
  ) {
    return
  }

  if (el.scrollHeight - el.scrollTop - el.clientHeight < 300) {
    void store.loadOlder()
  }
}

onMounted(() => void nextTick(onScroll))

watch(filtered, (rows) => {
  if (!rows.length) void nextTick(() => window.scrollTo(0, 0))
})
</script>

<template>
  <div class="history-page">
    <div class="hist-bar">
      <div class="hist-search">
        <Icon name="Search" :size="12" :stroke-width="2" class="hist-search-icon" />
        <input
            v-model="query"
            class="hist-search-input"
            spellcheck="false"
            :placeholder="t('history.filterPlaceholder')"
        />
      </div>

      <Segment
          class="hist-seg"
          size="sm"
          :grow="false"
          :options="sources"
          :value="source"
          @pick="source = $event as Source"
      />

      <DropdownMenu v-model:open="clearOpen">
        <DropdownMenuTrigger as-child>
          <button
              class="hist-clear"
              :class="{ open: clearOpen }"
              :disabled="store.records.length === 0"
              :title="t('history.clearTitle')"
              :aria-label="t('history.clearTitle')"
          >
            <Icon name="Trash2" :size="14" :stroke-width="1.8" />
          </button>
        </DropdownMenuTrigger>

        <DropdownMenuContent class="hist-menu" align="start" :side-offset="6">
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
            <span class="hist-menu-count">{{ store.records.length }}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <span v-if="sessionGroup" class="hist-chip">
        <span class="hist-chip-text">
          {{ t('history.session', { title: sessionTitle }) }}
        </span>
        <button
            class="hist-chip-x"
            :aria-label="t('history.session', { title: sessionTitle })"
            @click="store.clearSession()"
        >
          <Icon name="X" :size="8" :stroke-width="3" />
        </button>
      </span>

      <span class="hist-spacer"></span>
      <span class="hist-count">{{ count }}</span>
    </div>

    <div v-if="recording" class="hist-rec">
      <span class="hist-rec-dot"></span>
      <span class="hist-rec-title">{{ t('history.recording') }}</span>
      <span class="hist-rec-line">{{ liveLine }}</span>
      <button class="hist-rec-btn" @click="store.toggleCapture()">
        {{ t('history.pause') }}
      </button>
      <button class="hist-rec-btn" @click="exportHar()">
        {{ t('files.exportHar') }}
      </button>
    </div>

    <div ref="scrollEl" class="hist-scroll" @scroll.passive="onScroll">
      <div class="hist-grid hist-head">
        <span>{{ t('history.colMethod') }}</span>
        <span>{{ t('history.colAddress') }}</span>
        <span>{{ t('history.colStatus') }}</span>
        <span class="right">{{ t('history.colTime') }}</span>
        <span class="right">{{ t('history.colSize') }}</span>
        <span class="hist-src">{{ t('history.colSource') }}</span>
        <span class="right">{{ t('history.colWhen') }}</span>
      </div>

      <template v-for="g in groups" :key="g.label">
        <div class="hist-group">{{ g.label }}</div>

        <div
            v-for="r in g.items"
            :key="r.id"
            class="hist-grid hist-row"
            @click="select(r.id, $event)"
        >
          <span class="hist-method" :class="methodInkClass(r.method)">
            {{ r.method }}
          </span>

          <span class="hist-addr">
            <span class="hist-host">{{ splitAddress(r.url).host }}</span>{{ splitAddress(r.url).path }}
          </span>

          <span>
            <span class="hist-chip-status" :class="statusTone(r)">
              {{ formatStatus(r.status, r.error) }}
            </span>
          </span>

          <span class="right hist-num">{{ formatDuration(r.durationUs) }}</span>
          <span class="right hist-num">{{ sizeOf(r) }}</span>
          <span class="hist-src hist-src-cell">{{ sourceText(r) }}</span>
          <span class="right hist-num dim">{{ formatClock(r.startedAt) }}</span>
        </div>
      </template>

      <div v-if="groups.length === 0" class="hist-empty">
        {{ t('common.nothingFound') }}
      </div>

      <div
          v-if="!store.olderExhausted && store.records.length > 0"
          class="hist-more"
      >
        <span v-if="store.olderLoading" class="hist-more-note">
          {{ t('history.loadingOlder') }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.history-page {
  @apply flex-1 min-h-0 flex flex-col;
}

.hist-bar {
  @apply flex items-center flex-none;
  height: 3.5rem;
  gap: 0.625rem;
  padding: 0 1.25rem;
}

.hist-search {
  @apply flex items-center flex-none;
  width: 17.5rem;
  height: 1.875rem;
  gap: 0.4375rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  box-shadow: inset 0 0 0 1px var(--border-strong);
  color: var(--text-tertiary);
}

.hist-search-icon {
  @apply flex-none;
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

.hist-chip {
  @apply flex items-center min-w-0;
  flex: 0 1 auto;
  height: 1.75rem;
  max-width: 16.25rem;
  gap: 0.375rem;
  padding: 0 0.375rem 0 0.625rem;
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

.hist-spacer {
  @apply flex-1;
}

.hist-count {
  @apply flex-none;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.hist-clear {
  @apply flex items-center justify-center flex-none cursor-default;
  width: 1.75rem;
  height: 1.75rem;
  border: 0;
  border-radius: 0.4375rem;
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
  opacity: 0.4;
}

.hist-menu-head {
  @apply flex items-center flex-none text-text-tertiary;
  height: 1.625rem;
  padding: 0 0.5rem;
  font-size: var(--text-11-5);
}

.hist-menu-item {
  @apply flex items-center justify-between flex-none cursor-default;
  height: 1.75rem;
  gap: 0.625rem;
  padding: 0 0.5rem;
  border-radius: 0.375rem;
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
  margin: 0.25rem 0.375rem;
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
  padding: 0.625rem 1.25rem 1.25rem;
}

.hist-grid {
  display: grid;
  grid-template-columns: 3.875rem minmax(0, 1fr) 3.875rem 4.875rem 4.625rem 6.875rem 3.25rem;
  align-items: center;
  padding: 0 0.5rem;
}

.hist-head {
  height: 1.875rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.hist-row {
  height: 2.125rem;
  border-radius: 0.375rem;
  border-bottom: 1px solid var(--line2);
  cursor: default;
}

.hist-row:hover {
  background: var(--bg-hover);
}

.right {
  text-align: right;
}

.hist-group {
  font-size: var(--text-11-5);
  font-weight: 600;
  color: var(--text-tertiary);
  padding: 0.875rem 0.5rem 0.25rem;
}

.hist-method {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
}

.hist-addr {
  @apply min-w-0 truncate;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.hist-host {
  color: var(--text-tertiary);
}

.hist-chip-status {
  font-family: var(--mono);
  font-size: var(--text-11);
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

.hist-num {
  width: 55px;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

.hist-num.dim {
  color: var(--text-tertiary);
}

.hist-src {
  padding-left: 1.125rem;
}

.hist-src-cell {
  @apply truncate;
  color: var(--text-secondary);
}

.hist-empty {
  padding: 2.5rem 0.5rem;
  text-align: center;
  color: var(--text-tertiary);
}

.hist-more {
  @apply flex items-center justify-center;
  height: 3rem;
}

.hist-more-note {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}
</style>