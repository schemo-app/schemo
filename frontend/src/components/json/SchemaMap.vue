<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { refAutoReset } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import { prettyJson } from '../../lib/json'
import type { JsonApiDocument, Resource } from '../../lib/jsonapi'
import { allResources, resourceKey, resourceLabel } from '../../lib/jsonapi'
import { buildTypeInfos, capitalizeType } from '../../lib/schema'
import { useMessages } from '../../i18n'

const { t } = useMessages()

// The map has no row of its own: the answer's tab row searches it and copies it out, so the query
// comes in from there and the export goes out to it.
const {
  doc,
  highlightKey,
  query = '',
} = defineProps<{ doc: JsonApiDocument | null; highlightKey?: string | null; query?: string }>()
const emit = defineEmits<{ (e: 'fetch', url: string): void; (e: 'select', key: string): void }>()

const all = computed<Resource[]>(() => (doc ? allResources(doc) : []))

const types = computed(() => (doc ? buildTypeInfos(doc) : []))

const instancesByType = computed<Map<string, Resource[]>>(() => {
  const map = new Map<string, Resource[]>()
  for (const r of all.value) {
    const list = map.get(r.type) ?? []
    list.push(r)
    map.set(r.type, list)
  }
  return map
})

function instancesOf(type: string): Resource[] {
  return instancesByType.value.get(type) ?? []
}

function instanceLabel(res: Resource): string {
  const l = resourceLabel(res)
  return l === `${res.type}/${res.id}` ? res.id : l
}

const expandedTypes = ref<Set<string>>(new Set())

function toggleType(type: string) {
  const next = new Set(expandedTypes.value)
  if (next.has(type)) next.delete(type)
  else next.add(type)
  expandedTypes.value = next
}

const filteredTypes = computed(() => {
  const q = query.trim().toLowerCase()
  if (!q) return types.value
  return types.value.filter((t) => {
    if (t.label.toLowerCase().includes(q) || t.type.toLowerCase().includes(q)) return true
    if (t.attributes.some((a) => a.toLowerCase().includes(q))) return true
    if (t.rels.some((r) => r.name.toLowerCase().includes(q) || r.targetType.toLowerCase().includes(q))) return true
    return false
  })
})

const highlightType = refAutoReset<string | null>(null, 1500)

function goToType(type: string) {
  highlightType.value = type
  document.querySelector(`[data-type="${type}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
}

// Reverse sync: highlight the instance selected in the tree.
const highlightInstance = ref<string | null>(null)

function applyHighlight(k: string | null | undefined) {
  if (!k) return
  const type = k.split('/')[0]
  const next = new Set(expandedTypes.value)
  if (!next.has(type)) {
    next.add(type)
    expandedTypes.value = next
  }
  highlightInstance.value = k
  nextTick(() => {
    document.getElementById('inst-' + k)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  })
}

watch(() => highlightKey, applyHighlight)
onMounted(() => applyHighlight(highlightKey))


const EXPORT_FORMATS = [
  { id: 'mermaid', label: 'Mermaid (erDiagram)' },
  { id: 'dot', label: 'Graphviz DOT' },
  { id: 'json', label: 'JSON' },
] as const

type ExportId = (typeof EXPORT_FORMATS)[number]['id']

function entityName(type: string): string {
  let s = type.toUpperCase().replace(/[^A-Z0-9]+/g, '_')
  if (/^[0-9]/.test(s)) s = '_' + s
  return s
}

function attrName(a: string): string {
  return a.replace(/[^A-Za-z0-9_]/g, '_') || 'field'
}

function dotEscape(s: string): string {
  return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

function exportMermaid(): string {
  const L: string[] = ['erDiagram']
  for (const t of types.value) {
    L.push(`  ${entityName(t.type)} {`)
    for (const a of t.attributes) L.push(`    string ${attrName(a)}`)
    L.push('  }')
  }
  for (const t of types.value) {
    for (const r of t.rels) {
      if (!r.inDoc) continue
      const card = r.many ? '||--o{' : '||--o|'
      L.push(`  ${entityName(t.type)} ${card} ${entityName(r.targetType)} : ${r.name}`)
    }
  }
  return L.join('\n')
}

function exportDot(): string {
  const L: string[] = ['digraph {', '  rankdir=LR']
  for (const t of types.value) {
    const label = [t.label, ...t.attributes].join('\\n')
    L.push(`  "${dotEscape(t.type)}" [label="${dotEscape(label)}"]`)
  }
  for (const t of types.value) {
    for (const r of t.rels) {
      if (!r.inDoc) continue
      L.push(`  "${dotEscape(t.type)}" -> "${dotEscape(r.targetType)}" [label="${dotEscape(r.name)}"]`)
    }
  }
  L.push('}')
  return L.join('\n')
}

function exportJson(): string {
  const data = types.value.map((t) => ({
    type: t.type,
    count: t.count,
    attributes: t.attributes,
    relationships: t.rels.map((r) => ({
      name: r.name,
      targetType: r.targetType,
      cardinality: r.many ? 'to-many' : 'to-one',
      inDocument: r.inDoc,
      ...(r.relatedUrl ? { relatedUrl: r.relatedUrl } : {}),
    })),
    incoming: t.incoming.map((i) => ({ fromType: i.fromType, rel: i.rel })),
  }))
  return prettyJson({ types: data })
}

function generate(format: ExportId): string {
  if (format === 'mermaid') return exportMermaid()
  if (format === 'dot') return exportDot()
  return exportJson()
}

// What the tab row's copy offers on this tab: the map written out in one of three notations, and
// how many types the search left — the row says the count beside its field.
defineExpose({
  exportFormats: EXPORT_FORMATS,
  exportText: (format: ExportId) => (types.value.length ? generate(format) : ''),
  matchCount: computed(() => filteredTypes.value.length),
})

</script>

<template>
  <div class="schema">
    <div class="schema-body">
      <div v-if="filteredTypes.length === 0" class="empty">
        {{ types.length === 0 ? t('json.noMapData') : t('common.nothingFound') }}
      </div>

      <div v-else class="cards">
        <section
          v-for="info in filteredTypes"
          :key="info.type"
          :data-type="info.type"
          class="type-card"
          :class="{ highlight: highlightType === info.type }"
        >
          <button class="type-head" @click="toggleType(info.type)">
            <span class="caret" :class="{ open: expandedTypes.has(info.type) }">
              <Icon name="ChevronRight" :size="10" />
            </span>
            <span class="type-name">{{ info.label }}</span>
            <span class="type-count">{{ info.count }}</span>
          </button>

          <div class="type-body">
            <div v-if="info.attributes.length" class="type-attrs">
              <span v-for="a in info.attributes" :key="a" class="attr-chip mono">{{ a }}</span>
            </div>

            <div v-if="info.rels.length" class="type-rels">
              <div v-for="r in info.rels" :key="r.name + r.targetType" class="type-rel">
                <span class="rel-name">{{ r.name }}</span>
                <span class="rel-card">{{ r.many ? '1:N' : '1:1' }}</span>
                <span class="rel-arrow"><Icon name="ArrowRight" :size="12" /></span>
                <button v-if="r.inDoc" class="rel-target in-doc" @click="goToType(r.targetType)">
                  {{ capitalizeType(r.targetType) }}
                </button>
                <button v-else-if="r.relatedUrl" class="rel-target missing" @click="emit('fetch', r.relatedUrl)">
                  <span>{{ capitalizeType(r.targetType) }}</span>
                  <Icon name="ArrowUpRight" :size="12" />
                </button>
                <span v-else class="rel-target ghost">{{ capitalizeType(r.targetType) }}</span>
              </div>
            </div>

            <div v-if="info.incoming.length" class="type-incoming">
              <div class="incoming-title">{{ t('json.linkedFrom') }}</div>
              <div class="incoming-list">
                <button
                  v-for="inc in info.incoming"
                  :key="inc.fromType + inc.rel"
                  class="incoming-chip"
                  @click="goToType(inc.fromType)"
                >
                  <Icon name="ArrowLeft" :size="12" />
                  <span>{{ capitalizeType(inc.fromType) }}</span>
                  <span class="incoming-rel">{{ inc.rel }}</span>
                </button>
              </div>
            </div>

            <div v-if="expandedTypes.has(info.type)" class="type-instances">
              <div class="instances-title">{{ t('json.instances') }}</div>
              <button
                v-for="res in instancesOf(info.type)"
                :key="resourceKey(res.type, res.id)"
                :id="'inst-' + resourceKey(res.type, res.id)"
                class="instance"
                :class="{ highlighted: highlightInstance === resourceKey(res.type, res.id) }"
                @click="emit('select', resourceKey(res.type, res.id))"
              >
                {{ instanceLabel(res) }}
              </button>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.schema {
  @apply flex flex-col h-full min-h-0;
}

.schema-body {
  @apply flex-1 min-h-0 overflow-auto pt-3.5 px-4 pb-5;
}

.empty {
  @apply text-text-tertiary text-13 py-6 text-center;
}

.cards {
  @apply flex flex-col gap-2.5;
}

.type-card {
  @apply rounded-xl pt-2.5 px-3.5 pb-3.5;
  background: var(--bg);
  border: 1px solid var(--border);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.type-card.highlight {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.type-head {
  @apply flex items-center gap-1.5 w-full border-none bg-transparent py-1 px-0 cursor-pointer text-left;
  font: inherit;
  --wails-draggable: no-drag;
}

.caret {
  @apply inline-flex items-center justify-center flex-none w-3 h-3 text-text-secondary;
  transition: transform 0.12s ease;
}

.caret.open {
  @apply rotate-90;
}

.type-name {
  @apply text-14 font-semibold text-text;
}

.type-count {
  @apply text-11 text-text-tertiary rounded-lg py-px px-1.5;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  font-variant-numeric: tabular-nums;
}

.type-body {
  @apply pl-4.5;
}

.type-attrs {
  @apply flex flex-wrap gap-1 mt-2;
}

.attr-chip {
  @apply text-11 text-text-secondary rounded-md py-0.5 px-1.5;
  background: var(--bg-hover);
  border: 1px solid var(--border);
}

.type-rels {
  @apply flex flex-col gap-1.5 mt-2.5 pt-2.5 border-t border-border;
}

.type-rel {
  @apply flex items-center gap-2 text-13 flex-wrap;
}

.rel-name {
  @apply font-medium text-text;
}

.rel-card {
  @apply text-10 text-text-tertiary rounded-sm py-px px-1;
  border: 1px solid var(--border);
  font-family: var(--mono);
}

.rel-arrow {
  @apply text-text-tertiary;
}

.rel-target {
  @apply inline-flex items-center gap-1 border-none bg-transparent py-0.5 px-2 rounded-md text-13 cursor-pointer;
  font: inherit;
  --wails-draggable: no-drag;
}

.rel-target.in-doc {
  @apply text-accent bg-accent-soft;
}

.rel-target.in-doc:hover {
  @apply underline;
}

.rel-target.missing {
  color: var(--orange);
  border: 1px dashed color-mix(in srgb, var(--orange) 55%, transparent);
}

.rel-target.missing:hover {
  @apply border-solid;
}

.rel-target.ghost {
  @apply text-text-tertiary cursor-default;
}

.type-incoming {
  @apply mt-2.5 pt-2.5 border-t border-border;
}

.incoming-title {
  @apply text-11 uppercase tracking-wider text-text-tertiary mb-1;
}

.incoming-list {
  @apply flex flex-wrap gap-1;
}

.incoming-chip {
  @apply inline-flex items-center gap-1 text-text-secondary text-11 py-0.5 px-2 rounded-md cursor-pointer;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  font: inherit;
  --wails-draggable: no-drag;
}

.incoming-chip:hover {
  border-color: var(--accent);
  @apply text-text;
}

.incoming-rel {
  @apply text-text-tertiary;
  font-family: var(--mono);
}

.type-instances {
  @apply mt-2.5 pt-2.5 border-t border-border flex flex-col gap-1;
}

.instances-title {
  @apply text-11 uppercase tracking-wider text-text-tertiary mb-0.5;
}

.instance {
  @apply border-none bg-transparent text-left text-12 text-text-secondary py-1 px-2 rounded-md cursor-pointer;
  font: inherit;
  --wails-draggable: no-drag;
}

.instance:hover {
  @apply bg-bg-hover text-accent;
}

.instance.highlighted {
  @apply text-accent bg-accent-soft;
}
</style>
