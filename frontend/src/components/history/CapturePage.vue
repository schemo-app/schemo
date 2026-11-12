<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import { useCaptureStore } from '../../stores/capture'
import { useRecordsStore } from '../../stores/records'
import { useHarExport } from '../../composables/useHarExport'
import { useLoadOlder } from '../../composables/useLoadOlder'
import { useSetting } from '../../composables/useSettings'
import { groupLabel, tabKeyOf } from '../../lib/recordTabs'
import { isHttpError, isSlow, matchesRecord, repeatedRecordsByDay, responseSize } from '../../lib/requestRecord'
import { methodInkClass } from '../../lib/format'
import { EXTENSION_STORES } from '../../lib/extensions'
import { formatClock, formatDuration, t } from '../../i18n'
import CaptureFiltersSheet from './CaptureFiltersSheet.vue'
import TabFavicon from './TabFavicon.vue'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import Button from '../ui/Button.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'

const records = useRecordsStore()
const capture = useCaptureStore()
const filters = useSetting('captureFilters')
const selected = ref('')
const filtersOpen = ref(false)
const query = ref('')
const errorsOnly = ref(false)
const slowOnly = ref(false)
const expandedRepeats = ref(new Set<string>())
const scrollEl = useTemplateRef<HTMLElement>('scrollEl')
const { exportHar } = useHarExport(() => selected.value)
const { loadIfNearEnd } = useLoadOlder()

const sessions = computed(() => capture.sessions)
const selectedSession = computed(() => sessions.value.find(session => session.key === selected.value))
const selectedAction = computed(() => {
  const session = selectedSession.value
  if (!session?.tab) return t('capture.selectedOnly')
  return session.tab.paused ? t('capture.resumeSelected') : t('capture.pauseSelected')
})
const selectedNote = computed(() => {
  const session = selectedSession.value
  if (!session) return t('capture.selectTabHint')
  if (!session.tab) return t('capture.ended')
  return groupLabel(session)
})
const canToggleSelected = computed(() => !!selectedSession.value?.tab)
const openTabs = computed(() => capture.tabList)
const liveTabs = computed(() => openTabs.value.filter(tab => !tab.paused))
const recording = computed(() => capture.connected && capture.recording)
const captured = computed(() => records.browserRecords)
const matching = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return captured.value.filter(record =>
    (!selected.value || tabKeyOf(record) === selected.value) && (!needle || matchesRecord(record, needle))
  )
})
const rows = computed(() => matching.value.filter(record =>
  (!errorsOnly.value || isHttpError(record)) && (!slowOnly.value || isSlow(record))
))
const errorCount = computed(() => matching.value.filter(isHttpError).length)
const slowCount = computed(() => matching.value.filter(isSlow).length)
const filtering = computed(() => query.value.trim() || errorsOnly.value || slowOnly.value)

const groups = computed(() => repeatedRecordsByDay(rows.value, expandedRepeats.value, tabKeyOf))
watch(groups, () => {
  if (scrollEl.value) loadIfNearEnd(scrollEl.value)
}, { flush: 'post' })
const status = computed(() => {
  if (!capture.connected) return t('capture.notConnected')
  if (!openTabs.value.length) return t('extension.idle')
  if (!liveTabs.value.length) return t('capture.pausedAll')
  return t('capture.recordingTabs', { active: liveTabs.value.length, total: openTabs.value.length })
})

const rules = computed(() => {
  const hosts = filters.value.hosts ?? []
  const onOff = (on: boolean) => on ? t('capture.skipped') : t('capture.kept')
  return [
    { label: t('extension.hosts'), value: hosts.length ? hosts.join(', ') : t('extension.hostsAll') },
    { label: t('extension.skipStatic'), value: onOff(filters.value.static) },
    { label: t('extension.skipAnalytics'), value: onOff(filters.value.analytics) },
  ]
})

const introSteps = computed(() => [
  { title: t('capture.stepInstall'), note: t('capture.stepInstallNote') },
  { title: t('capture.stepOpen'), note: t('capture.stepOpenNote') },
  { title: t('capture.stepEnable'), note: t('capture.stepEnableNote') },
])

function openInstall(url: string) {
  void Browser.OpenURL(url)
}

function pick(key: string) {
  selected.value = selected.value === key ? '' : key
}

function showHistory() {
  records.openSession(selected.value)
}

function toggleSelected() {
  if (canToggleSelected.value) void capture.toggle(selected.value)
}

function openRow(id: string, repeated: boolean, event: MouseEvent) {
  if (!repeated) {
    void records.selectRecord(id, event.metaKey || event.ctrlKey)
    return
  }
  if (expandedRepeats.value.has(id)) expandedRepeats.value.delete(id)
  else expandedRepeats.value.add(id)
}

const onScroll = (event: Event) => loadIfNearEnd(event.currentTarget as HTMLElement)
</script>

<template>
  <div class="capture-page">
    <template v-if="!capture.connected && !sessions.length">
      <header class="cap-intro-head">
        <span class="cap-intro-label">{{ t('sidebar.capture') }}</span>
        <span class="cap-status" role="status">
          <span class="cap-dot"></span>{{ status }}
        </span>
      </header>
      <div class="cap-intro">
        <div class="intro-top">
          <span class="intro-tile"><Icon name="Puzzle" :size="24" :stroke-width="1.5" /></span>
          <span class="intro-text">
            <span class="intro-title">{{ t('capture.introTitle') }}</span>
            <span class="intro-note">{{ t('capture.introNote') }}</span>
          </span>
        </div>

        <div class="intro-steps">
          <div v-for="(step, i) in introSteps" :key="step.title" class="intro-step">
            <span class="intro-num" :class="{ now: i === 0 }">{{ i + 1 }}</span>
            <span class="intro-step-text">
              <span class="intro-step-title">{{ step.title }}</span>
              <span class="intro-step-note">{{ step.note }}</span>
            </span>
          </div>
        </div>

        <div class="intro-install">
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button variant="primary" size="lg">
                <Icon name="Download" :size="12" :stroke-width="2.2" />
                <span>{{ t('capture.install') }}</span>
                <Icon name="ChevronDown" :size="12" :stroke-width="2.2" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem v-for="listing in EXTENSION_STORES" :key="listing.name" @select="openInstall(listing.url)">
                <span class="flex-1">{{ listing.name }}</span>
                <Icon name="ArrowUpRight" :size="12" class="shrink-0 text-text-tertiary" />
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <span class="flex-1 min-w-0"></span>
          <span class="intro-wait">
            <Spinner />
            {{ t('capture.waiting') }}
          </span>
        </div>
      </div>
    </template>

    <template v-else>
      <div class="cap-head">
        <div class="cap-filters">
          <label class="cap-search">
            <Icon name="Search" :size="13" :stroke-width="1.5" />
            <input v-model="query" :aria-label="t('common.search')" :placeholder="t('history.filterPlaceholder')" spellcheck="false" />
          </label>
          <button type="button" class="cap-chip cap-quick" :class="{ selected: errorsOnly, quiet: !errorCount && !errorsOnly }" :aria-pressed="errorsOnly" @click="errorsOnly = !errorsOnly">
            <span class="cap-dot error"></span>{{ t('history.errors') }} <span class="text-text-tertiary">{{ errorCount }}</span>
          </button>
          <button type="button" class="cap-chip cap-quick" :class="{ selected: slowOnly, quiet: !slowCount && !slowOnly }" :aria-pressed="slowOnly" @click="slowOnly = !slowOnly">
            <span class="cap-dot slow"></span>{{ t('history.slow') }} <span class="text-text-tertiary">{{ slowCount }}</span>
          </button>
        </div>
        <div class="cap-actions">
          <span v-if="!capture.connected" class="cap-status" role="status">
            <span class="cap-dot"></span>{{ status }}
          </span>
          <span class="cap-count" :title="status">{{ t('history.requestsN', { n: rows.length }) }}</span>
          <button type="button" class="cap-history" :title="t('capture.showHistory')" :aria-label="t('capture.showHistory')" @click="showHistory">
            <Icon name="History" :size="16" :stroke-width="1.5" />
          </button>
          <Button
            size="field"
            :disabled="!captured.length"
            @click="exportHar()"
          >
            {{ t('files.exportHar') }}
          </Button>
          <div class="cap-control" :class="{ paused: capture.connected && !recording }">
            <button
              type="button"
              class="cap-toggle"
              :disabled="!capture.connected || !openTabs.length"
              :title="t('capture.allTabsHint')"
              :aria-label="recording ? t('capture.pauseAll') : t('capture.resumeAll')"
              @click="capture.toggle()"
            >
              <Icon v-if="recording" name="Pause" :size="12" :stroke-width="1.5" />
              <span v-else class="record-dot"></span>
              <span class="truncate">{{ recording ? t('capture.pauseAll') : t('capture.resumeAll') }}</span>
            </button>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <button
                  type="button"
                  class="cap-toggle-menu"
                  :disabled="!capture.connected"
                  :title="t('capture.chooseTabs')"
                  :aria-label="t('capture.chooseTabs')"
                >
                  <Icon name="ChevronDown" :size="11" :stroke-width="1.5" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent class="cap-menu">
                <DropdownMenuItem class="cap-menu-item" :disabled="!openTabs.length" @select="capture.toggle()">
                  <span>{{ recording ? t('capture.pauseAll') : t('capture.resumeAll') }}</span>
                  <span class="cap-menu-note">
                    {{ t('capture.openAndRecording', { total: openTabs.length, active: liveTabs.length }) }}
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem class="cap-menu-item" :disabled="!canToggleSelected" @select="toggleSelected">
                  <span>{{ selectedAction }}</span>
                  <span class="cap-menu-note">{{ selectedNote }}</span>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </div>
      <div class="cap-filter-bar">
        <div class="cap-chips">
          <button
            type="button"
            class="cap-chip"
            :class="{ selected: !selected }"
            :aria-pressed="!selected"
            @click="selected = ''"
          >
            {{ t('capture.allTabs') }}
          </button>
          <button
            v-for="session in sessions"
            :key="session.key"
            type="button"
            class="cap-chip cap-chip--tab"
            :class="{ selected: selected === session.key, ended: !session.tab }"
            :title="groupLabel(session)"
            :aria-pressed="selected === session.key"
            @click="pick(session.key)"
          >
            <TabFavicon :src="session.favIconUrl" :label="groupLabel(session)" :tab-key="session.key" />
            <span class="cap-chip-name">{{ groupLabel(session) }}</span>
            <span
              v-if="session.tab && !session.tab.paused && capture.connected"
              class="cap-dot live"
              :title="t('capture.recording')"
            ></span>
            <span v-else class="cap-chip-state">{{ session.tab ? t('capture.tabPaused') : t('capture.ended') }}</span>
          </button>
        </div>
      </div>
      <div ref="scrollEl" class="cap-scroll" @scroll.passive="onScroll">
        <template v-for="group in groups" :key="group.day">
          <div class="cap-group">
            {{ group.label }} <span class="font-normal text-text-tertiary">· {{ group.count }}</span>
          </div>
          <button
            v-for="row in group.rows"
            :key="row.repeatCount ? `repeat:${row.record.id}` : row.record.id"
            type="button"
            class="cap-row"
            :class="{ indented: row.indented }"
            :aria-expanded="row.repeatCount ? row.expanded : undefined"
            @click="openRow(row.record.id, !!row.repeatCount, $event)"
          >
            <span class="cap-method" :class="methodInkClass(row.record.method)">{{ row.record.method }}</span>
            <span class="cap-address-cell">
              <span class="cap-address" :title="row.repeatCount ? undefined : row.record.url">
                <span class="text-text-tertiary">{{ row.host }}</span>{{ row.path === '/' ? '' : row.path }}
              </span>
              <span v-if="row.repeatCount" class="cap-repeat" :title="row.expanded ? t('history.collapseRepeated') : t('history.showRepeated', { n: row.repeatCount })">
                ×{{ row.repeatCount }}<Icon :name="row.expanded ? 'ChevronDown' : 'ChevronRight'" :size="9" :stroke-width="1.5" />
              </span>
            </span>
            <span class="text-right">
              <StatusBadge :status="row.record.status" :failed="!!row.record.error" />
            </span>
            <span class="cap-num text-right">
              <span v-if="!row.expanded" :class="isSlow(row.record) && !row.repeatCount ? 'cap-slow' : 'text-text-secondary'">{{ formatDuration(row.record.durationUs) }}</span>
              <span v-if="!row.repeatCount && row.record.responseBody?.size" class="text-text-tertiary"> · {{ responseSize(row.record) }}</span>
            </span>
            <span class="cap-row-tab" :title="row.record.tabTitle || row.record.tabURL">
              <TabFavicon
                class="small"
                :src="row.record.favIconUrl"
                :label="row.record.tabTitle || row.record.tabURL || t('history.tab')"
                :tab-key="tabKeyOf(row.record)"
              />
            </span>
            <span class="cap-num text-right text-text-tertiary">{{ formatClock(row.record.startedAt) }}</span>
          </button>
        </template>
        <div
          v-if="!rows.length"
          class="cap-empty"
        >
          {{ filtering ? t('common.nothingFound') : selected ? t('capture.noTabRequests') : t('capture.noSessions') }}
        </div>
        <div v-if="records.olderLoading" class="cap-empty">{{ t('history.loadingOlder') }}</div>
      </div>
      <div class="cap-rules">
        <span
          v-for="rule in rules"
          :key="rule.label"
          class="truncate"
        >
          {{ rule.label }}: <span class="text-text-secondary">{{ rule.value }}</span>
        </span>
        <span class="flex-1 min-w-0"></span>
        <button type="button" class="cap-link" @click="filtersOpen = true">{{ t('capture.editRules') }}</button>
      </div>
      <CaptureFiltersSheet v-if="filtersOpen" @close="filtersOpen = false" @saved="filtersOpen = false" />
    </template>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.capture-page {
  @apply flex-1 min-w-0 min-h-0 flex flex-col;
  container-type: inline-size;
}

.cap-head {
  @apply items-center flex-none;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  min-height: 3.5rem;
  gap: 0.625rem;
  padding: 0.5rem 1.25rem;
}

.cap-filters,
.cap-actions {
  @apply flex items-center min-w-0 flex-wrap;
  gap: 0.625rem;
}

.cap-actions {
  justify-content: flex-end;
}

@container (max-width: 48rem) {
  .cap-head {
    grid-template-columns: minmax(0, 1fr);
  }
}

.cap-search {
  @apply flex items-center min-w-0 text-text-tertiary;
  flex: 0 1 17.5rem;
  min-width: 8.75rem;
  gap: 0.4375rem;
  height: 1.875rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.cap-search input {
  @apply flex-1 min-w-0 border-0 bg-transparent text-text;
  font: inherit;
  outline: none;
}

.cap-search:focus-within {
  box-shadow: inset 0 0 0 1px var(--accent);
}

.cap-count {
  @apply text-12 text-text-tertiary tabular-nums whitespace-nowrap;
}

.cap-history {
  @apply inline-grid place-items-center border-0 bg-transparent text-text-secondary cursor-pointer rounded-md;
  width: 1.75rem;
  height: 1.75rem;
}

.cap-history:hover {
  background: var(--bg-hover);
}

.cap-dot.slow {
  background: var(--orange);
}

.cap-quick.quiet {
  opacity: 0.5;
}

.cap-status {
  @apply flex flex-none items-center whitespace-nowrap text-12-5 text-text-tertiary;
  gap: 0.4375rem;
}

.cap-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--text-tertiary);
}

.cap-dot.live,
.cap-dot.error,
.record-dot {
  background: var(--red);
}

.cap-control {
  @apply flex min-w-0;
  border-radius: 0.5rem;
  background: var(--chip);
  color: var(--text);
}

.cap-control.paused {
  background: var(--red-soft);
  color: var(--red-text);
}

.cap-toggle,
.cap-toggle-menu {
  @apply flex items-center justify-center cursor-pointer border-none bg-transparent text-inherit;
  height: 1.875rem;
  font-weight: 500;
}

.cap-toggle {
  min-width: 0;
  gap: 0.5rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem 0 0 0.5rem;
}

.cap-toggle-menu {
  flex: none;
  width: 1.75rem;
  padding: 0;
  border-radius: 0 0.5rem 0.5rem 0;
  box-shadow: inset 1px 0 0 var(--border);
}

.cap-toggle:disabled,
.cap-toggle-menu:disabled {
  @apply opacity-45 cursor-default;
}

.cap-toggle:hover:not(:disabled),
.cap-toggle-menu:hover:not(:disabled) {
  background: var(--bg-hover);
}

.record-dot {
  @apply rounded-full;
  width: 0.5625rem;
  height: 0.5625rem;
}

.cap-menu {
  width: 18.75rem;
}

.cap-menu-item {
  @apply flex flex-col items-start;
  gap: 0.125rem;
  padding: 0.5rem 0.625rem;
}

.cap-menu-item[data-disabled] {
  @apply opacity-45 cursor-default;
}

.cap-menu-note {
  @apply text-12 text-text-tertiary truncate max-w-full;
}

.cap-filter-bar {
  @apply flex items-start flex-none;
  padding: 0 1.25rem 0.625rem;
}

.cap-chips {
  @apply flex flex-1 min-w-0 overflow-x-auto;
  scrollbar-width: none;
  gap: 0.375rem;
}

.cap-chip {
  @apply inline-flex items-center flex-none bg-transparent text-text border-none cursor-pointer;
  height: 1.75rem;
  max-width: 15.625rem;
  gap: 0.4375rem;
  padding: 0 0.75rem;
  border-radius: 0.875rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.cap-chip--tab {
  padding-left: 0.3125rem;
}

.cap-chip:hover,
.cap-chip.selected {
  background: var(--bg-hover);
}

.cap-chip.selected {
  box-shadow: inset 0 0 0 1px var(--text-tertiary);
}

.cap-chip.ended {
  opacity: 0.6;
}

.cap-chip-name {
  @apply truncate min-w-0;
}

.cap-chip-state {
  @apply flex-none text-11-5 text-text-tertiary;
}

.cap-link {
  @apply flex-none border-none bg-transparent text-accent cursor-pointer whitespace-nowrap text-12-5 p-0;
}

.cap-link:hover {
  text-decoration: underline;
}

.cap-scroll {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0 0.5rem 0.75rem;
}

.cap-row {
  @apply grid items-center min-w-0 w-full rounded-lg border-0 bg-transparent text-text text-left cursor-pointer;
  grid-template-columns: 3.25rem minmax(0, 1fr) 3.5rem minmax(4.25rem, 9.375rem) 1.625rem 2.875rem;
  column-gap: 0.5rem;
  height: 2.125rem;
  padding: 0 0.75rem;
}

.cap-row.indented {
  padding-left: 1.625rem;
}

@container (max-width: 30rem) {
  .cap-row {
    grid-template-columns: 2.625rem minmax(0, 1fr) 3.125rem 3.75rem 1.125rem 2.375rem;
    column-gap: 0.3125rem;
    padding-inline: 0.5rem;
  }

  .cap-row.indented {
    padding-left: 1.125rem;
  }
}

.cap-group {
  @apply text-12 font-semibold text-text-secondary;
  padding: 0.625rem 0.75rem 0.375rem;
}

.cap-address-cell {
  @apply flex items-center min-w-0 gap-2;
}

.cap-repeat {
  @apply inline-flex items-center shrink-0 rounded-full px-2 text-11 font-semibold text-text-secondary;
  gap: 0.1875rem;
  background: var(--chip);
}

.cap-slow {
  color: var(--orange-text);
  font-weight: 600;
}

.cap-row:hover {
  background: var(--bg-hover);
}

.cap-row-tab {
  @apply flex justify-center;
}

.cap-method {
  @apply font-mono text-11 font-bold;
}

.cap-address {
  @apply min-w-0 truncate font-mono text-12-5;
}

.cap-num {
  @apply tabular-nums text-12;
}

.cap-empty {
  @apply text-text-tertiary text-center;
  padding: 2.5rem 0.5rem;
}

.cap-rules {
  @apply flex items-center flex-none text-text-tertiary text-12-5 min-w-0;
  height: 2.5rem;
  gap: 1.375rem;
  padding: 0 1.5rem;
  border-top: 1px solid var(--border);
}

.cap-intro-head {
  @apply flex items-center flex-none flex-wrap;
  min-height: 4rem;
  gap: 0.5rem 0.875rem;
  padding: 0.5rem 1.5rem;
  border-bottom: 1px solid var(--border);
}

.cap-intro-label {
  @apply text-17 font-semibold;
  letter-spacing: -0.01em;
}

.cap-intro {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 2rem 1.5rem;
}

.intro-top {
  @apply flex items-start;
  max-width: 67.5rem;
  gap: 1rem;
}

.intro-tile {
  @apply flex items-center justify-center flex-none;
  width: 3rem;
  height: 3rem;
  border-radius: 0.75rem;
  background: var(--chip);
  color: var(--text-secondary);
}

.intro-text {
  @apply flex flex-col flex-1 min-w-0;
  max-width: 38.75rem;
  gap: 0.2rem;
}

.intro-title {
  @apply font-semibold;
  font-size: var(--text-20);
  letter-spacing: -0.015em;
}

.intro-note {
  color: var(--text-secondary);
  font-size: var(--text-14);
  line-height: 1.5;
  text-wrap: pretty;
}

.intro-steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(13.75rem, 1fr));
  max-width: 67.5rem;
  margin-top: 1.75rem;
  gap: 0.75rem;
}

.intro-step {
  @apply flex flex-col;
  gap: 0.625rem;
  padding: 1rem;
  border-radius: 0.625rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.intro-num {
  @apply flex items-center justify-center flex-none font-semibold;
  width: 1.375rem;
  height: 1.375rem;
  border-radius: 999px;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-12);
  font-variant-numeric: tabular-nums;
}

.intro-num.now {
  box-shadow: inset 0 0 0 1px var(--accent);
  background: var(--accent-soft);
  color: var(--accent);
}

.intro-step-text {
  @apply flex flex-col;
  gap: 0.1875rem;
}

.intro-step-title {
  @apply font-medium;
}

.intro-step-note {
  color: var(--text-tertiary);
  font-size: var(--text-12-5);
  line-height: 1.45;
  text-wrap: pretty;
}

.intro-install {
  @apply flex items-center flex-wrap;
  max-width: 67.5rem;
  margin-top: 1.75rem;
  gap: 0.625rem 1rem;
}

.intro-also {
  color: var(--text-tertiary);
  font-size: var(--text-12-5);
}

.intro-wait {
  @apply flex items-center;
  gap: 0.5625rem;
  color: var(--text-tertiary);
  font-size: var(--text-12-5);
}

</style>
