<script setup lang="ts">
import { computed, ref } from 'vue'
import { useEventListener } from '@vueuse/core'
import SettingsSection from './SettingsSection.vue'
import { Button, IconButton } from '../ui/button'
import Icon from '../ui/Icon.vue'
import { useCommands } from '../../composables/useCommands'
import { usePlatform } from '../../composables/usePlatform'
import { useSettings } from '../../composables/useSettings'
import { useMessages } from '../../i18n'
import { capsOf, COMBO_NONE, comboOfEvent } from '../../lib/commands'

// The keyboard as it is written down: every command the window carries out, what it wears now, and
// the one place a combination is changed. What is changed is kept in the settings and read back by
// the same registry every handler asks — so this page is what the keyboard does rather than a
// drawing of it.

const { t } = useMessages()
const { isMac } = usePlatform()
const { settings, setShortcut, resetShortcuts } = useSettings()
const { commands, combo } = useCommands()

const query = ref('')
// The command whose combination is being recorded, and the clash a recorded one ran into — the two
// cannot stand at once, and picking a row drops whatever the previous one was showing.
const recording = ref('')
const conflict = ref<{ id: string; combo: string; other: string } | null>(null)

const changed = computed(() => Object.keys(settings.value?.shortcuts ?? {}).length)
const anyChanged = computed(() => changed.value > 0)

interface Row {
  id: string
  label: string
  caps: string[]
  changed: boolean
  /** The clash drawn under this row, already worded: two commands cannot share a combination. */
  clash: string
}

const groups = computed(() => {
  const wanted = query.value.trim().toLowerCase()
  const found: { key: string; label: string; rows: Row[] }[] = []

  for (const command of commands) {
    const caps = capsOf(combo(command.id), isMac.value)
    const label = t(command.label)
    // The search looks through the words and the keys: somebody hunting for ⌘F is not looking for a
    // word the label has.
    if (
      wanted &&
      !label.toLowerCase().includes(wanted) &&
      !caps.join(' ').toLowerCase().includes(wanted) &&
      !caps.join('').toLowerCase().includes(wanted)
    ) {
      continue
    }

    const group = t(command.group)
    let at = found.find((one) => one.key === command.group)
    if (!at) {
      at = { key: command.group, label: group, rows: [] }
      found.push(at)
    }
    at.rows.push({
      id: command.id,
      label,
      caps,
      changed: settings.value?.shortcuts?.[command.id] !== undefined,
      clash: clashOf(command.id),
    })
  }
  return found
})

// What stands under a row when the combination somebody pressed is another command's: the words,
// and the two answers the buttons beside them carry out.
function clashOf(id: string): string {
  const held = conflict.value
  if (!held || held.id !== id) return ''
  const other = commands.find((command) => command.id === held.other)
  return t('shortcuts.conflict', {
    combo: capsOf(held.combo, isMac.value).join(isMac.value ? '' : '+'),
    name: other ? t(other.label) : held.other,
  })
}

function toggleRecord(id: string): void {
  conflict.value = null
  recording.value = recording.value === id ? '' : id
}

function cancel(): void {
  recording.value = ''
  conflict.value = null
}

// A combination is written down, or taken back: what is the default is not a change, and the
// stored map keeps only what somebody made different.
async function apply(id: string, wanted: string): Promise<void> {
  recording.value = ''
  conflict.value = null
  const from = commands.find((command) => command.id === id)?.combo ?? ''
  await setShortcut(id, wanted === from ? '' : wanted)
}

// Two commands on one keyboard would be one command: a combination another one holds already is not
// written down until the person says which of the two keeps it.
function recorded(id: string, pressed: string): void {
  const taken = commands.find((command) => command.id !== id && combo(command.id) === pressed)
  if (taken) {
    recording.value = ''
    conflict.value = { id, combo: pressed, other: taken.id }
    return
  }
  void apply(id, pressed)
}

// The one that was there first gives the combination up and is left wearing none — the two answers
// the clash offers, and this is the one that reassigns.
async function replace(): Promise<void> {
  const held = conflict.value
  if (!held) return
  conflict.value = null
  await setShortcut(held.id, held.combo)
  await setShortcut(held.other, COMBO_NONE)
}

async function resetAll(): Promise<void> {
  cancel()
  await resetShortcuts()
}

// Recording listens on the way down and swallows what it hears: a combination being recorded is one
// the window must not carry out in the middle of being written down.
function onRecordKey(event: KeyboardEvent): void {
  if (!recording.value) return
  event.preventDefault()
  event.stopPropagation()
  if (event.key === 'Escape') {
    cancel()
    return
  }
  const pressed = comboOfEvent(event)
  if (!pressed) return
  recorded(recording.value, pressed)
}

useEventListener(window, 'keydown', onRecordKey, { capture: true })
</script>

<template>
  <div class="pane">
    <div class="head">
      <div class="find">
        <label class="field">
          <Icon name="Search" :size="14" class="lens" />
          <input
            v-model="query"
            type="text"
            spellcheck="false"
            :placeholder="t('shortcuts.searchPlaceholder')"
          />
        </label>
        <Button v-if="anyChanged" variant="outline" size="field" @click="resetAll()">
          {{ t('shortcuts.resetAll') }}
        </Button>
      </div>
      <span class="note">
        {{ isMac ? t('shortcuts.platformNoteMac') : t('shortcuts.platformNote') }}
      </span>
    </div>

    <SettingsSection v-for="group in groups" :key="group.key" :label="group.label">
      <div
        v-for="row in group.rows"
        :key="row.id"
        class="row"
        :class="{ open: recording === row.id }"
      >
        <span class="text">
          <span class="label">{{ row.label }}</span>
          <span v-if="row.clash" class="clash">
            {{ row.clash }}
            <button type="button" class="act" @click="replace()">
              {{ t('shortcuts.replace') }}
            </button>
            <button type="button" class="plain" @click="cancel()">{{ t('common.cancel') }}</button>
          </span>
        </span>

        <button
          type="button"
          class="combo"
          :class="{ rec: recording === row.id }"
          :title="t('shortcuts.change')"
          @click="toggleRecord(row.id)"
        >
          <span v-if="recording === row.id" class="pressing">{{ t('shortcuts.recording') }}</span>
          <template v-else-if="row.caps.length">
            <span v-for="(cap, at) in row.caps" :key="at" class="cap">{{ cap }}</span>
          </template>
          <span v-else class="cap none">{{ t('shortcuts.unassigned') }}</span>
        </button>

        <span class="reset">
          <IconButton
            v-if="row.changed"
            variant="subtle"
            size="xl"
            :hint="t('shortcuts.reset')"
            @click="apply(row.id, '')"
          >
            <Icon name="RefreshCw" :size="14" :stroke-width="2" />
          </IconButton>
        </span>
      </div>
    </SettingsSection>

    <p v-if="!groups.length" class="empty">{{ t('common.nothingFound') }}</p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.125rem;
}

.head {
  @apply flex flex-col;
  gap: 0.625rem;
}

.find {
  @apply flex items-center gap-2.5;
}

.field {
  @apply flex items-center gap-2 h-8 rounded-lg;
  width: 20rem;
  padding: 0 0.5rem 0 0.625rem;
  background: var(--bg-panel);
  border: 1px solid var(--border-strong);
}

.lens {
  @apply flex-none text-text-tertiary;
}

.field input {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent text-13 text-text;
  font-family: inherit;
}

.note {
  @apply text-12 text-text-tertiary;
}

.empty {
  @apply m-0 text-13 text-text-secondary;
  padding: 2rem 0;
  text-align: center;
}

.row {
  @apply grid items-center gap-3;
  grid-template-columns: minmax(0, 1fr) auto 1.75rem;
  min-height: 2.75rem;
  padding: 0.375rem 0.625rem 0.375rem 1rem;
}

.text {
  @apply flex flex-col min-w-0;
  gap: 0.1875rem;
}

.label {
  @apply text-14;
}

.clash {
  @apply flex items-center text-12;
  gap: 0.625rem;
  color: var(--orange-text);
}

.act,
.plain {
  @apply border-0 bg-transparent p-0 cursor-pointer text-12;
  font-family: inherit;
}

.act {
  color: var(--accent);
}

.plain {
  @apply text-text-tertiary;
}

.act:hover,
.plain:hover {
  text-decoration: underline;
}

.combo {
  @apply inline-flex items-center gap-1 rounded-[0.4375rem] cursor-pointer;
  min-height: 1.875rem;
  padding: 0.1875rem 0.375rem;
  border: 1px solid transparent;
  background: transparent;
  font-family: inherit;
}

.combo:hover {
  border-color: var(--border-strong);
}

.combo.rec {
  background: var(--accent-soft);
  border-color: var(--accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 13%, transparent);
}

.pressing {
  @apply text-12 text-accent whitespace-nowrap;
  padding: 0 0.25rem;
}

.cap {
  @apply inline-flex items-center justify-center text-12 text-text-secondary whitespace-nowrap;
  min-width: 1.375rem;
  height: 1.375rem;
  padding: 0 0.375rem;
  border: 1px solid var(--border-strong);
  border-bottom-width: 2px;
  border-radius: 0.3125rem;
  background: var(--bg-inset);
}

.cap.none {
  @apply text-text-tertiary;
  font-style: italic;
}

.reset {
  @apply flex justify-center;
}
</style>
