<script setup lang="ts">
import { computed } from 'vue'
import { useAuthSchemes } from '../../composables/useAuthSchemes'
import { fold, sideBySide, type DiffLine } from '../../lib/lineDiff'
import { prettyJsonText } from '../../lib/json'
import { t, textOf } from '../../i18n'
import { Attribute, ChangeKind, Field, type Change } from '../../../bindings/schemo/internal/convert/merge'
import { AuthType } from '../../../bindings/schemo/internal/domain'

const { changes, kind, takeFile } = defineProps<{
  changes: Change[]
  /** What the item is: Go starts its id with it. */
  kind: string
  /** Whether the conflicts go the file's way. */
  takeFile: boolean
}>()

const { nameOf } = useAuthSchemes()

// Rows told apart by name share one table, as the request editor draws them.
const TABLES = new Set<Field>([
  Field.FieldParam,
  Field.FieldHeader,
  Field.FieldPathParam,
  Field.FieldFormField,
  Field.FieldVariable,
])

interface Cell {
  tone: 'old' | 'new' | 'gap' | ''
  sign: string
  n?: number
  key?: string
  text: string
}

type Line = { cells: Cell[]; conflict: boolean } | { skipped: number }

interface Section {
  id: string
  title: string
  /** Words are drawn in the interface's face and everything else as code. */
  prose: boolean
  numbered: boolean
  lines: Line[]
}

const GAP: Cell = { tone: 'gap', sign: '', text: '' }
const before = (text: string, key?: string): Cell => ({ tone: 'old', sign: '−', key, text })
const after = (text: string, key?: string): Cell => ({ tone: 'new', sign: '+', key, text })

const sections = computed(() => {
  const out: Section[] = []
  for (const change of changes) {
    if (!TABLES.has(change.field)) {
      out.push(valueSection(change))
      continue
    }
    let table = out.find((section) => section.id === change.field)
    if (!table) {
      const title = textOf(`spec.section.${change.field}`)
      table = { id: change.field, title, prose: false, numbered: false, lines: [] }
      out.push(table)
    }
    table.lines.push(rowLine(change))
  }
  return out
})

function rowLine(change: Change): Line {
  const described = change.attribute === Attribute.AttributeDescription
  const key = described ? `${change.name} · ${t('spec.attribute.description')}` : change.name
  return {
    conflict: !!change.conflict,
    cells: [
      change.kind === ChangeKind.ChangeAdded ? GAP : before(display(change, change.mine), key),
      change.kind === ChangeKind.ChangeRemoved ? GAP : after(display(change, change.theirs), key),
    ],
  }
}

function valueSection(change: Change): Section {
  const mine = display(change, change.mine)
  const theirs = display(change, change.theirs)
  const numbered =
    change.field === Field.FieldBody || change.field === Field.FieldExample || `${mine}${theirs}`.includes('\n')
  const whole: Line = {
    conflict: !!change.conflict,
    cells: [mine ? before(mine) : GAP, theirs ? after(theirs) : GAP],
  }
  return {
    id: change.id,
    title: title(change),
    prose:
      change.field === Field.FieldName ||
      change.field === Field.FieldDescription ||
      change.field === Field.FieldFolder,
    numbered,
    lines: numbered ? textLines(change, mine, theirs) : [whole],
  }
}

function textLines(change: Change, mine: string, theirs: string): Line[] {
  const rows = sideBySide(mine, theirs)
  // Texts alike line for line differ in what is not drawn (an example's headers), so show them whole.
  return (rows.some((row) => row.changed) ? fold(rows) : rows).map((row) => {
    if ('skipped' in row) return row
    const cell = (line: DiffLine | null, draw: (text: string) => Cell): Cell => {
      if (!line) return GAP
      const plain: Cell = { tone: '', sign: '', text: line.text }
      return { ...(row.changed ? draw(line.text) : plain), n: line.n }
    }
    return {
      conflict: !!change.conflict && row.changed,
      cells: [cell(row.left, before), cell(row.right, after)],
    }
  })
}

function title(change: Change) {
  const field = textOf(`spec.field.${change.field}`, { name: change.name ?? '' })
  const named = change.attribute === Attribute.AttributeName || change.attribute === Attribute.AttributeDescription
  return named ? `${field} · ${textOf(`spec.attribute.${change.attribute}`)}` : field
}

// JSON is laid out the same way on both sides, so that only what differs shows.
function display(change: Change, value = '') {
  if (change.field === Field.FieldBody || change.field === Field.FieldExample) {
    return prettyJsonText(value) ?? value
  }
  if (change.attribute === Attribute.AttributeEnabled) return t(value === 'true' ? 'spec.on' : 'spec.off')
  if (change.attribute === Attribute.AttributeFile) return t(value === 'true' ? 'spec.file' : 'spec.text')
  if (change.field === Field.FieldAuth && value) return authText(value)
  return value
}

// Go writes an authorization as its type followed by the fields it sets.
function authText(value: string) {
  const [type = '', ...fields] = value.split(' · ')
  let named = nameOf(type as AuthType)
  if (type === AuthType.AuthInherit) named = t(kind === 'settings' ? 'spec.unset' : 'spec.inherit')
  return [named, ...fields].join(' · ')
}
</script>

<template>
  <div class="diff">
    <div v-if="$slots.toolbar" class="resolve">
      <slot name="toolbar" />
    </div>
    <div class="line head">
      <div class="cell">{{ t('spec.current') }}</div>
      <div class="cell">{{ t('spec.incoming') }}</div>
    </div>
    <div v-for="section in sections" :key="section.id" class="section" :class="{ prose: section.prose }">
      <div class="title">{{ section.title }}</div>
      <template v-for="(line, at) in section.lines" :key="at">
        <div v-if="'skipped' in line" class="skipped">⋯</div>
        <div v-else class="line" :class="{ conflict: line.conflict }">
          <!-- A conflict has a side the update leaves out. -->
          <div
            v-for="(cell, side) in line.cells"
            :key="side"
            class="cell"
            :class="[cell.tone, { lost: line.conflict && side === (takeFile ? 0 : 1) }]"
          >
            <!-- A numbered line shows its side by colour, as editors do. -->
            <span class="mark" :class="{ num: section.numbered }">{{ section.numbered ? cell.n : cell.sign }}</span>
            <span v-if="cell.key" class="key" :title="cell.key">{{ cell.key }}</span>
            <pre>{{ cell.text }}</pre>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.diff {
  /* Every label and value starts here: past the mark's slot and the gap after it. */
  --text-edge: 1.625rem;

  @apply overflow-hidden;
  border-radius: 0.5rem;
  box-shadow: inset 0 0 0 1px var(--border);
  font-size: var(--text-12);
  line-height: 1.25rem;
}

.line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}

.resolve {
  @apply flex items-center;
  gap: 0.75rem;
  height: 2.25rem;
  padding: 0 0.25rem 0 var(--text-edge);
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
}

.section {
  border-top: 1px solid var(--border);
}

.title {
  @apply font-semibold;
  padding: 0.25rem 0.5rem 0.125rem var(--text-edge);
  color: var(--text-secondary);
}

.cell {
  @apply flex min-w-0;
  padding: 0.125rem 0.5rem 0.125rem 0;
  font-family: var(--mono);
}

.cell + .cell {
  border-left: 1px solid var(--border);
}

.head .cell {
  @apply font-semibold;
  padding: 0.25rem 0.5rem 0.25rem var(--text-edge);
  color: var(--text-tertiary);
  font-family: inherit;
}

.prose .cell {
  font-family: inherit;
}

.cell.old {
  background: var(--red-soft);
}

.cell.new {
  background: var(--green-soft);
}

.cell.gap {
  background: var(--chip);
}

.cell.lost {
  @apply opacity-45;
}

.line.conflict {
  position: relative;
}

.line.conflict::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 0.125rem;
  background: var(--orange);
}

/* The sign or line number sits in a slot as wide as a row's change mark, under it. */
.mark {
  @apply flex-none text-center select-none;
  width: 1rem;
  margin-right: 0.625rem;
  color: var(--text-tertiary);
  font-family: var(--mono);
}

.mark.num {
  @apply text-right;
  font-size: var(--text-11);
}

/* A fixed share of the cell, so a table's values stand in one column. */
.key {
  @apply flex-none truncate;
  width: 35%;
  padding-right: 0.5rem;
  color: var(--text-secondary);
}

pre {
  @apply m-0 flex-1 min-w-0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: inherit;
  user-select: text;
}

.skipped {
  @apply text-center select-none;
  color: var(--text-tertiary);
}
</style>
