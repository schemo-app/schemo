<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RecordSource } from '../../../bindings/schemo/internal/domain'
import {
  BridgeService,
  RecordsService,
} from '../../../bindings/schemo/internal/transport/wails'
import { useRecordsStore } from '../../stores/records'
import { useSetting } from '../../composables/useSettings'
import { useToast } from '../../composables/useToast'
import { groupHue, groupLabel, tabGroups } from '../../lib/recordTabs'
import { EXTENSION_STORES } from '../../lib/extensions'
import { formatDate, useMessages } from '../../i18n'
import CaptureFiltersSheet from './CaptureFiltersSheet.vue'
import Icon from '../ui/Icon.vue'
import { Switch } from '../ui/switch'
import { Browser } from '@wailsio/runtime'

// The capture: what the extension is recording right now, the browser tabs it has taken, and the
// rules the two sides agreed on. A session opens the history filtered by that tab, where its
// requests are read like any other record.
const store = useRecordsStore()
const toast = useToast()
const filters = useSetting('captureFilters')
const { t } = useMessages()

const listening = ref(false)

onMounted(async () => {
  try {
    listening.value = await BridgeService.Listening()
  } catch {
    // The bridge file is the extension's side; the page just says nothing is connected.
  }
})

const captured = computed(() =>
  store.records.filter((r) => r.source === RecordSource.SourceBrowser)
)

const sessions = computed(() =>
  tabGroups(captured.value).map((group) => ({
    key: group.key,
    label: groupLabel(group),
    host: group.url ? hostOf(group.url) : '',
    count: group.items.length,
    when: group.items[0] ? formatDate(group.items[0].startedAt, { hour: '2-digit', minute: '2-digit' }) : '',
    letter: groupLabel(group).slice(0, 1).toUpperCase(),
    tint: `hsl(${groupHue(group.key)}, 58%, 45%)`,
    live: store.capture.tabList.some((tab) => String(tab.tabId) === group.key),
  }))
)

function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

function openSession(key: string) {
  store.openSession(key)
}

const recording = computed(() => store.capture.recording)

const connectedNote = computed(() => {
  if (!store.capture.connected) return t('capture.notConnected')
  const browser = store.capture.browser || 'Chrome'
  return store.capture.version
    ? t('capture.connected', { browser, version: store.capture.version })
    : t('capture.connectedPlain', { browser })
})

const rules = computed(() => {
  const hosts = filters.value.hosts ?? []
  const onOff = (on: boolean) => (on ? t('capture.skipped') : t('capture.kept'))
  return [
    { label: t('extension.hosts'), value: hosts.length ? hosts.join(', ') : t('extension.hostsAll') },
    { label: t('extension.skipStatic'), value: onOff(filters.value.static) },
    { label: t('extension.skipAnalytics'), value: onOff(filters.value.analytics) },
  ]
})

async function exportHar() {
  const written = await RecordsService.ExportHar(t('files.exportHar'), '')
  if (written) toast.show(t('browser.saved'))
}

// The rules are read here and written here: the panel opens over the page rather than sending the
// window to the settings for one switch.
const filtersOpen = ref(false)

// What stands on the page while nothing is connected: the three steps the extension is brought in
// by, and the way to the page that carries it.

function openInstall(url: string) {
  void Browser.OpenURL(url)
}

const introSteps = computed(() => [
  { title: t('capture.stepInstall'), note: t('capture.stepInstallNote') },
  { title: t('capture.stepOpen'), note: t('capture.stepOpenNote') },
  { title: t('capture.stepEnable'), note: t('capture.stepEnableNote') },
])
</script>

<template>
  <div class="capture-page">
    <div class="cap-head">
      <span class="cap-title">{{ t('sidebar.capture') }}</span>
      <span v-if="store.capture.connected" class="cap-status">
        <span class="cap-dot ok"></span>{{ connectedNote }}
      </span>
      <span v-else class="cap-status none">
        <span class="cap-dot"></span>{{ connectedNote }}
      </span>
      <span class="cap-spacer"></span>
      <template v-if="store.capture.connected">
        <button class="cap-ghost" @click="exportHar()">{{ t('files.exportHar') }}</button>
        <span class="cap-rec">
          <span class="cap-rec-text">
            {{ recording ? t('capture.recording') : t('capture.paused') }}
          </span>
          <Switch
            :model-value="recording"
            :title="recording ? t('status.pauseCapture') : t('status.resumeCapture')"
            :aria-label="recording ? t('status.pauseCapture') : t('status.resumeCapture')"
            @update:model-value="store.toggleCapture()"
          />
        </span>
      </template>
    </div>

    <!-- Nobody is connected: the page has nothing to list, so it says what it would do and how to
         bring the extension to it. -->
    <div v-if="!store.capture.connected" class="cap-intro">
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
        <button v-for="store in EXTENSION_STORES" :key="store.name" class="intro-btn" @click="openInstall(store.url)">
          <Icon name="Download" :size="12" :stroke-width="2.2" />
          <span class="cap-line">{{ store.name }}</span>
        </button>
        <span class="intro-also">{{ t('capture.installAlso') }}</span>
        <span class="cap-spacer"></span>
        <span class="intro-wait">
          <span class="intro-spinner"></span>{{ t('capture.waiting') }}
        </span>
      </div>
    </div>

    <div v-else class="cap-scroll">
      <div class="cap-grid cap-head-row">
        <span></span>
        <span>{{ t('capture.colTab') }}</span>
        <span class="right">{{ t('capture.colCount') }}</span>
        <span class="cap-when">{{ t('capture.colWhen') }}</span>
        <span></span>
      </div>

      <div
        v-for="s in sessions"
        :key="s.key"
        class="cap-grid cap-row"
        @click="openSession(s.key)"
      >
        <span class="cap-tile" :style="{ background: s.tint }">{{ s.letter }}</span>
        <span class="cap-text">
          <span class="cap-name">{{ s.label }}</span>
          <span class="cap-host">{{ s.host }}</span>
        </span>
        <span class="right cap-num">{{ s.count }}</span>
        <span class="cap-when cap-when-cell">{{ s.when }}</span>
        <span class="cap-live-slot">
          <span v-if="s.live" class="cap-live">
            <span class="cap-dot live"></span>{{ t('capture.live') }}
          </span>
        </span>
      </div>

      <div v-if="sessions.length === 0" class="cap-empty">{{ t('capture.noSessions') }}</div>

      <div class="cap-rules-title">{{ t('capture.rules') }}</div>
      <div v-for="r in rules" :key="r.label" class="cap-grid cap-rule">
        <span class="cap-rule-label">{{ r.label }}</span>
        <span class="cap-rule-value">{{ r.value }}</span>
        <a class="cap-rule-edit" @click="filtersOpen = true">{{ t('common.change') }}</a>
      </div>
    </div>

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
  box-sizing: content-box;
}

.cap-title {
  @apply flex-none;
  font-size: var(--text-17);
  font-weight: 600;
  letter-spacing: -0.01em;
}

.cap-status {
  @apply flex items-center min-w-0 truncate;
  gap: 0.4375rem;
  font-size: var(--text-12-5);
  color: var(--text-secondary);
}

.cap-status.none {
  color: var(--text-tertiary);
}

.cap-dot {
  @apply flex-none;
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
  background: var(--text-tertiary);
}

.cap-status.none .cap-dot {
  background: transparent;
  box-shadow: inset 0 0 0 1.5px var(--text-tertiary);
}

.cap-dot.ok {
  background: var(--green);
}

.cap-dot.live {
  background: var(--red);
}

.cap-spacer {
  @apply flex-1;
}

.cap-ghost {
  @apply flex-none;
  height: 2rem;
  padding: 0 0.75rem;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text);
  font: inherit;
  cursor: default;
  box-shadow: inset 0 0 0 1px var(--border);
}

.cap-ghost:hover {
  background: var(--bg-hover);
}

.cap-rec {
  @apply flex items-center flex-none;
  gap: 0.5rem;
}

.cap-rec-text {
  @apply whitespace-nowrap;
  font-size: var(--text-12-5);
  color: var(--text-secondary);
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
  gap: 0.375rem;
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

.intro-btn {
  @apply flex items-center justify-center cursor-default border-0;
  height: 2rem;
  padding: 0 0.875rem;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: var(--accent);
  color: var(--accent-text);
  font: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.intro-btn:hover {
  filter: brightness(1.06);
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

.intro-spinner {
  @apply flex-none rounded-full;
  width: 0.6875rem;
  height: 0.6875rem;
  border: 1.5px solid var(--border);
  border-top-color: var(--text-tertiary);
  animation: spin 1.4s linear infinite;
}

.cap-scroll {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 1.125rem 1.5rem 1.5rem;
}

.cap-grid {
  display: grid;
  grid-template-columns: 2.25rem minmax(0, 1fr) 5.625rem 8.125rem 6.875rem;
  align-items: center;
  padding: 0 0.625rem;
}

.cap-head-row {
  height: 1.75rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
}

.cap-row {
  height: 3.25rem;
  border-radius: 0.5rem;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  cursor: default;
}

.cap-row:hover {
  background: var(--bg-hover);
}

.right {
  text-align: right;
}

.cap-tile {
  @apply flex items-center justify-center flex-none;
  width: 1.5rem;
  height: 1.5rem;
  border-radius: 0.375rem;
  color: #fff;
  font-size: var(--text-11);
  font-weight: 700;
}

.cap-text {
  @apply flex flex-col min-w-0;
  gap: 2px;
}

.cap-name {
  @apply truncate;
  font-weight: 500;
}

.cap-host {
  @apply truncate;
  font-family: var(--mono);
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
}

.cap-num {
  font-variant-numeric: tabular-nums;
}

.cap-when {
  padding-left: 1.5rem;
}

.cap-when-cell {
  color: var(--text-secondary);
}

.cap-live-slot {
  @apply flex justify-end;
}

.cap-live {
  @apply flex items-center flex-none;
  gap: 0.375rem;
  font-size: var(--text-12);
  color: var(--red-text);
}

.cap-empty {
  padding: 1.5rem 0.625rem;
  color: var(--text-tertiary);
}

.cap-rules-title {
  font-weight: 600;
  padding: 1.875rem 0.625rem 0.5rem;
}

.cap-rule {
  grid-template-columns: 12.5rem minmax(0, 1fr) auto;
  height: 2.5rem;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.cap-rule-label {
  color: var(--text-secondary);
}

.cap-rule-value {
  @apply truncate;
}

.cap-rule-edit {
  font-size: var(--text-12-5);
}

.cap-rule-edit:hover {
  text-decoration: underline;
}
</style>
