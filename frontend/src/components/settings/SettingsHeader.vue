<script setup lang="ts">
import { computed } from 'vue'
import { CATEGORIES, nameKey, subKey, type CategoryId } from './categories'
import { useSettings } from '../../composables/useSettings'
import { useMessages } from '../../i18n'
import { proxyActive, proxyLine } from '../../lib/proxyStatus'

// The head of the page: which section is open, or what the search came to. The proxy carries a pill
// of its own — its answer is a fact about the machine, not a choice on the screen.
const { active, query, count } = defineProps<{
  active: CategoryId
  query: string
  count: number
}>()

const { t } = useMessages()
const { settings, loadSettings } = useSettings()

const searching = computed(() => query.trim().length > 0)
const held = computed(() => CATEGORIES.find((one) => one.id === active) ?? CATEGORIES[1])

const title = computed(() => (searching.value ? t('settings.search.title') : t(nameKey(active))))

const subtitle = computed(() => {
  if (!searching.value) return t(subKey(held.value))
  return count > 0
    ? t('settings.search.found', { count: String(count) })
    : t('settings.search.for', { query: query.trim() })
})

const proxy = computed(() => settings.value?.proxy ?? null)
const pill = computed(() => proxyLine(t, proxy.value))
const on = computed(() => proxyActive(proxy.value))

void loadSettings()
</script>

<template>
  <div class="head">
    <span class="lines">
      <span class="title">{{ title }}</span>
      <span class="sub">{{ subtitle }}</span>
    </span>

    <span v-if="!searching && active === 'proxy' && pill" class="pill">
      <span class="dot" :class="{ on }"></span>
      <span>{{ pill }}</span>
    </span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.head {
  @apply flex items-start justify-between flex-none;
  gap: 1.5rem;
}

.lines {
  @apply flex flex-col min-w-0;
  gap: 0.25rem;
}

.title {
  @apply text-20 font-semibold;
  letter-spacing: -0.01em;
}

.sub {
  @apply text-13 text-text-secondary;
}

.pill {
  @apply flex-none inline-flex items-center h-7.5 px-3 rounded-full text-12 text-text;
  gap: 0.5rem;
  background: var(--bg-hover);
  white-space: nowrap;
}

.dot {
  @apply flex-none w-1.5 h-1.5 rounded-full;
  background: var(--text-tertiary);
}

.dot.on {
  background: var(--green);
}
</style>
