<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useFocus, useTimeoutFn } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import Segment from '../ui/Segment.vue'
import DialogButton from '../ui/DialogButton.vue'
import TransferCheck from './TransferCheck.vue'
import TransferDialog from './TransferDialog.vue'
import TransferField from './TransferField.vue'
import TransferRow from './TransferRow.vue'
import { useCollectionsStore } from '../../stores/collections'
import { useTransferStore, type TransferFormat } from '../../stores/transfer'
import { requestCount } from '../../lib/collectionTree'
import { useWailsEvent } from '../../composables/useWailsEvent'
import { formatBytes, t } from '../../i18n'
import type { Collection } from '../../../bindings/schemo/internal/domain'
import type { Preview } from '../../../bindings/schemo/internal/convert'

type Source = TransferFormat | 'ws'
type Via = 'file' | 'url' | 'paste'

const SOURCES: { id: Source; mono: string; vias: Via[] }[] = [
  { id: 'ws', mono: '⇄', vias: [] },
  { id: 'page', mono: 'WEB', vias: ['url'] },
  { id: 'schemo', mono: 'S', vias: ['file'] },
  { id: 'postman', mono: 'PM', vias: ['file', 'url'] },
  { id: 'insomnia', mono: 'IN', vias: ['file'] },
  { id: 'openapi', mono: 'OA', vias: ['file', 'url'] },
  { id: 'har', mono: 'HAR', vias: ['file'] },
  { id: 'curl', mono: '$_', vias: ['paste'] },
]

const transfer = useTransferStore()
const collections = useCollectionsStore()

const source = ref<Source | null>(null)
const via = ref<Via>('file')
// What Go last read: the token is good once, and the card and the button count what it names.
const preview = ref<Preview | null>(null)
// Which read may still land: an answer that comes back after the dialog moved on (another source,
// another way in) is not the preview of the screen it would arrive at, and its token would import
// the wrong thing.
let latestRead = 0
const url = ref('')
const curl = ref('')
const target = ref('')
const workspaceId = ref('')
const picked = ref<Record<string, boolean>>({})
// The address and the paste are the one thing to type on their screen: the caret is in it.
const field = useTemplateRef<HTMLElement>('field')
useFocus(field, { initialValue: true })

const current = computed(() => SOURCES.find((row) => row.id === source.value) ?? null)
const vias = computed(() => current.value?.vias ?? [])
const viaOptions = computed(() =>
  vias.value.map((one) => ({ value: one, label: one === 'file' ? t('transfer.file') : t('transfer.byUrl') }))
)
// The source as a format Go reads; the workspace screen has none.
const format = computed<TransferFormat | null>(() => {
  const id = source.value
  return id && id !== 'ws' ? id : null
})
// A paste arrives a keystroke at a time, so it is read behind a pause.
const pause = useTimeoutFn(() => void previewCurl(), 400, { immediate: false })

const otherWorkspaces = computed(() => transfer.sources.filter((one) => !one.active))
const workspaceOptions = computed(() => otherWorkspaces.value.map((one) => ({ id: one.id, label: one.name })))
const workspace = computed(
  () => otherWorkspaces.value.find((one) => one.id === workspaceId.value) ?? otherWorkspaces.value[0] ?? null
)
const pickedIds = computed(() =>
  (workspace.value?.collections ?? []).filter((one) => picked.value[one.id]).map((one) => one.id)
)

const title = computed(() => {
  const id = source.value
  if (!id) return t('transfer.import')
  if (id === 'ws') return t('transfer.fromWorkspace')
  return t('transfer.from', { source: t(`transfer.source.${id}`) })
})

const nameOf = (id: Source) => t(`transfer.source.${id}`)

function hintOf(id: Source) {
  return id === 'ws' ? t('counts.workspaces', otherWorkspaces.value.length) : t(`transfer.source.${id}Hint`)
}

// Only the sources read from a file say what a file may be; a paste has no drop zone to label, and
// a page is an address rather than a file.
const accept = computed(() => {
  const id = source.value
  if (!id || id === 'ws' || id === 'curl' || id === 'page') return ''
  return t(`transfer.accept.${id}`)
})

const found = computed(() => {
  const read = preview.value
  if (!read) return ''
  const folders = read.folders
    ? ` ${t('transfer.inFolders', { n: t('counts.folders', read.folders) })}`
    : ''
  return `${formatBytes(read.size)} · ${t('counts.requests', read.requests)}${folders}`
})

const urlPlaceholder = computed(() => {
  if (source.value === 'openapi') return 'https://api.example.com/openapi.yaml'
  if (source.value === 'page') return 'https://pages.example.com/petstore'
  return 'https://www.postman.com/collections/…'
})

// Every place a collection can land, nested folders included, counted over the whole subtree.
const places = computed(() => {
  const out: { id: string; label: string }[] = []
  const walk = (list: Collection[]) => {
    for (const row of list) {
      out.push({ id: row.id, label: `${row.name} · ${t('counts.requests', requestCount(row))}` })
      walk(row.children ?? [])
    }
  }
  walk(collections.tree)
  return out
})

// What the new collection would be called before anything is read: a document names itself, a paste
// of commands has no name of its own (the window says where it came from), and nothing read yet
// names nothing.
const newName = computed(() => {
  if (preview.value?.name) return t('transfer.newCollection', { name: preview.value.name })
  if (source.value === 'curl') {
    return t('transfer.newCollection', { name: t('transfer.fromCurl') })
  }
  return t('transfer.newCollectionPlain')
})

const targets = computed(() => {
  const top = source.value === 'ws' ? t('transfer.separate') : newName.value
  return [{ id: '', label: top }, ...places.value]
})

const ready = computed(() => {
  if (transfer.busy) return false
  if (source.value === 'ws') return pickedIds.value.length > 0
  const read = preview.value
  if (!read) return false
  return via.value !== 'paste' || read.requests > 0
})

const primaryLabel = computed(() => {
  if (source.value === 'ws') {
    return t('transfer.importCollections', { n: t('counts.collections', pickedIds.value.length) })
  }
  const read = preview.value
  return read && read.requests > 0
    ? t('transfer.doImportCount', { n: read.requests })
    : t('transfer.doImport')
})

// Every open starts over; the workspaces are read as well, because the list counts them before the
// workspace screen asks.
watch(
  () => transfer.importOpen,
  (open) => {
    pause.stop()
    forgetPreview()
    if (!open) return
    source.value = null
    via.value = 'file'
    url.value = ''
    curl.value = ''
    // The id the dialog was opened for may name a collection that is not in the tree any more (a
    // tab left open over a deleted row). The select offers what exists, so it starts on the new
    // collection unless the target is one of them.
    target.value = places.value.some((place) => place.id === transfer.importParent)
      ? transfer.importParent
      : ''
    workspaceId.value = ''
    picked.value = {}
    void transfer.loadWorkspaces()

    // A link from outside opened this dialog about one page: it is read at once, because the
    // person who followed the link has already said which collection they want.
    if (transfer.linkedPage) openLinked(transfer.linkedPage)
  },
  { immediate: true }
)

// A link that arrives while the dialog is already open is the same request arriving late, and the
// dialog is still where it is read.
watch(
  () => transfer.linkedPage,
  (page) => {
    if (page) openLinked(page)
  }
)

function forgetPreview() {
  latestRead++
  preview.value = null
}

// Takes the answer of a read as the preview, unless the dialog has moved on or asked again since.
async function showPreview(read: Promise<Preview | null>) {
  const ticket = ++latestRead
  const answer = await read
  if (answer && ticket === latestRead) preview.value = answer
}

function openLinked(page: string) {
  transfer.linkedPage = ''
  source.value = 'page'
  via.value = 'url'
  forgetPreview()
  url.value = page
  void readUrl()
}

function open(row: { id: Source; vias: Via[] }) {
  source.value = row.id
  via.value = row.vias[0] ?? 'file'
  forgetPreview()
  if (row.id !== 'ws') return
  picked.value = {}
  void transfer.loadWorkspaces()
}

function back() {
  source.value = null
  forgetPreview()
}

// A preview names what Go read, so it does not survive a move to another via.
function pickVia(next: Via) {
  if (via.value === next) return
  via.value = next
  forgetPreview()
}

function toggle(id: string) {
  picked.value = { ...picked.value, [id]: !picked.value[id] }
}

function pickWorkspace(id = '') {
  workspaceId.value = id
  picked.value = {}
}

async function pickFile() {
  const id = format.value
  if (!id) return
  await showPreview(transfer.previewFile(id, t('transfer.pickFile', { source: nameOf(id) })))
}

async function readUrl() {
  const id = format.value
  const address = url.value.trim()
  if (!id || !address) return
  await showPreview(transfer.previewUrl(id, address))
}

// A half-written command reads as nothing, and the last preview that did read stays rather than
// blinking out under the button.
async function previewCurl() {
  const id = format.value
  const text = curl.value
  if (id !== 'curl' || !text.trim()) return
  await showPreview(transfer.previewText(id, text, '', true))
}

useWailsEvent('files-dropped', (event) => {
  const { files, target } = event.data
  const id = format.value
  const path = files?.[0]
  if (!transfer.importOpen || via.value !== 'file' || !id || !path || target !== 'collection-import-drop') return
  void showPreview(transfer.previewPath(id, path))
})

async function commit() {
  if (!ready.value) return
  if (source.value === 'ws') {
    const chosen = workspace.value
    if (!chosen) return
    const done = await transfer.importFromWorkspace(
      chosen.id,
      pickedIds.value,
      target.value,
      chosen.name
    )
    if (done) transfer.closeTransfer()
    return
  }
  const read = preview.value
  if (!read) return
  if (await transfer.apply(read.token, target.value, t('transfer.fromCurl'))) {
    transfer.closeTransfer()
  }
}
</script>

<template>
  <TransferDialog :open="transfer.importOpen" :title @close="transfer.closeTransfer()">
    <template v-if="source" #lead>
      <button type="button" class="back" :title="t('transfer.back')" @click="back">
        <Icon name="ChevronLeft" :size="14" :stroke-width="2.4" />
      </button>
    </template>

    <div v-if="!source" class="-mx-2.5 flex flex-col">
      <TransferRow
        v-for="row in SOURCES"
        :key="row.id"
        :mono="row.mono"
        :name="nameOf(row.id)"
        :hint="hintOf(row.id)"
        @click="open(row)"
      />
    </div>

    <template v-else-if="source === 'ws'">
      <TransferField
        :label="t('transfer.workspace')"
        :model-value="workspace?.id ?? ''"
        :options="workspaceOptions"
        @update:model-value="pickWorkspace"
      />

      <TransferField :label="t('transfer.collections')">
        <div class="-mx-2.5 flex flex-col">
          <div
            v-for="one in workspace?.collections ?? []"
            :key="one.id"
            class="coll"
            @click="toggle(one.id)"
          >
            <TransferCheck :on="!!picked[one.id]" />
            <span class="cname">{{ one.name }}</span>
            <span class="ccount">{{ t('counts.requests', one.requests) }}</span>
          </div>
        </div>
      </TransferField>

      <TransferField v-model="target" :label="t('transfer.where')" :options="targets" />
    </template>

    <template v-else>
      <Segment
        v-if="vias.length > 1"
        size="sm"
        :options="viaOptions"
        :value="via"
        @pick="pickVia($event as Via)"
      />

      <template v-if="via === 'file'">
        <div
          v-if="!preview"
          id="collection-import-drop"
          class="drop"
          data-file-drop-target
          @click="pickFile"
        >
          <span>
            {{ t('transfer.drop') }} <span class="drop-pick">{{ t('transfer.choose') }}</span>
          </span>
          <span class="drop-hint">{{ accept }}</span>
        </div>
        <div v-else class="file">
          <Icon name="FileText" :size="16" :stroke-width="1.7" class="file-icon" />
          <div class="file-text">
            <span class="file-name">{{ preview.file }}</span>
            <span class="file-meta">{{ found }}</span>
          </div>
          <button type="button" class="replace" @click="preview = null">
            {{ t('transfer.replace') }}
          </button>
        </div>
      </template>

      <input
        v-else-if="via === 'url'"
        v-model="url"
        ref="field"
        class="url"
        spellcheck="false"
        :placeholder="urlPlaceholder"
        @input="preview = null"
        @keydown.enter="readUrl"
      />

      <textarea
        v-else
        v-model="curl"
        ref="field"
        class="paste"
        spellcheck="false"
        placeholder="curl https://api.example.com/v1/products"
        @input="pause.start()"
      ></textarea>

      <TransferField v-model="target" :label="t('transfer.where')" :options="targets" />
    </template>

    <template v-if="source" #foot>
      <DialogButton @click="transfer.closeTransfer()">{{ t('transfer.cancel') }}</DialogButton>
      <DialogButton primary :disabled="!ready" @click="commit">{{ primaryLabel }}</DialogButton>
    </template>
  </TransferDialog>
</template>

<style scoped>
@reference "../../style.css";

.back {
  @apply flex items-center justify-center flex-none border-0 cursor-pointer;
  width: 1.625rem;
  height: 1.625rem;
  margin-left: -0.5rem;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-secondary);
}

.back:hover {
  background: var(--bg-hover);
}

.coll {
  @apply flex items-center;
  height: 2.25rem;
  gap: 0.625rem;
  padding: 0 0.6rem;
  border-radius: 0.5rem;
  cursor: default;
}

.coll:hover {
  background: var(--bg-hover);
}

.cname {
  @apply flex-1 min-w-0 font-medium;
}

.ccount {
  @apply flex-none;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.drop {
  @apply flex flex-col items-center justify-center text-center;
  height: 8.75rem;
  gap: 0.25rem;
  border-radius: 0.625rem;
  border: 1.5px dashed var(--border);
  background: var(--bg-panel);
  cursor: default;
}

.drop.file-drop-target-active {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.drop-pick {
  color: var(--accent);
}

.drop-hint {
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.file {
  @apply flex items-center;
  gap: 0.6875rem;
  padding: 0.625rem 0.3rem 0.625rem 0.75rem;
  border-radius: 0.625rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.file-icon {
  @apply flex-none;
  color: var(--text-secondary);
}

.file-text {
  @apply flex flex-col flex-1 min-w-0;
  gap: 0.0625rem;
}

.file-name {
  @apply font-medium truncate;
}

.file-meta {
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.replace {
  @apply flex-none border-0 cursor-pointer;
  height: 1.625rem;
  padding: 0 0.5rem;
  border-radius: 0.3rem;
  background: transparent;
  color: var(--accent);
  font: inherit;
}

.replace:hover {
  background: var(--bg-hover);
}

.url,
.paste {
  @apply w-full border-0 outline-none;
  box-sizing: border-box;
  border-radius: 0.5rem;
  background: var(--bg-panel);
  box-shadow: inset 0 0 0 1px var(--border);
  padding: 0 0.6rem;
  color: var(--text);
  font-family: var(--mono);
  font-size: var(--text-12-5);
  user-select: text;
}

.url {
  height: 2rem;
}

.paste {
  @apply resize-none;
  height: 9.375rem;
  padding: 0.5625rem 0.625rem;
  line-height: 1.55;
}

.url:focus,
.paste:focus {
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}
</style>
