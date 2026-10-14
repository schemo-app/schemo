<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import {
  buildJsonTree,
  isContainer,
  jsonOrder,
  jsonValueView,
  type JsonNode,
} from '../../lib/jsonTree'
import { textOf } from '../../i18n'

// A JSON body, read as a tree: every value under the key that names it, in the order it was written.
// An answer that is JSON:API has a tree of its own — this one is for everything else the server
// sends, which is most of what a server sends.
const {
  value,
  query = '',
  collapsed = {},
  selected = '$',
} = defineProps<{
  value: unknown
  query?: string
  collapsed?: Record<string, boolean>
  selected?: string
}>()

const emit = defineEmits<{
  (e: 'select', path: string): void
  (e: 'toggle', path: string): void
}>()

const nodes = computed(() => buildJsonTree(value))

const rows = computed(() => jsonOrder(nodes.value).filter((node) => node.path !== '$' && !hidden(node)))

/** A row under a folded one is not drawn: what is folded is the whole of what it holds. */
function hidden(node: JsonNode): boolean {
  for (let up = node.parent; up && up !== '$'; up = nodes.value.get(up)?.parent ?? null) {
    if (collapsed[up]) return true
  }
  return false
}

function count(kind: 'fields' | 'items', n: number): string {
  return textOf(kind === 'fields' ? 'response.jsonFields' : 'response.jsonItems', n)
}

function view(node: JsonNode) {
  return jsonValueView(node, count)
}

const needle = computed(() => query.trim().toLowerCase())

function hit(node: JsonNode): boolean {
  const q = needle.value
  if (!q) return false
  if (String(node.key).toLowerCase().includes(q)) return true
  if (isContainer(node.kind)) return false
  return String(node.value).toLowerCase().includes(q)
}

/** How many rows the query found, counted over the whole document rather than its drawn part. */
const matches = computed(() => {
  const q = needle.value
  if (!q) return 0
  let n = 0
  for (const node of nodes.value.values()) {
    if (node.path === '$') continue
    if (String(node.key).toLowerCase().includes(q)) {
      n++
      continue
    }
    if (!isContainer(node.kind) && String(node.value).toLowerCase().includes(q)) n++
  }
  return n
})

defineExpose({ matches, nodes })

function pick(node: JsonNode, event: MouseEvent) {
  if (event.detail > 1) return
  emit('select', node.path)
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
    <div
      v-for="node in rows"
      :key="node.path"
      class="tree-row"
      :class="{ selected: node.path === selected, hit: hit(node) }"
      :style="{ paddingLeft: `${0.75 + (node.depth - 1) * 1.125}rem` }"
      @click="pick(node, $event)"
      @dblclick="wantToggle(node)"
    >
      <span
        class="tree-caret"
        :class="{ open: !collapsed[node.path], leaf: !isContainer(node.kind) }"
        @click.stop="wantToggle(node)"
      >
        <Icon name="ChevronRight" :size="8" :stroke-width="3" />
      </span>
      <span class="tree-key" :class="{ index: typeof node.key === 'number' }">{{ node.key }}</span>
      <span class="tree-colon">:</span>
      <span class="tree-val" :class="INK[view(node).ink]">{{ view(node).text }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.json-tree {
  @apply flex flex-col;
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
  @apply flex-none inline-flex items-center justify-center text-text-tertiary;
  width: 1rem;
  height: 1.25rem;
  transition: transform 0.12s ease;
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
  margin-right: 0.375rem;
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
