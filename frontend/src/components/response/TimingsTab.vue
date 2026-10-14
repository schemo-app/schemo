<script setup lang="ts">
import { computed } from 'vue'
import type { RecordView } from '../../lib/requestRecord'
import { formatMicros, useMessages } from '../../i18n'
import { splitAddress } from '../../lib/address'

const { record } = defineProps<{ record: RecordView }>()
const { t } = useMessages()

interface Phase {
  label: string
  us: number
  // Where the phase starts and how much of the whole it takes, both read off the bars above it: the
  // design draws the request as a waterfall, so a phase after a slow one is drawn after it.
  offset: number
  fill: string
}

// Only the phases that happened are drawn, and which those are is Go's answer. An absent phase is one
// that did not take place at all — a zero would say it was measured and took no time. A request sent
// with connection reuse has no DNS, TCP or TLS because nothing was dialled; a captured one carries
// only the two phases a page can time.
const phases = computed<Phase[]>(() => {
  const all = [
    { label: t('response.timings.dns'), us: record.dnsUs, fill: 'var(--text-tertiary)' },
    { label: t('response.timings.connect'), us: record.connectUs, fill: 'var(--accent)' },
    { label: t('response.timings.tls'), us: record.tlsUs, fill: 'var(--tok-bool)' },
    { label: t('response.timings.wait'), us: record.waitUs, fill: 'var(--orange)' },
    { label: t('response.timings.download'), us: record.downloadUs, fill: 'var(--green)' },
  ]
  let at = 0
  return all
    .filter((phase) => phase.us != null)
    .map((phase) => {
      const offset = at
      at += phase.us ?? 0
      return { label: phase.label, us: phase.us ?? 0, offset, fill: phase.fill }
    })
})

const totalUs = computed(() => Math.max(1, record.durationUs))

// A bar is placed where the phases before it ended, so the track reads as one request rather than as
// five bars of their own.
function place(phase: Phase): Record<string, string> {
  return {
    left: `${Math.min(100, (phase.offset / totalUs.value) * 100)}%`,
    width: `${Math.min(100, (phase.us / totalUs.value) * 100)}%`,
    background: phase.fill,
  }
}

// The line under the bars. The design's sentence names the wait inside the total, because that is the
// part of a request the server owns; where nothing was timed waiting, the sentence stops at the total.
const totalNote = computed(() => {
  const wait = record.waitUs
  if (wait == null) return t('response.timings.total')
  return t('response.timings.totalNote', { wait: formatMicros(wait) })
})

// A request that reused a connection has no dial phases to show. The connect phase is what tells the
// two apart — a fresh connection always has one, a reused one never does, and a request that never
// reached a server has neither it nor a wait, so it is already answered for by the note above.
const reusedConnection = computed(
  () =>
    record.connectUs == null &&
    record.source === 'manual' &&
    (record.waitUs != null || record.downloadUs != null)
)

const connectionNote = computed(() => {
  if (phases.value.length === 0) return t('response.timings.unmeasured')
  if (reusedConnection.value) return t('response.timings.reused')
  if (record.connectUs == null) return ''
  return t('response.timings.fresh', { host: splitAddress(record.url).host })
})
</script>

<template>
  <div class="timings">
    <!-- The total is a headline and not a bar: the phases below are its parts and each is drawn
         against it, so a bar of its own length would be the one bar saying nothing. -->
    <div class="head">
      <span class="total">{{ formatMicros(record.durationUs) }}</span>
      <span class="caption">{{ totalNote }}</span>
    </div>
    <div class="bars">
      <div v-for="p in phases" :key="p.label" class="timing-row">
        <span class="timing-label">{{ p.label }}</span>
        <div class="timing-track">
          <div class="timing-fill" :style="place(p)"></div>
        </div>
        <span class="timing-value">{{ formatMicros(p.us) }}</span>
      </div>
    </div>
    <span v-if="connectionNote" class="timing-foot">{{ connectionNote }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.timings {
  @apply min-h-full flex flex-col bg-bg;
  padding: 1.125rem 1.5rem 1.5rem;
  gap: 1.125rem;
  max-width: 47.5rem;
}

.head {
  @apply flex items-baseline flex-wrap;
  gap: 0.75rem;
}

.total {
  @apply font-semibold;
  font-size: 1.5rem;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
}

.caption {
  @apply text-13 text-text-tertiary;
}

.bars {
  @apply flex flex-col;
  gap: 0.75rem;
}

.timing-row {
  @apply grid items-center;
  grid-template-columns: 10.625rem minmax(0, 1fr) 4.375rem;
  gap: 0.875rem;
}

.timing-label {
  @apply text-13 text-text-secondary;
}

.timing-track {
  @apply relative;
  height: 0.625rem;
  border-radius: 0.3125rem;
  background: var(--chip);
}

.timing-fill {
  @apply absolute;
  top: 0;
  bottom: 0;
  min-width: 3px;
  border-radius: 0.3125rem;
}

.timing-value {
  @apply text-right text-text;
  font-family: var(--mono);
  font-size: var(--text-12-5);
  font-variant-numeric: tabular-nums;
}

.timing-foot {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}
</style>
