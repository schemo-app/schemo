<script setup lang="ts">
import { computed, ref } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuCheckboxItem, DropdownMenuItemIndicator } from 'reka-ui'
import { useCollectionsStore } from '../../stores/collections'
import { METHODS, methodInkClass } from '../../lib/format'
import { addressOf } from '../../lib/address'
import { findCollection } from '../../lib/collectionTree'
import { urlPieces } from '../../lib/vars'
import { formatDate, t } from '../../i18n'
import MarkdownText from '../ui/MarkdownText.vue'
import Icon from '../ui/Icon.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import type { Collection, LevelRow } from '../../../bindings/schemo/internal/domain'

// What the collection holds, and what each request last answered: the page reads like a table of
// contents, and a row is the way into the request itself.
const {
  rows,
  loading = false,
} = defineProps<{
  rows: {
    row: LevelRow
    status: { code: number; text: string }
    when: number
    bad: boolean
  }[]
  loading?: boolean
}>()

const emit = defineEmits<{ open: [row: LevelRow]; openDocs: [edit: boolean] }>()

const collections = useCollectionsStore()

// The level the page is about, read off the tree by the same answer the page's header uses.
const level = computed(() => findCollection(collections.tree, collections.collectionId))
const description = computed(() => (level.value?.description ?? '').split(/\n\s*\n/).find(paragraph => paragraph.trim() && !/^\s*#/.test(paragraph))?.trim() ?? '')
const hasDescription = computed(() => !!level.value?.description?.trim())
const query = ref('')
const methods = ref(new Set<string>())
const methodCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const { row } of rows) {
    if (row.method) counts.set(row.method, (counts.get(row.method) ?? 0) + 1)
  }
  return counts
})
const methodOptions = computed(() => [...new Set([
  ...METHODS,
  ...methodCounts.value.keys(),
])])
const methodLabel = computed(() => {
  const { size } = methods.value
  if (!size) return t('history.filterAll')
  return size > 2 ? t('collections.methodsSelected', { n: size }) : [...methods.value].join(', ')
})

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return rows.filter(({ row }) =>
    (!methods.value.size || methods.value.has(row.method ?? '')) &&
    (!needle || row.name.toLowerCase().includes(needle) || (row.url ?? '').toLowerCase().includes(needle))
  )
})

const paths = computed(() => {
  const segments = filtered.value.map(({ row }) => addressOf(row.url ?? '').split('/'))
  const first = segments[0] ?? []
  let count = 0
  if (segments.length > 1) {
    for (const segment of first.slice(0, -1)) {
      if (/^[:{]/.test(segment) || !segments.every(parts => count < parts.length - 1 && parts[count] === segment)) break
      count++
    }
  }
  return {
    prefix: first.slice(0, count).join('/'),
    tails: new Map(filtered.value.map(({ row }, index) => {
      const tail = segments[index]?.slice(count).join('/') ?? ''
      return [row.id, urlPieces(count ? '/' + tail : tail)]
    })),
  }
})

const foldersByRequest = computed(() => {
  const folders = new Map<string, Collection>()
  function index(folder: Collection, group: Collection) {
    for (const request of folder.items ?? []) folders.set(request.id, group)
    for (const child of folder.children ?? []) index(child, group)
  }
  for (const folder of level.value?.children ?? []) index(folder, folder)
  return folders
})

const groups = computed(() => {
  const grouped = new Map<string, { folder?: Collection; rows: typeof rows }>()
  grouped.set('', { rows: [] })
  for (const folder of level.value?.children ?? []) grouped.set(folder.id, { folder, rows: [] })
  for (const item of filtered.value) {
    const folder = foldersByRequest.value.get(item.row.id)
    grouped.get(folder?.id ?? '')?.rows.push(item)
  }
  return [...grouped.values()].filter(group => group.rows.length)
})

function toggleMethod(method: string, checked: boolean) {
  if (checked) methods.value.add(method)
  else methods.value.delete(method)
}

function whenOf(at: number): string {
  return at ? formatDate(at, { hour: '2-digit', minute: '2-digit' }) : ''
}
</script>

<template>
  <div class="overview">
    <div v-if="hasDescription" class="desc">
      <div v-if="description" class="desc-preview" @click="emit('openDocs', false)">
        <MarkdownText :text="description" />
      </div>
      <button type="button" class="desc-link" @click="emit('openDocs', false)">
        {{ t('collections.descOpen') }}
      </button>
    </div>
    <button v-else type="button" class="desc-link self" @click="emit('openDocs', true)">
      {{ t('collections.descAdd') }}
    </button>

    <div class="table">
      <div class="overview-filters">
        <label class="overview-search">
          <Icon name="Search" :size="13" :stroke-width="1.5" />
          <input v-model="query" :aria-label="t('collections.filterPlaceholder')" :placeholder="t('collections.filterPlaceholder')" spellcheck="false" />
        </label>
        <DropdownMenuRoot>
          <DropdownMenuTrigger as-child>
            <button type="button" class="overview-methods">
              <span class="text-text-secondary">{{ t('collections.columnMethod') }}</span>
              <span class="font-medium">{{ methodLabel }}</span>
              <Icon name="ChevronDown" :size="10" :stroke-width="1.5" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" class="overview-method-menu">
            <DropdownMenuCheckboxItem
              v-for="method in methodOptions"
              :key="method"
              class="menu-item overview-method-option"
              :model-value="methods.has(method)"
              @update:model-value="toggleMethod(method, $event === true)"
              @select.prevent
            >
              <span class="method-check">
                <DropdownMenuItemIndicator><Icon name="Check" :size="9" :stroke-width="2.5" /></DropdownMenuItemIndicator>
              </span>
              <span class="method flex-1" :class="methodInkClass(method)">{{ method }}</span>
              <span class="text-text-tertiary">{{ methodCounts.get(method) ?? 0 }}</span>
            </DropdownMenuCheckboxItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem @select="methods.clear()">{{ t('collections.resetMethods') }}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuRoot>
        <span v-if="filtered.length !== rows.length" class="overview-count">{{ t('history.countOf', { shown: filtered.length, total: rows.length }) }}</span>
      </div>
      <!-- The headings name columns that are not there: an empty collection says so below instead. -->
      <div v-if="rows.length" class="head">
        <span>{{ t('collections.columnMethod') }}</span>
        <span>{{ t('collections.columnName') }}</span>
        <span>{{ t('collections.columnPath') }}</span>
        <span>{{ t('collections.columnResponse') }}</span>
        <span class="right">{{ t('collections.columnWhen') }}</span>
      </div>

      <template v-for="group in groups" :key="group.folder?.id ?? ''">
        <div v-if="group.folder" class="overview-group">
          <button type="button" @click="collections.select(group.folder.id)">{{ group.folder.name }}</button>
          <span>· {{ group.rows.length }}</span>
        </div>
        <button
          v-for="item in group.rows"
          :key="item.row.id"
          type="button"
          class="row"
          @click="emit('open', item.row)"
        >
          <span class="method" :class="methodInkClass(item.row.method ?? 'GET')">{{ item.row.method }}</span>
          <span class="name">{{ item.row.name }}</span>
          <span class="path mono" :title="item.row.url">
            <span v-if="paths.prefix" class="text-text-tertiary">{{ paths.prefix }}</span>
            <span
              v-for="piece in paths.tails.get(item.row.id)"
              :key="piece.start"
              :class="{ 'text-accent': piece.tokenName || piece.kind === 'path-param' }"
            >{{ piece.text }}</span>
          </span>
          <span>
            <StatusBadge :status="item.status.code" :failed="item.bad">
              {{ item.status.text }}
            </StatusBadge>
          </span>
          <span class="when">{{ whenOf(item.when) }}</span>
        </button>
      </template>

      <div v-if="!rows.length && !loading" class="none">{{ t('collections.noRequests') }}</div>
      <div v-else-if="rows.length && !filtered.length" class="none">{{ t('common.nothingFound') }}</div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.overview {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 1.25rem 1.5rem;
  gap: 1rem;
}

.desc {
  @apply flex flex-col gap-1.5;
  max-width: 45rem;
  line-height: 1.55;
}

.desc-preview {
  max-height: 4.125rem;
  overflow: hidden;
  cursor: default;
}

.desc-link {
  @apply self-start cursor-pointer border-0 bg-transparent p-0 text-accent;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.desc-link.self {
  align-self: flex-start;
}

.table {
  @apply flex flex-col;
}

.overview-filters {
  @apply flex flex-wrap items-center gap-2;
  padding-bottom: 0.625rem;
}

.overview-search,
.overview-methods {
  @apply flex items-center gap-1.5 min-w-0 bg-transparent border-0;
  height: 1.875rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.overview-search {
  @apply text-text-tertiary;
  flex: 0 1 17.5rem;
  min-width: 8.75rem;
}

.overview-search input {
  @apply flex-1 min-w-0 border-0 bg-transparent text-text;
  outline: none;
  font: inherit;
}

.overview-search:focus-within {
  box-shadow: inset 0 0 0 1px var(--accent);
}

.overview-methods {
  @apply text-text cursor-pointer;
  font: inherit;
}

.overview-methods:hover {
  background: var(--bg-hover);
}

.overview-method-menu {
  width: 13.125rem;
}

.overview-method-option {
  gap: 0.625rem;
}

.method-check {
  @apply inline-grid place-items-center rounded border border-border text-accent-text;
  width: 0.875rem;
  height: 0.875rem;
}

.overview-method-option[data-state="checked"] .method-check {
  background: var(--accent);
  border-color: var(--accent);
}

.overview-count {
  @apply ml-auto text-12 text-text-tertiary whitespace-nowrap;
}

.overview-group {
  @apply flex items-center gap-1.5 text-12 text-text-tertiary;
  height: 1.875rem;
  padding: 0 0.625rem;
  margin-top: 0.375rem;
}

.overview-group button {
  @apply border-0 bg-transparent p-0 text-text-secondary cursor-pointer font-semibold;
}

.overview-group button:hover {
  color: var(--text);
}

.head,
.row {
  display: grid;
  grid-template-columns: 3.5rem minmax(0, 0.6fr) minmax(0, 1fr) 5rem 6rem;
  column-gap: clamp(0.375rem, 2vw, 1.5rem);
  align-items: center;
  padding: 0 0.6rem;
}

.head {
  height: 1.875rem;
  margin-bottom: 0.25rem;
  font-size: var(--text-12);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.row {
  height: 2.25rem;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: default;
}

.row:hover {
  background: var(--bg-hover);
}

.method {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
}

.name {
  @apply truncate font-medium;
}

.path {
  @apply min-w-0 truncate text-text-secondary;
  font-size: var(--text-12);
}

.when {
  @apply whitespace-nowrap text-text-tertiary;
  text-align: right;
  font-size: var(--text-12);
}

.right {
  text-align: right;
}

.none {
  @apply text-text-tertiary;
  padding: 1.5rem 0.625rem;
}
</style>
