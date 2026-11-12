<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef, watch } from 'vue'
import { PopoverRoot as Popover, PopoverTrigger } from 'reka-ui'
import Button from '../ui/Button.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import Input from '../ui/Input.vue'
import Select from '../ui/Select.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { Transfer } from '../../../bindings/schemo/internal/convert'
import type { JsonNode } from '../../lib/jsonTree'
import {
    cellText,
    tableCell,
    tableColumns,
    tableRows,
    tableValues,
    type TableColumn,
    type TableSort,
} from '../../lib/jsonTable'
import { callOrToast } from '../../lib/calls'
import { t } from '../../i18n'

const { arrays, selected = '$', query = '' } = defineProps<{
    arrays: JsonNode[]
    selected?: string
    query?: string
}>()
const emit = defineEmits<{ inspect: [path: string, event?: MouseEvent] }>()
const arrayPath = ref('')
const filter = ref('')
const hidden = reactive(new Set<string>())
const sort = ref<TableSort | null>(null)
const page = ref(0)
const saving = ref(false)
const scroll = useTemplateRef<HTMLElement>('scroll')
const pageSize = 100

watch(() => arrays, () => {
    if (arrays.some(array => array.path === arrayPath.value)) return
    const preferred = arrays.find(array => array.path === selected) ?? arrays.find(array => array.path === '$.data')
    arrayPath.value = preferred?.path ?? arrays[0]?.path ?? ''
}, { immediate: true })

const array = computed(() => arrays.find(array => array.path === arrayPath.value))
const options = computed(() => arrays.map(array => ({ value: array.path, label: `${array.path} (${array.children.length})` })))
const columns = computed(() => array.value ? tableColumns(array.value) : [])
const visible = computed(() => columns.value.filter(column => !hidden.has(column.id)))
const rows = computed(() => array.value ? tableRows(array.value, visible.value, [query, filter.value], sort.value) : [])
const pageCount = computed(() => Math.max(1, Math.ceil(rows.value.length / pageSize)))
const pageStart = computed(() => page.value * pageSize)
const pageLabel = computed(() => {
    const total = rows.value.length
    const end = Math.min(pageStart.value + pageSize, total)
    const shown = total ? `${pageStart.value + 1}–${end}` : 0
    return t('response.table.count', { shown, total })
})
const pageRows = computed(() => {
    const start = pageStart.value
    return rows.value.slice(start, start + pageSize).map(row => ({
        path: row.path,
        number: Number(row.key) + 1,
        cells: visible.value.map(column => {
            const value = tableCell(row, column)
            const missing = value === undefined
            return { id: column.id, text: cellText(value), missing, quiet: missing || value === null }
        }),
    }))
})
const label = (column: TableColumn) => column.key ?? t('response.table.value')

watch(arrayPath, () => {
    hidden.clear()
    sort.value = null
    filter.value = ''
})
watch(rows, () => {
    page.value = 0
})
watch(page, () => {
    if (scroll.value) scroll.value.scrollTop = 0
})

function toggleColumn(column: TableColumn) {
    if (hidden.has(column.id)) hidden.delete(column.id)
    else hidden.add(column.id)
    if (sort.value?.column.id === column.id) sort.value = null
}

function sortBy(column: TableColumn) {
    const current = sort.value
    const sameColumn = current?.column.id === column.id
    if (sameColumn && current.descending) {
        sort.value = null
        return
    }
    sort.value = { column, descending: sameColumn }
}

function sortDirection(column: TableColumn): 'ascending' | 'descending' | 'none' {
    if (sort.value?.column.id !== column.id) return 'none'
    return sort.value.descending ? 'descending' : 'ascending'
}

async function exportCSV() {
    if (saving.value || !visible.value.length) return
    saving.value = true
    try {
        const headers = visible.value.map(label)
        const values = tableValues(rows.value, visible.value)
        await callOrToast(
            Transfer.SaveCSV(t('response.table.export'), headers, values),
            'response.table.exportFailed'
        )
    } finally {
        saving.value = false
    }
}

defineExpose({ matches: computed(() => rows.value.length) })
</script>

<template>
  <div class="json-table">
    <div class="table-tools">
      <label v-if="arrays.length > 1" class="table-array">
        <span class="table-label">{{ t('response.table.array') }}</span>
        <Select v-model="arrayPath" :options />
      </label>
      <Input
        v-model="filter"
        class="table-filter"
        size="sm"
        :placeholder="t('response.table.filter')"
        :aria-label="t('response.table.filter')"
      />
      <Popover>
        <PopoverTrigger as-child>
          <Button size="sm" :disabled="!columns.length">
            {{ t('response.table.columns') }} · {{ visible.length }}/{{ columns.length }}
          </Button>
        </PopoverTrigger>
        <PopoverContent class="table-columns" align="end">
          <label v-for="column in columns" :key="column.id" class="column-option">
            <input
              type="checkbox"
              :checked="!hidden.has(column.id)"
              :disabled="visible.length === 1 && !hidden.has(column.id)"
              @change="toggleColumn(column)"
            />
            <span>{{ label(column) }}</span>
          </label>
        </PopoverContent>
      </Popover>
      <Button size="sm" :disabled="saving || !visible.length" @click="exportCSV">
        <Icon name="Download" :size="13" />
        CSV
      </Button>
    </div>
    <div ref="scroll" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th class="row-index" scope="col">#</th>
            <th v-for="column in visible" :key="column.id" scope="col" :aria-sort="sortDirection(column)">
              <button type="button" class="column-sort" @click="sortBy(column)">
                <span>{{ label(column) }}</span>
                <Icon v-if="sort?.column.id === column.id" :name="sort.descending ? 'ChevronDown' : 'ChevronUp'" :size="12" />
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in pageRows"
            :key="row.path"
            :class="{ selected: row.path === selected }"
            @contextmenu.prevent="emit('inspect', row.path, $event)"
          >
            <td class="row-index">
              <button
                type="button"
                class="row-open"
                :title="t('response.table.open', { path: row.path })"
                @click="emit('inspect', row.path)"
              >
                {{ row.number }}
                <Icon name="ArrowUpRight" :size="11" />
              </button>
            </td>
            <td v-for="cell in row.cells" :key="cell.id" :title="cell.text">
              <span class="cell" :class="{ quiet: cell.quiet }">
                {{ cell.missing ? '—' : cell.text }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="!rows.length" class="table-empty">{{ t('response.table.empty') }}</div>
    </div>
    <div class="table-footer">
      <span>{{ pageLabel }}</span>
      <span class="table-page">{{ t('response.pageOf', { page: page + 1, total: pageCount }) }}</span>
      <IconButton
        variant="bare"
        size="sm"
        :hint="t('response.previous')"
        :aria-label="t('response.previous')"
        :disabled="page === 0"
        @click="page--"
      >
        <Icon name="ChevronLeft" :size="14" />
      </IconButton>
      <IconButton
        variant="bare"
        size="sm"
        :hint="t('response.next')"
        :aria-label="t('response.next')"
        :disabled="page + 1 >= pageCount"
        @click="page++"
      >
        <Icon name="ChevronRight" :size="14" />
      </IconButton>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.json-table {
  @apply flex flex-col flex-1 min-h-0 min-w-0;
}

.table-tools {
  @apply flex flex-wrap items-center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--border);
}

.table-tools :deep(.select) {
  min-width: 0;
  max-width: 100%;
}

.table-array {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  max-width: 100%;
}

.table-label {
  @apply text-12 text-text-tertiary;
}

.table-tools :deep(select) {
  max-width: 18rem;
}

.table-filter {
  flex: 1 1 8rem;
  min-width: 6rem;
}

.table-columns {
  max-height: 20rem;
  overflow-y: auto;
}

.column-option {
  @apply flex items-center cursor-pointer text-13;
  gap: 0.5rem;
  padding: 0.375rem 0.25rem;
  overflow-wrap: anywhere;
}

.column-option input {
  accent-color: var(--accent);
}

.table-scroll {
  @apply flex-1 min-h-0 overflow-auto;
}

table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  font-family: var(--mono);
  font-size: var(--text-12);
}

th,
td {
  text-align: left;
  padding: 0 0.75rem;
  border-bottom: 1px solid var(--border);
  height: 2rem;
}

th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--bg);
  color: var(--text-secondary);
  font-weight: 500;
}

.column-sort,
.row-open {
  @apply inline-flex items-center cursor-pointer border-0 bg-transparent;
  gap: 0.375rem;
  padding: 0;
  color: inherit;
  font: inherit;
  min-height: 2rem;
  white-space: nowrap;
}

.row-open:hover {
  color: var(--accent);
}

.row-index {
  width: 1%;
  color: var(--text-tertiary);
}

.cell {
  display: block;
  min-width: 3rem;
  max-width: 22rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quiet {
  color: var(--text-tertiary);
}

tbody tr:hover {
  background: var(--bg-hover);
}

tbody tr.selected {
  background: var(--accent-soft);
}

.table-empty {
  @apply text-center text-13 text-text-tertiary;
  padding: 2rem;
}

.table-footer {
  @apply flex items-center text-12 text-text-tertiary;
  gap: 0.5rem;
  padding: 0.375rem 0.75rem;
  border-top: 1px solid var(--border);
}

.table-page {
  margin-left: auto;
}
</style>
