<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef } from 'vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import Segment from '../ui/Segment.vue'
import TextEditor from '../ui/TextEditor.vue'
import KvRow from './KvRow.vue'
import { statusBadgeClass } from '../../lib/format'
import { tryParseJson } from '../../lib/json'
import { responseParser } from '../../lib/responseFormat'
import { viewerLanguage } from '../../lib/viewerLanguage'
import { useRowOrder } from '../../composables/useRowOrder'
import { useIndent } from '../../composables/useIndent'
import { callOrToast } from '../../lib/calls'
import { Drafts } from '../../../bindings/schemo/internal/request'
import { formatBytes, t } from '../../i18n'
import { BodyKind, type ResponseExample } from '../../../bindings/schemo/internal/domain'

// An example written by hand: every field of it is a field, because there is nothing to write it
// with but one's own hands. What is typed is told to the list as it is typed, and the list is the
// caller's. The caller makes a new one of this for every example, so the rows below are seeded
// once.
const { example, fresh = false } = defineProps<{
  example: ResponseExample
  /** A new example: the name is the first thing to write, so the caret starts in it. */
  fresh?: boolean
}>()

const emit = defineEmits<{
  (e: 'patch', fields: Partial<ResponseExample>): void
  (e: 'remove'): void
}>()


const pane = defineModel<'body' | 'headers'>('pane', { required: true })

const nameEl = useTemplateRef<HTMLInputElement>('nameEl')

onMounted(() => {
  if (!fresh) return
  nameEl.value?.focus()
  nameEl.value?.select()
})

function setStatus(input: HTMLInputElement) {
  const digits = input.value.replace(/\D/g, '').slice(0, 3)
  input.value = digits
  emit('patch', { status: digits ? Number(digits) : 0 })
}

const validJson = computed(() => tryParseJson(example.body ?? '').ok)
const bodyInfo = computed(() => {
  const body = example.body ?? ''
  if (!body.trim()) return { text: '', bad: false, format: false }
  if (bodyLanguage.value === 'json' && !validJson.value) return { text: t('request.examples.bodyBad'), bad: true, format: false }
  const size = formatBytes(new TextEncoder().encode(body).length)
  return { text: validJson.value ? t('request.examples.bodyJson', { size }) : size, bad: false, format: validJson.value }
})

// Go lays the text out: a number keeps its digits and the editor's own indent is the one used.
const { indent } = useIndent()

async function formatBody() {
  const body = example.body ?? ''
  const next = await callOrToast(
    Drafts.FormatBody(BodyKind.BodyJSON, body, indent.value),
    'request.editFailed'
  )
  if (next === undefined || next === body || example.body !== body) return
  emit('patch', { body: next })
}

// The grammar of the body being written, asked of the example's own content type — the same question
// the answer is read with.
const bodyLanguage = computed(() => {
  const type = (example.headers ?? []).find((header) => header.name.toLowerCase() === 'content-type')?.value ?? ''
  if (responseParser(type, null) === 'json-stringify') return 'json'
  return viewerLanguage(type, example.body ?? '', validJson.value)
})

// The headers, as the kv table edits them: rows with an id of their own so a drag has something to
// hold. The list is the table's while the example is open, and every change is written through.
let seq = 0
const rows = ref<{ id: string; name: string; value: string }[]>(
  (example.headers ?? []).map((header) => ({ id: `h${seq++}`, name: header.name, value: header.value }))
)

const panes = computed(() => [
  { value: 'body', label: t('request.chips.body') },
  { value: 'headers', label: t('request.chips.headers'), count: rows.value.length },
])

function writeHeaders() {
  emit('patch', { headers: rows.value.map(({ name, value }) => ({ name, value })) })
}

const table = useTemplateRef<HTMLElement>('headerTable')
const { press, states } = useRowOrder({
  rows: () => rows.value,
  table,
  move: async (id, position) => {
    const from = rows.value.findIndex((row) => row.id === id)
    if (from < 0 || from === position) return
    const [row] = rows.value.splice(from, 1)
    rows.value.splice(position, 0, row)
    writeHeaders()
  },
})

function renameHeader(index: number, name: string) {
  rows.value[index].name = name
  writeHeaders()
}

function valueHeader(index: number, value: string) {
  rows.value[index].value = value
  writeHeaders()
}

function removeHeader(index: number) {
  rows.value.splice(index, 1)
  writeHeaders()
}

async function addHeader() {
  const id = `h${seq++}`
  rows.value.push({ id, name: '', value: '' })
  writeHeaders()
  await nextTick()
  table.value?.querySelector<HTMLInputElement>(`[data-row="${id}"]`)?.focus()
}
</script>

<template>
  <div class="example-form">
    <div class="example-toolbar">
      <div class="example-picker">
        <input
          class="example-status"
          :class="example.status ? statusBadgeClass(example.status) : 'neutral'"
          :value="example.status || ''"
          :aria-label="t('request.examples.statusTitle')"
          :title="t('request.examples.statusTitle')"
          inputmode="numeric"
          spellcheck="false"
          placeholder="000"
          @input="setStatus($event.target as HTMLInputElement)"
        />
        <span class="example-name">
          <span aria-hidden="true">{{ example.name || t('request.examples.namePlaceholder') }}</span>
          <input
            ref="nameEl"
            :value="example.name"
            :aria-label="t('request.examples.namePlaceholder')"
            :placeholder="t('request.examples.namePlaceholder')"
            :title="example.name"
            spellcheck="false"
            @input="emit('patch', { name: ($event.target as HTMLInputElement).value })"
          />
        </span>
        <slot />
      </div>
      <Segment
        class="example-panes"
        variant="toolbar"
        size="xs"
        :grow="false"
        :options="panes"
        :value="pane"
        @pick="pane = $event as 'body' | 'headers'"
      />
      <div class="example-tools">
        <template v-if="pane === 'body'">
          <span v-if="bodyInfo.text" class="example-info" :class="{ bad: bodyInfo.bad }" :title="bodyInfo.text">
            {{ bodyInfo.text }}
          </span>
          <IconButton
            v-if="bodyInfo.format"
            variant="bare"
            size="bar"
            :hint="t('request.examples.format')"
            :aria-label="t('request.examples.format')"
            @click="formatBody"
          >
            <Icon name="AlignLeft" :size="16" :stroke-width="1.5" />
          </IconButton>
        </template>
        <IconButton
          variant="danger"
          size="bar"
          :hint="t('request.examples.deleteTitle')"
          :aria-label="t('request.examples.deleteTitle')"
          @click="emit('remove')"
        >
          <Icon name="Trash2" :size="16" :stroke-width="1.5" />
        </IconButton>
      </div>
    </div>

    <div v-if="pane === 'body'" class="example-body">
      <TextEditor
        :model-value="example.body ?? ''"
        :language="bodyLanguage"
        placeholder='{ "id": 1 }'
        @update:model-value="emit('patch', { body: $event })"
      />
    </div>
    <div v-else ref="headerTable" class="kv example-headers">
      <KvRow
        v-for="(row, i) in rows"
        :key="row.id"
        :enabled="true"
        :checkable="false"
        :name="row.name"
        :row-id="row.id"
        :name-placeholder="t('request.examples.columnName')"
        :class="states[i]?.class"
        :style="states[i]?.style"
        grabbable
        @rename="renameHeader(i, $event)"
        @remove="removeHeader(i)"
        @grab="press($event, row.id, i)"
      >
        <input
          class="kv-input mono"
          :value="row.value"
          :placeholder="t('request.examples.columnValue')"
          spellcheck="false"
          @input="valueHeader(i, ($event.target as HTMLInputElement).value)"
        />
      </KvRow>
      <button type="button" class="kv-row kv-add" @click="addHeader">
        <span></span>
        <span class="kv-ghost mono">{{ t('request.examples.columnName') }}</span>
        <span class="kv-ghost mono">{{ t('request.examples.columnValue') }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.example-form {
  @apply flex flex-1 flex-col min-h-0 min-w-0;
  container-type: inline-size;
}

.example-toolbar {
  @apply flex flex-none items-center min-w-0 border-b border-border;
  min-height: 2.625rem;
  gap: 0.625rem;
  padding: 0.375rem 0.75rem 0.375rem 1rem;
}

.example-picker {
  @apply flex items-center min-w-0 rounded-md;
  height: 1.75rem;
  gap: 0.125rem;
  padding: 0 0.1875rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.example-status,
.example-name input {
  @apply h-[1.375rem] min-w-0 rounded-sm border-0 outline-none;
}

.example-status {
  @apply flex-none w-9 text-center text-12 font-mono font-semibold p-0;
}

.example-status.neutral {
  background: var(--chip);
  color: var(--text-secondary);
}

.example-status:focus,
.example-name input:focus {
  box-shadow: 0 0 0 1px var(--accent);
}

.example-name {
  @apply inline-grid items-center min-w-0 font-medium;
  max-width: 17.5rem;
}

.example-name > * {
  grid-area: 1 / 1;
  padding: 0 0.375rem;
}

.example-name span {
  @apply invisible overflow-hidden whitespace-pre;
}

.example-name input {
  @apply w-0 min-w-full bg-transparent text-text;
  font: inherit;
}

.example-name input:focus {
  background: var(--bg);
}

.example-panes {
  @apply flex-none;
}

.example-tools {
  @apply flex items-center min-w-0 gap-0.5 ml-auto;
}

.example-info {
  @apply truncate text-12 text-text-tertiary;
  padding: 0 0.25rem;
}

.example-info.bad {
  color: var(--red-text);
}

.example-body {
  @apply flex flex-1 min-h-0;
  padding: 0.5rem 0.9375rem 0;
}

.example-headers {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0.375rem 0.75rem 1rem;
}

.example-headers .kv-row {
  grid-template-columns: 1rem minmax(0, min(13.75rem, 38%)) minmax(0, 1fr) 1.75rem;
}

.example-headers .kv-add {
  @apply cursor-text;
}

@container (max-width: 31rem) {
  .example-toolbar {
    gap: 0.375rem;
    padding-inline: 0.625rem;
  }

  .example-picker {
    flex: 1;
  }

  .example-info {
    display: none;
  }
}
</style>
