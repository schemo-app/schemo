<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import type { Protocol, StreamMessage } from '../../../bindings/schemo/internal/domain'
import { useStreamTranscript } from '../../composables/useStreamTranscript'
import { formatBytes, formatClock, formatDuration, t } from '../../i18n'
import type { RecordView } from '../../lib/requestRecord'
import { formatStatus, statusBadgeClass } from '../../lib/format'
import { protocolOf } from '../../lib/protocol'
import Button from '../ui/Button.vue'
import IconButton from '../ui/IconButton.vue'
import Icon from '../ui/Icon.vue'
import RequestShortcuts from '../request/RequestShortcuts.vue'

const { protocol, record, attemptId, loading } = defineProps<{
  protocol: Protocol
  record: RecordView | null
  attemptId: string | null
  loading: boolean
}>()
const emit = defineEmits<{ disconnect: [] }>()
const query = ref('')
const searching = ref(false)
const searchInput = useTemplateRef<HTMLInputElement>('searchInput')
const direction = ref('')
const selected = ref<StreamMessage | null>(null)
const cleared = ref('')
const stream = computed(() => protocolOf(protocol).stream)
const activeId = computed(() => attemptId ?? (record?.active ? record.id : null))
const current = computed(() => {
  if (!record || record.protocol !== protocol || cleared.value === record.id) return null
  return loading && activeId.value !== record.id ? null : record
})
const { connected, messages, count, now } = useStreamTranscript(activeId, current)
const connecting = computed(() => loading && activeId.value !== record?.id && !connected.value)
const duration = computed(() => {
  if (!current.value) return ''
  if (!current.value.active && !current.value.status) return formatDuration(current.value.durationUs)
  const elapsed = current.value.active ? now.value - current.value.startedAt : current.value.durationUs / 1000
  const seconds = Math.max(0, Math.floor(elapsed / 1000))
  return [Math.floor(seconds / 60), seconds % 60].map(value => String(value).padStart(2, '0')).join(':')
})
const state = computed(() => {
  if (stream.value && (connected.value || current.value?.active)) return t(stream.value.opened)
  if (connecting.value) return t('request.protocol.connecting')
  if (current.value && stream.value) return t(stream.value.closed)
  return t('request.protocol.offline')
})
const rows = computed(() => {
  const search = query.value.trim().toLowerCase()
  return messages.value.filter(message => (!direction.value || direction.value === message.direction) &&
    (!search || [message.name, message.id, message.data].some(text => text?.toLowerCase().includes(search))))
})
const filters = computed(() => [
  { value: 'in', label: t('request.protocol.received') },
  { value: 'out', label: t('request.protocol.sent') },
].map(filter => ({ ...filter, count: messages.value.filter(message => message.direction === filter.value).length })))
const searchCount = computed(() => query.value.trim()
  ? t('request.protocol.logMatches', { shown: rows.value.length, total: messages.value.length }) : '')
const emptyHint = computed(() => {
  if (count.value) return t('common.nothingFound')
  if (stream.value && (loading || current.value)) return t(stream.value.empty)
  return ''
})
const selectedText = computed(() => {
  if (!selected.value) return ''
  try {
    return JSON.stringify(JSON.parse(selected.value.data), null, 2)
  } catch {
    return selected.value.data
  }
})

watch(activeId, () => {
  selected.value = null
})

function closeSearch() {
  searching.value = false
  query.value = ''
}

async function toggleSearch() {
  if (searching.value) {
    closeSearch()
    return
  }
  searching.value = true
  await nextTick()
  searchInput.value?.focus()
}

function clearLog() {
  cleared.value = current.value?.id ?? ''
  selected.value = null
  direction.value = ''
  closeSearch()
}

function pick(message: StreamMessage) {
  const previous = selected.value
  selected.value = previous?.sequence === message.sequence ? null : message
}
</script>

<template>
  <section class="stream-log">
    <div class="stream-toolbar">
      <span class="stream-state" :class="{ connected }" role="status"><span class="stream-dot"></span>{{ state }}</span>
      <span v-if="duration" class="stream-duration">{{ duration }}</span>
      <div v-if="stream?.outgoing && count" class="stream-filters">
        <button
          v-for="filter in filters" :key="filter.value" type="button" class="stream-filter"
          :aria-pressed="direction === filter.value" :disabled="!filter.count"
          @click="direction = direction === filter.value ? '' : filter.value"
        >
          <span class="stream-dot" :class="filter.value"></span>{{ filter.label }}
          <span class="text-text-tertiary">{{ filter.count }}</span>
        </button>
      </div>
      <span v-if="stream && !stream.outgoing && count" class="stream-count">{{ t(stream.count, count) }}</span>
      <span
        v-if="stream && !stream.outgoing && current?.status" class="stream-status"
        :class="statusBadgeClass(current.status, { failed: !!current.error })"
      >
        {{ formatStatus(current.status, current.statusText) }}
      </span>
      <div class="stream-tools">
        <IconButton
          v-if="count" class="stream-tool" variant="bare" size="bar" :class="{ active: searching }"
          :hint="t('request.protocol.logSearch')" :aria-label="t('request.protocol.logSearch')"
          :aria-expanded="searching" @click="toggleSearch"
        >
          <Icon name="Search" :size="16" />
        </IconButton>
        <!-- Send on the request sends a message while connected, so disconnecting lives with the log. -->
        <Button v-if="stream?.outgoing && connected" size="bar" class="stream-disconnect" @click="emit('disconnect')">
          {{ t(stream.stop) }}
        </Button>
        <IconButton
          v-if="!activeId && current && count" class="stream-tool" variant="bare" size="bar"
          :hint="t('request.protocol.clearLog')" :aria-label="t('request.protocol.clearLog')" @click="clearLog"
        >
          <Icon name="Trash2" :size="15" />
        </IconButton>
      </div>
    </div>

    <div v-if="searching" class="stream-search">
      <Icon name="Search" :size="13" />
      <input
        ref="searchInput" v-model="query" spellcheck="false" :aria-label="t('request.protocol.logSearch')"
        :placeholder="stream ? t(stream.search) : ''"
        @keydown.esc.stop="closeSearch"
      />
      <span v-if="searchCount" :class="{ 'stream-error': !rows.length }">{{ searchCount }}</span>
      <IconButton variant="bare" size="sm" :aria-label="t('common.close')" @click="closeSearch">
        <Icon name="X" :size="12" />
      </IconButton>
    </div>

    <p v-if="current?.error" class="stream-notice stream-error" role="alert">{{ current.error }}</p>
    <p v-else-if="current && !current.active && !count && stream?.outgoing" class="stream-notice">{{ current.statusText }}</p>
    <p v-if="count > messages.length" class="stream-notice">{{ t('request.protocol.logTruncated') }}</p>
    <RequestShortcuts v-if="!rows.length && !emptyHint" :protocol />
    <div v-else-if="!rows.length" class="stream-empty">
      {{ emptyHint }}
    </div>
    <div v-else class="stream-list">
      <button
        v-for="message in rows" :key="message.sequence" type="button"
        class="stream-row" :aria-pressed="selected?.sequence === message.sequence" @click="pick(message)"
      >
        <Icon
          :name="message.direction === 'in' ? 'ArrowDown' : 'ArrowUp'" :size="13" :stroke-width="2.2"
          :class="message.direction === 'in' ? 'stream-in' : 'text-accent'"
        />
        <span class="stream-summary">
          <span v-if="message.name" class="stream-name" :title="message.id">{{ message.name }}</span>
          <span v-else-if="message.binary" class="stream-name">{{ t('request.protocol.binary') }}</span>
          <span class="stream-message">{{ message.data }}</span>
        </span>
        <span class="stream-metric">{{ formatBytes(message.bytes) }}</span>
        <time class="stream-metric">{{ formatClock(message.at, true) }}</time>
      </button>
    </div>

    <pre v-if="selected" class="stream-detail">{{ selectedText }}</pre>
    <pre v-else-if="current?.responseBody && !count" class="stream-detail">{{ current.responseBody }}</pre>
  </section>
</template>

<style scoped>
@reference "../../style.css";

.stream-log {
  @apply flex flex-col min-h-0 min-w-0 flex-1;
  container-type: inline-size;
}

.stream-toolbar {
  @apply flex flex-none flex-wrap items-center text-13 text-text-tertiary;
  min-height: 2.625rem;
  gap: 0.25rem 0.625rem;
  padding: 0.25rem 0.75rem 0.25rem 1.25rem;
  border-bottom: 1px solid var(--line2);
}

.stream-state,
.stream-filter {
  @apply inline-flex items-center whitespace-nowrap;
  gap: 0.4375rem;
}

.stream-state.connected {
  color: var(--text);
}

.stream-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--text-tertiary);
}

.stream-state.connected .stream-dot,
.stream-dot.in {
  background: var(--green-text);
}

.stream-dot.out {
  background: var(--accent);
}

.stream-in {
  color: var(--green-text);
}

.stream-error {
  color: var(--red-text);
}

.stream-duration {
  @apply font-mono text-12 text-text-secondary tabular-nums whitespace-nowrap;
}

.stream-filters {
  @apply flex flex-wrap;
  gap: 0.375rem;
  margin-left: auto;
}

.stream-filter {
  @apply text-text-secondary;
  height: 1.75rem;
  padding: 0 0.6875rem;
  border-radius: 0.875rem;
  box-shadow: inset 0 0 0 1px var(--border);
  cursor: pointer;
}

.stream-filter:hover:not(:disabled) {
  color: var(--text);
}

.stream-filter[aria-pressed='true'],
.stream-tool.active {
  background: var(--bg-hover);
  box-shadow: none;
  color: var(--text);
}

.stream-filter:disabled {
  opacity: 0.5;
  cursor: default;
}

.stream-count {
  @apply text-12 text-text-tertiary whitespace-nowrap;
  margin-left: auto;
}

.stream-status {
  @apply flex-none whitespace-nowrap font-semibold text-12;
  padding: 0.125rem 0.4375rem;
  line-height: 1rem;
  border-radius: 0.3125rem;
  margin-left: auto;
  margin-right: 0.5rem;
}

.stream-count ~ .stream-status,
.stream-status ~ .stream-tools {
  margin-left: 0;
}

.stream-tools {
  @apply flex items-center flex-none;
  margin-left: auto;
}

.stream-disconnect {
  margin-inline: 0.25rem;
}

.stream-filters ~ .stream-tools,
.stream-count ~ .stream-tools {
  margin-left: 0;
}

.stream-search {
  @apply flex flex-none items-center text-12 text-text-tertiary;
  min-height: 2.375rem;
  gap: 0.5rem;
  padding: 0 0.75rem 0 1rem;
  background: var(--raise);
  border-bottom: 1px solid var(--line2);
}

.stream-search input {
  @apply flex-1 min-w-0 border-0 bg-transparent outline-none font-mono text-13 text-text;
}

.stream-notice,
.stream-empty {
  @apply flex-none break-words;
  padding: 0.375rem 1.25rem;
}

.stream-notice {
  font-size: var(--text-12);
}

.stream-empty {
  @apply text-13 text-text-tertiary;
  padding-block: 0.875rem;
}

.stream-list {
  @apply flex flex-1 flex-col min-h-0 overflow-auto;
  gap: 1px;
  padding: 0.375rem 0.5rem;
}

.stream-row {
  @apply grid flex-none w-full items-center min-w-0 rounded-lg text-left cursor-pointer;
  grid-template-columns: 1rem minmax(0, 1fr) 3.5rem 4rem;
  column-gap: 0.75rem;
  height: 2.125rem;
  padding-inline: 0.75rem;
}

.stream-row:hover,
.stream-row[aria-pressed='true'] {
  background: var(--bg-hover);
}

.stream-summary {
  @apply flex items-baseline min-w-0 overflow-hidden;
  gap: 0.625rem;
}

.stream-name {
  @apply flex-none truncate text-text-secondary;
  max-width: 40%;
}

.stream-message {
  @apply min-w-0 truncate font-mono;
  font-size: 0.78125rem;
}

.stream-metric {
  @apply truncate text-right text-12 text-text-tertiary tabular-nums;
}

.stream-detail {
  @apply min-h-0 overflow-auto border-t border-border font-mono text-13;
  flex: 0 1 38%;
  padding: 0.625rem 1.25rem;
  line-height: 1.3125rem;
  user-select: text;
}

@container (max-width: 30rem) {
  .stream-toolbar {
    column-gap: 0.375rem;
  }

  .stream-filters {
    margin-left: 0;
  }

  .stream-row {
    grid-template-columns: 1rem minmax(0, 1fr) 3.25rem 3.75rem;
    column-gap: 0.375rem;
  }
}
</style>
