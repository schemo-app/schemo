<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import Button from '../ui/Button.vue'
import Switch from '../ui/Switch.vue'
import WhatsNewSheet from './WhatsNewSheet.vue'
import { UpdateChannel } from '../../../bindings/schemo/internal/domain'
import { useAppInfo } from '../../composables/useAppInfo'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useUpdateCheck } from '../../composables/useUpdateCheck'
import { useWailsEvent } from '../../composables/useWailsEvent'
import { formatVersion } from '../../lib/format'
import { formatCheckedAt, t } from '../../i18n'
import logoUrl from '../../assets/logo.svg'

// What the app is right now, which releases it may be offered, and whether it looks for them on its
// own. The card at the top is the app itself, with the two things a person does about that.
const { loadSettings, save } = useSettings()
const { phase, latest, available, notes, checkedAt, error, check, openWindow, checkIfRequested } = useUpdateCheck()

const { name: appName, version, build } = useAppInfo()
const newsOpen = ref(false)

const channels = computed(() => [
  { value: UpdateChannel.ChannelStable, label: t('settings.channelStable') },
  { value: UpdateChannel.ChannelBeta, label: t('settings.channelBeta') },
])

// The stored choice until the window has read it: the same defaults Go answers with.
const channel = useSetting('updateChannel')
const autoCheck = useSetting('updateCheckAuto')

const channelNote = computed(() =>
  channel.value === UpdateChannel.ChannelBeta
    ? t('settings.channelBetaNote')
    : t('settings.channelStableNote')
)

// Under the switch and not beside the button: it describes how the app behaves on its own.
const lastChecked = computed(() =>
  checkedAt.value ? t('update.lastChecked', { at: formatCheckedAt(checkedAt.value) }) : ''
)

// A failed call leaves the version off and the name alone — the update window's own fallback.
const title = computed(() =>
  [appName.value || 'Schemo', formatVersion(version.value)].filter(Boolean).join(' ')
)

// The state the app is in, and the way to it when there is something to look at.
const stateLine = computed(() => {
  switch (phase.value) {
    case 'checking':
      return t('update.checkingLong')
    case 'installing':
      return t('update.installing')
    case 'uptodate':
      return t('update.upToDate')
    case 'available':
      return t('update.availableLinkShort', { version: formatVersion(latest.value) })
    case 'error':
      return error.value
    default:
      return checkedAt.value
        ? t('update.lastChecked', { at: formatCheckedAt(checkedAt.value) })
        : t('settings.build', { build: build.value || '—' })
  }
})

const tone = computed(() => {
  switch (phase.value) {
    case 'uptodate':
      return 'ok'
    case 'error':
      return 'bad'
    case 'available':
      return 'news'
    default:
      return 'quiet'
  }
})

const busy = computed(() => phase.value === 'checking' || phase.value === 'installing')
const openable = computed(() => phase.value === 'available' || phase.value === 'installing')
const newsVersion = computed(() => available.value ? latest.value : version.value)

// The notes of the version on offer; with nothing on offer, the current release's own.
const newsLine = computed(() =>
  t('settings.whatsNewLine', {
    channel: channel.value === UpdateChannel.ChannelBeta
      ? t('settings.channelBeta')
      : t('settings.channelStable'),
    version: formatVersion(newsVersion.value),
  })
)

// The "Check for updates…" command parks a check and this page is the one that takes it — as it
// mounts, or hearing `update-check` while already open.
useWailsEvent('update-check', () => void checkIfRequested())

onMounted(() => {
  void loadSettings()
  void checkIfRequested()
})
</script>

<template>
  <div class="pane">
    <div class="version">
      <img class="mark" :src="logoUrl" alt="" draggable="false" />
      <span class="about">
        <span class="name">{{ title }}</span>
        <button v-if="openable" type="button" class="state" :class="tone" @click="openWindow()">
          {{ stateLine }}
        </button>
        <span v-else class="state" :class="tone">{{ stateLine }}</span>
      </span>
      <div class="acts">
        <Button variant="outline" size="field" @click="newsOpen = true">
          {{ t('settings.whatsNew') }}
        </Button>
        <Button variant="primary" size="field" :disabled="busy" @click="check()">
          {{ t('settings.checkNow') }}
        </Button>
      </div>
    </div>

    <SettingsSection :label="t('settings.sec.channel')">
      <SettingsRow :title="t('settings.channel')" :note="channelNote">
        <Segment
          size="sm"
          :grow="false"
          :options="channels"
          :value="channel"
          @pick="save({ updateChannel: $event as UpdateChannel })"
        />
      </SettingsRow>
      <SettingsRow :title="t('settings.checkAutomatically')" :note="lastChecked">
        <Switch
          :model-value="autoCheck"
          :aria-label="t('settings.checkAutomatically')"
          @update:model-value="save({ updateCheckAuto: !!$event })"
        />
      </SettingsRow>
      <!-- A row the updater has nothing behind: it downloads and installs in one go, and has no queue
           to keep a download in. Drawn shut, and saying so. -->
      <SettingsRow
        :title="t('settings.autoDownload')"
        :note="t('settings.autoDownloadNote')"
        :soon="t('settings.soon')"
      >
        <Switch :model-value="false" disabled :aria-label="t('settings.autoDownload')" />
      </SettingsRow>
    </SettingsSection>

    <WhatsNewSheet
      v-if="newsOpen"
      :app="appName || 'Schemo'"
      :version="formatVersion(newsVersion)"
      :line="newsLine"
      :current="!available"
      :notes="notes"
      @close="newsOpen = false"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.625rem;
}

.version {
  @apply flex items-center gap-3.5 rounded-[0.625rem];
  padding: 1rem;
  border: 1px solid var(--border);
}

.mark {
  @apply flex-none;
  width: 2.75rem;
  height: 2.75rem;
}

.about {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.125rem;
}

.name {
  @apply text-15 font-semibold overflow-hidden text-ellipsis whitespace-nowrap;
}

.state {
  @apply text-left bg-transparent border-0 p-0 text-12 overflow-hidden text-ellipsis
         whitespace-nowrap text-text-tertiary;
  font: inherit;
  font-size: var(--text-12);
}

button.state {
  @apply cursor-pointer;
}

button.state:hover {
  text-decoration: underline;
}

.state.ok {
  color: var(--green-text);
}

.state.bad {
  color: var(--red-text);
}

.state.news {
  color: var(--accent);
  font-weight: 600;
}

.acts {
  @apply flex items-center gap-2 flex-none;
}
</style>
