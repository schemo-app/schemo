<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import IconButton from '../ui/IconButton.vue'
import Icon from '../ui/Icon.vue'
import FileDrop from '../ui/FileDrop.vue'
import TokenField from '../ui/TokenField.vue'
import KvRow from './KvRow.vue'
import PaneSelect, { type PaneOption } from './PaneSelect.vue'
import { registerBodyEditor, type BodySpan } from '../../lib/intents'
import { Drafts } from '../../../bindings/schemo/internal/request'
import { useRowOrder } from '../../composables/useRowOrder'
import { callOrToast } from '../../lib/calls'
import { type MessageKey, t } from '../../i18n'
import type { RequestSource } from '../../lib/requestSource'
import { BodyKind, RowKind, type FormRow } from '../../../bindings/schemo/internal/domain'
import { useIndent } from '../../composables/useIndent'

// The body, as the pane draws it: the one section whose subject is a format rather than a list. Five
// branches in one file, because they share every style below and are never used apart.
const { source } = defineProps<{ source: RequestSource }>()

const { indent, insert } = useIndent()


const KINDS: { kind: BodyKind; label: MessageKey; type: string }[] = [
  { kind: BodyKind.BodyJSON, label: 'request.body.kinds.json', type: 'application/json' },
  { kind: BodyKind.BodyXML, label: 'request.body.kinds.xml', type: 'application/xml' },
  { kind: BodyKind.BodyRaw, label: 'request.body.kinds.raw', type: 'text/plain' },
  { kind: BodyKind.BodyForm, label: 'request.body.kinds.form', type: 'multipart/form-data' },
  { kind: BodyKind.BodyBinary, label: 'request.body.kinds.binary', type: 'application/octet-stream' },
]

const kind = computed(() => source.bodyKind)
const current = computed(() => KINDS.find((one) => one.kind === kind.value) ?? KINDS[2])

const options = computed<PaneOption[][]>(() => [
  KINDS.map((one) => ({ key: one.kind, label: t(one.label), hint: one.type })),
])

const isText = computed(
  () => kind.value === BodyKind.BodyJSON || kind.value === BodyKind.BodyXML || kind.value === BodyKind.BodyRaw,
)
const canFormat = computed(() => kind.value === BodyKind.BodyJSON || kind.value === BodyKind.BodyXML)

// A plain textarea, not a highlighted one: `{{tokens}}` are painted over a real input. The gutter is
// numbers only and as tall as the text, so the two scroll as one.
const lines = computed(() => Math.max(source.body.split('\n').length, 1))
// An empty body is still a place to write one: the editor opens a few lines tall rather than one.
const MIN_LINES = 6

const area = useTemplateRef<HTMLTextAreaElement>('area')
const findOpen = ref(false)
const findText = ref('')
const findInput = useTemplateRef<HTMLInputElement>('findInput')

// Selects a stretch of the text and scrolls to it. The editor is as tall as its text, so the
// nearest scrolling ancestor moves.
async function showSpan(span: BodySpan) {
  await nextTick()
  const textarea = area.value
  if (!textarea) return
  textarea.focus({ preventScroll: true })
  textarea.setSelectionRange(span.start, span.end)

  const line = textarea.value.slice(0, span.start).split('\n').length - 1
  const height = parseFloat(getComputedStyle(textarea).lineHeight) || 20
  let pane: HTMLElement | null = textarea.parentElement
  while (pane && pane.scrollHeight <= pane.clientHeight) pane = pane.parentElement
  if (!pane) return
  const target = textarea.getBoundingClientRect().top - pane.getBoundingClientRect().top + line * height
  if (target < 0 || target > pane.clientHeight - 2 * height) {
    pane.scrollBy({ top: target - pane.clientHeight / 3 })
  }
}

let unregister: (() => void) | null = null
onMounted(() => (unregister = registerBodyEditor((span) => void showSpan(span))))
onBeforeUnmount(() => unregister?.())

function onInput(e: Event) {
  source.setBody((e.target as HTMLTextAreaElement).value)
}

// Reformatting replaces the text with a re-typed version of itself, so it goes through the same
// buffer the typing does and is handed over on the same clock. Go reads the notation; an answer
// that arrives after the person has typed on is about text that is no longer there, and is let go.
async function format() {
  const text = source.body
  const next = await callOrToast(Drafts.FormatBody(kind.value, text, indent.value), 'request.editFailed')
  if (next === undefined || next === text || source.body !== text) return
  source.setBody(next)
  void source.flush()
}

async function toggleFind() {
  findOpen.value = !findOpen.value
  if (!findOpen.value) return
  await nextTick()
  findInput.value?.focus()
}

// A find that reports nothing is a find that did nothing: the selection stays where the caret was,
// which is where the user will look for the text themselves.
function find() {
  const textarea = area.value
  const needle = findText.value
  if (!textarea || needle === '') return
  const from = textarea.selectionEnd ?? 0
  const at = textarea.value.indexOf(needle, from) >= 0 ? textarea.value.indexOf(needle, from) : textarea.value.indexOf(needle)
  if (at < 0) return
  textarea.focus()
  textarea.setSelectionRange(at, at + needle.length)
}

function patch(id: string, patch: Record<string, unknown>) {
  void source.patchRow(RowKind.RowForm, id, patch)
}

function toggle(row: FormRow) {
  void source.toggleRow(RowKind.RowForm, row.id, !row.enabled)
}

const formEl = useTemplateRef<HTMLElement>('formEl')

// The fields of the form are dragged into their order the way the parameters are: the body is sent
// in the order the rows stand.
const { press, states } = useRowOrder({
  rows: () => source.form,
  table: formEl,
  move: (id, position) => source.moveRow(RowKind.RowForm, id, position),
})

// The empty row at the end is a place to type rather than a button, as in the parameters: pressing
// it makes the field and puts the caret in its name.
async function addField() {
  const id = await source.addRow(RowKind.RowForm)
  await nextTick()
  formEl.value?.querySelector<HTMLInputElement>(`[data-row="${id}"]`)?.focus()
}

async function pickFile(row: FormRow) {
  const path = await source.pickBodyFile()
  if (path) patch(row.id, { src: path, file: true })
}

async function pickBody() {
  const path = await source.pickBodyFile()
  if (path) void source.setBodyFile(path)
}

// The last path segment, for a chip that has room for a name and not for a path.
function baseName(path: string): string {
  const parts = path.split(/[\\/]/)
  return parts[parts.length - 1] || path
}

</script>

<template>
  <div class="body">
    <div class="body-head">
      <PaneSelect
        :label="t(current.label)"
        :options
        :current="kind"
        @select="source.setBodyKind($event as BodyKind)"
      />
      <span class="body-type mono">{{ current.type }}</span>
      <span class="body-spacer"></span>
      <div v-if="isText" class="body-tools">
        <input
          v-if="findOpen"
          ref="findInput"
          v-model="findText"
          class="find-input"
          :placeholder="t('request.body.findInBody')"
          spellcheck="false"
          @keydown.enter.prevent="find"
          @keydown.esc="findOpen = false"
        />
        <button v-if="canFormat" type="button" class="tool" @click="format">
          <Icon name="List" :size="14" :stroke-width="1.75" />
          <span class="cap-line">{{ t('request.body.format') }}</span>
        </button>
        <IconButton
          variant="bare"
          size="bar"
          class="tool-square"
          :class="{ on: findOpen }"
          :hint="t('request.body.findInBody')"
          @click="toggleFind"
        >
          <Icon name="Search" :size="14" :stroke-width="1.75" />
        </IconButton>
      </div>
    </div>

    <div v-if="isText" class="code">
      <div class="gutter mono" aria-hidden="true">
        <span v-for="n in lines" :key="n">{{ n }}</span>
      </div>
      <textarea
        ref="area"
        :value="source.body"
        class="editor mono"
        :rows="Math.max(lines, MIN_LINES)"
        wrap="off"
        :placeholder="kind === BodyKind.BodyRaw ? t('request.body.rawPlaceholder') : ''"
        spellcheck="false"
        @keydown.tab.prevent="insert"
        @input="onInput"
        @blur="source.flush()"
      ></textarea>
    </div>

    <!-- The form is the same table as the parameters and headers: a row per field and an empty row
    at the end for the next one. What a field adds is the clip at the end of its row, which makes it a
    file. -->
    <div v-else-if="kind === BodyKind.BodyForm" ref="formEl" class="kv">
      <KvRow
        v-for="(row, index) in source.form"
        :key="row.id"
        :enabled="row.enabled"
        :name="row.name"
        :row-id="row.id"
        :class="states[index].class"
        :style="states[index].style"
        grabbable
        :name-placeholder="t('request.placeholderKey')"
        @toggle="toggle(row)"
        @rename="patch(row.id, { name: $event })"
        @remove="source.removeRow(RowKind.RowForm, row.id)"
        @grab="press($event, row.id, index)"
      >
        <FileDrop
          v-if="row.file"
          as="button"
          type="button"
          class="file"
          @files="([path]) => patch(row.id, { src: path, file: true })"
          @click="pickFile(row)"
        >
          <Icon name="Download" :size="12" :stroke-width="2" />
          <span>{{ row.src ? baseName(row.src) : t('request.body.chooseFile') }}</span>
        </FileDrop>
        <TokenField
          v-else
          variant="cell"
          :value="row.value"
          :placeholder="t('request.placeholderValue')"
          @change="patch(row.id, { value: $event })"
        />
        <!-- The clip is this table's own tool: it is what makes a field of a row into a file. -->
        <template #tools>
          <IconButton
            variant="subtle"
            size="xl"
            class="clip"
            :class="{ lit: row.file }"
            :hint="row.file ? t('request.body.makeTextField') : t('request.body.makeFileField')"
            @click="patch(row.id, { file: !row.file })"
          >
            <Icon name="Paperclip" :size="14" :stroke-width="1.75" />
          </IconButton>
        </template>
      </KvRow>

      <button type="button" class="kv-row kv-add" @click="addField">
        <span></span>
        <span class="kv-check ghost"></span>
        <span class="kv-ghost mono">{{ t('request.pane.addField') }}</span>
        <span class="kv-ghost mono">{{ t('request.placeholderValue') }}</span>
        <span></span>
      </button>
    </div>

    <FileDrop v-else class="drop" @files="([path]) => source.setBodyFile(path)">
      <Icon name="Download" :size="24" :stroke-width="1.5" />
      <span class="drop-name">{{ source.bodyFile ? baseName(source.bodyFile) : t('request.body.noFileChosen') }}</span>
      <button type="button" class="drop-btn" @click="pickBody">{{ t('request.body.chooseFile') }}</button>
    </FileDrop>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.body {
  @apply flex flex-col gap-2;
  padding: 0.625rem 1.25rem 0.875rem;
}

.body-head {
  @apply flex items-center gap-2.5;
  height: 2rem;
  margin-bottom: 0.125rem;
}

.body-type {
  @apply whitespace-nowrap text-text-tertiary;
  font-size: var(--text-12-5);
}

.body-spacer {
  @apply flex-1;
}

.body-tools {
  @apply flex items-center gap-0.5 text-text-secondary;
}

.tool {
  @apply flex items-center gap-2 cursor-pointer border-0;
  height: 1.75rem;
  padding: 0 0.5rem;
  border-radius: 0.3rem;
  background: transparent;
  color: inherit;
  font-family: inherit;
  font-size: var(--text-12-5);
  --wails-draggable: no-drag;
}

.tool:hover {
  @apply bg-bg-hover text-text;
}

.body-tools :deep(.tool-square.on) {
  @apply bg-bg-hover text-text;
}

.find-input {
  @apply outline-none;
  width: 10.625rem;
  height: 1.625rem;
  margin-right: 0.25rem;
  padding: 0 0.5rem;
  border: 1px solid var(--border-strong);
  border-radius: 0.3rem;
  background: var(--bg-inset);
  color: var(--text);
  font-size: var(--text-12-5);
}

.find-input:focus {
  border-color: var(--accent);
}

.code {
  @apply flex;
  padding: 0.25rem 0 0.125rem;
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  font-size: var(--text-13);
  line-height: 1.25rem;
}

.gutter {
  @apply flex flex-col flex-none text-right select-none text-text-tertiary;
  width: 2.25rem;
  padding-right: 1rem;
  opacity: 0.7;
}

.editor {
  @apply flex-1 min-w-0 border-0 outline-none resize-none p-0 bg-transparent;
  font-family: var(--mono);
  font-size: var(--mono-size);
  line-height: 1.25rem;
  color: var(--text);
  white-space: pre;
  overflow-x: auto;
  overflow-y: hidden;
}

.editor::placeholder {
  color: var(--text-tertiary);
}

.kv-row {
  grid-template-columns: 1rem 1.5rem minmax(8.75rem, 15rem) minmax(0, 1fr) 3.5rem;
}

.file {
  @apply inline-flex items-center gap-2 cursor-pointer min-w-0 text-text-tertiary;
  height: 1.625rem;
  padding: 0 0.5rem;
  border-radius: 0.3rem;
  border: 1px dashed var(--border-strong);
  background: transparent;
  font-size: var(--text-12-5);
}

.file:hover {
  border-color: var(--accent);
  color: var(--text);
}

.file span {
  @apply truncate;
}

.kv :deep(.clip.lit) {
  background: var(--accent-soft);
  color: var(--accent);
}

.drop {
  @apply flex flex-col items-center justify-center gap-2.5 text-text-tertiary;
  height: 7.625rem;
  border-radius: 0.5rem;
  border: 1px dashed var(--border-strong);
  background: var(--bg-sidebar);
}

.drop-name {
  @apply truncate text-text-secondary;
  max-width: 90%;
  font-size: var(--text-13);
}

.drop-btn {
  @apply cursor-pointer text-text;
  height: 2rem;
  padding: 0 1rem;
  border: 1px solid var(--border-strong);
  border-radius: 0.5rem;
  background: var(--bg-inset);
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.drop-btn:hover {
  @apply bg-bg-hover;
}
</style>
