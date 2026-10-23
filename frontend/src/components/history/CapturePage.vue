<script setup lang="ts">
import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { useCaptureStore } from '../../stores/capture'
import { useRecordsStore } from '../../stores/records'
import { useHarExport } from '../../composables/useHarExport'
import { useLoadOlder } from '../../composables/useLoadOlder'
import { useSetting } from '../../composables/useSettings'
import { groupHue, groupLabel, tabKeyOf } from '../../lib/recordTabs'
import { recordsByDay, responseSize } from '../../lib/requestRecord'
import { splitAddress } from '../../lib/address'
import { methodInkClass, statusTone } from '../../lib/format'
import { EXTENSION_STORES } from '../../lib/extensions'
import { formatClock, formatDuration, formatStatus, t } from '../../i18n'
import CaptureFiltersSheet from './CaptureFiltersSheet.vue'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import Button from '../ui/Button.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'

const records = useRecordsStore()
const capture = useCaptureStore()
const filters = useSetting('captureFilters')
const selected = ref('')
const filtersOpen = ref(false)
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
const rows = computed(() => captured.value.filter(record => !selected.value || tabKeyOf(record) === selected.value))

const groups = computed(() => recordsByDay(rows.value))
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

const onScroll = (event: Event) => loadIfNearEnd(event.currentTarget as HTMLElement)
</script>

<template>
  <div class="capture-page">
    <div class="cap-head">
      <span class="cap-title">{{ t('sidebar.capture') }}</span>
      <span
        class="cap-status"
        :class="capture.connected ? 'text-text-secondary' : 'text-text-tertiary'"
      >
        <span class="cap-dot" :class="{ live: recording }"></span>
        {{ status }}
      </span>
      <span class="flex-1 min-w-0"></span>
      <Button
        v-if="captured.length || capture.connected"
        size="field"
        @click="exportHar()"
      >
        {{ t('files.exportHar') }}
      </Button>
      <div v-if="capture.connected" class="cap-control" :class="{ paused: !recording }">
        <button
          type="button"
          class="cap-toggle"
          :disabled="!openTabs.length"
          :title="t('capture.allTabsHint')"
          @click="capture.toggle()"
        >
          <Icon v-if="recording" name="Pause" :size="12" :stroke-width="1.5" />
          <span v-else class="record-dot"></span>
          {{ recording ? t('capture.pauseAll') : t('capture.resumeAll') }}
        </button>
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <button
              type="button"
              class="cap-toggle-menu"
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
    <div v-if="!capture.connected && !sessions.length" class="cap-intro">
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
        <span class="intro-also">{{ t('capture.installAlso') }}</span>
        <span class="flex-1 min-w-0"></span>
        <span class="intro-wait">
          <Spinner />
          {{ t('capture.waiting') }}
        </span>
      </div>
    </div>

    <template v-else>
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
            <span
              class="cap-avatar"
              :style="{ '--tab-hue': groupHue(session.key) }"
            >
              {{ groupLabel(session).slice(0, 1).toUpperCase() }}
            </span>
            <span class="cap-chip-name">{{ groupLabel(session) }}</span>
            <span
              v-if="session.tab && !session.tab.paused && capture.connected"
              class="cap-dot live"
              :title="t('capture.recording')"
            ></span>
            <span v-else class="cap-chip-state">{{ session.tab ? t('capture.tabPaused') : t('capture.ended') }}</span>
          </button>
        </div>
        <div class="cap-filter-actions">
          <span>{{ t('history.requestsN', { n: rows.length }) }}</span>
          <button type="button" class="cap-link" @click="showHistory()">{{ t('capture.showHistory') }}</button>
        </div>
      </div>
      <div class="cap-scroll" :class="{ filtered: !!selected }" @scroll.passive="onScroll">
        <div class="cap-grid cap-table-head">
          <span>{{ t('history.colWhen') }}</span>
          <span v-if="!selected">{{ t('capture.colTab') }}</span>
          <span>{{ t('history.colMethod') }}</span>
          <span>{{ t('history.colAddress') }}</span>
          <span>{{ t('history.colStatus') }}</span>
          <span class="text-right">{{ t('history.colTime') }}</span>
          <span class="text-right">{{ t('history.colSize') }}</span>
        </div>
        <template v-for="group in groups" :key="group.label">
          <div class="cap-group">{{ group.label }}</div>
          <button
            v-for="record in group.items"
            :key="record.id"
            type="button"
            class="cap-grid cap-row"
            @click="records.selectRecord(record.id, $event.metaKey || $event.ctrlKey)"
          >
            <span class="cap-num text-text-tertiary">{{ formatClock(record.startedAt) }}</span>
            <span v-if="!selected" class="cap-row-tab">
              <span
                class="cap-avatar small"
                :style="{ '--tab-hue': groupHue(tabKeyOf(record)) }"
              >
                {{ (record.tabTitle || record.tabURL || t('history.tab')).slice(0, 1).toUpperCase() }}
              </span>
              <span class="truncate">{{ record.tabTitle || splitAddress(record.tabURL || record.url).host }}</span>
            </span>
            <span class="cap-method" :class="methodInkClass(record.method)">{{ record.method }}</span>
            <span class="cap-address">
              <span class="text-text-tertiary">{{ splitAddress(record.url).host }}</span>{{ splitAddress(record.url).path }}
            </span>
            <span>
              <span class="cap-status-badge" :class="statusTone(record)">
                {{ formatStatus(record.status, record.error) }}
              </span>
            </span>
            <span class="cap-num text-text-secondary text-right">{{ formatDuration(record.durationUs) }}</span>
            <span class="cap-num text-text-secondary text-right">
              {{ responseSize(record) }}
            </span>
          </button>
        </template>
        <div
          v-if="!rows.length"
          class="cap-empty"
        >
          {{ selected ? t('capture.noTabRequests') : t('capture.noSessions') }}
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
    </template>
    <CaptureFiltersSheet v-if="filtersOpen" @close="filtersOpen = false" @saved="filtersOpen = false" />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.capture-page {
  @apply flex-1 min-h-0 flex flex-col;
}

.cap-head {
  @apply flex items-center flex-none;
  height: 4rem;
  gap: 0.875rem;
  padding: 0 1.5rem;
  border-bottom: 1px solid var(--border);
}

.cap-title {
  @apply flex-none text-17 font-semibold;
  letter-spacing: -0.01em;
}

.cap-status {
  @apply flex items-center min-w-0 truncate text-12-5;
  gap: 0.4375rem;
}

.cap-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--text-tertiary);
}

.cap-dot.live,
.record-dot {
  background: var(--red);
}

.cap-control {
  @apply flex flex-none;
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
  height: 2rem;
  font-weight: 500;
}

.cap-toggle {
  gap: 0.5rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem 0 0 0.5rem;
}

.cap-toggle-menu {
  width: 1.75rem;
  padding: 0;
  border-radius: 0 0.5rem 0.5rem 0;
  box-shadow: inset 1px 0 0 var(--border);
}

.cap-toggle:disabled {
  @apply opacity-45 cursor-default;
}

.cap-toggle:hover:not(:disabled),
.cap-toggle-menu:hover {
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
  gap: 0.75rem;
  padding: 0.875rem 1.5rem 0.75rem;
}

.cap-chips {
  @apply flex flex-1 min-w-0 flex-wrap;
  gap: 0.3rem;
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

.cap-avatar {
  @apply flex items-center justify-center flex-none rounded-full text-white text-10 font-bold;
  width: 1.125rem;
  height: 1.125rem;
  background: hsl(var(--tab-hue) 58% 45%);
}

.cap-avatar.small {
  font-size: 0.5625rem;
  width: 1rem;
  height: 1rem;
}

.cap-filter-actions {
  @apply flex items-center flex-none whitespace-nowrap text-12 text-text-tertiary;
  height: 1.75rem;
  gap: 0.875rem;
}

.cap-link {
  @apply flex-none border-none bg-transparent text-accent cursor-pointer whitespace-nowrap text-12-5 p-0;
}

.cap-link:hover {
  text-decoration: underline;
}

.cap-scroll {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0 1.5rem 0.75rem;
}

.cap-grid {
  display: grid;
  grid-template-columns: 3.25rem minmax(5.625rem, 10.625rem) 3.25rem minmax(10rem, 1fr) 3.5rem 4rem 4.25rem;
  align-items: center;
  min-width: 42rem;
  padding: 0 0.5rem;
}

.filtered .cap-grid {
  grid-template-columns: 3.25rem 3.25rem minmax(10rem, 1fr) 3.5rem 4rem 4.25rem;
  min-width: 30rem;
}

.cap-table-head {
  height: 1.75rem;
  color: var(--text-tertiary);
  font-size: var(--text-11-5);
}

.cap-group {
  @apply text-11-5 font-semibold text-text-tertiary;
  padding: 0.75rem 0.5rem 0.25rem;
}

.cap-row {
  @apply w-full border-0 bg-transparent text-text text-left cursor-pointer;
  height: 2.125rem;
  border-radius: 0.3rem;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.cap-row:hover {
  background: var(--bg-hover);
}

.cap-row-tab {
  @apply flex items-center min-w-0 text-text-secondary;
  gap: 0.4375rem;
  padding-right: 0.75rem;
}

.cap-method {
  @apply font-mono text-11 font-bold;
}

.cap-address {
  @apply min-w-0 truncate font-mono text-12;
}

.cap-status-badge {
  @apply font-mono text-11 font-semibold;
  padding: 0.125rem 0.3rem;
  border-radius: 0.3125rem;
}

.is-ok {
  background: var(--green-soft);
  color: var(--green-text);
}

.is-bad {
  background: var(--red-soft);
  color: var(--red-text);
}

.cap-num {
  @apply tabular-nums;
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
  gap: 0.3rem;
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
  font-size: var(--text-11-5);
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
