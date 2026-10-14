<script setup lang="ts">
import { computed } from 'vue'
import { buildJsonTree, isContainer, jsonOrder, type JsonKind, type JsonNode } from '../../lib/jsonTree'
import { textOf, useMessages } from '../../i18n'

// The same body as the tree draws, listed: every address in the document with what stands there and
// how much of it. A tree is read by walking it; a list is read by scanning it.
const {
  value,
  query = '',
  selected = '$',
} = defineProps<{
  value: unknown
  query?: string
  selected?: string
}>()

const emit = defineEmits<{ (e: 'select', path: string): void }>()

const { t } = useMessages()

const nodes = computed(() => buildJsonTree(value))
const rows = computed(() => jsonOrder(nodes.value).filter((node) => node.path !== '$'))

const needle = computed(() => query.trim().toLowerCase())

function hit(node: JsonNode): boolean {
  const q = needle.value
  if (!q) return false
  if (node.path.toLowerCase().includes(q)) return true
  return !isContainer(node.kind) && String(node.value).toLowerCase().includes(q)
}

/** The last step of the address, and what stands before it: the list indents by the first. */
function stepOf(node: JsonNode): { parent: string; last: string } {
  if (typeof node.key === 'number') {
    const cut = node.path.lastIndexOf('[')
    return { parent: node.path.slice(0, cut), last: node.path.slice(cut) }
  }
  const cut = node.path.lastIndexOf('.')
  if (cut < 0) return { parent: '', last: node.path }
  return { parent: node.path.slice(0, cut), last: node.path.slice(cut) }
}

/** How many rows the query names — the search beside the row counts with it. */
const matches = computed(() => (needle.value ? rows.value.filter(hit).length : 0))

defineExpose({ matches })

function sizeOf(node: JsonNode): string {
  if (node.kind === 'object') return textOf('response.jsonFields', node.children.length)
  if (node.kind === 'array') return textOf('response.jsonItems', node.children.length)
  if (node.kind === 'string') return textOf('response.map.chars', String(node.value).length)
  return ''
}

const TYPE_INK: Record<JsonKind, string> = {
  object: 'type-plain',
  array: 'type-plain',
  string: 'type-str',
  number: 'type-num',
  boolean: 'type-type',
  null: 'type-type',
}
</script>

<template>
  <div class="json-map">
    <div class="map-head">
      <span class="map-path-head">{{ t('response.map.path') }}</span>
      <span>{{ t('response.map.type') }}</span>
      <span class="map-size-head">{{ t('response.map.size') }}</span>
    </div>
    <div
      v-for="node in rows"
      :key="node.path"
      class="map-row"
      :class="{ selected: node.path === selected, hit: hit(node) }"
      @click="emit('select', node.path)"
    >
      <span class="map-addr mono">
        <span class="map-parent">{{ stepOf(node).parent }}</span>{{ stepOf(node).last }}
      </span>
      <span>
        <span class="map-type mono" :class="TYPE_INK[node.kind]">{{ textOf(`inspector.kinds.${node.kind}`) }}</span>
      </span>
      <span class="map-size">{{ sizeOf(node) }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.json-map {
  padding: 0 1.25rem 1.25rem;
}

.map-head {
  @apply grid items-center text-text-tertiary;
  grid-template-columns: minmax(0, 1fr) 5.25rem 6rem;
  height: 1.75rem;
  font-size: var(--text-11-5);
  border-bottom: 1px solid var(--border);
}

.map-path-head {
  padding-left: 0.625rem;
}

.map-size-head {
  text-align: right;
  padding-right: 0.625rem;
}

.map-row {
  @apply grid items-center cursor-default;
  grid-template-columns: minmax(0, 1fr) 5.25rem 6rem;
  height: 2rem;
  border-bottom: 1px solid var(--line2);
}

.map-row:hover {
  background: var(--bg-hover);
}

.map-row.selected {
  background: var(--accent-soft);
}

.map-row.hit {
  background: var(--hit);
}

.map-addr {
  @apply min-w-0 truncate;
  padding-left: 0.625rem;
  font-size: var(--text-12-5);
}

.map-parent {
  color: var(--text-tertiary);
}

.map-type {
  font-size: var(--text-11);
  padding: 0.125rem 0.4375rem;
  border-radius: 0.3125rem;
  background: var(--bg-hover);
  color: var(--text-secondary);
}

.type-str {
  background: var(--red-soft);
  color: var(--tok-str);
}

.type-num {
  background: var(--accent-soft);
  color: var(--tok-num);
}

.type-type {
  background: var(--accent-soft);
  color: var(--purple);
}

.map-size {
  @apply whitespace-nowrap;
  text-align: right;
  padding-right: 0.625rem;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}
</style>
