<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { useCollectionsStore } from '../../stores/collections'
import { findCollection, findNode } from '../../lib/collectionTree'
import { formatMicros, useMessages } from '../../i18n'
import type { ScriptRun, ScriptScope } from '../../../bindings/schemo/internal/domain'

// What the scripts around this request came to: every test they declared, and everything they
// printed. A run that broke a check says so in its own row.
const { runs, loading } = defineProps<{ runs: ScriptRun[]; loading: boolean }>()

const store = useCollectionsStore()
const { t } = useMessages()

// Which level a report came from. A report keeps the node id and not the node, so the name is the
// tree's answer — and a level deleted since the run has none.
function levelName(run: ScriptRun): string {
  const id = run.nodeId ?? ''
  return findNode(store.tree, id)?.name ?? findCollection(store.tree, id)?.name ?? ''
}

// Held to the scopes the core declares, so a third one is a failure here rather than a tab drawing
// a raw word.
const SCOPE_LABELS = computed<Record<string, string>>(() => ({
  pre: t('response.scripts.pre'),
  post: t('response.scripts.post'),
} satisfies Record<Exclude<`${ScriptScope}`, ''>, string>))

// A check that failed is what the tab is for: an ordinary run of a script with no checks says nothing
// here, and the line above each script is what says it ran at all.
function hasChecks(run: ScriptRun): boolean {
  return (run.tests ?? []).length > 0 || (run.logs ?? []).length > 0 || !run.ok || Boolean(run.error)
}

function count(run: ScriptRun, passed: boolean): number {
  return (run.tests ?? []).filter((test) => test.passed === passed).length
}

// What a test's row says beside its name: the level its script belongs to.
function srcOf(run: ScriptRun): string {
  return levelName(run) || t('response.scripts.requestSrc')
}

// How long one check took, when the clock saw it.
function msOf(test: { durationUs: number }): string {
  return test.durationUs > 0 ? formatMicros(test.durationUs) : ''
}
</script>

<template>
  <div v-if="runs.length === 0 && !loading" class="none">
    <span class="none-title">{{ t('response.scripts.none') }}</span>
    <span class="none-hint">{{ t('response.scripts.noneHint') }}</span>
  </div>

  <div v-else class="scripts">
    <div v-if="loading" class="loading">{{ t('response.scripts.loading') }}</div>

    <template v-else>
      <section v-for="run in runs" :key="run.id" class="run">
        <div class="run-head">
          <template v-if="run.tests?.length">
            <span class="pill ok">{{ t('response.scripts.passed', count(run, true)) }}</span>
            <span class="pill" :class="count(run, false) ? 'bad' : 'chip'">
              {{ t('response.scripts.failed', count(run, false)) }}
            </span>
          </template>
          <span class="run-caption">{{ SCOPE_LABELS[run.scope] ?? run.scope }}</span>
        </div>

        <div v-if="run.error" class="run-error mono">{{ run.error }}</div>

        <div v-if="run.tests?.length" class="tests">
          <div v-for="(test, i) in run.tests" :key="i" class="test-wrap">
            <div class="test">
              <span class="test-mark" :class="test.passed ? 'ok' : 'bad'">
                <Icon :name="test.passed ? 'Check' : 'X'" :size="8" :stroke-width="4" />
              </span>
              <span class="test-name cap-line">{{ test.name }}</span>
              <span class="test-src">{{ srcOf(run) }}</span>
              <span class="test-ms">{{ msOf(test) }}</span>
            </div>
            <div v-if="!test.passed && test.error" class="test-fail">{{ test.error }}</div>
          </div>
        </div>

        <div v-if="run.logs?.length" class="console">
          <span class="console-label">{{ t('response.scripts.console') }}</span>
          <div class="console-box mono">
            <div v-for="(line, i) in run.logs" :key="i" class="console-line" :class="line.level">
              {{ line.message }}
            </div>
          </div>
        </div>

        <div v-if="!hasChecks(run)" class="run-quiet">{{ t('response.scripts.quiet') }}</div>
      </section>
    </template>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.scripts {
  @apply flex flex-col;
  padding: 0.875rem 1.25rem 1.25rem;
  gap: 0.875rem;
}

.run {
  @apply flex flex-col;
  gap: 0.875rem;
}

.run-head {
  @apply flex items-center;
  gap: 0.5rem;
}

.pill {
  @apply font-semibold;
  font-size: var(--text-12);
  padding: 0.1875rem 0.5625rem;
  border-radius: 0.375rem;
}

.pill.ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.pill.bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.pill.chip {
  @apply text-text-tertiary;
  background: var(--chip);
}

.run-caption {
  @apply min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12-5);
}

.run-error {
  @apply text-13;
  color: var(--red-text);
}

.tests {
  @apply flex flex-col;
}

.test-wrap {
  @apply flex flex-col;
  border-bottom: 1px solid var(--line2);
}

.test {
  @apply flex items-center;
  height: 2.5rem;
  gap: 0.625rem;
}

.test-mark {
  @apply flex items-center justify-center flex-none rounded-full;
  width: 1rem;
  height: 1rem;
  color: #fff;
}

.test-mark.ok {
  background: var(--green);
}

.test-mark.bad {
  background: var(--red);
}

.test-name {
  @apply flex-1 min-w-0 truncate;
}

.test-src {
  @apply flex-none whitespace-nowrap text-text-tertiary;
  font-size: var(--text-12);
}

.test-ms {
  @apply flex-none whitespace-nowrap text-text-tertiary;
  width: 2.5rem;
  text-align: right;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.test-fail {
  @apply whitespace-pre-wrap break-words;
  margin: 0 0 0.625rem 1.625rem;
  padding: 0.5rem 0.75rem;
  border-radius: 0.5rem;
  background: var(--red-soft);
  font-size: var(--text-13);
}

.console {
  @apply flex flex-col;
  gap: 0.375rem;
}

.console-label {
  @apply font-semibold text-text-tertiary;
  font-size: var(--text-12);
}

.console-box {
  padding: 0.625rem 0.75rem;
  border-radius: 0.5625rem;
  background: var(--raise);
  font-size: var(--text-12-5);
  line-height: 21px;
}

.console-line {
  @apply whitespace-pre-wrap break-words text-text-secondary;
}

.console-line::before {
  content: '→ ';
}

.console-line.warn {
  @apply text-orange;
}

.console-line.error {
  @apply text-red;
}

.run-quiet {
  @apply text-13 text-text-tertiary;
}

.none {
  @apply flex flex-col;
  padding: 1.25rem;
  gap: 0.375rem;
  max-width: 32.5rem;
  line-height: 1.55;
}

.none-title {
  @apply font-semibold;
}

.none-hint {
  @apply text-text-tertiary;
}

.loading {
  @apply text-13 text-text-tertiary;
}
</style>
