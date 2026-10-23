<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import { useWailsEvent } from '../../composables/useWailsEvent'
import { useTabsStore } from '../../stores/tabs'
import { t } from '../../i18n'
import { methodInkClass } from '../../lib/format'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import type { TabView } from '../../lib/tabView'

type Entry = TabView & { id: string; preview: boolean; dirty: boolean; pinned: boolean; sub: string }
const { entries, hiddenCount } = defineProps<{ entries: Entry[]; hiddenCount: number }>()
const tabs = useTabsStore()
const open = ref(false)
const query = ref('')
const index = ref(0)
const input = ref<HTMLInputElement | null>(null)
const list = ref<HTMLElement | null>(null)
const keyMode = ref(false)
const recent = ref<string[]>([])
watch(() => tabs.activeId, (id) => {
  if (id) recent.value = [id, ...recent.value.filter((used) => used !== id)]
}, { immediate: true })
const filtered = computed(() => {
  const q = query.value.trim().toLocaleLowerCase()
  const rank = (id: string) => {
    const at = recent.value.indexOf(id)
    return at < 0 ? Number.MAX_SAFE_INTEGER : at
  }
  return [...entries].sort((a, b) => rank(a.id) - rank(b.id))
    .filter((entry) => !q || `${entry.title} ${entry.method} ${entry.sub}`.toLocaleLowerCase().includes(q))
})
const selected = computed(() => filtered.value[Math.min(index.value, filtered.value.length - 1)])
watch(query, () => {
  keyMode.value = false
  index.value = 0
})
watch(open, (shown) => {
  if (shown) {
    query.value = ''
    index.value = 0
    void nextTick(() => input.value?.focus())
  } else keyMode.value = false
})
watch([index, () => filtered.value.length], () => {
  index.value = Math.min(index.value, Math.max(0, filtered.value.length - 1))
  void nextTick(() => list.value?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
})
function choose(entry = selected.value) {
  if (!entry) return
  open.value = false
  void tabs.activate(entry.id)
}
function move(delta: number) {
  const length = filtered.value.length
  if (length) index.value = (index.value + delta + length) % length
}
function onKey(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    event.stopPropagation()
    move(event.key === 'ArrowDown' ? 1 : -1)
  } else if (event.key === 'Enter') {
    event.preventDefault()
    choose()
  }
}
function cycle(backward: boolean, fromKey = true) {
  if (entries.length < 2) return
  if (!open.value) {
    open.value = true
    void nextTick(() => {
      keyMode.value = fromKey
      index.value = backward ? filtered.value.length - 1 : 1
    })
  } else move(backward ? -1 : 1)
}

// macOS delivers Ctrl+Tab through the native window because WKWebView consumes it.
useWailsEvent('menu-command', (event) => {
  if (event.data === 'tab-switch-next' || event.data === 'tab-switch-previous') {
    cycle(event.data === 'tab-switch-previous', false)
  }
})

// Releasing Control commits the tab previewed by a browser key event.
useEventListener(window, 'keydown', (event: KeyboardEvent) => {
  if (event.key !== 'Tab' || !event.ctrlKey || event.altKey || event.metaKey || entries.length < 2) return
  event.preventDefault()
  cycle(event.shiftKey)
})
useEventListener(window, 'keyup', (event: KeyboardEvent) => {
  if (event.key === 'Control' && open.value && keyMode.value) choose()
})
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button v-if="hiddenCount || open" type="button" class="tab-overflow" :title="`${t('tabs.allTabs')} · Ctrl+Tab`" :aria-label="t('tabs.allTabs')">
        <span>{{ hiddenCount ? `+${hiddenCount}` : entries.length }}</span>
        <Icon name="ChevronDown" :size="10" :stroke-width="2.6" />
      </button>
    </PopoverTrigger>
    <PopoverContent class="tab-switcher" align="end" :side-offset="7" @keydown="onKey" @close-auto-focus.prevent>
      <label class="navigation-popover-search">
        <Icon name="Search" :size="13" />
        <input ref="input" v-model="query" :placeholder="t('tabs.findOpen')" :aria-label="t('tabs.findOpen')" spellcheck="false" role="combobox" aria-expanded="true" aria-controls="open-tab-list" :aria-activedescendant="selected ? `open-tab-${selected.id}` : undefined" />
        <span class="navigation-popover-count">{{ filtered.length }}<template v-if="query.trim()"> / {{ entries.length }}</template></span>
      </label>
      <div id="open-tab-list" ref="list" class="navigation-popover-list" role="listbox" :aria-label="t('tabs.allTabs')">
        <div v-for="(entry, i) in filtered" :id="`open-tab-${entry.id}`" :key="entry.id" class="navigation-popover-row switcher-row" role="option" :aria-selected="entry.id === selected?.id" :data-selected="entry.id === selected?.id" @mouseenter="index = i" @click="choose(entry)">
          <span class="switcher-mark">
            <span v-if="entry.method" class="switcher-method" :class="methodInkClass(entry.method)">{{ entry.method }}</span>
            <Icon v-else-if="entry.icon" :name="entry.icon" :size="14" :stroke-width="1.8" />
          </span>
          <span class="switcher-title" :class="{ current: tabs.activeId === entry.id, preview: entry.preview }">{{ entry.title }}</span>
          <span v-if="entry.sub" class="switcher-sub">{{ entry.sub }}</span>
          <span class="switcher-slot">
            <button v-if="entry.id === selected?.id && !entry.pinned" type="button" class="switcher-close" :title="t('tabs.closeTab')" :aria-label="t('tabs.closeTab')" @click.stop="tabs.close(entry.id)"><Icon name="X" :size="10" /></button>
            <span v-else-if="entry.dirty" class="switcher-dirty" :title="t('tabs.unsavedClose')"></span>
            <Icon v-else-if="entry.pinned" name="Pin" :size="11" />
          </span>
        </div>
        <div v-if="!filtered.length" class="navigation-popover-empty">{{ t('common.nothingFound') }}</div>
      </div>
      <button type="button" class="navigation-popover-footer" @click="tabs.closeOthers(tabs.activeId); open = false">
        <span class="switcher-title">{{ t('tabs.closeOthers') }}</span>
        <span class="switcher-footer-hint">{{ t('tabs.closeOthersHint') }}</span>
      </button>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.tab-overflow {
  @apply flex items-center flex-none border-0 cursor-default;
  height: 1.625rem;
  margin-left: 0.3rem;
  padding: 0 0.3rem 0 0.5rem;
  gap: 0.25rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  color: var(--text);
  font: inherit;
  font-size: var(--text-12);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  --wails-draggable: no-drag;
}

.tab-overflow:hover {
  background: var(--bg-hover);
}

.switcher-row {
  padding: 0 0.25rem 0 0.5rem;
}

.switcher-row[data-selected='true'] {
  background: var(--bg-hover);
}

.switcher-mark {
  @apply flex items-center flex-none;
  width: 2.875rem;
  color: var(--text-secondary);
}

.switcher-method {
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 600;
}

.switcher-title {
  @apply flex-1 min-w-0 truncate;
}

.switcher-title.current {
  font-weight: 600;
}

.switcher-title.preview {
  font-style: italic;
}

.switcher-sub {
  @apply truncate text-text-tertiary;
  max-width: 34%;
  font-size: var(--text-12);
}

.switcher-slot,
.switcher-close {
  @apply flex items-center justify-center flex-none;
  width: 1.375rem;
  height: 1.375rem;
  color: var(--text-tertiary);
}

.switcher-close {
  width: 1.25rem;
  height: 1.25rem;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
}

.switcher-close:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.switcher-dirty {
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 50%;
  background: var(--text-tertiary);
}

.switcher-footer-hint {
  @apply flex-none text-text-tertiary;
  font-size: var(--text-12);
}
</style>
