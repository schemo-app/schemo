<script setup lang="ts">
import { computed } from 'vue'
import { SearchKind } from '../../../bindings/schemo/internal/domain'
import type { PaletteRow } from '../../stores/search'
import { kindIcon, noteLabel } from '../../lib/searchKinds'
import { formatAgo } from '../../i18n'
import Icon from '../ui/Icon.vue'
import MatchText from './MatchText.vue'

const { row } = defineProps<{ row: PaletteRow; query: string; active: boolean }>()
defineEmits<{ (e: 'activate'): void }>()

const trail = computed(() => {
  const path = row.hit?.path ?? []
  return path.filter((step) => step !== '').join(' › ')
})

const icon = computed(() => row.command?.icon ?? kindIcon(row.hit?.kind ?? SearchKind.SearchAction))
const label = computed(() => row.command?.label ?? row.hit?.title ?? '')
const badge = computed(() => row.hit?.badge ?? '')

const note = computed(() => {
  const shaped = noteLabel(row.hit?.note)
  if (shaped) return shaped
  const at = row.hit?.at
  return at ? formatAgo(at) : ''
})
</script>

<template>
  <div class="search-row" :class="{ 'search-row-active': active }" @click="$emit('activate')">
    <span v-if="badge" class="search-badge">{{ badge }}</span>
    <Icon v-else :name="icon" :size="14" class="search-glyph" />
    <span class="search-title" :class="{ 'search-title-mono': !!badge }">
      <MatchText :text="label" :query="query" />
    </span>
    <span v-if="trail" class="search-trail"><MatchText :text="trail" :query="query" /></span>
    <span v-if="note" class="search-note">{{ note }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.search-row {
  @apply flex items-center gap-2.5 mx-2 px-2 py-1.5 rounded-md cursor-default;
  transition: background 0.15s ease;
}

.search-row:hover:not(.search-row-active) {
  background: var(--bg-hover);
}

.search-row-active {
  background: var(--accent-soft);
}

.search-badge {
  @apply inline-flex items-center justify-center flex-none w-11 h-5 rounded-[0.25rem] text-10 font-bold;
  font-family: var(--mono);
  color: var(--accent);
  background: var(--accent-soft);
}

.search-glyph {
  @apply flex-none text-accent;
}

.search-title {
  @apply flex-none text-13 text-text truncate;
}

.search-title-mono {
  @apply flex-1 min-w-0 text-13;
  font-family: var(--mono);
}

.search-trail {
  @apply flex-none text-11 text-text-tertiary truncate max-w-47.5;
}

.search-note {
  @apply flex-none ml-auto text-11 text-text-tertiary whitespace-nowrap;
}
</style>
