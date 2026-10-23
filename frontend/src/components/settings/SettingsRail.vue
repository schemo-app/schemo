<script setup lang="ts">
import { computed, onMounted } from 'vue'
import Icon from '../ui/Icon.vue'
import { CATEGORIES, metaKey, nameKey, type CategoryId } from '../../lib/settingsCategories'
import { useAppInfo } from '../../composables/useAppInfo'
import { useUpdateCheck } from '../../composables/useUpdateCheck'
import { useAccount } from '../../composables/useAccount'
import { useMcp } from '../../composables/useMcp'
import { useSettings } from '../../composables/useSettings'
import { t } from '../../i18n'
import { formatVersion } from '../../lib/format'
import { proxyLine } from '../../lib/proxyStatus'
import { COMMANDS } from '../../lib/commands'

// The window's index: a search field, one row per category, and the version at the foot.
const { active, query, counts } = defineProps<{
  active: CategoryId
  query: string
  counts: Partial<Record<CategoryId, number>>
}>()
const emit = defineEmits<{
  (e: 'select', id: CategoryId): void
  (e: 'search', value: string): void
  (e: 'open-first'): void
}>()

const { phase, latest } = useUpdateCheck()
const { state: accountState, serverDown } = useAccount()
const { settings, loadSettings } = useSettings()
const { running: serverRunning, busy: serverBusy, loadMCP } = useMcp()
const { name: appName, version, build } = useAppInfo()

const searching = computed(() => query.trim().length > 0)

// The build and how the app stands on updates. Each state is a whole sentence of the catalogue, for
// the translator to read whole; a check that never ran says nothing about releases.
const meta = computed(() => {
  const said = { build: build.value || '—' }
  switch (phase.value) {
    case 'available':
      return t('settings.buildAvailable', { ...said, version: formatVersion(latest.value) })
    case 'uptodate':
      return t('settings.buildCurrent', said)
    default:
      return t('settings.build', said)
  }
})

const versionLabel = computed(() =>
  [appName.value, formatVersion(version.value)].filter(Boolean).join(' ')
)

// What stands under a section's name. Two of them say something the section cannot: the proxy, what
// it amounts to right now, and the account, who is signed in; the words for nobody being signed in
// are the state's own.
function line(category: (typeof CATEGORIES)[number]): string {
  // The proxy answers with nothing until its settings arrive, and a row of the rail always has a
  // second line to draw: the section's own description is what stands in for it.
  if (category.id === 'proxy') return proxyLine(t, settings.value?.proxy) || t(metaKey(category))
  if (category.id === 'account') {
    if (serverDown.value) return t('account.serverDown')
    const email = accountState.value?.account?.email
    if (email) return email
  }
  // The keyboard says how many commands there are, and how many of them were made different.
  if (category.id === 'shortcuts') {
    const changed = Object.keys(settings.value?.shortcuts ?? {}).length
    return changed
      ? t('shortcuts.railChanged', { n: changed })
      : t('shortcuts.railCount', { n: COMMANDS.length })
  }
  // The MCP server says whether it is up: a port is either listened on or not, and that is the
  // line a person opens the section for.
  if (category.id === 'mcp') {
    const mcp = settings.value?.mcp
    if (!mcp?.enabled) return t('mcp.railOff')
    if (serverBusy.value) return t('mcp.railBusy')
    if (serverRunning.value) return t('mcp.railRunning', { port: mcp.port })
  }
  return t(metaKey(category))
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('search', '')
  else if (e.key === 'Enter') emit('open-first')
}

function clear() {
  emit('search', '')
}

onMounted(() => {
  void loadSettings()
  // Which sections are up right now: the proxy's line and the server's are readings, and a rail
  // that opened without them would draw the two as off until something changed.
  void loadMCP()
})
</script>

<template>
  <nav class="rail">
    <div class="search">
      <label class="field">
        <Icon name="Search" :size="14" class="lens" />
        <input
          :value="query"
          type="text"
          spellcheck="false"
          :placeholder="t('settings.search.placeholder')"
          @input="emit('search', ($event.target as HTMLInputElement).value)"
          @keydown="onKey"
        />
        <button
          v-if="searching"
          type="button"
          class="wipe"
          :title="t('settings.search.clear')"
          @click="clear"
        >
          <Icon name="X" :size="8" :stroke-width="3.4" />
        </button>
      </label>
    </div>

    <div class="rows">
      <button
        v-for="category in CATEGORIES"
        :key="category.id"
        type="button"
        class="row"
        :class="{
          active: !searching && active === category.id,
          quiet: searching && !counts[category.id],
        }"
        @click="emit('select', category.id)"
      >
        <span class="tile" :style="{ background: category.tile }">
          <Icon :name="category.icon" :size="14" :stroke-width="1.9" />
        </span>
        <span class="lines">
          <span class="name">{{ t(nameKey(category.id)) }}</span>
          <span class="meta">{{ line(category) }}</span>
        </span>
        <span v-if="searching && counts[category.id]" class="count">{{ counts[category.id] }}</span>
      </button>
    </div>

    <div class="foot">
      <span class="foot-version">{{ versionLabel }}</span>
      <span class="foot-meta">{{ meta }}</span>
    </div>
  </nav>
</template>

<style scoped>
@reference "../../style.css";

.rail {
  @apply flex-none w-58.5 flex flex-col border-r;
  padding: 0.5rem 0.5rem 0;
  background: var(--bg-sidebar);
  border-color: var(--border);
}

.search {
  @apply flex-none;
  padding: 0.75rem 0.5rem 0.25rem;
}

.field {
  @apply flex items-center gap-2 h-8 rounded-lg;
  padding: 0 0.5rem 0 0.625rem;
  background: var(--bg-panel);
  border: 1px solid var(--border-strong);
  --wails-draggable: no-drag;
}

.lens {
  @apply flex-none text-text-tertiary;
}

.field input {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent text-13 text-text;
  font-family: inherit;
}

.wipe {
  @apply flex-none inline-flex items-center justify-center rounded-full cursor-pointer
         text-text-secondary;
  width: 1rem;
  height: 1rem;
  padding: 0;
  border: 0;
  background: var(--bg-active);
}

.rows {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col;
  gap: 0.125rem;
  padding: 0.3rem 0 0.75rem;
}

.row {
  @apply flex items-center gap-3 text-left border-0 rounded-lg bg-transparent cursor-pointer;
  font: inherit;
  padding: 0.3rem 0.5rem;
  color: var(--text);
  --wails-draggable: no-drag;
}

.row:hover {
  @apply bg-bg-hover;
}

.row.active {
  @apply bg-accent-soft;
}

.row.quiet {
  @apply opacity-40;
}

.row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.tile {
  @apply inline-flex items-center justify-center flex-none w-6 h-6
         rounded-md text-white;
}

:root.dark .tile {
  color: var(--bg-panel);
}

.lines {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 1px;
}

.name {
  @apply text-14 overflow-hidden text-ellipsis whitespace-nowrap;
}

.row.active .name {
  @apply font-semibold;
}

.meta {
  @apply text-12 text-text-tertiary overflow-hidden text-ellipsis whitespace-nowrap;
}

.count {
  @apply flex-none inline-flex items-center justify-center h-5 min-w-5 px-1.5 rounded-full
         text-11 font-semibold;
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  color: var(--accent);
}

.foot {
  @apply flex-none flex flex-col;
  padding: 0.75rem 0.5rem 0.875rem;
  gap: 0.125rem;
  margin: 0 0.5rem;
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.foot-version {
  @apply text-13 text-text-secondary;
}

.foot-meta {
  @apply text-12 text-text-tertiary;
}
</style>
