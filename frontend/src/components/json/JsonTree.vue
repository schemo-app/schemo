<script setup lang="ts">
import { computed } from 'vue'
import JsonRows from './JsonRows.vue'
import JsonValueNote from './JsonValueNote.vue'
import { isContainer, jsonValueView, type JsonNode } from '../../lib/jsonTree'
import type { InlineReadings } from '../../lib/reading'
import { t, textOf } from '../../i18n'

// A JSON body, read as a tree: every value under the key that names it, in the order it was
// written. An answer that is JSON:API has a tree of its own; this one is for everything else the
// server sends.
const {
  nodes,
  readings,
  query = '',
  collapsed = {},
  selected = '$',
} = defineProps<{
  nodes: ReadonlyMap<string, JsonNode>
  readings?: InlineReadings
  query?: string
  collapsed?: Record<string, boolean>
  selected?: string
}>()

const emit = defineEmits<{
  inspect: [path: string, event?: MouseEvent]
  toggle: [path: string]
}>()

const rows = computed(() => {
  const visible: JsonNode[] = []

  function add(node: JsonNode) {
    visible.push(node)
    if (!collapsed[node.path]) node.children.forEach(add)
  }

  nodes.get('$')?.children.forEach(add)
  return visible
})

function count(kind: 'fields' | 'items', n: number): string {
  return textOf(kind === 'fields' ? 'response.jsonFields' : 'response.jsonItems', n)
}

function view(node: JsonNode) {
  return jsonValueView(node, count)
}

const needle = computed(() => query.trim().toLowerCase())

function matchesSearch(node: JsonNode): boolean {
  const searched = needle.value
  if (!searched) return false
  if (String(node.key).toLowerCase().includes(searched)) return true
  if (isContainer(node.kind)) return false
  return String(node.value).toLowerCase().includes(searched)
}

/** How many rows the query found, counted over the whole document rather than its drawn part. */
const matches = computed(() => {
  if (!needle.value) return 0
  let count = 0
  for (const node of nodes.values()) {
    if (node.path !== '$' && matchesSearch(node)) count++
  }
  return count
})

defineExpose({ matches })

function pick(node: JsonNode, event: MouseEvent) {
  if (event.detail > 1) return
  emit('inspect', node.path)
}

function wantToggle(node: JsonNode) {
  if (isContainer(node.kind)) emit('toggle', node.path)
}

const INK: Record<string, string> = {
  string: 'val-str',
  number: 'val-num',
  type: 'val-type',
  quiet: 'val-quiet',
}
</script>

<template>
  <div class="json-tree">
    <JsonRows :rows :row-height="1.625" :aria-label="textOf('response.views.tree')" v-slot="{ node }">
      <div
        class="tree-row"
        :class="{ selected: node.path === selected, hit: matchesSearch(node) }"
        :style="{ paddingLeft: `${0.75 + (node.depth - 1) * 1.125}rem` }"
        @click="pick(node, $event)"
        @dblclick="wantToggle(node)"
        @contextmenu.prevent="emit('inspect', node.path, $event)"
      >
        <button
          v-if="isContainer(node.kind)"
          type="button"
          class="tree-caret"
          :class="{ open: !collapsed[node.path] }"
          :aria-expanded="!collapsed[node.path]"
          :title="t(collapsed[node.path] ? 'response.raw.unfoldLine' : 'response.raw.foldLine')"
          :aria-label="t(collapsed[node.path] ? 'response.raw.unfoldLine' : 'response.raw.foldLine')"
          @click.stop="wantToggle(node)"
          @dblclick.stop
        >›</button>
        <span v-else class="tree-caret leaf" aria-hidden="true"></span>
        <span class="tree-key" :class="{ index: typeof node.key === 'number' }">{{ node.key }}</span>
        <span class="tree-colon">:</span>
        <span class="tree-val" :class="INK[view(node).ink]">{{ view(node).text }}</span>
        <JsonValueNote v-if="readings?.[node.path]" :reading="readings[node.path]" />
      </div>
    </JsonRows>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.json-tree {
  @apply flex flex-col flex-1 min-h-0;
  padding: 0.125rem 0 1.25rem;
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.tree-row {
  @apply flex items-center cursor-default;
  height: 1.625rem;
  padding-right: 1rem;
  white-space: nowrap;
  min-width: 0;
}

.tree-row:hover {
  background: var(--bg-hover);
}

.tree-row.selected {
  background: var(--accent-soft);
}

.tree-row.hit {
  background: var(--hit);
}

.tree-caret {
  @apply flex-none inline-grid place-items-center text-text-tertiary border-0 bg-transparent;
  width: 1rem;
  height: 1.25rem;
  padding: 0;
  font: var(--mono-size)/1 var(--mono);
  transition: transform 0.12s ease;
}

.tree-caret:hover {
  @apply cursor-pointer text-text;
}

.tree-caret.open {
  transform: rotate(90deg);
}

.tree-caret.leaf {
  visibility: hidden;
}

.tree-key {
  @apply flex-none;
  color: var(--tok-key);
}

.tree-key.index {
  color: var(--text-tertiary);
}

.tree-colon {
  @apply flex-none text-text-tertiary;
  margin-right: 0.3rem;
}

.tree-val {
  @apply min-w-0 truncate;
}

.val-str {
  color: var(--tok-str);
}

.val-num {
  color: var(--tok-num);
}

.val-type {
  color: var(--purple);
}

.val-quiet {
  color: var(--text-tertiary);
}
</style>
