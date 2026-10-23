<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Collection } from '../../../bindings/schemo/internal/domain'
import { flattenTree } from '../../lib/treeRows'
import { findCollection, findNode, requestCount } from '../../lib/collectionTree'
import { methodInkClass } from '../../lib/format'
import { useCollectionsStore } from '../../stores/collections'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'

const { collection } = defineProps<{ collection: Collection }>()
const emit = defineEmits<{ open: [id: string] }>()
const collections = useCollectionsStore()
const query = ref('')
const expanded = ref<Record<string, boolean>>({})
const count = computed(() => requestCount(collection))
const searchLabel = computed(() => t('collections.findIn', { name: collection.name }))
const rows = computed(() => {
  const q = query.value.trim().toLocaleLowerCase()
  const tree = flattenTree([collection], (id) => id === collection.id || !!q || !!expanded.value[id]).slice(1)
  if (!q) return tree
  return tree.filter((row) => {
    if (row.kind !== 'request') return false
    const node = findNode([collection], row.id)
    return `${row.name} ${row.method} ${node?.url ?? ''}`.toLocaleLowerCase().includes(q)
  }).map((row) => ({
    ...row,
    sub: row.collectionId === collection.id ? '' : findCollection([collection], row.collectionId)?.name ?? '',
  }))
})

function choose(id: string) {
  emit('open', id)
}

function toggle(id: string) {
  expanded.value[id] = !expanded.value[id]
}

function openFirst() {
  const first = rows.value.find((row) => row.kind === 'request')
  if (first) choose(first.id)
}
</script>

<template>
  <label class="navigation-popover-search">
    <Icon name="Search" :size="13" />
    <input
      v-model="query"
      :placeholder="searchLabel"
      :aria-label="searchLabel"
      spellcheck="false"
      @keydown.enter.prevent="openFirst"
    />
    <span class="navigation-popover-count">{{ query.trim() ? `${rows.length} / ${count}` : count }}</span>
  </label>
  <div class="navigation-popover-list">
    <button
      v-for="row in rows"
      :key="row.id"
      type="button"
      class="navigation-popover-row peek-row"
      :class="{ active: row.id === collections.selectedId, folder: row.kind === 'collection' }"
      :style="{ '--peek-depth': query.trim() ? 0 : row.depth - 1 }"
      :aria-expanded="row.kind === 'collection' ? row.expanded : undefined"
      @click="row.kind === 'collection' ? toggle(row.id) : choose(row.id)"
    >
      <template v-if="row.kind === 'collection'">
        <Icon :name="row.expanded ? 'ChevronDown' : 'ChevronRight'" :size="10" class="peek-caret" />
        <Icon name="Folder" :size="14" class="peek-folder" />
      </template>
      <span v-else class="peek-method" :class="methodInkClass(row.method)">{{ row.method }}</span>
      <span class="peek-name">{{ row.name }}</span>
      <span v-if="'sub' in row && row.sub" class="peek-sub">{{ row.sub }}</span>
    </button>
    <div v-if="!rows.length" class="navigation-popover-empty">{{ t(query.trim() ? 'common.nothingFound' : 'collections.emptyPeek') }}</div>
  </div>
  <button type="button" class="navigation-popover-footer" @click="choose(collection.id)">
    <span class="peek-name">{{ t('collections.openNamed', { name: collection.name }) }}</span>
    <Icon name="ArrowUpRight" :size="12" :stroke-width="2.2" />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.peek-row {
  padding: 0 0.5rem 0 calc(0.5rem + var(--peek-depth) * 1.0625rem);
}

.peek-row.folder {
  padding-left: calc(0.3rem + var(--peek-depth) * 1.0625rem);
}

.peek-row:hover,
.peek-row.active {
  background: var(--bg-hover);
}

.peek-name {
  @apply flex-1 min-w-0 truncate;
}

.peek-method {
  width: 2.875rem;
  flex: none;
  font-family: var(--mono);
  font-size: var(--text-12);
  font-weight: 600;
}

.peek-caret,
.peek-folder {
  color: var(--text-tertiary);
}

.peek-sub {
  @apply flex-none truncate text-text-tertiary;
  max-width: 38%;
  font-size: var(--text-12);
}
</style>
