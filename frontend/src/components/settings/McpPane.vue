<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import { Button } from '../ui/button'
import { Switch } from '../ui/switch'
import Icon from '../ui/Icon.vue'
import type { MCPMode } from '../../../bindings/schemo/internal/domain'
import { useSettings } from '../../composables/useSettings'
import { useMcp } from '../../composables/useMcp'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { prettyJson } from '../../lib/json'
import { MCP } from '../../lib/layout'
import { describeFailure, formatAgo, useMessages } from '../../i18n'

// The MCP server as the stage draws it: what it is and where it listens, the token and the
// configuration an agent is given, what that agent may do, and who has called. The server obeys
// the settings the moment they are saved, so the status card at the top is a reading of the same
// answer the rows below write — never a second thing to keep in step.

const { t } = useMessages()
const { settings, setMCP, rotateMCPToken } = useSettings()
const { running, busy, agents, loadMCP, disconnectAgent, revealToken } = useMcp()
const workspaces = useWorkspacesStore()

const failure = ref('')
const shown = ref(false)
const rotated = ref(false)
const { copied, copy } = useCopyFeedback<'token' | 'config'>(2000)
const portText = ref('')

// The token's value is not in the settings the window mirrors: it arrives when the eye is opened, and
// it is dropped when it is closed. What is always there is the fact and the tail.
const revealed = ref('')
const mcp = computed(() => settings.value?.mcp ?? null)
const spaces = computed(() => workspaces.list ?? [])
const picked = computed(() => mcp.value?.spaces ?? [])
const canSend = computed(() => mcp.value?.mode === 'send')

const tail = computed(() => mcp.value?.tokenTail ?? '')
const masked = computed(() =>
  mcp.value?.hasToken ? `${MCP.tokenPrefix}${'•'.repeat(20)}${tail.value}` : ''
)
const tokenShown = computed(() => (shown.value ? revealed.value : masked.value))

// What an agent's configuration holds, as it will be pasted: the address, and the token — with the
// eye closed the token in it is a mask and a tail, which is enough to recognise and not to use.
const config = computed(() => {
  const token = shown.value ? revealed.value : `${MCP.tokenPrefix}…${tail.value}`
  return prettyJson({
    mcpServers: {
      schemo: {
        url: `http://127.0.0.1:${mcp.value?.port ?? 0}/mcp`,
        headers: { Authorization: `Bearer ${token}` },
      },
    },
  })
})

onMounted(() => {
  void loadMCP()
  if (!spaces.value.length) void workspaces.load()
})

// The field follows the setting rather than starting empty: the port is drawn from the answer the
// section is drawn from, and a save puts it back the way the server took it.
watch(
  () => mcp.value?.port,
  (port) => {
    if (port !== undefined) portText.value = String(port)
  },
  { immediate: true }
)

const title = computed(() => {
  if (busy.value) return t('mcp.busyTitle')
  if (mcp.value?.enabled) return t('mcp.running', { port: mcp.value.port })
  return t('mcp.off')
})

const note = computed(() => {
  if (busy.value) return t('mcp.busyNote', { port: mcp.value?.port ?? '' })
  if (!mcp.value?.enabled) return t('mcp.offNote')
  return canSend.value ? t('mcp.onNote') : t('mcp.onNoteRead')
})

const spacesNote = computed(() => {
  if (mcp.value?.allSpaces) return t('mcp.spacesAllNote')
  return t('mcp.spacesPickNote', { n: picked.value.length, m: spaces.value.length })
})

function agentNote(lastAt: number, today: number): string {
  return [t('mcp.lastCall', { ago: formatAgo(lastAt) }), t('mcp.callsToday', { n: today })].join(' · ')
}

// One edit, one save and one apply: a refusal lands under the rows it was about, worded by the
// catalogue. The answering settings are the source of the rows, so what is drawn is what was saved.
async function save(patch: Parameters<typeof setMCP>[0]): Promise<void> {
  failure.value = ''
  try {
    const saved = await setMCP(patch)
    portText.value = String(saved.mcp.port)
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

function commitPort(): void {
  const port = Number(portText.value.trim())
  if (!Number.isInteger(port) || port === mcp.value?.port) {
    portText.value = String(mcp.value?.port ?? '')
    return
  }
  void save({ port })
}

function setMode(value: string): void {
  void save({ mode: value as MCPMode })
}

function pickSpaces(value: string): void {
  void save({ allSpaces: value === 'all' })
}

function toggleSpace(id: string): void {
  const next = picked.value.includes(id)
    ? picked.value.filter((held) => held !== id)
    : [...picked.value, id]
  void save({ spaces: next })
}

async function rotate(): Promise<void> {
  failure.value = ''
  rotated.value = true
  // The value that was on screen is not the one the server now accepts, so it goes.
  shown.value = false
  revealed.value = ''
  try {
    const saved = await rotateMCPToken()
    portText.value = String(saved.mcp.port)
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

// Opening the eye is the one moment the value crosses: it is asked for then, and let go of when the
// eye closes — a section left open does not keep somebody else's token in the window.
async function toggleToken(): Promise<void> {
  if (shown.value) {
    shown.value = false
    revealed.value = ''
    return
  }
  failure.value = ''
  try {
    revealed.value = await revealToken()
    shown.value = true
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

// Nobody copies a mask, so a copy with the eye still closed opens it first.
async function copyToken(): Promise<void> {
  if (!revealed.value) await toggleToken()
  if (revealed.value) await copy(revealed.value, 'token')
}
</script>

<template>
  <div v-if="mcp" class="pane">
    <!-- Where the server stands: a lamp, what it is, and the one switch that decides it. -->
    <div class="status">
      <span class="dot" :class="{ live: running, warn: busy }"></span>
      <span class="status-text">
        <span class="status-title">{{ title }}</span>
        <span class="status-note">{{ note }}</span>
      </span>
      <Switch
        :model-value="mcp.enabled"
        :aria-label="t('mcp.off')"
        @update:model-value="save({ enabled: !!$event })"
      />
    </div>

    <template v-if="mcp.enabled">
      <SettingsSection :label="t('mcp.connection')">
        <SettingsRow :title="t('mcp.port')" :note="t('mcp.portNote')">
          <input
            v-model="portText"
            class="port"
            inputmode="numeric"
            spellcheck="false"
            @keydown.enter="($event.target as HTMLInputElement).blur()"
            @blur="commitPort"
          />
        </SettingsRow>
        <SettingsRow
          :title="t('mcp.token')"
          :note="rotated ? t('mcp.tokenRotated') : t('mcp.tokenNote')"
        >
          <Button variant="outline" size="field" @click="rotate">{{ t('mcp.rotate') }}</Button>
        </SettingsRow>
        <div class="card-block">
          <span class="veiled">
            <span class="token">{{ tokenShown }}</span>
            <button
              type="button"
              class="icon-button"
              :title="shown ? t('mcp.hide') : t('mcp.show')"
              @click="toggleToken"
            >
              <Icon :name="shown ? 'EyeOff' : 'Eye'" :size="12" />
            </button>
            <button type="button" class="copy" @click="copyToken">
              {{ copied === 'token' ? t('mcp.copiedToken') : t('mcp.copy') }}
            </button>
          </span>
        </div>
        <SettingsRow :title="t('mcp.agentConfig')" :note="t('mcp.agentConfigNote')">
          <Button variant="outline" size="field" @click="copy(config, 'config')">
            {{ copied === 'config' ? t('mcp.copiedConfig') : t('mcp.copy') }}
          </Button>
        </SettingsRow>
        <div class="card-block">
          <pre class="config">{{ config }}</pre>
        </div>
      </SettingsSection>

      <SettingsSection :label="t('mcp.allowed')">
        <SettingsRow
          :title="t('mcp.mode')"
          :note="canSend ? t('mcp.modeNoteSend') : t('mcp.modeNoteRead')"
        >
          <Segment
            :value="mcp.mode"
            :grow="false"
            :options="[
              { value: 'read', label: t('mcp.modeRead') },
              { value: 'send', label: t('mcp.modeSend') },
            ]"
            @pick="setMode"
          />
        </SettingsRow>
        <SettingsRow v-if="canSend" :title="t('mcp.ask')" :note="t('mcp.askNote')">
          <Switch
            :model-value="mcp.ask"
            :aria-label="t('mcp.ask')"
            @update:model-value="save({ ask: !!$event })"
          />
        </SettingsRow>
        <SettingsRow :title="t('mcp.spaces')" :note="spacesNote">
          <Segment
            :value="mcp.allSpaces ? 'all' : 'pick'"
            :grow="false"
            :options="[
              { value: 'all', label: t('mcp.spacesAll') },
              { value: 'pick', label: t('mcp.spacesPick') },
            ]"
            @pick="pickSpaces"
          />
        </SettingsRow>
        <div v-if="!mcp.allSpaces" class="card-block">
          <div class="chips">
            <button
              v-for="space in spaces"
              :key="space.id"
              type="button"
              class="chip"
              :class="{ picked: picked.includes(space.id) }"
              @click="toggleSpace(space.id)"
            >
              <span class="tick">
                <Icon v-if="picked.includes(space.id)" name="Check" :size="8" :stroke-width="4" />
              </span>
              <span class="swatch" :style="{ background: space.color }"></span>
              <span class="cap-line">{{ space.name }}</span>
            </button>
          </div>
        </div>
        <!-- The one rule that is not a choice: an agent composes nothing, so it is never handed the
             values it would compose with. -->
        <SettingsRow :title="t('mcp.secrets')" :note="t('mcp.secretsNote')">
          <span class="chip-static">{{ t('mcp.secretsAlways') }}</span>
        </SettingsRow>
      </SettingsSection>

      <SettingsSection :label="t('mcp.agents')">
        <template v-if="agents.length">
          <SettingsRow
            v-for="agent in agents"
            :key="agent.id"
            :title="agent.name"
            :note="agentNote(agent.lastAt, agent.today)"
          >
            <Button variant="outline" size="field" @click="disconnectAgent(agent.id)">
              {{ t('mcp.disconnect') }}
            </Button>
          </SettingsRow>
        </template>
        <div v-else class="card-block">
          <p class="empty">{{ t('mcp.agentsEmpty') }}</p>
        </div>
      </SettingsSection>
    </template>

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

.dot.warn {
  background: var(--orange);
}

.status-text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.125rem;
}

.status-title {
  @apply text-14 font-semibold;
}

.status-note {
  @apply text-12 text-text-tertiary;
  line-height: 1.45;
}

.port {
  @apply text-13 rounded-[0.4375rem] outline-none;
  width: 5rem;
  height: 1.875rem;
  padding: 0 0.625rem;
  font-family: var(--mono);
  color: var(--text);
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.card-block {
  padding: 0.75rem 1rem 0.875rem;
}

.veiled {
  @apply relative flex items-center h-8 rounded-[0.4375rem] overflow-hidden;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.token {
  @apply flex-1 min-w-0 text-12;
  padding: 0 0.625rem;
  font-family: var(--mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.icon-button {
  @apply inline-flex items-center justify-center w-7.5 h-7.5 cursor-pointer
         text-text-tertiary;
  flex: none;
  background: transparent;
  border: 0;
  padding: 0;
}

.icon-button:hover {
  @apply text-text;
}

.copy {
  @apply flex-none inline-flex items-center h-7.5 px-3 text-12 cursor-pointer;
  color: var(--accent);
  background: transparent;
  border: 0;
  border-left: 1px solid var(--border);
  font-family: inherit;
}

.copy:hover {
  background: var(--bg-hover);
}

.config {
  @apply m-0 text-12 rounded-lg;
  padding: 0.625rem 0.75rem;
  font-family: var(--mono);
  line-height: 1.6;
  color: var(--text-secondary);
  background: var(--bg-sidebar);
  white-space: pre;
  overflow-x: auto;
}

.chips {
  @apply flex flex-wrap gap-2;
}

.chip {
  @apply inline-flex items-center gap-2 h-7.5 text-13 rounded-[0.4375rem] cursor-pointer;
  padding: 0 0.625rem;
  font-family: inherit;
  color: var(--text);
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.chip.picked {
  background: var(--sel-card);
  border-color: var(--accent);
}

.tick {
  @apply inline-flex items-center justify-center flex-none;
  width: 0.875rem;
  height: 0.875rem;
  box-sizing: border-box;
  border-radius: 0.25rem;
  border: 1.5px solid var(--border-strong);
  color: #fff;
}

.chip.picked .tick {
  background: var(--accent);
  border-color: var(--accent);
}

:root.dark .chip.picked .tick {
  color: var(--bg-panel);
}

.swatch {
  @apply flex-none rounded-[0.125rem];
  width: 0.5rem;
  height: 0.5rem;
}

.chip-static {
  @apply inline-flex items-center h-6 px-2.25 text-12 rounded-md;
  color: var(--text-secondary);
  background: var(--bg-hover);
}

.empty {
  @apply m-0 text-12 text-text-tertiary;
  line-height: 1.5;
}
</style>
