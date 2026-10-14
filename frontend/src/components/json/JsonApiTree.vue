<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useMessages } from '../../i18n'
import { prettyJson } from '../../lib/json'
import {
  buildResourceIndex,
  dataResources,
  resourceMatchesQuery,
  type JsonApiDocument,
  type Resource,
} from '../../lib/jsonapi'
import ResourceNode from './ResourceNode.vue'
import Icon from '../ui/Icon.vue'

const { t } = useMessages()

const { doc, query = '', highlightKey } = defineProps<{
  doc: JsonApiDocument
  highlightKey?: string | null
  query?: string
}>()

const emit = defineEmits<{
  (e: 'fetch', url: string): void
  (e: 'select', key: string): void
  (e: 'inspect', path: string): void
}>()

const resourceIndex = computed(() => buildResourceIndex(doc))
const primaryData = computed(() => dataResources(doc))
const included = computed(() => doc.included ?? [])
const errors = computed(() => doc.errors ?? [])

const includedFlatIndex = computed(() => {
  const map = new Map<string, number>()
  included.value.forEach((r, i) => map.set(r.type + '/' + r.id, i))
  return map
})

function includedPath(r: Resource): string {
  return 'included[' + (includedFlatIndex.value.get(r.type + '/' + r.id) ?? 0) + ']'
}

const highlightedKey = ref<string | null>(null)

const typeFilter = ref<string | null>(null)
const openGroups = ref<Set<string>>(new Set())
const showAllTypes = ref(false)

function toggleTypeFilter(type: string) {
  typeFilter.value = typeFilter.value === type ? null : type
}

// A group shows its resources only when asked for: a search or a picked type is that request, and
// so is a click on its head — or a jump from a relationship, see `forceExpand`. Otherwise
// `included` reads as a summary of what is in the document rather than a wall of cards.
function isGroupOpen(type: string): boolean {
  if (query.trim() || typeFilter.value) return true
  return openGroups.value.has(type)
}

function toggleGroup(type: string) {
  const next = new Set(openGroups.value)
  if (next.has(type)) next.delete(type)
  else next.add(type)
  openGroups.value = next
}

function forceExpand(type: string) {
  if (!isGroupOpen(type)) toggleGroup(type)
}

function highlightAndScroll(key: string) {
  highlightedKey.value = key
  // A relationship can jump into a collapsed group — expand it so the scroll target exists.
  forceExpand(key.split('/')[0])
  nextTick(() => {
    const el = document.getElementById('res-' + key)
    el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
}

watch(
  () => highlightKey,
  (k) => {
    if (k) highlightAndScroll(k)
  }
)

onMounted(() => {
  if (highlightKey) highlightAndScroll(highlightKey)
})

function jumpTo(key: string) {
  highlightAndScroll(key)
  emit('select', key)
}

function isHighlighted(key: string): boolean {
  return highlightedKey.value === key
}

const jsonapiVersion = computed(() => {
  const j = doc.jsonapi
  if (j && typeof j === 'object' && !Array.isArray(j)) {
    const v = (j as Record<string, unknown>).version
    if (typeof v === 'string' && v) return v
  }
  return ''
})

const metaText = computed(() => {
  const m = doc.meta
  return m == null ? '' : prettyJson(m)
})

const isFiltering = computed(() => query.trim().length > 0)

const filteredData = computed(() => {
  const q = query.trim().toLowerCase()
  if (!q) return primaryData.value
  return primaryData.value.filter((r) => resourceMatchesQuery(r, q))
})

const filteredIncluded = computed(() => {
  const q = query.trim().toLowerCase()
  if (!q) return included.value
  return included.value.filter((r) => resourceMatchesQuery(r, q))
})

const typeCounts = computed(() => {
  const map = new Map<string, number>()
  for (const r of filteredIncluded.value) {
    map.set(r.type, (map.get(r.type) ?? 0) + 1)
  }
  return Array.from(map.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count)
})

const visibleTypeChips = computed(() => (showAllTypes.value ? typeCounts.value : typeCounts.value.slice(0, 4)))
const hiddenTypeCount = computed(() => Math.max(0, typeCounts.value.length - 4))

const includedGroups = computed(() => {
  const map = new Map<string, Resource[]>()
  for (const r of filteredIncluded.value) {
    if (typeFilter.value && r.type !== typeFilter.value) continue
    const list = map.get(r.type) ?? []
    list.push(r)
    map.set(r.type, list)
  }
  return Array.from(map.entries()).map(([type, resources]) => ({ type, resources }))
})

const noResults = computed(
  () => isFiltering.value && filteredData.value.length === 0 && filteredIncluded.value.length === 0
)
</script>

<template>
  <div class="ja-root">
    <div v-if="errors.length">
      <div class="ja-section-title">errors</div>
      <pre class="code px-4">{{ prettyJson(errors) }}</pre>
    </div>

    <div v-if="jsonapiVersion" class="ja-link">
      <span class="ja-link-name">jsonapi</span>
      <span class="ja-meta-value mono">v{{ jsonapiVersion }}</span>
    </div>

    <div v-if="metaText">
      <div class="ja-section-title">meta</div>
      <pre class="code px-4">{{ metaText }}</pre>
    </div>

    <div v-if="noResults" class="ja-no-results">{{ t('common.nothingFound') }}</div>

    <template v-if="filteredData.length">
      <div class="ja-section-title">
        data · {{ t('counts.resources', filteredData.length) }}
      </div>
      <ResourceNode
        v-for="(r, i) in filteredData"
        :key="r.type + '/' + r.id"
        :resource="r"
        :resource-index
        :path="'data[' + i + ']'"
        :highlighted="isHighlighted(r.type + '/' + r.id)"
        @jump="jumpTo"
        @fetch="(u) => emit('fetch', u)"
        @inspect="(p) => emit('inspect', p)"
      />
    </template>

    <template v-if="filteredIncluded.length">
      <div class="ja-section-title ja-included-head">
        <span>
          included · {{ t('counts.resources', filteredIncluded.length) }}
          · {{ t('json.types', typeCounts.length) }}
        </span>
        <span v-if="typeCounts.length" class="type-chips">
          <button
            v-for="tc in visibleTypeChips"
            :key="tc.type"
            class="type-chip"
            :class="{ active: typeFilter === tc.type }"
            @click="toggleTypeFilter(tc.type)"
          >
            {{ tc.type }} {{ tc.count }}
          </button>
          <button v-if="hiddenTypeCount && !showAllTypes" class="type-chip more" @click="showAllTypes = true">
            {{ t('json.more', { n: hiddenTypeCount }) }}
          </button>
        </span>
      </div>

      <div v-for="g in includedGroups" :key="g.type" class="included-group">
        <button class="included-group-head" @click="toggleGroup(g.type)">
          <span class="ja-caret" :class="{ open: isGroupOpen(g.type) }">
            <Icon name="ChevronRight" :size="10" />
          </span>
          <span class="ja-type-badge">{{ g.type }}</span>
          <span class="group-res-count">
            {{ t('counts.resources', g.resources.length) }}
            <template v-if="!isGroupOpen(g.type)"> {{ t('json.expandGroup') }}</template>
          </span>
        </button>
        <div v-if="isGroupOpen(g.type)">
          <ResourceNode
            v-for="r in g.resources"
            :key="r.type + '/' + r.id"
            :resource="r"
            :resource-index
            :path="includedPath(r)"
            :highlighted="isHighlighted(r.type + '/' + r.id)"
            @jump="jumpTo"
            @fetch="(u) => emit('fetch', u)"
            @inspect="(p) => emit('inspect', p)"
          />
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.ja-no-results {
  @apply p-4 text-center text-text-tertiary text-12;
}

.ja-included-head {
  @apply flex items-center gap-2 justify-between flex-wrap;
}

.type-chips {
  @apply inline-flex flex-wrap gap-1;
}

.type-chip {
  @apply text-12 text-purple py-px px-1.5 rounded-sm border-0 cursor-pointer;
  font-family: var(--mono);
  background: color-mix(in srgb, var(--purple) 14%, transparent);
}

.type-chip.active {
  outline: 1px solid var(--purple);
}

.type-chip.more {
  color: var(--text-secondary);
  background: var(--bg-hover);
}

.included-group {
  @apply mb-1;
}

.included-group-head {
  @apply flex items-center gap-2 py-2 px-3 rounded-lg text-left cursor-pointer select-none;
  width: calc(100% - 2rem);
  margin: 0.25rem 1rem;
  border: 1px solid var(--border);
  background: var(--bg);
  --wails-draggable: no-drag;
}

.included-group-head:hover {
  @apply bg-bg-hover;
}

.group-res-count {
  @apply flex-1 min-w-0 text-12 text-text-secondary;
}
</style>
