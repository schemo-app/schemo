<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import ClearHistorySheet from './ClearHistorySheet.vue'
import Segment from '../ui/Segment.vue'
import { Input } from '../ui/input'
import { Button } from '../ui/button'
import { Switch } from '../ui/switch'
import { HTTPVersion, Retention } from '../../../bindings/schemo/internal/domain'
import type { RequestsPatch } from '../../../bindings/schemo/internal/usecase/settings'
import { RecordsService, SystemService } from '../../../bindings/schemo/internal/transport/wails'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { formatBytes, useMessages } from '../../i18n'

// How the app sends what a person asks and what it keeps of it. Every field is committed as it is
// left — there is no Save here to forget.
const { t } = useMessages()
const { loadSettings, setRequests, setRetention } = useSettings()
const { withNotice } = useSheetNotice()

const sent = useSetting('requests')

const retentions = computed(() => [
  { value: Retention.RetainWeek, label: t('settings.retentionWeek') },
  { value: Retention.RetainMonth, label: t('settings.retentionMonth') },
  { value: Retention.RetainForever, label: t('settings.retentionForever') },
])

const retention = useSetting('historyRetention')

const versions = computed(() => [
  { value: HTTPVersion.HTTPAuto, label: t('settings.httpAuto') },
  { value: HTTPVersion.HTTP1, label: t('settings.http1') },
])

// What the space on screen is holding, read here and not in Go's snapshot: it changes with every
// request that goes out, and the settings window is the one place it is shown.
const count = ref(0)
const bytes = ref(0)
const clearing = ref(false)

const held = computed(() => t('counts.requests', count.value))
const size = computed(() => formatBytes(bytes.value))

// The two numbers are text under a pointer while they are typed into, so they are held here and put
// back from the settings whenever an answer arrives — as the proxy's address is.
const timeout = ref('')
const hops = ref('')

watch(sent, syncNumbers, { immediate: true })

function syncNumbers() {
  timeout.value = String(sent.value.timeout)
  hops.value = String(sent.value.maxRedirects)
}

async function write(patch: RequestsPatch) {
  await withNotice(() => setRequests(patch))
}

function commitTimeout() {
  const seconds = Number.parseInt(timeout.value.trim() || '0', 10)
  if (!Number.isFinite(seconds) || seconds === sent.value.timeout) return
  void write({ timeout: seconds })
}

function commitHops() {
  const wanted = Number.parseInt(hops.value.trim() || '0', 10)
  if (!Number.isFinite(wanted) || wanted === sent.value.maxRedirects) return
  void write({ maxRedirects: wanted })
}

// The name the app goes by when nobody has written another: Go's answer, shown as the field's own
// value, so an untouched field reads as what it comes to and not as an empty box.
const defaultAgent = ref('')
const agent = computed(() => sent.value.userAgent || defaultAgent.value)

// The field is only written when it was touched: an answer arriving from another window must not
// send back the name it was drawn with, and leaving a field alone is not a change to it.
function commitAgent(event: Event) {
  const wanted = (event.target as HTMLInputElement).value.trim()
  if (wanted !== agent.value) void write({ userAgent: wanted })
}

async function readHistory() {
  await withNotice(async () => {
    const stats = await RecordsService.History()
    count.value = stats?.count ?? 0
    bytes.value = stats?.bytes ?? 0
  })
}

async function changeRetention(value: string) {
  await withNotice(() => setRetention(value as Retention))
  // The window that shortened it is the one that has to show the result: the records the rule just
  // dropped are exactly what the line beside this segment counts.
  await readHistory()
}

function cleared() {
  clearing.value = false
  void readHistory()
}

onMounted(() => {
  void loadSettings().then(readHistory)
  SystemService.UserAgent()
    .then((name) => {
      defaultAgent.value = name ?? ''
    })
    .catch(() => {})
})
</script>

<template>
  <div class="pane">
    <SettingsSection :label="t('settings.sec.send')" :note="t('settings.sendNote')">
      <SettingsRow :title="t('settings.timeout')" :note="t('settings.timeoutNote')">
        <span class="number">
          <input
            v-model="timeout"
            class="digits"
            spellcheck="false"
            @keydown.enter="commitTimeout"
            @blur="commitTimeout"
          />
          <span class="unit">{{ t('settings.seconds') }}</span>
        </span>
      </SettingsRow>

      <SettingsRow
        :title="t('settings.redirects')"
        :note="sent.followRedirects ? t('settings.redirectsNote') : t('settings.redirectsOff')"
      >
        <span v-if="sent.followRedirects" class="number">
          <input
            v-model="hops"
            class="digits"
            spellcheck="false"
            @keydown.enter="commitHops"
            @blur="commitHops"
          />
          <span class="unit">{{ t('settings.hops') }}</span>
        </span>
        <Switch
          :model-value="sent.followRedirects"
          :aria-label="t('settings.redirects')"
          @update:model-value="write({ followRedirects: !!$event })"
        />
      </SettingsRow>

      <SettingsRow :title="t('settings.cookies')" :note="t('settings.cookiesNote')">
        <Switch
          :model-value="sent.cookies"
          :aria-label="t('settings.cookies')"
          @update:model-value="write({ cookies: !!$event })"
        />
      </SettingsRow>

      <SettingsRow :title="t('settings.httpVersion')" :note="t('settings.httpVersionNote')">
        <Segment
          size="sm"
          :grow="false"
          :options="versions"
          :value="sent.httpVersion"
          @pick="write({ httpVersion: $event as HTTPVersion })"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('settings.sec.headers')" :note="t('settings.headersNote')">
      <SettingsRow :title="t('settings.userAgent')" :note="t('settings.userAgentNote')">
        <Input
          class="agent"
          mono
          :model-value="agent"
          spellcheck="false"
          @keydown.enter="commitAgent"
          @blur="commitAgent"
        />
      </SettingsRow>

      <SettingsRow :title="t('settings.acceptEncoding')" :note="t('settings.acceptEncodingNote')">
        <Switch
          :model-value="sent.acceptEncoding"
          :aria-label="t('settings.acceptEncoding')"
          @update:model-value="write({ acceptEncoding: !!$event })"
        />
      </SettingsRow>

      <SettingsRow :title="t('settings.showAuto')" :note="t('settings.showAutoNote')">
        <Switch
          :model-value="sent.showAuto"
          :aria-label="t('settings.showAuto')"
          @update:model-value="write({ showAuto: !!$event })"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('settings.sec.history')" :note="t('settings.historyNote')">
      <SettingsRow :title="t('settings.retention')">
        <Segment size="sm"
          :grow="false" :options="retentions" :value="retention" @pick="changeRetention" />
      </SettingsRow>
      <SettingsRow :title="t('settings.clearHistory')" :note="`${held} · ${size}`">
        <Button variant="danger" size="field" @click="clearing = true">
          {{ t('settings.clearAction') }}
        </Button>
      </SettingsRow>
    </SettingsSection>

    <ClearHistorySheet
      v-if="clearing"
      :count
      :bytes
      @close="clearing = false"
      @cleared="cleared"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.625rem;
}

.number {
  @apply inline-flex items-center h-7.5 rounded-[0.375rem] flex-none;
  border: 1px solid var(--border-strong);
  background: var(--bg-inset);
}

.digits {
  @apply border-0 outline-none bg-transparent text-13 text-text text-right;
  font-family: inherit;
  width: 3.25rem;
  padding: 0 0.25rem 0 0.625rem;
}

.unit {
  @apply text-13 text-text-tertiary;
  padding-right: 0.625rem;
}

.agent {
  @apply w-60;
}
</style>
