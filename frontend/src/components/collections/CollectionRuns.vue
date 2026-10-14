<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { useCollectionsStore } from '../../stores/collections'
import { findNode } from '../../lib/collectionTree'
import { formatDate, formatMicros, useMessages } from '../../i18n'
import { methodInkClass } from '../../lib/format'
import type { CollectionRun, CollectionRunResult } from '../../../bindings/schemo/internal/domain'

// How the collection's last run went, request by request: what each one answered, how long it took,
// and what its tests said. A run that broke a check says so in its own row.
const { run } = defineProps<{ run: CollectionRun | null; levelId: string }>()

const { t } = useMessages()
const store = useCollectionsStore()

const results = computed(() =>
  [...(run?.results ?? [])].sort((a, b) => a.position - b.position)
)

const passedCount = computed(() => results.value.filter((one) => one.ok).length)
const allGood = computed(() => !!run && passedCount.value === results.value.length)

function nameOf(result: CollectionRunResult): string {
  return findNode(store.tree, result.nodeId)?.name ?? t('collections.deletedRequest')
}

function methodOf(result: CollectionRunResult): string {
  return findNode(store.tree, result.nodeId)?.method ?? ''
}

function whenOf(at: number): string {
  return at ? formatDate(at, { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' }) : ''
}

function failed(result: CollectionRunResult): boolean {
  if (result.skipped) return false
  return !result.ok || result.assertionsPassed < result.assertionsTotal
}

function statusOf(result: CollectionRunResult): string {
  if (result.skipped) return t('collections.skippedShort')
  if (result.error) return t('collections.failed')
  return result.status ? String(result.status) : '—'
}

function testsOf(result: CollectionRunResult): string {
  return result.assertionsTotal
    ? t('collections.ofTotal', { passed: result.assertionsPassed, total: result.assertionsTotal })
    : ''
}

function open(result: CollectionRunResult) {
  void store.openRunResult(result)
}
</script>

<template>
  <div v-if="!run" class="runs-empty">{{ t('collections.neverRan') }}</div>

  <div v-else class="runs">
    <div class="run-list">
      <div class="run-card selected">
        <span class="run-line">
          <span class="run-when">{{ whenOf(run.finishedAt || run.startedAt) }}</span>
          <span class="run-res" :class="{ bad: !allGood }">{{ passedCount }} / {{ results.length }}</span>
        </span>
        <span class="run-note">{{ run.environment || t('titlebar.noEnvironment') }} · {{ formatMicros(run.durationUs) }}</span>
      </div>
    </div>

    <div class="run-detail">
      <div class="run-head">
        <span class="run-title">{{ whenOf(run.finishedAt || run.startedAt) }}</span>
        <span class="run-chip" :class="allGood ? 'is-ok' : 'is-bad'">
          {{ t('collections.ofRuns', { passed: passedCount, total: results.length }) }}
        </span>
        <span class="run-meta">{{ run.environment || t('titlebar.noEnvironment') }} · {{ formatMicros(run.durationUs) }}</span>
        <span class="run-gap"></span>
      </div>

      <div class="rows">
        <button
          v-for="result in results"
          :key="`${result.nodeId}-${result.position}`"
          type="button"
          class="row px-3"
          @click="open(result)"
        >
          <span class="dot" :class="failed(result) ? 'bad' : 'ok'">
            <Icon :name="failed(result) ? 'X' : 'Check'" :size="8" :stroke-width="4" />
          </span>
          <span class="method" :class="methodInkClass(methodOf(result))">{{ methodOf(result) }}</span>
          <span class="name">{{ nameOf(result) }}</span>
          <span class="status" :class="failed(result) ? 'bad' : 'plain'">{{ statusOf(result) }}</span>
          <span class="time">{{ result.durationUs ? formatMicros(result.durationUs) : '' }}</span>
          <span class="tests">{{ testsOf(result) }}</span>
        </button>
      </div>

      <div v-if="!results.length" class="runs-empty">{{ t('collections.neverRan') }}</div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.runs {
  @apply grid flex-1 min-h-0;
  grid-template-columns: minmax(11.25rem, 17.5rem) minmax(0, 1fr);
}

.run-list {
  @apply flex flex-col min-h-0 overflow-y-auto;
  padding: 0.625rem 0.5rem;
  gap: 2px;
  border-right: 1px solid var(--border);
}

.run-card {
  @apply flex flex-col cursor-default;
  padding: 0.5625rem 0.75rem;
  gap: 3px;
  border-radius: 0.5rem;
}

.run-card:hover {
  background: var(--bg-hover);
}

.run-card.selected {
  background: var(--bg-hover);
}

.run-line {
  @apply flex items-baseline justify-between;
  gap: 0.5rem;
}

.run-when {
  @apply font-medium;
}

.run-res {
  @apply font-semibold tabular-nums;
  color: var(--green-text);
}

.run-res.bad {
  color: var(--red-text);
}

.run-note {
  @apply text-12 text-text-tertiary;
}

.run-detail {
  @apply flex flex-col min-h-0 overflow-y-auto;
  padding: 1rem 1.5rem;
}

.run-head {
  @apply flex items-center;
  gap: 0.625rem;
  padding-bottom: 0.75rem;
}

.run-title {
  @apply flex-none whitespace-nowrap font-semibold;
  font-size: var(--text-14);
}

.run-chip {
  @apply flex-none whitespace-nowrap font-semibold;
  font-size: var(--text-11-5);
  padding: 0.125rem 0.4375rem;
  border-radius: 0.3125rem;
}

.run-chip.is-ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.run-chip.is-bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.run-meta {
  @apply min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12-5);
}

.run-gap {
  @apply flex-1;
}

.rows {
  @apply flex flex-col;
}

.row {
  display: grid;
  grid-template-columns: 1.75rem 3.75rem minmax(7.5rem, 1fr) 2.75rem 4rem 2.5rem;
  align-items: center;
  height: 2.5rem;
  border: 0;
  border-top: 1px solid var(--line2);
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: default;
}

.row:hover {
  background: var(--bg-hover);
}

.dot {
  @apply flex items-center justify-center rounded-full;
  width: 1rem;
  height: 1rem;
  color: #fff;
}

.dot.ok {
  background: var(--green);
}

.dot.bad {
  background: var(--red);
}

.method {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
}

.name {
  @apply truncate;
}

.status {
  font-family: var(--mono);
  font-size: var(--text-12);
}

.status.plain {
  color: var(--text-secondary);
}

.status.bad {
  color: var(--red-text);
}

.time {
  @apply tabular-nums;
  text-align: right;
  color: var(--text-secondary);
}

.tests {
  @apply tabular-nums text-text-tertiary;
  text-align: right;
}

.runs-empty {
  @apply text-text-tertiary;
  padding: 2.5rem 1.5rem;
  max-width: 32.5rem;
  line-height: 1.55;
}
</style>
