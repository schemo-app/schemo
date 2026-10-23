<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import CopyRow from '../ui/CopyRow.vue'
import type { Reading, ReadingRow } from '../../../bindings/schemo/internal/domain'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { formatAgo, formatDate, formatIn, textOf, t } from '../../i18n'

// Go says the facts as data — a moment is a moment, not a sentence — and the words and the local
// time are drawn here.
const { reading, value, canFollow = false } = defineProps<{
  reading: Reading
  /** The value that was read, for the actions that take it somewhere. */
  value: string
  canFollow?: boolean
}>()

const emit = defineEmits<{ fetch: [url: string] }>()

const { copied, copy } = useCopyFeedback()

const rows = computed<ReadingRow[]>(() => reading.rows ?? [])

/** A parameter of an address is named by itself; every other row by the catalogue. */
function labelOf(row: ReadingRow): string {
  return row.key.startsWith('param:') ? row.key.slice(6) : textOf(`inspector.rows.${row.key}`)
}

function local(iso: string): string {
  return formatDate(Date.parse(iso), {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

const WEEK = 7 * 24 * 3600 * 1000

/** How far a moment is from now, in words, when that is near enough to be worth saying. */
function relative(iso: string): string {
  const at = Date.parse(iso)
  const gap = at - Date.now()
  if (Math.abs(gap) > WEEK) return ''
  return gap > 0 ? formatIn(at) : formatAgo(at)
}

// A token past its expiry is what somebody opens this card to check.
const expired = (row: ReadingRow): boolean => row.key === 'exp' && Date.parse(row.value) < Date.now()
</script>

<template>
  <section class="reading">
    <header class="reading-head">
      <span class="reading-title">{{ textOf(`inspector.readings.${reading.kind}`) }}</span>
      <IconButton
        v-if="reading.nested"
        variant="bare"
        size="sm"
        :hint="copied ? t('common.copied') : t('common.copy')"
        @click="copy(reading.nested)"
      >
        <Icon :name="copied ? 'Check' : 'Copy'" :size="12" :stroke-width="1.8" />
      </IconButton>
    </header>

    <CopyRow
      v-for="row in rows"
      :key="row.key"
      :label="labelOf(row)"
      :text="row.value"
      :mono="row.shape !== 'text'"
    >
      <template v-if="row.shape === 'time'">
        {{ local(row.value) }}
        <span v-if="relative(row.value)" class="sub">· {{ relative(row.value) }}</span>
        <span v-if="expired(row)" class="flag">{{ t('inspector.expired') }}</span>
      </template>
      <template v-else-if="row.shape === 'colour'">
        <span class="swatch" :style="{ background: row.value }"></span>{{ row.value }}
      </template>
      <template v-else-if="row.shape === 'text'">{{ textOf(`inspector.values.${row.value}`) }}</template>
    </CopyRow>

    <pre v-if="reading.nested" class="nested">{{ reading.nested }}</pre>

    <button
      v-if="reading.kind === 'url' && canFollow"
      type="button"
      class="follow"
      @click="emit('fetch', value)"
    >
      {{ t('inspector.follow') }}
    </button>
  </section>
</template>

<style scoped>
@reference "../../style.css";

.reading {
  @apply flex flex-col gap-0.5 rounded-lg px-3 py-2;
  background: var(--bg-hover);
}

.reading-head {
  @apply flex items-center justify-between;
  min-height: 1.5rem;
}

.reading-title {
  @apply section-label;
}

.sub {
  @apply text-text-tertiary;
  font-family: inherit;
}

.flag {
  @apply ml-2 rounded-sm px-1.5 text-11 font-semibold text-red;
  background: var(--red-soft);
}

.swatch {
  @apply inline-block align-middle mr-2 rounded-sm;
  width: 0.875rem;
  height: 0.875rem;
  border: 1px solid var(--border-strong);
}

.nested {
  @apply mt-1 overflow-auto rounded-md p-2 text-12 text-text-secondary;
  max-height: 11.25rem;
  font-family: var(--mono);
  background: var(--bg-inset);
  white-space: pre-wrap;
  word-break: break-all;
}

.follow {
  @apply mt-1 self-start cursor-pointer border-0 bg-transparent text-13 text-accent;
  padding: 0.125rem 0;
  --wails-draggable: no-drag;
}
</style>
