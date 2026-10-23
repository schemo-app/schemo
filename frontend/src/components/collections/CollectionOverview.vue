<script setup lang="ts">
import { computed } from 'vue'
import { useCollectionsStore } from '../../stores/collections'
import { statusBadgeClass } from '../../lib/format'
import { splitAddress } from '../../lib/address'
import { findCollection } from '../../lib/collectionTree'
import { formatDate, t } from '../../i18n'
import { methodInkClass } from '../../lib/format'
import MarkdownText from '../ui/MarkdownText.vue'
import type { LevelRow } from '../../../bindings/schemo/internal/domain'

// What the collection holds, and what each request last answered: the page reads like a table of
// contents, and a row is the way into the request itself.
const {
  rows,
  loading = false,
} = defineProps<{
  rows: {
    row: LevelRow
    status: { text: string; cls: string }
    when: number
    bad: boolean
  }[]
  loading?: boolean
}>()

const emit = defineEmits<{ open: [row: LevelRow]; openDocs: [edit: boolean] }>()

const collections = useCollectionsStore()

// The level the page is about, read off the tree by the same answer the page's header uses.
const level = computed(() => findCollection(collections.tree, collections.collectionId))
const description = computed(() => level.value?.description ?? '')

// The preview is the description read at a glance: what does not fit behind the mask is what the
// reader opens the description for.
const masked = computed(() => description.value.length > 260)

function pathOf(url: string): string {
  const { host, path } = splitAddress(url)
  return host + path
}

function whenOf(at: number): string {
  return at ? formatDate(at, { hour: '2-digit', minute: '2-digit' }) : ''
}
</script>

<template>
  <div class="overview">
    <div v-if="description" class="desc">
      <div class="desc-preview" :class="{ masked }" @click="emit('openDocs', false)">
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
      <!-- The headings name columns that are not there: an empty collection says so below instead. -->
      <div v-if="rows.length" class="head">
        <span>{{ t('collections.columnMethod') }}</span>
        <span>{{ t('collections.columnName') }}</span>
        <span>{{ t('collections.columnPath') }}</span>
        <span>{{ t('collections.columnResponse') }}</span>
        <span class="right">{{ t('collections.columnWhen') }}</span>
      </div>

      <button
        v-for="item in rows"
        :key="item.row.id"
        type="button"
        class="row"
        @click="emit('open', item.row)"
      >
        <span class="method" :class="methodInkClass(item.row.method ?? 'GET')">{{ item.row.method }}</span>
        <span class="name">{{ item.row.name }}</span>
        <span class="path mono">{{ pathOf(item.row.url ?? '') }}</span>
        <span>
          <span class="status" :class="item.bad ? 'is-bad' : statusBadgeClass(Number(item.status.text) || 0)">
            {{ item.status.text }}
          </span>
        </span>
        <span class="when">{{ whenOf(item.when) }}</span>
      </button>

      <div v-if="!rows.length && !loading" class="none">{{ t('collections.noRequests') }}</div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.overview {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 1.25rem 1.5rem;
  gap: 1.25rem;
}

.desc {
  @apply flex flex-col gap-1.5;
  max-width: 45rem;
  line-height: 1.55;
}

.desc-preview {
  max-height: 8.25rem;
  overflow: hidden;
  cursor: default;
}

.desc-preview.masked {
  -webkit-mask-image: linear-gradient(to bottom, #000 55%, transparent);
  mask-image: linear-gradient(to bottom, #000 55%, transparent);
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

.head,
.row {
  display: grid;
  grid-template-columns: 4rem 13.75rem minmax(0, 1fr) 4.375rem 3.75rem;
  align-items: center;
  padding: 0 0.6rem;
}

.head {
  height: 1.75rem;
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border);
}

.row {
  height: 2.5rem;
  border: 0;
  border-bottom: 1px solid var(--line2);
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

.status {
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 600;
  padding: 0.125rem 0.3rem;
  border-radius: 0.3125rem;
  background: var(--bg-hover);
  color: var(--text-tertiary);
}

.status.is-bad {
  background: var(--red-soft);
  color: var(--red-text);
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
