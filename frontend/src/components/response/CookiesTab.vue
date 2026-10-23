<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useCookiesStore } from '../../stores/cookies'
import { domainLabel, expiryLabel, flagLabel, stateClass, stateLabel, touchedJar } from '../../lib/responseCookies'
import { callOrToast } from '../../lib/calls'
import { splitAddress } from '../../lib/address'
import { matchesQuery } from '../../lib/match'
import { CookieState } from '../../../bindings/schemo/internal/domain'
import type { JarCookie } from '../../../bindings/schemo/internal/domain'
import type { RecordView } from '../../lib/requestRecord'
import type { RequestSource } from '../../lib/requestSource'
import CaptureButton from './CaptureButton.vue'
import MatchText from '../search/MatchText.vue'
import { CaptureSource } from '../../../bindings/schemo/internal/domain'
import { t } from '../../i18n'
import { Cookies } from '../../../bindings/schemo/internal/request'

const { record, host: keeper = null, query = '' } = defineProps<{
  record: RecordView
  /** The request the response is of, when a cookie can be kept from it. */
  host?: RequestSource | null
  /** What the row's search asks: the cookies that hold it stay, marked where they do. */
  query?: string
}>()

const cookies = useCookiesStore()

// What the answer wrote, when it wrote: the tab is about this response first, and the jar is what
// it has to say about a response that wrote nothing. Every line of a write carries what a row
// shows: its value, domain, expiry and what became of it in the cookies.
const writes = computed(() => record.cookieWrites)

// The jar's own cookies for the address, read only for the answer that set none: a person looking at
// a response that wrote nothing still wants to know what this host holds.
const jarRows = ref<JarCookie[]>([])

async function loadJar() {
  const address = record.url
  const rows = (await callOrToast(Cookies.ForURL(address), 'cookies.readFailed')) ?? []
  // The record on screen may have changed while the jar was read: its rows are not this one's.
  if (address === record.url) jarRows.value = rows
}

watch(
  () => record.cookieWrites,
  () => {
    if (writes.value.length === 0) void loadJar()
  },
  { immediate: true }
)

type CookieRow = {
  name: string
  value: string
  domain?: string
  path?: string
  expiresAt?: number | null
  secure?: boolean
  httpOnly?: boolean
  sameSite?: string
  hostOnly?: boolean
  state?: CookieState
}

const rows = computed<CookieRow[]>(() =>
  writes.value.length > 0
    ? writes.value.map((write) => ({ ...write }))
    : jarRows.value.map((cookie) => ({ ...cookie }))
)

const shown = computed(() =>
  rows.value.filter((row) => matchesQuery(query, row.name, row.value, row.domain))
)

defineExpose({ matches: computed(() => shown.value.length) })

// Whether anything was written at all: the answer clearing nothing in the jar is the answer whose
// table is the jar's own.
const touched = computed(() => touchedJar(writes.value))

// The count is the writes that happened, not the cookies the answer named: a cookie that was
// refused did not change the jar, and the sentence is about the cookies.
const written = computed(
  () => writes.value.filter((write) => write.state !== CookieState.CookieNotSaved).length
)

// The host the request went to; the record keeps the resolved address.
const host = computed(() => splitAddress(record.url).host)

// A path is drawn only when it is not `/`: same-name cookies on one host differ by path.
function pathOf(path: string | undefined): string {
  return path && path !== '/' ? path : ''
}

// The cookie of that name the answer set, if any; the jar may hold a later value.
const setBy = (name: string) => writes.value.find((write) => write.name === name)

// The line over the table is about the answer, not the table: what it left in the jar, or that the
// jar below is the earlier answers' doing. An answer that set nothing gets its own sentence.
const note = computed(() => {
  if (writes.value.length === 0) {
    return jarRows.value.length === 0 ? t('response.cookies.none') : t('response.cookies.untouched')
  }
  return touched.value
    ? t('response.cookies.saved', { n: written.value, host: host.value })
    : t('response.cookies.keptOut')
})

// The manager is opened on the address this answer went to: a person reading an answer about
// api.example.com means that domain, not the one the manager was last left on.
function openManager() {
  void cookies.openManager(record.url)
}
</script>

<template>
  <div class="resp-cookies">
    <!-- The way into the manager, and the way back from what the answer wrote, are the answer's
         row, beside the tabs; here is only what this answer did to the cookies. -->
    <div class="head">
      <span class="note">{{ note }}</span>
    </div>

    <div v-if="rows.length > 0" class="table">
      <div class="grid head-row">
        <span class="c-name">{{ t('response.cookies.name') }}</span>
        <span>{{ t('response.cookies.value') }}</span>
        <span>{{ t('response.cookies.domain') }}</span>
        <span>{{ t('response.cookies.expires') }}</span>
        <span>{{ t('response.cookies.jar') }}</span>
        <span></span>
      </div>

      <div
        v-for="c in shown"
        :key="`${c.domain}|${c.path}|${c.name}`"
        class="grid row"
        role="button"
        tabindex="0"
        :title="t('cookies.openInManager')"
        @click="openManager"
        @keydown.enter="openManager"
      >
        <span class="c-name mono" :title="flagLabel(c)"><MatchText :text="c.name" :query /></span>
        <span class="c-value mono"><MatchText :text="c.value" :query /></span>
        <span class="c-domain" :title="t('cookies.reachHint')">
          <MatchText :text="domainLabel(c.domain, c.hostOnly)" :query /><span v-if="pathOf(c.path)" class="path">{{ pathOf(c.path) }}</span>
        </span>
        <span class="c-expires">{{ expiryLabel(c.expiresAt) }}</span>
        <span class="c-state" :title="flagLabel(c)">
          <template v-if="c.state">
            <span class="dot" :class="stateClass(c.state)"></span>
            <span class="c-state-text" :class="stateClass(c.state)">{{ stateLabel(c.state) }}</span>
          </template>
        </span>
        <span class="c-act">
          <CaptureButton
            v-if="keeper && setBy(c.name)"
            :host="keeper"
            :source="CaptureSource.CaptureCookie"
            :path="c.name"
            :value="setBy(c.name)!.value"
          />
        </span>
      </div>

      <div v-if="shown.length === 0" class="none">{{ t('common.nothingFound') }}</div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.resp-cookies {
  @apply flex flex-col;
  padding: 0.75rem 1.25rem 1.25rem;
  gap: 0.625rem;
}

.head {
  @apply flex items-center;
  gap: 0.75rem;
}

.note {
  @apply flex-1 min-w-0 text-text-secondary;
  line-height: 1.5;
}

.table {
  @apply flex flex-col;
}

.grid {
  display: grid;
  grid-template-columns:
    minmax(0, 0.7fr) minmax(0, 1.8fr) minmax(0, 1.1fr) minmax(0, 0.8fr) minmax(0, 0.9fr) 1.75rem;
  align-items: center;
}

.head-row {
  height: 1.75rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.row {
  height: 2.5rem;
  border-bottom: 1px solid var(--line2);
  cursor: default;
}

.row:hover {
  background: var(--bg-hover);
}

.c-name {
  @apply min-w-0 truncate;
  padding-left: 0.5rem;
  padding-right: 0.625rem;
  font-size: var(--text-12-5);
}

.c-value {
  @apply min-w-0 truncate;
  padding-right: 0.75rem;
  font-size: var(--text-12-5);
  color: var(--tok-str);
}

.c-domain {
  @apply min-w-0 truncate text-text-secondary;
  padding-right: 0.75rem;
}

.c-domain .path {
  @apply ml-1.5 text-text-tertiary;
}

.c-expires {
  @apply min-w-0 truncate text-text-secondary;
  padding-right: 0.625rem;
}

.c-state {
  @apply flex items-center min-w-0;
  gap: 0.3rem;
  font-size: var(--text-12);
}

.c-state-text {
  @apply min-w-0 truncate;
}

.c-act {
  @apply flex items-center justify-center;
  width: 1.75rem;
  height: 1.75rem;
}

.row :deep(.capture-btn) {
  width: 1.5rem;
  height: 1.5rem;
}

.row:hover :deep(.capture-btn) {
  opacity: 1;
}

.c-state .dot {
  @apply flex-none rounded-full;
  width: 0.3rem;
  height: 0.3rem;
  background: currentColor;
}

.none {
  @apply flex items-center text-text-tertiary;
  height: 2.5rem;
  padding: 0 0.5rem;
}

</style>
