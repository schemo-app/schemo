<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import ChipField from './ChipField.vue'
import { Button } from '../ui/button'
import { Switch } from '../ui/switch'
import Icon from '../ui/Icon.vue'
import { BridgeService } from '../../../bindings/schemo/internal/transport/wails'
import type { CaptureFilters } from '../../../bindings/schemo/internal/domain'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useWailsEvent } from '../../composables/events/useWailsEvent'
import { describeFailure, useMessages } from '../../i18n'
import { EXTENSION_STORES } from '../../lib/extensions'

// The browser extension as the site draws it: whether one is connected at the top, how the two
// sides behave together under it, and what the extension lets through at the foot. The switches
// are the extension's to carry out — the app keeps them and hands them over — so every one of them
// is saved and sent in the same click.

const { t } = useMessages()
const { setCaptureFilters, setExtension } = useSettings()

const failure = ref('')

// ---- the connection ----------------------------------------------------

// What the extension last said about itself. The window asks once on open — the frames arrive when
// something changes, so waiting for the next one would be waiting for nothing — and follows them
// after that.
const connected = ref(false)
const browser = ref('')
const extensionVersion = ref('')
const tabs = ref(0)
const paused = ref(false)

onMounted(async () => {
  try {
    const status = await BridgeService.Status()
    connected.value = status?.connected === true
    browser.value = status?.state?.browser ?? ''
    extensionVersion.value = status?.state?.version ?? ''
    tabs.value = status?.state?.tabs ?? 0
    paused.value = status?.state?.paused === true
  } catch {
    // Nothing to draw but "not connected", which is what the state starts as.
  }
})

useWailsEvent('capture-state', (ev) => {
  const state = ev.data
  connected.value = true
  browser.value = state?.browser ?? ''
  extensionVersion.value = state?.version ?? ''
  tabs.value = state?.tabs ?? 0
  paused.value = state?.paused === true
})

useWailsEvent('capture-disconnected', () => {
  connected.value = false
})

const statusTitle = computed(() =>
  connected.value ? [t('extension.connected'), browser.value].filter(Boolean).join(' · ') : t('extension.none')
)

const statusNote = computed(() => {
  if (!connected.value) return t('extension.noneNote')
  if (paused.value) return t('extension.paused')
  const capture = tabs.value ? t('extension.capturing', { tabs: t('counts.tabs', { n: tabs.value }) }) : t('extension.idle')
  return [extensionVersion.value ? t('extension.version', { version: extensionVersion.value }) : '', capture]
    .filter(Boolean)
    .join(' · ')
})

function openInstall(url: string): void {
  void Browser.OpenURL(url)
}

// ---- connection settings -----------------------------------------------

const extension = useSetting('extension')

function setAutoConnect(on: boolean): void {
  void saveExtension({ autoConnect: on })
}

function setRememberTabs(on: boolean): void {
  void saveExtension({ rememberTabs: on })
}

// One click, one save and one frame to the extension; a refusal lands under the rows it was about.
async function saveExtension(patch: { autoConnect?: boolean; rememberTabs?: boolean }): Promise<void> {
  failure.value = ''
  try {
    await setExtension(patch)
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

// ---- capture filters ---------------------------------------------------

// A missing list of hosts is every site, which is what an empty one means — the extension reads
// it exactly so. That is why «only the list» picked before a first host is written lives here and
// not in the settings: an empty list saved as the list would be the same state as «every site»,
// and the segment could never leave it.
const filters = useSetting('captureFilters')
const picking = ref(false)
const listed = computed(() => picking.value || (filters.value.hosts ?? []).length > 0)

async function saveFilters(patch: Partial<CaptureFilters>): Promise<void> {
  failure.value = ''
  try {
    await setCaptureFilters({ ...filters.value, hosts: [...(filters.value.hosts ?? [])], ...patch })
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

function pickHosts(value: string): void {
  if (value === 'list') {
    // The field is what picking the list is for; the list itself is saved with its first host.
    picking.value = true
    return
  }
  // «Every site» is the list emptied, and an emptied list is what goes out with it.
  picking.value = false
  void saveFilters({ hosts: [] })
}

function setHosts(hosts: string[]): void {
  // A list emptied by hand goes back to every site: the two are one state in the filters.
  if (!hosts.length) picking.value = false
  void saveFilters({ hosts })
}
</script>

<template>
  <div class="pane">
    <!-- Where the extension stands: a dot, and what it is. The ways to get it are offered here and
         only here — an extension nobody has is a thing to install, not to connect. -->
    <div class="status">
      <span class="dot" :class="{ live: connected }"></span>
      <span class="status-text">
        <span class="status-title">{{ statusTitle }}</span>
        <span class="status-note">{{ statusNote }}</span>
      </span>
      <span v-if="!connected" class="get">
        <Button
          v-for="store in EXTENSION_STORES"
          :key="store.name"
          variant="outline"
          size="field"
          @click="openInstall(store.url)"
        >
          {{ store.name }}
          <Icon name="ArrowUpRight" :size="10" :stroke-width="2.2" />
        </Button>
      </span>
    </div>

    <SettingsSection :label="t('extension.connection')">
      <SettingsRow :title="t('extension.autoConnect')" :note="t('extension.autoConnectNote')">
        <Switch
          :model-value="extension.autoConnect"
          :aria-label="t('extension.autoConnect')"
          @update:model-value="setAutoConnect(!!$event)"
        />
      </SettingsRow>
      <SettingsRow :title="t('extension.rememberTabs')" :note="t('extension.rememberTabsNote')">
        <Switch
          :model-value="extension.rememberTabs"
          :aria-label="t('extension.rememberTabs')"
          @update:model-value="setRememberTabs(!!$event)"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('extension.filters')">
      <SettingsRow
        :title="t('extension.hosts')"
        :note="listed ? t('extension.hostsListNote') : t('extension.hostsAllNote')"
      >
        <Segment
          :value="listed ? 'list' : 'all'"
          :grow="false"
          :options="[
            { value: 'all', label: t('extension.hostsAll') },
            { value: 'list', label: t('extension.hostsList') },
          ]"
          @pick="pickHosts"
        />
      </SettingsRow>
      <div v-if="listed" class="card-block hosts">
        <ChipField
          :values="filters.hosts ?? []"
          :placeholder="t('extension.hostsPlaceholder')"
          @change="setHosts"
        />
      </div>
      <SettingsRow :title="t('extension.skipStatic')" :note="t('extension.skipStaticNote')">
        <Switch
          :model-value="filters.static"
          :aria-label="t('extension.skipStatic')"
          @update:model-value="saveFilters({ static: !!$event })"
        />
      </SettingsRow>
      <SettingsRow :title="t('extension.skipAnalytics')" :note="t('extension.skipAnalyticsNote')">
        <Switch
          :model-value="filters.analytics"
          :aria-label="t('extension.skipAnalytics')"
          @update:model-value="saveFilters({ analytics: !!$event })"
        />
      </SettingsRow>
      <SettingsRow :title="t('extension.onlyJson')" :note="t('extension.onlyJsonNote')">
        <Switch
          :model-value="filters.json"
          :aria-label="t('extension.onlyJson')"
          @update:model-value="saveFilters({ json: !!$event })"
        />
      </SettingsRow>
    </SettingsSection>

    <p v-if="failure" class="bad">{{ failure }}</p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.125rem;
}

.bad {
  @apply m-0 text-13 text-red;
}

.status {
  @apply flex items-center gap-3 border border-border rounded-[0.625rem];
  padding: 0.875rem 1rem;
}

.dot {
  @apply flex-none block rounded-full;
  width: 0.5rem;
  height: 0.5rem;
  background: var(--text-tertiary);
}

.dot.live {
  background: var(--green-text);
}

.status-text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.125rem;
}

.status-title {
  @apply text-14 font-semibold;
}

.status-note {
  @apply text-12 text-text-tertiary truncate;
}

.get {
  @apply flex-none flex items-center gap-2;
}

.hosts {
  padding: 0.625rem 1rem 0.75rem;
}
</style>
