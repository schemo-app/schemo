<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import SettingsSection from './SettingsSection.vue'
import ChipField from './ChipField.vue'
import Icon from '../ui/Icon.vue'
import { Input } from '../ui/input'
import { Button } from '../ui/button'
import { Switch } from '../ui/switch'
import { ProxyMode, ProxyProtocol } from '../../../bindings/schemo/internal/domain'
import type { EnvEntry, ProxyProbe } from '../../../bindings/schemo/internal/domain'
import type { ProxyPatch } from '../../../bindings/schemo/internal/usecase/settings'
import { SettingsService } from '../../../bindings/schemo/internal/transport/wails'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { useMessages } from '../../i18n'

// Where the app's requests leave it: through the proxy the environment names, through one written
// here, or none at all. Every field is committed as it is left — there is no Save here to forget.
const { t } = useMessages()
const { loadSettings, setProxy, revealProxyPassword } = useSettings()
const { withNotice } = useSheetNotice()

const proxy = useSetting('proxy')
const mode = computed(() => proxy.value.mode)

// Three ways out, each said as what it means rather than as what it is called.
const modes = computed(() => [
  {
    value: ProxyMode.ProxySystem,
    title: t('settings.proxy.modeSystem'),
    note: t('settings.proxy.noteSystem'),
  },
  {
    value: ProxyMode.ProxyManual,
    title: t('settings.proxy.modeManual'),
    note: t('settings.proxy.noteManual'),
  },
  {
    value: ProxyMode.ProxyOff,
    title: t('settings.proxy.modeOff'),
    note: t('settings.proxy.noteOff'),
  },
])

// The protocol is a name of the wire's, not a word of the interface's: HTTP, HTTPS and SOCKS5 are
// spelled the same in every language.
const protocols = [ProxyProtocol.ProxyHTTP, ProxyProtocol.ProxyHTTPS, ProxyProtocol.ProxySOCKS5]

// Read when the pane opens: it is a fact about the machine, not a preference.
const environment = ref<EnvEntry[]>([])

const protocol = computed(() => proxy.value.protocol)

// Held while they are typed into, and put back from the settings whenever an answer arrives.
const host = ref('')
const port = ref('')
const user = ref('')
const password = ref('')
// Whether the password was typed into at all. The window never holds the stored value, so a field
// left alone is not an empty password — it is nobody having touched it.
const passwordTouched = ref(false)
const shown = ref(false)
// Whether the two credential fields are drawn. It follows what is stored, and turning it off
// takes the credentials away — a proxy that asks for nothing needs no fields.
const authShown = ref(false)

watch(proxy, syncFields, { immediate: true })

function syncFields() {
  host.value = proxy.value.host
  port.value = String(proxy.value.port || '')
  user.value = proxy.value.user
  password.value = ''
  passwordTouched.value = false
  shown.value = false
  authShown.value = Boolean(proxy.value.user || proxy.value.hasPassword)
}

function toggleAuth(on: boolean) {
  authShown.value = on
  if (!on) void write({ user: '', password: '' })
}

function pickProtocol(value: string) {
  void write({ protocol: value as ProxyProtocol })
}

// A refusal is drawn rather than swallowed: a control that quietly does nothing is one nobody can
// tell from a control that is broken.
async function write(patch: ProxyPatch) {
  await withNotice(() => setProxy(patch))
}

// What a whole address pasted into the field comes to. Go reads it apart — it is an outside format,
// and a second reader of it here would be a second answer — the fields follow the patch.
async function commitHost() {
  const wanted = host.value.trim()
  if (wanted === proxy.value.host) return
  if (!wanted.includes('://') && !wanted.includes('@')) {
    void write({ host: wanted })
    return
  }
  await withNotice(async () => setProxy(await SettingsService.ParseProxyAddress(wanted)))
}

function commitPort() {
  const wanted = Number.parseInt(port.value.trim() || '0', 10)
  if (!Number.isFinite(wanted) || wanted === proxy.value.port) return
  void write({ port: wanted })
}

function commitUser() {
  const wanted = user.value.trim()
  if (wanted !== proxy.value.user) void write({ user: wanted })
}

function commitPassword() {
  if (passwordTouched.value) void write({ password: password.value })
}

// The eye is the one call that hands the value over: the answers the window draws carry
// `hasPassword` and never the password itself.
async function toggleShown() {
  if (shown.value) {
    shown.value = false
    return
  }
  await withNotice(async () => {
    password.value = await revealProxyPassword()
    passwordTouched.value = false
    shown.value = true
  })
}

// What the environment names, taken over by hand: the address is read as any pasted one is, and
// the mode follows it — the point of the link is that the fields are what the environment was.
async function takeFromEnvironment() {
  const held = environment.value.find((one) => one.key === 'HTTP_PROXY' && one.value)
  if (!held) {
    void write({ mode: ProxyMode.ProxyManual })
    return
  }
  await withNotice(async () =>
    setProxy({ ...(await SettingsService.ParseProxyAddress(held.value)), mode: ProxyMode.ProxyManual })
  )
}

// The one thing on this screen that talks to the machine: a tunnel through the proxy, opened and
// closed. Nothing travels through it.
const probing = ref(false)
const probe = ref<ProxyProbe | null>(null)

async function test() {
  probing.value = true
  probe.value = null
  await withNotice(async () => {
    probe.value = await SettingsService.TestProxy()
  })
  probing.value = false
}

const probeLine = computed(() => {
  const held = probe.value
  if (!held) return ''
  if (held.ok) return t('settings.proxy.probeOk', { ms: String(held.millis) })
  if (held.needsAuth) return t('settings.proxy.probeAuth')
  if (held.error) return held.error
  return t('settings.proxy.probeRefused', { status: String(held.status) })
})

onMounted(async () => {
  void loadSettings()
  try {
    environment.value = (await SettingsService.ProxyEnvironment()) ?? []
  } catch {
    // Nothing to do: the table is a fact about the machine and the pane works without it.
  }
})
</script>

<template>
  <div class="pane">
    <div class="modes-block">
      <span class="group-label">{{ t('settings.proxy.modeGroup') }}</span>
      <div class="modes">
        <button
          v-for="one in modes"
          :key="one.value"
          type="button"
          class="mode"
          :class="{ on: mode === one.value }"
          @click="write({ mode: one.value })"
        >
          <span class="radio" :class="{ on: mode === one.value }"></span>
          <span class="mode-lines">
            <span class="mode-title">{{ one.title }}</span>
            <span class="mode-note">{{ one.note }}</span>
          </span>
        </button>
      </div>
    </div>

    <!-- What the system mode amounts to: the variables as they stand, and the way to stop following
         them. Read at launch, because that is when the engine reads them. The card is drawn by hand
         rather than from a section: the note and the link stand under it, and a section's note is
         under the card as well — but with no room beside it for an action. -->
    <div v-if="mode === ProxyMode.ProxySystem" class="env">
      <span class="group-label">{{ t('settings.proxy.environment') }}</span>
      <div class="env-card">
        <div v-for="entry in environment" :key="entry.key" class="env-row">
          <span class="env-key">{{ entry.key }}</span>
          <span class="env-value" :class="{ unset: !entry.value }">
            {{ entry.value || t('settings.proxy.envUnset') }}
          </span>
        </div>
      </div>
      <div class="env-foot">
        <span class="note">{{ t('settings.proxy.envNote') }}</span>
        <button type="button" class="link" @click="takeFromEnvironment">
          {{ t('settings.proxy.envToManual') }}
        </button>
      </div>
    </div>

    <!-- The server is a card of blocks rather than of rows: a host, a port and a pair of credentials
         do not fit the right-hand slot a switch gets, so each takes the whole card and is named above
         itself — which is how the drawing has it. -->
    <SettingsSection v-else-if="mode === ProxyMode.ProxyManual" :label="t('settings.proxy.server')">
      <div class="card-block">
        <span class="address">
          <span class="protocol">
            <select :value="protocol" @change="pickProtocol(($event.target as HTMLSelectElement).value)">
              <option v-for="one in protocols" :key="one" :value="one">{{ one }}</option>
            </select>
            <Icon name="ChevronDown" :size="8" :stroke-width="2.6" />
          </span>
          <input
            v-model="host"
            class="part"
            spellcheck="false"
            placeholder="proxy.example.com"
            @keydown.enter="commitHost"
            @blur="commitHost"
          />
          <span class="colon">:</span>
          <input
            v-model="port"
            class="port"
            spellcheck="false"
            placeholder="3128"
            @keydown.enter="commitPort"
            @blur="commitPort"
          />
        </span>
        <span class="note">{{ t('settings.proxy.addressNote') }}</span>
      </div>

      <div class="card-block">
        <div class="ask">
          <span class="lines">
            <span class="line-title">{{ t('settings.proxy.auth') }}</span>
            <span class="note">{{ t('settings.proxy.authNote') }}</span>
          </span>
          <Switch
            :model-value="authShown"
            :aria-label="t('settings.proxy.auth')"
            @update:model-value="toggleAuth(!!$event)"
          />
        </div>
        <!-- The credentials belong to the switch above them: the pair appears under it rather than
             on a line of its own, and the two are named the way a field is named. -->
        <div v-if="authShown" class="pair">
          <label class="field">
            <span class="field-label">{{ t('settings.proxy.user') }}</span>
            <Input
              v-model="user"
              spellcheck="false"
              @keydown.enter="commitUser"
              @blur="commitUser"
            />
          </label>
          <label class="field">
            <span class="field-label">{{ t('settings.proxy.password') }}</span>
            <span class="veiled">
              <Input
                v-model="password"
                :type="shown ? 'text' : 'password'"
                spellcheck="false"
                autocomplete="off"
                :placeholder="proxy.hasPassword ? t('settings.proxy.passwordSet') : ''"
                @input="passwordTouched = true"
                @keydown.enter="commitPassword"
                @blur="commitPassword"
              />
              <button
                type="button"
                class="eye"
                :aria-label="t('settings.proxy.reveal')"
                @click="toggleShown"
              >
                <Icon :name="shown ? 'EyeOff' : 'Eye'" :size="12" />
              </button>
            </span>
          </label>
        </div>
      </div>

      <div class="card-block">
        <span class="lines">
          <span class="line-title">{{ t('settings.proxy.bypass') }}</span>
          <span class="note">{{ t('settings.proxy.bypassNote') }}</span>
        </span>
        <ChipField
          :values="proxy.bypass ?? []"
          :placeholder="t('settings.proxy.bypassAdd')"
          @change="write({ bypass: $event })"
        />
      </div>

      <!-- The way to ask whether any of it works, on the card's own foot: the answer stands beside
           the button that asked for it, and it is the last thing on the card. -->
      <div class="card-block foot">
        <Button size="field" :disabled="probing" @click="test">
          {{ probing ? t('settings.proxy.probeRunning') : probe ? t('settings.proxy.probeAgain') : t('settings.proxy.probeRun') }}
        </Button>
        <span v-if="probe" class="answer" :class="{ ok: probe.ok, bad: !probe.ok }">
          <span class="dot" :class="{ ok: probe.ok }"></span>
          <span class="answer-text">{{ probeLine }}</span>
        </span>
      </div>
    </SettingsSection>

    <div v-else class="direct">{{ t('settings.proxy.offNote') }}</div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.625rem;
}

.modes-block {
  @apply flex flex-col gap-2.5;
}

.group-label {
  @apply text-12 font-semibold text-text-tertiary;
}

.env {
  @apply flex flex-col gap-2.5;
}

.env-card {
  @apply border border-border rounded-[0.625rem] overflow-hidden;
}

.env-row {
  @apply grid items-center gap-4;
  grid-template-columns: 9.375rem minmax(0, 1fr);
  min-height: 2.75rem;
  padding: 0 1rem;
}

.env-row + .env-row {
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.env-key {
  @apply text-12 text-text-secondary;
  font-family: var(--mono);
}

.env-value {
  @apply text-13;
  font-family: var(--mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.env-value.unset {
  @apply text-text-tertiary;
}

.env-foot {
  @apply flex items-center justify-between gap-4;
}

.env-foot .note {
  @apply text-12 text-text-tertiary;
}

.link {
  @apply flex-none text-13 cursor-pointer;
  padding: 0.25rem 0;
  font-family: inherit;
  color: var(--accent);
  background: transparent;
  border: 0;
  white-space: nowrap;
}

.modes {
  @apply grid gap-2.5;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.mode {
  @apply flex items-start gap-2.5 text-left cursor-pointer rounded-[0.625rem] bg-bg-panel;
  font: inherit;
  padding: 0.75rem 0.875rem;
  border: 1px solid var(--border);
}

.mode:hover {
  border-color: var(--border-strong);
}

.mode.on {
  border-color: var(--accent);
  background: var(--sel-card);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.radio {
  @apply flex-none rounded-full mt-px;
  width: 1rem;
  height: 1rem;
  box-sizing: border-box;
  border: 1.5px solid var(--border-strong);
  background: var(--bg-panel);
}

.radio.on {
  border: 5px solid var(--accent);
}

.mode-lines {
  @apply flex flex-col min-w-0;
  gap: 0.125rem;
}

.mode-title {
  @apply text-14 font-semibold text-text;
}

.mode-note {
  @apply text-12 text-text-secondary;
  line-height: 1.4;
  text-wrap: pretty;
}

.card-block {
  @apply flex flex-col;
  gap: 0.5rem;
  padding: 0.875rem 1rem;
}

.lines {
  @apply flex flex-col min-w-0;
  gap: 0.125rem;
}

.line-title {
  @apply text-14;
}

.ask {
  @apply flex items-center justify-between gap-4;
}

.pair {
  @apply grid gap-2.5;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.field {
  @apply flex flex-col;
  gap: 0.25rem;
}

.field-label {
  @apply text-12 text-text-tertiary;
}

.foot {
  @apply flex-row items-center;
  gap: 0.875rem;
  padding: 0.75rem 1rem;
  background: var(--bg-hover);
}

.answer {
  @apply inline-flex items-center gap-2 text-13 min-w-0;
}

.answer.ok {
  color: var(--green-text);
}

.answer.bad {
  color: var(--red-text);
}

.answer-text {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap;
}

.address {
  @apply flex items-stretch h-8.5 rounded-[0.375rem];
  border: 1px solid var(--border-strong);
  background: var(--bg-panel);
}

.protocol {
  @apply relative inline-flex items-center flex-none rounded-l-md;
  border-right: 1px solid var(--border-strong);
  background: var(--bg-hover);
}

.protocol select {
  @apply h-full border-0 outline-none bg-transparent cursor-pointer text-13 font-semibold;
  appearance: none;
  font-family: inherit;
  color: var(--text);
  padding: 0 1.5rem 0 0.75rem;
}

.protocol :deep(svg) {
  @apply absolute pointer-events-none text-text-tertiary;
  right: 0.5rem;
}

.part {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent text-text;
  font-family: var(--mono);
  font-size: var(--text-13);
  padding: 0 0.75rem;
}

.colon {
  @apply inline-flex items-center text-text-tertiary;
  font-family: var(--mono);
  font-size: var(--text-13);
}

.port {
  @apply flex-none border-0 outline-none bg-transparent text-text;
  font-family: var(--mono);
  font-size: var(--text-13);
  width: 4rem;
  padding: 0 0.75rem 0 0.5rem;
}

.veiled {
  @apply relative flex items-center min-w-0;
}

.veiled :deep(.inp) {
  @apply flex-1 min-w-0;
  padding-right: 1.625rem;
}

.eye {
  @apply absolute right-1 inline-flex items-center justify-center w-5 h-5
         rounded-md cursor-pointer text-text-tertiary;
  background: transparent;
  border: 0;
}

.eye:hover {
  @apply text-text bg-bg-hover;
}

.dot {
  @apply flex-none w-1.5 h-1.5 rounded-full;
  background: var(--red);
}

.dot.ok {
  background: var(--green);
}

.direct {
  @apply rounded-[0.625rem] text-13 text-text-secondary;
  padding: 0.875rem 1rem;
  background: var(--bg-hover);
}
</style>
