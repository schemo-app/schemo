<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { DialogTitle, VisuallyHidden } from 'reka-ui'
import { SearchKind } from '../../../bindings/schemo/internal/domain'
import { useSearchStore } from '../../stores/search'
import { t } from '../../i18n'
import { usePlatform } from '../../composables/usePlatform'
import Dialog from '../ui/Dialog.vue'
import Icon from '../ui/Icon.vue'
import Input from '../ui/Input.vue'
import SearchRow from './SearchRow.vue'

const { open } = defineProps<{ open: boolean }>()

const search = useSearchStore()
const { shortcut } = usePlatform()

const field = ref<InstanceType<typeof Input> | null>(null)
const list = useTemplateRef<HTMLElement>('list')

const chips = computed<{ kind: SearchKind | null; label: string }[]>(() => [
  { kind: null, label: t('search.everything') },
  { kind: SearchKind.SearchRequest, label: t('search.kinds.request') },
  { kind: SearchKind.SearchCollection, label: t('search.kinds.collection') },
  { kind: SearchKind.SearchEnvironment, label: t('search.kinds.environment') },
  { kind: SearchKind.SearchHistory, label: t('search.kinds.history') },
  { kind: SearchKind.SearchAction, label: t('search.kinds.action') },
])

const activeRow = computed(() => search.rows[search.activeIndex] ?? null)

defineEmits<{ (e: 'close'): void }>()

watch(
  () => open,
  async (isOpen) => {
    if (!isOpen) return
    await nextTick()
    field.value?.focus()
  }
)

watch(
  () => search.activeIndex,
  () => {
    void nextTick(() => list.value?.querySelector('.search-row-active')?.scrollIntoView({ block: 'nearest' }))
  }
)

// The palette owns the keyboard while it is open: its keys must not bubble to the window, where
// ⌘↵ is the request being sent — the palette's own ⌘↵ opens in the background, and nothing else.
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    e.stopPropagation()
    search.move(e.key === 'ArrowDown' ? 1 : -1)
    return
  }
  if (e.key !== 'Enter') return
  if (e.target instanceof HTMLElement && e.target.closest('button')) return
  e.preventDefault()
  e.stopPropagation()
  void search.activate(search.active, e.metaKey || e.ctrlKey)
}
</script>

<template>
  <Dialog class="search-palette" placement="top" :open @keydown="onKeydown" @update:open="$emit('close')">
    <VisuallyHidden><DialogTitle>{{ t('common.search') }}</DialogTitle></VisuallyHidden>

    <div class="search-head">
      <Icon name="Search" :size="16" class="search-lens" />
      <Input
        :model-value="search.text"
        variant="bare"
        class="search-field"
        :placeholder="t('search.placeholder')"
        @update:model-value="search.setText"
      />
      <button class="keycap search-esc" @mousedown.prevent @click="$emit('close')">Esc</button>
    </div>

    <div class="search-chips">
      <button
        v-for="chip in chips"
        :key="chip.label"
        class="search-chip"
        :class="{ 'search-chip-on': search.kind === chip.kind }"
        @mousedown.prevent
        @click="search.setKind(chip.kind)"
      >
        {{ chip.label }}
      </button>
    </div>

    <div ref="list" class="search-list">
      <template v-for="section in search.sections" :key="section.kind">
        <div class="search-group">{{ section.label }} · {{ section.total }}</div>
        <SearchRow
          v-for="(row, i) in section.rows"
          :key="row.hit?.id ?? row.command?.id ?? i"
          :row
          :query="search.query"
          :active="activeRow === row"
          @activate="search.activate(row)"
        />
      </template>

      <div v-if="search.rows.length === 0" class="search-empty">{{ t('common.nothingFound') }}</div>
    </div>

    <div class="search-foot">
      <span class="search-hint"><span class="keycap">↑↓</span>{{ t('search.hint.move') }}</span>
      <span class="search-hint"><span class="keycap">↵</span>{{ t('search.hint.open') }}</span>
      <span class="search-hint">
        <span class="keycap">{{ shortcut('↵') }}</span>{{ t('search.hint.background') }}
      </span>
      <span class="search-hint search-hint-end">
        <span class="keycap">Esc</span>{{ t('search.hint.close') }}
      </span>
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

.search-head {
  @apply flex items-center gap-2.5 flex-none h-13 px-4;
  border-bottom: 1px solid var(--border);
}

.search-lens {
  @apply flex-none text-text-tertiary;
}

.search-field {
  @apply flex-1 min-w-0 text-14;
}

.search-esc {
  @apply flex-none cursor-default;
  transition: background 0.15s ease, color 0.15s ease;
}

.search-esc:hover {
  @apply text-text;
  background: var(--bg-hover);
}

.search-chips {
  @apply flex items-center gap-0.5 flex-none px-3 py-2;
  border-bottom: 1px solid var(--border);
}

.search-chip {
  @apply px-2.5 py-1 rounded-md text-12 text-text-secondary;
  transition: background 0.15s ease, color 0.15s ease;
}

.search-chip:hover {
  background: var(--bg-hover);
}

.search-chip-on {
  color: var(--accent);
  font-weight: 600;
  background: var(--accent-soft);
}

.search-list {
  @apply py-1.5 overflow-y-auto;
  max-height: 26.25rem;
}

.search-group {
  @apply px-4 pt-2.5 pb-1 text-11 text-text-tertiary uppercase;
  font-family: var(--mono);
  letter-spacing: 0.08em;
}

.search-empty {
  @apply px-4 py-6 text-center text-13 text-text-tertiary;
}

.search-foot {
  @apply flex items-center gap-3.5 flex-none h-8.5 px-4 text-11 text-text-tertiary;
  border-top: 1px solid var(--border);
}

.search-hint {
  @apply flex items-center gap-1.5;
}

.search-hint-end {
  @apply ml-auto;
}
</style>
