<script setup lang="ts">
import { computed, ref } from 'vue'
import { type Record as RequestRecord } from '../../../bindings/schemo/internal/domain'
import { useRecordsStore } from '../../stores/records'
import { useTabsStore } from '../../stores/tabs'
import type { PaneSource } from '../../stores/content'
import { splitAddress } from '../../lib/address'
import { capsOf } from '../../lib/commands'
import { methodInkClass, statusTone } from '../../lib/format'
import { formatClock, formatDuration, formatStatus, t } from '../../i18n'
import { useCommands } from '../../composables/useCommands'
import { usePlatform } from '../../composables/usePlatform'
import Segment from '../ui/Segment.vue'

// The page of a request with nothing in it: the history read back two ways (what happened last,
// host by host, and what happens most) and the two things one does first. A click fills the line
// and touches nothing else; the record stays what it was.
const { source } = defineProps<{ source: PaneSource }>()

const records = useRecordsStore()
const tabs = useTabsStore()
const { caps } = useCommands()
const { isMac } = usePlatform()

const view = ref<'recent' | 'freq'>('recent')

const views = computed(() => [
  { value: 'recent', label: t('request.recent') },
  { value: 'freq', label: t('request.frequent') },
])

const pasteKeys = computed(() => capsOf('mod+V', isMac.value))
const saveKeys = computed(() => caps('save'))

// A record as a row of these lists: where it went, split the way the rows draw it.
interface BlankRow {
  record: RequestRecord
  host: string
  path: string
}

function shape(record: RequestRecord, at = record.url): BlankRow {
  const parts = splitAddress(at)
  return { record, host: parts.host, path: parts.path || '/' }
}

const recentGroups = computed(() => {
  const groups: { host: string; rows: BlankRow[] }[] = []
  for (const record of records.records.slice(0, 14)) {
    const row = shape(record)
    let group = groups.find((one) => one.host === row.host)
    if (!group) {
      group = { host: row.host, rows: [] }
      groups.push(group)
    }
    group.rows.push(row)
  }
  return groups
})

// Ids in an address are the same call made about different things: one row per shape, counted.
function normalized(url: string): string {
  return url
    .split('?')[0]
    .replace(/\/(\d+|[0-9a-f]{8}-[0-9a-f-]{27,})(?=\/|$)/gi, '/:id')
}

const frequent = computed(() => {
  const seen = new Map<string, { row: BlankRow; n: number }>()
  for (const record of records.records) {
    const url = normalized(record.url)
    const key = `${record.method} ${url}`
    const found = seen.get(key)
    if (found) found.n += 1
    else seen.set(key, { row: shape(record, url), n: 1 })
  }

  const list = [...seen.values()].sort((a, b) => b.n - a.n).slice(0, 10)
  const most = list.length ? list[0].n : 1
  return list.map((one) => ({ ...one, pct: `${Math.round((one.n / most) * 100)}%` }))
})

async function fill(row: RequestRecord) {
  await source.setMethod(row.method)
  source.setUrl(row.url)
  const bodies = await records.loadBodies(row.id)
  if (bodies?.request) source.setBody(bodies.request)
  await source.flush()
}
</script>

<template>
  <div class="blank-page">
    <div class="blank-tips">
      <span class="blank-tip">
        <span v-if="pasteKeys.length" class="blank-keys">
          <span v-for="key in pasteKeys" :key="key" class="blank-key">{{ key }}</span>
        </span>
        <span class="blank-tip-title">{{ t('request.recentCurlTitle') }}</span>
        <span class="blank-tip-note">{{ t('request.recentCurlNote') }}</span>
      </span>
      <span class="blank-tip">
        <span v-if="saveKeys.length" class="blank-keys">
          <span v-for="key in saveKeys" :key="key" class="blank-key">{{ key }}</span>
        </span>
        <span class="blank-tip-title">{{ t('request.recentSaveTitle') }}</span>
        <span class="blank-tip-note">{{ t('request.recentSaveNote') }}</span>
      </span>
    </div>

    <div class="blank-views">
      <Segment
        class="flex-none"
        size="xs"
        :grow="false"
        :options="views"
        :value="view"
        @pick="view = $event as 'recent' | 'freq'"
      />
      <span class="blank-copy">{{ t('request.recentCopy') }}</span>
      <button type="button" class="blank-link" @click="tabs.openHistory()">
        {{ t('request.allHistory') }}
      </button>
    </div>

    <template v-if="view === 'recent'">
      <template v-for="group in recentGroups" :key="group.host">
        <div v-if="group.host" class="blank-host-row">
          <span class="blank-host">{{ group.host }}</span>
          <span class="blank-host-count">{{ t('history.requestsN', { n: group.rows.length }) }}</span>
        </div>
        <button
          v-for="row in group.rows"
          :key="row.record.id"
          type="button"
          class="blank-row blank-recent-row"
          @click="fill(row.record)"
        >
          <span class="blank-method" :class="methodInkClass(row.record.method)">
            {{ row.record.method }}
          </span>
          <span class="blank-path">{{ row.path }}</span>
          <span>
            <span class="blank-status" :class="statusTone(row.record)">
              {{ formatStatus(row.record.status, row.record.error) }}
            </span>
          </span>
          <span class="blank-num">{{ formatDuration(row.record.durationUs) }}</span>
          <span class="blank-num dim">{{ formatClock(row.record.startedAt) }}</span>
        </button>
      </template>
    </template>

    <template v-else>
      <div v-if="frequent.length" class="blank-freq-head">
        <span>{{ t('history.colMethod') }}</span>
        <span>{{ t('history.colAddress') }}</span>
        <span>{{ t('request.freqCalls') }}</span>
        <span class="right">{{ t('request.freqLast') }}</span>
      </div>
      <button
        v-for="one in frequent"
        :key="one.row.record.id"
        type="button"
        class="blank-row blank-freq-row"
        @click="fill(one.row.record)"
      >
        <span class="blank-method" :class="methodInkClass(one.row.record.method)">
          {{ one.row.record.method }}
        </span>
        <span class="blank-path">
          <span class="blank-host-part">{{ one.row.host }}</span>{{ one.row.path }}
        </span>
        <span class="blank-calls">
          <span class="blank-calls-n">{{ one.n }}</span>
          <span class="blank-bar">
            <span class="blank-bar-fill" :style="{ width: one.pct }"></span>
          </span>
        </span>
        <span class="blank-num dim">{{ formatClock(one.row.record.startedAt) }}</span>
      </button>
    </template>

    <div v-if="records.records.length === 0" class="blank-empty">
      {{ t('history.emptySent') }}
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.blank-page {
  @apply flex-1 min-h-0 overflow-auto flex flex-col;
  padding: 1rem 1.25rem 1.5rem;
}

.blank-tips {
  @apply flex flex-wrap items-center;
  gap: 0.5rem 1.5rem;
  padding: 0 0.625rem 1.125rem;
  margin-bottom: 0.875rem;
  border-bottom: 1px solid var(--line2);
  font-size: var(--text-12-5);
  color: var(--text-secondary);
}

.blank-tip {
  @apply flex items-center;
  gap: 0.5rem;
}

.blank-keys {
  @apply flex;
  gap: 0.125rem;
}

.blank-key {
  @apply flex items-center justify-center flex-none;
  min-width: 1.25rem;
  height: 1.25rem;
  box-sizing: border-box;
  padding: 0 0.3125rem;
  border-radius: 0.3125rem;
  background: var(--chip);
  box-shadow: inset 0 0 0 0.5px var(--border), 0 0.5px 0 var(--border);
  font-size: var(--text-11-5);
  color: var(--text-secondary);
}

.blank-tip-title {
  color: var(--text);
}

.blank-tip-note {
  color: var(--text-tertiary);
}

.blank-views {
  @apply flex items-center;
  gap: 0.75rem;
  padding: 0 0.125rem 0.625rem;
}

.blank-copy {
  @apply flex-1 min-w-0 truncate;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

.blank-link {
  @apply flex-none cursor-pointer border-0 bg-transparent p-0;
  font: inherit;
  font-size: var(--text-12-5);
  color: var(--accent);
}

.blank-link:hover {
  text-decoration: underline;
}

.blank-host-row {
  @apply flex items-baseline;
  gap: 0.5rem;
  padding: 0.875rem 0.625rem 0.3rem;
  border-bottom: 1px solid var(--border);
}

.blank-host {
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 600;
}

.blank-host-count {
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

.blank-row {
  @apply grid items-center flex-none cursor-default border-0;
  border-radius: 0.4375rem;
  border-bottom: 1px solid var(--line2);
  background: transparent;
  font: inherit;
  color: inherit;
  text-align: left;
}

.blank-row:hover {
  background: var(--bg-hover);
}

.blank-recent-row {
  grid-template-columns: 4rem minmax(0, 1fr) 4rem 5rem 3.5rem;
  height: 2.125rem;
  padding: 0 0.6rem;
}

.blank-freq-head {
  @apply grid items-center flex-none;
  grid-template-columns: 4rem minmax(0, 1fr) 8.75rem 3.5rem;
  height: 1.75rem;
  padding: 0 0.6rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.blank-freq-row {
  grid-template-columns: 4rem minmax(0, 1fr) 8.75rem 3.5rem;
  height: 2.125rem;
  padding: 0 0.6rem;
}

.right {
  text-align: right;
}

.blank-method {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
}

.blank-path {
  @apply min-w-0 truncate;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.blank-host-part {
  color: var(--text-tertiary);
}

.blank-status {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 0.3125rem;
}

.blank-status.is-ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.blank-status.is-bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.blank-num {
  text-align: right;
  font-size: var(--text-12);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.blank-num.dim {
  color: var(--text-tertiary);
}

.blank-calls {
  @apply flex items-center;
  gap: 0.5rem;
  padding-right: 1rem;
}

.blank-calls-n {
  width: 1.5rem;
  text-align: right;
  font-size: var(--text-12);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

.blank-bar {
  @apply flex-1 overflow-hidden;
  height: 0.25rem;
  border-radius: 0.125rem;
  background: var(--chip);
}

.blank-bar-fill {
  display: block;
  height: 100%;
  border-radius: 0.125rem;
  background: var(--text-tertiary);
  opacity: 0.55;
}

.blank-empty {
  padding: 1rem 0.5rem;
  color: var(--text-tertiary);
  font-size: var(--text-13);
}
</style>
