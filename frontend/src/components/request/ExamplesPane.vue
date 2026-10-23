<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import IconButton from '../ui/IconButton.vue'
import ExampleForm from './ExampleForm.vue'
import ExampleLive from './ExampleLive.vue'
import type { RequestSource } from '../../lib/requestSource'
import { exampleName, exampleOf, isKeepable } from '../../lib/examples'
import { formatBytes, formatMicros, t } from '../../i18n'
import type { ResponseExample } from '../../../bindings/schemo/internal/domain'

// The answers kept beside a request: what the server last said, read-only, and the examples (what
// an import carried in and what a send was saved as). The list is here; the last answer is read in
// ExampleLive and an example is written in ExampleForm.
const { source } = defineProps<{ source: RequestSource }>()


// Which of the two is on the right: the last answer, or the example picked. By index, because an
// example is a value of the list and not a thing with a name of its own.
const picked = ref(-1)
const live = computed(() => picked.value < 0)
const example = computed<ResponseExample | null>(() => source.responses[picked.value] ?? null)
// The index of the example just made, whose name is the first thing to write.
const fresh = ref(-1)

// The list can lose rows from where this is not looking — a delete here, a new example from the
// answer's own toolbar — and the index has to stay an index of something.
watch(
  () => source.responses.length,
  (len) => {
    if (picked.value >= len) picked.value = len ? len - 1 : -1
  }
)

const record = computed(() => source.record)

const liveMeta = computed(() => {
  const now = record.value
  if (!now) return t('request.examples.notSent')
  return `${formatMicros(now.durationUs)} · ${formatBytes(now.responseBytes ?? 0)}`
})

function toExamples() {
  const now = record.value
  if (now && isKeepable(now)) addExample(exampleOf(now, exampleName(now.status)))
}

function addExample(one: ResponseExample) {
  const list = [...source.responses, one]
  source.saveExamples(list)
  picked.value = list.length - 1
  fresh.value = picked.value
}

function blankExample() {
  addExample({
    name: '',
    status: 200,
    body: '',
    headers: [{ name: 'Content-Type', value: 'application/json; charset=utf-8' }],
  })
}

function patch(fields: Partial<ResponseExample>) {
  source.saveExamples(source.responses.map((one, i) => (i === picked.value ? { ...one, ...fields } : one)))
}

function removeExample() {
  const index = picked.value
  const list = source.responses.filter((_, i) => i !== index)
  const next = index + 1 < source.responses.length ? index : index - 1
  source.saveExamples(list)
  picked.value = list.length ? Math.min(next, list.length - 1) : -1
  fresh.value = -1
}
</script>

<template>
  <div class="examples">
    <div class="ex-list">
      <div class="ex-live" :class="{ on: live }" @click="picked = -1">
        <span class="ex-slot">
          <Spinner v-if="source.loading" class="text-accent" />
          <StatusBadge v-else-if="record" :status="record.status" />
          <span v-else class="ex-blank"></span>
        </span>
        <span class="ex-live-text">
          <span class="ex-live-name">{{ t('request.examples.liveTitle') }}</span>
          <span class="ex-live-meta">
            {{ source.loading ? t('request.examples.sending') : liveMeta }}
          </span>
        </span>
      </div>

      <div class="ex-head">
        <span class="ex-head-label">{{ t('request.chips.examples') }}</span>
        <IconButton
          variant="bare"
          size="md"
          :hint="t('request.examples.createBlank')"
          @click="blankExample"
        >
          <Icon name="Plus" :size="14" :stroke-width="2.2" />
        </IconButton>
      </div>

      <button
        v-for="(one, i) in source.responses"
        :key="i"
        type="button"
        class="ex-row"
        :class="{ on: i === picked }"
        @click="picked = i"
      >
        <span class="ex-slot">
          <StatusBadge :status="one.status ?? 0" />
        </span>
        <span class="ex-name" :class="{ anon: !one.name }">
          {{ one.name || t('request.examples.untitled') }}
        </span>
      </button>

      <span v-if="!source.responses.length" class="ex-hint">
        {{ t('request.examples.panelEmpty') }}
      </span>
    </div>

    <div class="ex-main">
      <ExampleLive
        v-if="live"
        :source
        :meta="liveMeta"
        @keep="toExamples"
        @blank="blankExample"
      />
      <ExampleForm
        v-else-if="example"
        :key="`${picked}:${source.responses.length}`"
        :example
        :fresh="picked === fresh"
        @patch="patch"
        @remove="removeExample"
      />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.examples {
  @apply grid flex-1 min-h-0;
  grid-template-columns: 15rem minmax(0, 1fr);
  min-height: 14rem;
}

.ex-list {
  @apply flex flex-col min-h-0 overflow-auto;
  padding: 0.5rem;
  gap: 0.0625rem;
  border-right: 1px solid var(--border);
}

.ex-live {
  @apply flex items-center flex-none cursor-default;
  height: 2.5rem;
  gap: 0.5rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
}

.ex-live:hover,
.ex-live.on {
  background: var(--bg-hover);
}

.ex-slot {
  @apply flex justify-center flex-none;
  width: 2.125rem;
}

.ex-blank {
  width: 1.625rem;
  height: 1.125rem;
  border-radius: 0.3125rem;
  box-sizing: border-box;
  box-shadow: inset 0 0 0 1px var(--border);
}

.ex-live-text {
  @apply flex flex-col min-w-0;
  line-height: 1.25;
}

.ex-live-name {
  @apply truncate;
  font-size: var(--text-13);
}

.ex-live-meta {
  @apply truncate text-text-tertiary;
  font-size: var(--text-11-5);
}

.ex-head {
  @apply flex items-center flex-none;
  height: 1.875rem;
  gap: 0.3rem;
  padding: 0.5rem 0.3rem 0 0.5rem;
  margin-top: 0.3rem;
  border-top: 1px solid var(--border);
}

.ex-head-label {
  @apply flex-1 text-text-tertiary;
  font-size: var(--text-11-5);
}

.ex-row {
  @apply flex items-center flex-none cursor-default border-0 bg-transparent text-left;
  height: 2rem;
  gap: 0.5rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  font-family: inherit;
}

.ex-row:hover,
.ex-row.on {
  background: var(--bg-hover);
}

.ex-row.on .ex-name {
  font-weight: 500;
}

.ex-name {
  @apply min-w-0 truncate;
  font-size: var(--text-13);
  color: var(--text);
}

.ex-name.anon {
  color: var(--text-tertiary);
}

.ex-hint {
  padding: 0.25rem 0.5rem;
  font-size: var(--text-12);
  color: var(--text-tertiary);
  line-height: 1.45;
  text-wrap: pretty;
}

.ex-main {
  @apply flex flex-col min-w-0 min-h-0;
}
</style>
