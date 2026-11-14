<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { PopoverRoot, PopoverTrigger } from 'reka-ui'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import type { OperationGroup, SchemaOperation } from '../../lib/schemaEditor'

const { label, placeholder, searchLabel, groups, loaded, disabled, unfolded = false } = defineProps<{
  label: string
  placeholder: string
  searchLabel: string
  groups: OperationGroup[]
  loaded: boolean
  disabled: boolean
  // GraphQL has a couple of groups, each worth seeing at once; services fold to the chosen one.
  unfolded?: boolean
}>()
const emit = defineEmits<{ schema: [] }>()

const open = defineModel<boolean>('open', { default: false })
const query = ref('')
// Only the groups the user folded or unfolded; the rest follow the chosen item.
const folded = ref<Record<string, boolean>>({})
const total = computed(() => groups.reduce((count, group) => count + group.items.length, 0))
const search = computed(() => query.value.trim().toLocaleLowerCase())

type Row =
  | { kind: 'group', key: string, name: string, count: number, expanded: boolean }
  | { kind: 'item', key: string, item: SchemaOperation, sub: string, nested: boolean }

const rows = computed<Row[]>(() => {
  if (search.value) {
    return groups.flatMap(group => group.items
      .filter(item => `${item.name} ${group.name}`.toLocaleLowerCase().includes(search.value))
      .map(item => ({ kind: 'item' as const, key: item.key, item, sub: group.short, nested: false })))
  }
  return groups.flatMap(group => {
    const expanded = folded.value[group.name] ?? (unfolded || group.items.some(item => item.selected))
    const head = { kind: 'group' as const, key: `group:${group.name}`, name: group.name, count: group.items.length, expanded }
    if (!expanded) return [head]
    return [head, ...group.items.map(item => ({ kind: 'item' as const, key: item.key, item, sub: '', nested: true }))]
  })
})
const found = computed(() => rows.value.filter(row => row.kind === 'item').length)

function toggleOpen(value: boolean) {
  open.value = value
}

watch(open, value => {
  if (value) query.value = ''
})

function choose(item: SchemaOperation) {
  if (item.disabled) return
  item.select()
  open.value = false
}

function chooseFirst() {
  const first = rows.value.find(row => row.kind === 'item' && !row.item.disabled)
  if (first?.kind === 'item') choose(first.item)
}

function toSchema() {
  open.value = false
  emit('schema')
}
</script>

<template>
  <PopoverRoot :open @update:open="toggleOpen">
    <PopoverTrigger as-child>
      <button type="button" class="operation-trigger" :class="{ chosen: label }" :title="label || placeholder" :disabled>
        <span class="truncate">{{ label || placeholder }}</span>
        <Icon name="ChevronDown" :size="10" :stroke-width="2.6" />
      </button>
    </PopoverTrigger>
    <PopoverContent class="operation-popover" align="end" :side-offset="4" :aria-label="placeholder" @close-auto-focus.prevent>
      <label class="navigation-popover-search">
        <Icon name="Search" :size="13" />
        <input
          v-model="query" :placeholder="searchLabel" :aria-label="searchLabel" spellcheck="false"
          @keydown.enter.prevent="chooseFirst"
        />
        <span v-if="loaded" class="navigation-popover-count">{{ search ? `${found} / ${total}` : total }}</span>
      </label>
      <!-- Focus stays in the search field, so typing goes on after a group is unfolded. -->
      <div class="navigation-popover-list" @mousedown.prevent>
        <template v-for="row in rows" :key="row.key">
          <button
            v-if="row.kind === 'group'" type="button" class="navigation-popover-row picker-row group"
            :aria-expanded="row.expanded" @click="folded[row.name] = !row.expanded"
          >
            <Icon :name="row.expanded ? 'ChevronDown' : 'ChevronRight'" :size="10" :stroke-width="2.8" class="picker-caret" />
            <span class="picker-name">{{ row.name }}</span>
            <span class="picker-count">{{ row.count }}</span>
          </button>
          <button
            v-else type="button" class="navigation-popover-row picker-row"
            :class="{ active: row.item.selected, nested: row.nested }" :disabled="row.item.disabled"
            :title="row.item.disabled ? t('request.protocol.clientStreaming') : undefined" @click="choose(row.item)"
          >
            <span class="picker-name">{{ row.item.name }}</span>
            <span v-if="row.sub" class="picker-sub">{{ row.sub }}</span>
          </button>
        </template>
        <div v-if="!loaded || (search && !found)" class="navigation-popover-empty">
          {{ t(loaded ? 'common.nothingFound' : 'request.protocol.schemaUnloaded') }}
        </div>
      </div>
      <button type="button" class="navigation-popover-footer" @click="toSchema">
        <span class="picker-name">{{ t(loaded ? 'request.protocol.showSchema' : 'request.protocol.loadSchema') }}</span>
        <Icon name="ArrowUpRight" :size="12" :stroke-width="2.2" />
      </button>
    </PopoverContent>
  </PopoverRoot>
</template>

<style scoped>
@reference "../../style.css";

.operation-trigger {
  @apply flex items-center min-w-0 w-full whitespace-nowrap text-12 font-medium cursor-pointer;
  height: 1.625rem;
  gap: 0.375rem;
  padding: 0 0.375rem 0 0.5625rem;
  border-radius: 0.375rem;
  font-family: var(--font-sans);
  color: var(--text-tertiary);
}

.operation-trigger.chosen {
  color: var(--text);
  background: var(--chip);
}

.operation-trigger:hover:not(:disabled),
.operation-trigger[data-state='open'] {
  background: var(--bg-hover);
}

.operation-trigger :deep(svg) {
  flex: none;
  color: var(--text-tertiary);
}

.picker-row {
  padding: 0 0.5rem;
}

.picker-row.group {
  padding-left: 0.375rem;
}

.picker-row.nested {
  padding-left: 1.5625rem;
}

.picker-row:hover:not(:disabled),
.picker-row.active {
  background: var(--bg-hover);
}

.picker-row:disabled {
  @apply opacity-50;
}

.picker-name {
  @apply flex-1 min-w-0 truncate;
}

.picker-caret {
  color: var(--text-tertiary);
}

.picker-count {
  flex: none;
  font-size: var(--text-12);
  font-variant-numeric: tabular-nums;
  color: var(--text-tertiary);
}

.picker-sub {
  @apply flex-none truncate text-text-tertiary;
  max-width: 48%;
  font-size: var(--text-12);
}
</style>
