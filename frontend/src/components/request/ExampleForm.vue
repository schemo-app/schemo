<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef } from 'vue'
import Icon from '../ui/Icon.vue'
import TextEditor from '../ui/TextEditor.vue'
import KvRow from './KvRow.vue'
import ExampleView from './ExampleView.vue'
import { statusBadgeClass } from '../../lib/format'
import { STATUS_TEXT } from '../../lib/layout'
import { tryParseJson } from '../../lib/json'
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


const pane = ref<'body' | 'headers'>('body')

const nameEl = useTemplateRef<HTMLInputElement>('nameEl')

onMounted(() => {
  if (!fresh) return
  nameEl.value?.focus()
  nameEl.value?.select()
})

const reason = computed(
  () =>
    STATUS_TEXT[example.status ?? 0] ||
    (example.status ? t('request.examples.statusCustom') : t('request.examples.statusEmpty'))
)

function setStatus(value: string) {
  const digits = value.replace(/\D/g, '').slice(0, 3)
  emit('patch', { status: digits ? Number(digits) : 0 })
}

const bodyInfo = computed(() => {
  const body = example.body ?? ''
  if (!body.trim()) return { text: t('request.examples.bodyEmpty'), bad: false, format: false }
  if (!tryParseJson(body).ok) return { text: t('request.examples.bodyBad'), bad: true, format: false }
  const size = formatBytes(new TextEncoder().encode(body).length)
  return { text: t('request.examples.bodyJson', { size }), bad: false, format: true }
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
  return viewerLanguage(type, example.body ?? '', tryParseJson(example.body ?? '').ok)
})

// The headers, as the kv table edits them: rows with an id of their own so a drag has something to
// hold. The list is the table's while the example is open, and every change is written through.
let seq = 0
const rows = ref<{ id: string; name: string; value: string }[]>(
  (example.headers ?? []).map((header) => ({ id: `h${seq++}`, name: header.name, value: header.value }))
)

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
  <ExampleView v-model:pane="pane" :count="rows.length">
    <template #title>
      <input
        ref="nameEl"
        class="name"
        :value="example.name"
        :placeholder="t('request.examples.namePlaceholder')"
        spellcheck="false"
        @input="emit('patch', { name: ($event.target as HTMLInputElement).value })"
      />
      <label
        class="status"
        :class="example.status ? statusBadgeClass(example.status) : 'neutral'"
        :title="t('request.examples.statusTitle')"
      >
        <input
          class="status-input"
          :value="example.status || ''"
          inputmode="numeric"
          spellcheck="false"
          placeholder="000"
          @input="setStatus(($event.target as HTMLInputElement).value)"
        />
        <span class="reason">{{ reason }}</span>
      </label>
      <button
        type="button"
        class="del"
        :title="t('request.examples.deleteTitle')"
        @click="emit('remove')"
      >
        <Icon name="Trash2" :size="14" :stroke-width="1.8" />
      </button>
    </template>

    <template v-if="pane === 'body'" #bar>
      <span class="info" :class="{ bad: bodyInfo.bad }">{{ bodyInfo.text }}</span>
      <span class="flex-1"></span>
      <button v-if="bodyInfo.format" type="button" class="format" @click="formatBody">
        {{ t('request.examples.format') }}
      </button>
    </template>

    <div v-if="pane === 'body'" class="body">
      <div class="box">
        <TextEditor
          :model-value="example.body ?? ''"
          :language="bodyLanguage"
          placeholder='{ "id": 1 }'
          @update:model-value="emit('patch', { body: $event })"
        />
      </div>
    </div>
    <div v-else ref="headerTable" class="kv heads">
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
        @toggle="() => {}"
      >
        <input
          class="kv-input"
          :value="row.value"
          :placeholder="t('request.examples.columnValue')"
          spellcheck="false"
          @input="valueHeader(i, ($event.target as HTMLInputElement).value)"
        />
      </KvRow>

      <button type="button" class="kv-row kv-add" @click="addHeader">
        <span></span>
        <span class="kv-ghost mono">+ {{ t('request.examples.columnName') }}</span>
        <span class="kv-ghost mono">{{ t('request.examples.columnValue') }}</span>
        <span></span>
      </button>
    </div>
  </ExampleView>
</template>

<style scoped>
@reference "../../style.css";

.name {
  @apply flex-1 min-w-20 border-0 outline-none bg-transparent text-text font-semibold;
  height: 2rem;
  box-sizing: border-box;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  font-family: inherit;
  font-size: var(--text-15);
}

.name:hover,
.name:focus {
  background: var(--raise);
}

.status {
  @apply flex items-center flex-none;
  height: 1.75rem;
  gap: 0.125rem;
  padding: 0 0.5625rem 0 0.1875rem;
  border-radius: 0.4375rem;
}

.status.neutral {
  background: var(--chip);
  color: var(--text-secondary);
}

.status-input {
  @apply text-center font-semibold border-0 outline-none bg-transparent;
  width: 2.25rem;
  height: 1.375rem;
  box-sizing: border-box;
  border-radius: 0.3125rem;
  color: inherit;
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.status-input:focus {
  background: var(--bg);
}

.reason {
  @apply font-medium whitespace-nowrap;
  font-size: var(--text-12);
}

.del {
  @apply flex items-center justify-center flex-none cursor-pointer border-0 bg-transparent text-text-tertiary;
  width: 1.75rem;
  height: 1.75rem;
  border-radius: 0.4375rem;
}

.del:hover {
  background: var(--bg-hover);
  color: var(--red);
}

.info {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.info.bad {
  color: var(--red);
}

.format {
  @apply flex-none cursor-pointer border-0 bg-transparent p-0 text-accent;
  font-family: inherit;
  font-size: var(--text-12);
}

.body {
  @apply flex flex-1 min-h-0;
  padding: 0 1.25rem 1rem;
}

.box {
  @apply flex flex-1 min-h-0 overflow-hidden;
  min-height: 5rem;
  border-radius: 0.5625rem;
  background: var(--raise);
  padding: 0.75rem 0.875rem;
}

.heads {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0 1.25rem 1rem;
}

.heads .kv-row {
  grid-template-columns: 1rem minmax(0, 0.8fr) minmax(0, 1.4fr) 1.75rem;
}

.heads .kv-add {
  @apply cursor-text;
}
</style>
