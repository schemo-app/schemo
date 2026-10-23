<script setup lang="ts">
import { computed, ref } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Spinner from '../ui/Spinner.vue'
import Button from '../ui/Button.vue'
import Switch from '../ui/Switch.vue'
import { Code } from '../../../bindings/schemo/internal/domain'
import { useAccount } from '../../composables/useAccount'
import { describeFailure, formatAgo, formatIn, t } from '../../i18n'
import { syncingOff, useSyncStore } from '../../stores/sync'
import { useSyncStatus } from '../../composables/useAppEvents'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useWorkspacesStore } from '../../stores/workspaces'

// Where syncing stands in one coloured line, the switch for this device, and what of it may leave.
// The pane draws the engine, so it follows it: the status as it opens and whatever the engine says
// while somebody looks.

const sync = useSyncStore()
const workspaces = useWorkspacesStore()
const { failure } = useAccount()
const { save } = useSettings()

useSyncStatus()

const off = computed(() => syncingOff(sync.workspaces))
const linkedWorkspaces = computed(() => sync.workspaces.filter((workspace) => workspace.remoteId !== ''))
const waiting = computed(() => sync.waitingCount)

// What the switch does, in one line: the status above already says where syncing stands, so this
// note is about the switch itself.
const syncNote = computed(() => (off.value ? t('sync.toggleOff') : t('sync.toggleOn')))

// Each reason a round comes to nothing, said as the thing it is about: "the server is not
// answering" about a workspace that is gone sends a person to wait on a server that is answering fine.
function failureWord(code: Code): string {
  switch (code) {
    case Code.CodeServerUnreachable:
      return t('sync.noServer')
    case Code.CodeWorkspaceMissing:
    case Code.CodeNotFound:
      return t('sync.gone')
    case Code.CodeNotAllowed:
      return t('sync.notAllowed')
    default:
      return t('sync.refused')
  }
}

// A round somebody asked for is the button's own business. The engine also tells when a round of
// its own is on, and the row draws both the same way.
const runningNow = ref(false)

async function syncNow(): Promise<void> {
  if (runningNow.value) return
  runningNow.value = true
  try {
    await sync.runRound()
  } catch {
    // The round that fell over recorded its failure, and the answer carrying it may be the one that
    // was lost: the pane reads the status again rather than keeping a picture of what just failed.
    await sync.load()
  } finally {
    runningNow.value = false
  }
}

// The switch is one call, and what it answers is what is drawn: the engine is the side that knows
// whether a workspace was paused, and a window that decided it on its own would be a second answer.
function toggleSync(on: boolean): void {
  void (on ? sync.resume() : sync.pause())
}

// What of this device may leave it. The engine reads the answer before its next push, so a change
// lands in the round that follows rather than in one already on its way.
const scopes = useSetting('syncScopes')

async function setScope(patch: { runs?: boolean; secrets?: boolean }): Promise<void> {
  failure.value = ''
  try {
    await save({ syncScopes: patch })
  } catch (error) {
    failure.value = describeFailure(error)
  }
}

const conflict = computed(() => sync.status?.conflict ?? null)

// The workspace the conflict stands in, named as the rail names it; a workspace gone from the list falls
// back to the row the edit was about.
const conflictName = computed(() => {
  const held = conflict.value
  if (!held) return ''
  const workspace = sync.workspaces.find((one) => one.id === held.workspaceId)
  return workspace?.name || held.name
})

// The one coloured line: where syncing stands and the one thing worth doing about it. Counts are of
// the round in flight (the engine publishes them as they grow) and of the last exchange once it is
// over.
const syncRow = computed(() => {
  if (off.value) {
    return { tone: 'quiet', title: t('sync.offTitle'), note: t('sync.offNote'), action: '', act: doNothing }
  }
  if ((sync.status?.round ?? false) || runningNow.value) {
    return { tone: 'run', title: t('sync.nowRunning'), note: exchanging(), action: '', act: doNothing }
  }
  if (sync.status?.failure) {
    // A server that is away is not painted: nothing is wrong with the app, and the row says the
    // same thing in ordinary ink — see downNote.
    const away = sync.status.failure.code === Code.CodeServerUnreachable
    return {
      tone: away ? 'down' : 'bad',
      title: failureWord(sync.status.failure.code),
      note: away ? downNote() : waitingNote(),
      action: t('account.retry'),
      act: syncNow,
    }
  }
  if (conflict.value) {
    return {
      tone: 'warn',
      title: t('sync.conflictIn', { name: conflictName.value }),
      note: t('sync.conflictNote'),
      action: t('sync.openWorkspace'),
      act: openWorkspace,
    }
  }
  return { tone: 'ok', title: t('sync.allSynced'), note: lastExchange(), action: t('sync.now'), act: syncNow }
})

function doNothing(): void {}

function exchanging(): string {
  const sent = sync.status?.pushed ?? 0
  const got = sync.status?.pulled ?? 0
  if (!sent && !got) return ''
  return t('sync.exchanging', { sent: t('counts.changes', { n: sent }), got: String(got) })
}

// The line while the server is away: the last exchange, whatever is still on this device, and when
// the engine will try again. It is the only place the pane reports it; everything else shows the
// last data it has.
function downNote(): string {
  const parts: string[] = []
  const at = sync.status?.roundAt ?? 0
  if (at) parts.push(t('sync.lastExchange', { when: formatAgo(at) }))
  if (waiting.value) parts.push(t('sync.notSent', { n: waiting.value }))
  const retryAt = sync.status?.retryAt ?? 0
  if (retryAt) parts.push(t('sync.retryIn', { when: formatIn(retryAt) }))
  return parts.join(' · ')
}

/** What is still on this device, for the refusals that are not about the server being away. */
function waitingNote(): string {
  return waiting.value ? t('sync.notSent', { n: waiting.value }) : ''
}

function lastExchange(): string {
  const parts: string[] = []
  const at = sync.status?.roundAt ?? 0
  if (at) parts.push(t('sync.lastExchange', { when: formatAgo(at) }))
  if (linkedWorkspaces.value.length) parts.push(t('counts.workspaces', { n: linkedWorkspaces.value.length }))
  if (waiting.value) parts.push(t('sync.notSent', { n: waiting.value }))
  return parts.join(' · ')
}

function openWorkspace(): void {
  if (conflict.value) void workspaces.switch(conflict.value.workspaceId)
}
</script>

<template>
  <SettingsSection :label="t('account.sync')">
    <!-- The one coloured line: where syncing stands, with the one thing worth doing about it. -->
    <div class="card-block status" :class="syncRow.tone">
      <span class="status-glyph">
        <Spinner v-if="syncRow.tone === 'run'" class="text-accent" />
        <span v-else class="dot"></span>
      </span>
      <span class="status-text">
        <span class="status-title">{{ syncRow.title }}</span>
        <span v-if="syncRow.note" class="status-note">{{ syncRow.note }}</span>
      </span>
      <Button v-if="syncRow.action" variant="outline" size="field" @click="syncRow.act()">
        {{ syncRow.action }}
      </Button>
    </div>
    <SettingsRow :title="t('sync.thisDevice')" :note="syncNote">
      <Switch
        :model-value="!off"
        :aria-label="t('sync.thisDevice')"
        :disabled="!linkedWorkspaces.length"
        @update:model-value="toggleSync(!!$event)"
      />
    </SettingsRow>
    <!-- What of this device is allowed out, drawn only while anything leaves it at all: the two
         answers are about a sync that is on. -->
    <SettingsRow v-if="!off" :title="t('account.syncRuns')" :note="t('account.syncRunsNote')">
      <Switch
        :model-value="scopes.runs"
        :aria-label="t('account.syncRuns')"
        @update:model-value="setScope({ runs: !!$event })"
      />
    </SettingsRow>
    <SettingsRow
      v-if="!off"
      :title="t('account.syncSecrets')"
      :note="scopes.secrets ? t('account.syncSecretsOn') : t('account.syncSecretsOff')"
    >
      <Switch
        :model-value="scopes.secrets"
        :aria-label="t('account.syncSecrets')"
        @update:model-value="setScope({ secrets: !!$event })"
      />
    </SettingsRow>
  </SettingsSection>
</template>

<style scoped>
@reference "../../style.css";

.status {
  @apply flex items-center gap-3;
  padding: 0.875rem 1rem;
}

.status.run,
.status.down {
  color: var(--text);
}

.status.down .status-glyph {
  color: var(--orange);
}

.status.bad {
  color: var(--red-text);
  background: color-mix(in srgb, var(--red-text) 10%, transparent);
}

.status.warn {
  color: var(--orange-text);
  background: color-mix(in srgb, var(--orange-text) 12%, transparent);
}

.status.ok {
  color: var(--green-text);
}

.status-glyph {
  @apply flex-none inline-flex items-center justify-center;
  width: 1.125rem;
  height: 1.125rem;
}

.dot {
  @apply block rounded-full;
  width: 0.5rem;
  height: 0.5rem;
  background: currentColor;
}

.status-text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.125rem;
}

.status-title {
  @apply text-14 font-semibold;
}

.status-note {
  @apply text-12 truncate;
  color: color-mix(in srgb, currentColor 72%, var(--text));
}

.status.quiet .status-title,
.status.quiet .status-note {
  color: var(--text-secondary);
}
</style>
