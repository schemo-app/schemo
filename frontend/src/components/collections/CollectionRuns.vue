<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger, PopoverClose, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { Runs } from '../../../bindings/schemo/internal/request'
import type { CollectionRun, CollectionRunResult } from '../../../bindings/schemo/internal/domain'
import { useCollectionsStore } from '../../stores/collections'
import { useWorkspaceSnapshot } from '../../composables/useWorkspaceSnapshot'
import { callOrToast } from '../../lib/calls'
import { findNode } from '../../lib/collectionTree'
import { methodInkClass } from '../../lib/format'
import { formatDate, formatMicros, t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import CollectionRunChecks from './CollectionRunChecks.vue'

const { run, runs, levelId, loading, canRerun } = defineProps<{
  run: CollectionRun | null
  runs: CollectionRun[]
  levelId: string
  loading: boolean
  canRerun: boolean
}>()
const emit = defineEmits<{ rerun: [] }>()

const collections = useCollectionsStore()
const chosen = ref('')
const errorsOnly = ref(false)
const detail = useWorkspaceSnapshot<CollectionRun | null>(null)

const available = computed(() => {
  if (!run) return runs
  const saved = runs.some(item => item.id === run.id)
  if (saved) return runs.map(item => item.id === run.id ? run : item)
  return [run, ...runs]
})
const selection = computed(() => available.value.find(item => item.id === chosen.value) ?? available.value[0])
const selectedIndex = computed(() => available.value.findIndex(item => item.id === selection.value?.id) + 1)
const current = computed(() => selection.value?.id === run?.id ? run : detail.state.value)
const inProgress = computed(() => !!collections.running && run?.id === selection.value?.id && !run?.finishedAt)

watch(() => run?.id, (id) => {
  if (id) chosen.value = ''
})

watch(
  () => selection.value?.id === run?.id ? '' : selection.value?.id,
  (id, _previous, onCleanup) => {
    onCleanup(() => detail.reset())
    if (id) void callOrToast(detail.take(Runs.Read(levelId, '', id)), 'collections.readFailed')
  },
  { immediate: true }
)

const results = computed(() => {
  const ordered = [...(current.value?.results ?? [])].sort((a, b) => a.position - b.position)
  return ordered.map(result => {
    const request = findNode(collections.tree, result.nodeId)
    return {
      result,
      name: request?.name ?? t('collections.deletedRequest'),
      method: request?.method ?? '',
      failed: !result.skipped && (!result.ok || result.assertionsPassed < result.assertionsTotal),
    }
  })
})
const errorCount = computed(() => results.value.filter(item => item.failed).length)
const visible = computed(() => errorsOnly.value ? results.value.filter(item => item.failed) : results.value)

function whenOf(item: CollectionRun): string {
  return formatDate(item.finishedAt || item.startedAt, { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })
}

function metaOf(item: CollectionRun): string {
  return [item.environment || t('titlebar.noEnvironment'), item.durationUs ? formatMicros(item.durationUs) : ''].filter(Boolean).join(' · ')
}

function summaryOf(item: CollectionRun): string {
  const sent = item.results?.filter(result => !result.skipped)
  const passed = sent ? sent.filter(result => result.ok).length : item.passed
  return t('collections.ofRuns', { passed, total: sent?.length ?? item.passed + item.failed })
}

function open(result: CollectionRunResult) {
  void collections.openRunResult(result)
}
</script>

<template>
  <div v-if="!selection" class="runs-empty">{{ loading ? t('collections.loadingRuns') : t('collections.neverRan') }}</div>
  <div v-else class="runs">
    <div class="run-head">
      <DropdownMenuRoot>
        <DropdownMenuTrigger as-child>
          <button type="button" class="run-select" :aria-label="t('collections.chooseRun')" :title="t('collections.chooseRun')">
            <span class="truncate font-medium">{{ whenOf(selection) }}</span>
            <span class="run-meta">{{ t('collections.ofTotal', { passed: selectedIndex, total: available.length }) }}</span>
            <Icon name="ChevronDown" :size="10" :stroke-width="1.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" class="run-menu">
          <DropdownMenuItem
            v-for="item in available"
            :key="item.id"
            class="run-menu-item"
            :class="{ selected: item.id === selection.id }"
            @select="chosen = item.id"
          >
            <span class="run-dot" :class="{ bad: item.failed }"></span>
            <span class="run-option">
              <span class="truncate font-medium">{{ whenOf(item) }}</span>
              <span class="run-meta truncate">{{ metaOf(item) }}</span>
            </span>
            <span class="run-meta">{{ summaryOf(item) }}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuRoot>
      <span class="run-chip" :class="selection.failed || errorCount ? 'bad' : 'ok'">{{ summaryOf(current ?? selection) }}</span>
      <span class="run-meta run-environment">{{ metaOf(selection) }}</span>
      <span class="flex-1 min-w-0"></span>
      <button
        type="button"
        class="run-errors"
        :aria-pressed="errorsOnly"
        :class="{ selected: errorsOnly, quiet: !errorCount && !errorsOnly }"
        :title="t('collections.failedTestsOnly')"
        @click="errorsOnly = !errorsOnly"
      >
        <span class="run-dot bad"></span>{{ t('history.errors') }} <span class="run-meta">{{ errorCount }}</span>
      </button>
      <button type="button" class="run-repeat" :disabled="!canRerun" @click="emit('rerun')">{{ t('response.repeat') }}</button>
    </div>

    <div class="run-columns">
      <span>{{ t('collections.columnMethod') }}</span>
      <span>{{ t('collections.columnName') }}</span>
      <span>{{ t('collections.columnTests') }}</span>
      <span>{{ t('collections.columnResponse') }}</span>
      <span class="text-right">{{ t('history.colTime') }}</span>
    </div>

    <div v-for="{ result, method, name, failed } in visible" :key="result.position" class="run-row" @click="open(result)">
      <span class="font-mono text-12 font-bold" :class="methodInkClass(method)">{{ method }}</span>
      <button type="button" class="run-request" :title="name" @click.stop="open(result)">{{ name }}</button>
      <span class="run-tests">
        <PopoverRoot v-if="failed && (result.recordId || result.error)">
          <PopoverTrigger as-child>
            <button type="button" class="run-test-button" :title="t('collections.showTests')" :aria-label="t('collections.showTests')" @click.stop>
              <span class="run-dot bad"></span>
              <span>{{ result.assertionsTotal ? t('collections.ofTotal', { passed: result.assertionsPassed, total: result.assertionsTotal }) : t('collections.failed') }}</span>
              <Icon name="ChevronDown" :size="10" :stroke-width="1.5" />
            </button>
          </PopoverTrigger>
          <PopoverContent :aria-label="t('collections.showTests')">
            <div class="run-test-popover" @click.stop>
              <div class="run-test-details">
                <CollectionRunChecks v-if="result.recordId" :record-id="result.recordId" />
                <p v-if="result.error" class="run-test-error run-error-message">{{ result.error }}</p>
              </div>
              <PopoverClose as-child>
                <button type="button" class="run-open" @click="open(result)">{{ t('collections.openRunRequest') }}</button>
              </PopoverClose>
            </div>
          </PopoverContent>
        </PopoverRoot>
        <span v-else-if="result.assertionsTotal || failed" class="run-test-summary">
          <span class="run-dot" :class="{ bad: failed }"></span>
          {{ result.assertionsTotal ? t('collections.ofTotal', { passed: result.assertionsPassed, total: result.assertionsTotal }) : t('collections.failed') }}
        </span>
        <span v-else class="run-meta">—</span>
      </span>
      <span>
        <StatusBadge v-if="result.status" :status="result.status" />
        <span v-else class="run-meta" :class="{ 'run-test-error': failed }">
          {{ result.skipped ? t('collections.skippedShort') : failed ? t('collections.failed') : '—' }}
        </span>
      </span>
      <span class="run-meta text-right tabular-nums">{{ result.durationUs ? formatMicros(result.durationUs) : '—' }}</span>
    </div>
    <div v-if="!visible.length" class="runs-empty">
      {{ t(inProgress ? 'collections.runInProgress' : detail.loading.value ? 'collections.loadingRuns' : errorsOnly ? 'common.nothingFound' : 'collections.noRunResults') }}
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.runs {
  @apply flex flex-col flex-1 min-h-0 overflow-auto;
  padding: 1.25rem 1.5rem 1.5rem;
}

.run-head {
  @apply flex items-center flex-wrap min-w-0 gap-2.5;
  padding-bottom: 0.625rem;
}

.run-select,
.run-repeat,
.run-errors {
  @apply inline-flex flex-none items-center min-w-0 gap-2 border-0 bg-transparent text-text cursor-pointer;
  height: 1.875rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  box-shadow: inset 0 0 0 1px var(--border);
  font: inherit;
}

.run-select {
  flex-shrink: 1;
}

.run-repeat:disabled {
  opacity: 0.45;
  cursor: default;
}

.run-errors {
  height: 1.75rem;
  border-radius: 0.875rem;
  color: var(--text-secondary);
}

.run-errors.quiet {
  opacity: 0.5;
}

.run-select:hover,
.run-repeat:hover:not(:disabled),
.run-errors:hover,
.run-errors.selected,
.run-menu-item.selected {
  background: var(--bg-hover);
}

.run-menu {
  width: 18.75rem;
  max-height: 21.25rem;
  overflow-y: auto;
}

.run-option {
  @apply flex flex-col flex-1 min-w-0;
  gap: 0.125rem;
}

.run-dot {
  @apply shrink-0 rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--green);
}

.run-dot.bad {
  background: var(--red);
}

.run-chip {
  @apply whitespace-nowrap text-12 font-semibold;
  padding: 0.125rem 0.4375rem;
  border-radius: 0.3125rem;
}

.run-chip.ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.run-chip.bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.run-meta {
  @apply text-12 text-text-tertiary;
}

.run-environment {
  @apply truncate;
}

.run-columns,
.run-row {
  display: grid;
  grid-template-columns: 3.5rem minmax(0, 0.6fr) minmax(0, 1fr) 5rem 6rem;
  column-gap: clamp(0.375rem, 2vw, 1.5rem);
  align-items: center;
  padding: 0 0.625rem;
  min-width: 25rem;
}

.run-columns {
  @apply text-12 text-text-tertiary;
  height: 1.875rem;
  margin-bottom: 0.25rem;
  border-bottom: 1px solid var(--border);
}

.run-row {
  @apply rounded-lg cursor-pointer;
  height: 2.25rem;
}

.run-row:hover {
  background: var(--bg-hover);
}

.run-request {
  @apply truncate min-w-0 cursor-pointer text-left border-0 bg-transparent text-text p-0 font-medium;
}

.run-tests {
  @apply min-w-0;
}

.run-test-summary,
.run-test-button {
  @apply inline-flex items-center gap-2 whitespace-nowrap tabular-nums text-text-secondary;
}

.run-test-button {
  @apply cursor-pointer rounded-md border-0 bg-transparent text-red p-0;
  min-height: 1.5rem;
  padding-inline: 0.375rem;
  margin-left: -0.375rem;
}

.run-test-button:hover,
.run-test-button[data-state="open"] {
  background: var(--bg-active);
}

.run-test-popover {
  @apply flex flex-col;
  width: min(20rem, var(--reka-popover-content-available-width, 100vw));
  max-height: var(--reka-popover-content-available-height, 100vh);
  padding: 0.375rem;
}

.run-test-details {
  @apply min-h-0 overflow-y-auto;
}

.run-error-message {
  @apply px-2.5 py-1.5 text-12 select-text;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.run-open {
  @apply shrink-0 text-left rounded-md w-full cursor-pointer border-0 bg-transparent text-text;
  min-height: 2rem;
  padding: 0 0.625rem;
}

.run-open:hover {
  background: var(--bg-hover);
}

.run-test-error {
  color: var(--red-text);
}

.runs-empty {
  @apply text-text-tertiary;
  padding: 2.5rem 1.5rem;
}
</style>
