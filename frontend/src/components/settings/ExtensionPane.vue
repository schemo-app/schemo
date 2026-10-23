<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { Browser } from '@wailsio/runtime'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import ChipField from './ChipField.vue'
import Button from '../ui/Button.vue'
import Switch from '../ui/Switch.vue'
import Icon from '../ui/Icon.vue'
import type { CaptureFilters } from '../../../bindings/schemo/internal/domain'
import { useFailure } from '../../composables/useFailure'
import { useSetting, useSettings } from '../../composables/useSettings'
import { t } from '../../i18n'
import { EXTENSION_STORES } from '../../lib/extensions'
import { Extension, type CaptureState } from '../../../bindings/schemo/internal/agent'
import { useWailsEvent } from '../../composables/useWailsEvent'

// The browser extension: whether one is connected at the top, how the two sides behave together,
// and what the extension lets through at the foot. The switches are the extension's to carry out
// (the app keeps them and hands them over), so each is saved and sent in the same click.

const { save } = useSettings()

const { failure, run } = useFailure()

const { isConnected, browser, version, tabCount, isPaused } = useExtensionStatus()

const statusTitle = computed(() =>
  isConnected.value
    ? [t('extension.connected'), browser.value].filter(Boolean).join(' · ')
    : t('extension.none')
)

const statusNote = computed(() => {
  if (!isConnected.value) return t('extension.noneNote')
  if (isPaused.value) return t('extension.paused')
  const capture = tabCount.value
    ? t('extension.capturing', { tabs: t('counts.tabs', { n: tabCount.value }) })
    : t('extension.idle')
  return [version.value ? t('extension.version', { version: version.value }) : '', capture]
    .filter(Boolean)
    .join(' · ')
})

function openInstall(url: string): void {
  void Browser.OpenURL(url)
}

const extension = useSetting('extension')

function setAutoConnect(on: boolean): void {
  void saveExtension({ autoConnect: on })
}

function setRememberTabs(on: boolean): void {
  void saveExtension({ rememberTabs: on })
}

// One click, one save and one frame to the extension; a refusal lands under the rows it was about.
async function saveExtension(patch: { autoConnect?: boolean; rememberTabs?: boolean }): Promise<void> {
  await run(() => save({ extension: patch }))
}

// A missing list of hosts is every site, which is what an empty one means; the extension reads it
// exactly so. That is why "only the list" picked before a first host is written lives here and not
// in the settings: an empty list saved as the list would be the same state as "every site", and the
// segment could never leave it.
const filters = useSetting('captureFilters')
const picking = ref(false)
const listed = computed(() => picking.value || (filters.value.hosts ?? []).length > 0)

async function saveFilters(patch: Partial<CaptureFilters>): Promise<void> {
  await run(() => save({ captureFilters: patch }))
}

function pickHosts(value: string): void {
  if (value === 'list') {
    // The field is what picking the list is for; the list itself is saved with its first host.
    picking.value = true
    return
  }
  // "Every site" is the list emptied, and an emptied list is what goes out with it.
  picking.value = false
  void saveFilters({ hosts: [] })
}

function setHosts(hosts: string[]): void {
  // A list emptied by hand goes back to every site: the two are one state in the filters.
  if (!hosts.length) picking.value = false
  void saveFilters({ hosts })
}

// What the browser extension last said about itself, as the settings window draws it. The window
// asks once on open (frames arrive when something changes, so waiting for the next one would wait
// for nothing) and follows them after that.
function useExtensionStatus() {
  const isConnected = ref(false)
  const browser = ref('')
  const version = ref('')
  const tabCount = ref(0)
  const isPaused = ref(false)

  function take(state: Partial<CaptureState> | null | undefined) {
    browser.value = state?.browser ?? ''
    version.value = state?.version ?? ''
    tabCount.value = state?.tabs ?? 0
    isPaused.value = state?.paused === true
  }

  onMounted(async () => {
    try {
      const status = await Extension.Status()
      isConnected.value = status?.connected === true
      take(status?.state)
    } catch {
      // Nothing to draw but "not connected", which is what the state starts as.
    }
  })

  useWailsEvent('capture-state', (event) => {
    isConnected.value = true
    take(event.data)
  })

  useWailsEvent('capture-disconnected', () => {
    isConnected.value = false
  })

  return { isConnected, browser, version, tabCount, isPaused }
}
</script>

<template>
  <div class="pane">
    <!-- Where the extension stands: a dot, and what it is. The ways to get it are offered here and
         only here — an extension nobody has is a thing to install, not to connect. -->
    <div class="status">
      <span class="dot" :class="{ live: isConnected }"></span>
      <span class="status-text">
        <span class="status-title">{{ statusTitle }}</span>
        <span class="status-note">{{ statusNote }}</span>
      </span>
      <span v-if="!isConnected" class="get">
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
