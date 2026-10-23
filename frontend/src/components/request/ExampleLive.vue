<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '../ui/Icon.vue'
import EmptyState from '../ui/EmptyState.vue'
import Spinner from '../ui/Spinner.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import HeaderTable from '../response/HeaderTable.vue'
import ExampleView from './ExampleView.vue'
import type { RequestSource } from '../../lib/requestSource'
import { isKeepable } from '../../lib/examples'
import { formatStatus } from '../../lib/format'
import { t } from '../../i18n'

// What the server last said to this request, as it is read: the status, the body and the headers,
// with the way to keep it as an example. There is nothing to write here, and it says so.
const { source, meta } = defineProps<{ source: RequestSource; meta: string }>()

const emit = defineEmits<{ (e: 'keep'): void; (e: 'blank'): void }>()


const pane = ref<'body' | 'headers'>('body')
const record = computed(() => source.record)

const statusLine = computed(() =>
  record.value ? formatStatus(record.value.status, record.value.statusText) : ''
)
</script>

<template>
  <div v-if="source.loading" class="wait">
    <Spinner class="text-accent" />
    {{ t('request.examples.waiting') }}
  </div>

  <ExampleView
    v-else-if="record"
    v-model:pane="pane"
    live
    :count="record.responseHeaders?.length"
  >
    <template #title>
      <span class="title">{{ t('request.examples.liveTitle') }}</span>
      <StatusBadge :status="record.status">{{ statusLine }}</StatusBadge>
      <span class="flex-1"></span>
      <button v-if="isKeepable(record)" type="button" class="keep" @click="emit('keep')">
        <Icon name="BookmarkPlus" :size="14" :stroke-width="1.8" />
        <span class="cap-line">{{ t('request.examples.toExamples') }}</span>
      </button>
    </template>

    <template #bar>
      <span class="meta">{{ meta }}</span>
      <span class="flex-1"></span>
      <span class="ro">{{ t('request.examples.readOnly') }}</span>
    </template>

    <div v-if="pane === 'body'" class="body">
      <pre v-if="record.responseBody" class="pre">{{ record.responseBody }}</pre>
      <span v-else class="note">{{ t('request.examples.bodyEmpty') }}</span>
    </div>
    <div v-else class="heads">
      <div class="head-row">
        <span>{{ t('request.examples.columnName') }}</span>
        <span>{{ t('request.examples.columnValue') }}</span>
      </div>
      <HeaderTable
        :headers="(record.responseHeaders ?? []).map((header) => ({ name: header.name, value: header.value }))"
        :empty="t('request.examples.noHeaders')"
      />
    </div>
  </ExampleView>

  <EmptyState
    v-else
    :title="t('request.examples.liveEmptyTitle')"
    :text="t('request.examples.liveEmptyHint')"
  >
    <button type="button" class="blank" @click="emit('blank')">
      <Icon name="Plus" :size="12" :stroke-width="2.2" />
      <span class="cap-line">{{ t('request.examples.createBlank') }}</span>
    </button>
  </EmptyState>
</template>

<style scoped>
@reference "../../style.css";

.wait {
  @apply flex flex-1 items-center justify-center;
  gap: 0.625rem;
  color: var(--text-secondary);
  font-size: var(--text-13);
}

.title {
  @apply min-w-0 truncate font-semibold;
  font-size: var(--text-15);
}

.keep {
  @apply flex items-center flex-none cursor-pointer border-0 text-text whitespace-nowrap;
  height: 1.75rem;
  gap: 0.3rem;
  padding: 0 0.625rem 0 0.5rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  font-family: inherit;
  font-size: var(--text-12-5);
}

.keep:hover {
  background: var(--bg-hover);
}

.meta {
  @apply whitespace-nowrap text-text-secondary;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.ro {
  @apply whitespace-nowrap text-text-tertiary;
  font-size: var(--text-12);
}

.body {
  @apply flex flex-1 min-h-0;
  padding: 0 1.25rem 1rem;
}

.pre {
  @apply m-0 h-full w-full overflow-auto;
  box-sizing: border-box;
  border-radius: 0.5625rem;
  background: var(--raise);
  color: var(--text);
  padding: 0.75rem 0.875rem;
  font-family: var(--mono);
  font-size: var(--text-12-5);
  line-height: 1.3125rem;
  white-space: pre;
}

.note {
  padding: 0.75rem 0.125rem;
  color: var(--text-tertiary);
  font-size: var(--text-13);
}

.heads {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0 1.25rem 1rem;
}

.head-row {
  @apply grid items-center;
  grid-template-columns: minmax(8.75rem, 13.75rem) minmax(0, 1fr);
  height: 1.875rem;
  border-bottom: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-11-5);
}

.head-row span:first-child {
  padding-left: 0.5rem;
}

.blank {
  @apply flex items-center cursor-pointer border-0 text-text;
  height: 1.875rem;
  gap: 0.3rem;
  padding: 0 0.75rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  margin-top: 0.3rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.blank:hover {
  background: var(--bg-hover);
}
</style>
