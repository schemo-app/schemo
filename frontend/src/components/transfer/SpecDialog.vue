<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useFocus } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import FileDrop from '../ui/FileDrop.vue'
import Segment from '../ui/Segment.vue'
import DialogButton from '../ui/DialogButton.vue'
import Modal from '../ui/Modal.vue'
import Spinner from '../ui/Spinner.vue'
import SpecItem from './SpecItem.vue'
import { useTransferStore } from '../../stores/transfer'
import { useCollectionsStore } from '../../stores/collections'
import { editorsOf } from '../../stores/content'
import { findCollection, requestIds } from '../../lib/collectionTree'
import { fileName } from '../../lib/format'
import { t } from '../../i18n'
import { Specs, type SpecReview } from '../../../bindings/schemo/internal/convert'
import type { Item } from '../../../bindings/schemo/internal/convert/merge'
import { SpecKind } from '../../../bindings/schemo/internal/domain'

const transfer = useTransferStore()
const collections = useCollectionsStore()

const open = computed(() => transfer.specCollection !== '')
const review = ref<SpecReview | null>(null)
const choosing = ref(false)
const via = ref<'file' | 'url'>('file')
const url = ref('')
const checked = ref(new Set<string>())
// The items whose conflicts go the file's way.
const takeFile = ref(new Set<string>())
const expanded = ref(new Set<string>())
const applying = ref(false)
// An answer to an earlier question must not land in a dialog that has moved on.
let latest = 0

const field = useTemplateRef<HTMLElement>('field')
useFocus(field, { initialValue: true })

watch(() => transfer.specCollection, (id) => void start(id), { immediate: true })

async function start(id: string) {
  const turn = ++latest
  review.value = null
  choosing.value = false
  via.value = 'file'
  url.value = ''
  if (!id) return
  const source = await transfer.specSource(id)
  if (turn !== latest) return
  if (!source) {
    choosing.value = true
    return
  }
  if (source.kind === SpecKind.SpecURL) {
    via.value = 'url'
    url.value = source.location
  }
  await show(() => Specs.ReviewLinked(id))
}

// A review that did not come (a failed read, a closed file dialog) leaves the choice of source.
async function show(read: () => Promise<SpecReview | null>) {
  const turn = ++latest
  choosing.value = false
  review.value = null
  const answer = await transfer.specReview(read)
  if (turn !== latest) return
  if (!answer) {
    choosing.value = true
    return
  }
  checked.value = new Set(rowsOf(answer).filter((item) => item.selected).map((item) => item.id))
  takeFile.value = new Set()
  expanded.value = new Set()
  review.value = answer
}

const disputed = (item: Item) => item.changes?.some((change) => change.conflict) ?? false

// One list: what has something to choose first, then other changes, then new and gone requests.
function rowsOf(answer: SpecReview): Item[] {
  const { added, changed, removed, settings, environments } = answer.review
  const updated = [...(settings ? [settings] : []), ...(environments ?? []), ...(changed ?? [])]
  return [
    ...updated.filter(disputed),
    ...updated.filter((item) => !disputed(item)),
    ...(added ?? []),
    ...(removed ?? []),
  ]
}

const rows = computed(() => (review.value ? rowsOf(review.value) : []))

const inside = computed(() => {
  const level = findCollection(collections.tree, transfer.specCollection)
  return requestIds(level ? [level] : [])
})
const unsaved = computed(() =>
  editorsOf(inside.value)
    .filter((editor) => editor.unsaved.value)
    .map((editor) => editor.node.value?.node.name ?? '')
)

function toggle(set: Set<string>, id: string, on = !set.has(id)) {
  const next = new Set(set)
  if (on) next.add(id)
  else next.delete(id)
  return next
}

function chooseSource() {
  latest++
  review.value = null
  choosing.value = true
}

function pickFile() {
  const id = transfer.specCollection
  if (transfer.busy) return
  void show(() => Specs.PickFile(id, t('spec.title')))
}

function dropFile([path]: string[]) {
  const id = transfer.specCollection
  if (transfer.busy || !path) return
  void show(() => Specs.ReviewFile(id, path))
}

function readUrl() {
  const id = transfer.specCollection
  const address = url.value.trim()
  if (transfer.busy || !address) return
  void show(() => Specs.ReviewURL(id, address))
}

const primary = computed(() => {
  if (choosing.value) return via.value === 'url' ? t('spec.load') : ''
  if (!review.value) return ''
  return rows.value.length ? t('spec.update') : t('spec.done')
})
const ready = computed(() => {
  if (transfer.busy || applying.value) return false
  if (choosing.value) return url.value.trim() !== ''
  if (!review.value) return false
  // With nothing to change, finishing only remembers the file.
  return !rows.value.length || (checked.value.size > 0 && !unsaved.value.length)
})

async function commit() {
  if (!ready.value) return
  if (choosing.value) {
    readUrl()
    return
  }
  const found = review.value
  if (!found) return
  applying.value = true
  try {
    const theirs = rows.value
      .filter((item) => takeFile.value.has(item.id))
      .flatMap((item) => (item.changes ?? []).filter((change) => change.conflict).map((change) => change.id))
    const choices = { selected: [...checked.value], theirs }
    if (await transfer.specApply(found.token, choices, editorsOf(inside.value))) {
      transfer.closeTransfer()
    } else {
      // The token is spent, so the next attempt reads the source again.
      chooseSource()
    }
  } finally {
    applying.value = false
  }
}
</script>

<template>
  <Modal :open :title="t('spec.title')" variant="wide" @close="transfer.closeTransfer()">
    <template v-if="choosing">
      <Segment
        size="sm"
        :options="[
          { value: 'file', label: t('transfer.file') },
          { value: 'url', label: t('transfer.byUrl') },
        ]"
        :value="via"
        @pick="via = $event as 'file' | 'url'"
      />
      <FileDrop
        v-if="via === 'file'"
        as="button"
        type="button"
        class="drop"
        :disabled="!open || transfer.busy"
        @files="dropFile"
        @click="pickFile"
      >
        <span>{{ t('transfer.drop') }} <span class="drop-pick">{{ t('transfer.choose') }}</span></span>
        <span class="drop-hint">{{ t('transfer.accept.openapi') }}</span>
      </FileDrop>
      <input
        v-else
        ref="field"
        v-model="url"
        class="url"
        spellcheck="false"
        placeholder="https://api.example.com/openapi.yaml"
        @keydown.enter="readUrl"
      />
    </template>

    <div v-else-if="!review" class="note">
      <Spinner aria-hidden="true" />
      {{ t('spec.loading') }}
    </div>

    <template v-else>
      <div class="file">
        <Icon
          :name="review.source.kind === SpecKind.SpecURL ? 'Link' : 'FileText'"
          :size="16"
          :stroke-width="1.7"
          class="file-icon"
        />
        <div class="file-text">
          <span class="file-name">{{ fileName(review.source.location) }}</span>
          <span class="file-meta" :title="review.source.location">{{ review.source.location }}</span>
        </div>
        <button type="button" class="replace" @click="chooseSource">{{ t('transfer.replace') }}</button>
      </div>

      <div v-if="rows.length" class="list">
        <div class="list-head">
          <span class="list-title">{{ t('spec.changes') }}</span>
          <span>{{ rows.length }}</span>
        </div>
        <div class="-mx-2.5">
          <SpecItem
            v-for="item in rows"
            :key="item.id"
            :item
            :checked="checked.has(item.id)"
            :open="expanded.has(item.id)"
            :take-file="takeFile.has(item.id)"
            @toggle="checked = toggle(checked, item.id)"
            @expand="expanded = toggle(expanded, item.id)"
            @resolve="takeFile = toggle(takeFile, item.id, $event)"
          />
        </div>
      </div>
    </template>

    <template #foot>
      <span v-if="choosing" class="foot-note">{{ t(via === 'url' ? 'spec.urlHint' : 'spec.fileHint') }}</span>
      <span v-else-if="review && !rows.length" class="foot-note">
        <Icon name="Check" :size="13" :stroke-width="2.6" class="note-ok" />
        {{ t('spec.nothing') }}
      </span>
      <span v-else-if="rows.length && unsaved.length" class="foot-note">
        <Icon name="TriangleAlert" :size="13" :stroke-width="2.2" class="note-warn" />
        {{ t('spec.dirty', { names: unsaved.join(', ') }) }}
      </span>
      <DialogButton @click="transfer.closeTransfer()">{{ t('transfer.cancel') }}</DialogButton>
      <DialogButton
        v-if="primary"
        primary
        class="inline-flex items-center gap-1.5"
        :disabled="!ready"
        :aria-busy="applying || undefined"
        @click="commit"
      >
        <Spinner v-if="applying" aria-hidden="true" />
        {{ primary }}
      </DialogButton>
    </template>
  </Modal>
</template>

<style scoped>
@reference "../../style.css";

.drop {
  @apply flex flex-col flex-none items-center justify-center text-center;
  height: 8.75rem;
  gap: 0.25rem;
  border-radius: 0.625rem;
  border: 1px dashed var(--border);
  background: var(--bg-panel);
  cursor: default;
}

.drop-pick {
  color: var(--accent);
}

.drop-hint {
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

/* A note stands in the buttons' row, as in the app's other forms. */
.foot-note {
  @apply flex flex-1 items-center self-center min-w-0;
  gap: 0.375rem;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.url {
  @apply w-full flex-none border-0 outline-none;
  box-sizing: border-box;
  height: 2rem;
  border-radius: 0.5rem;
  background: var(--bg-panel);
  box-shadow: inset 0 0 0 1px var(--border);
  padding: 0 0.625rem;
  color: var(--text);
  font-family: var(--mono);
  font-size: var(--text-13);
  user-select: text;
}

.url:focus {
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}

.file {
  @apply flex flex-none items-center;
  gap: 0.6875rem;
  padding: 0.625rem 0.3125rem 0.625rem 0.75rem;
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
  @apply truncate;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.replace {
  @apply flex-none border-0 cursor-pointer;
  height: 1.625rem;
  padding: 0 0.5rem;
  border-radius: 0.3125rem;
  background: transparent;
  color: var(--accent);
  font: inherit;
}

.replace:hover {
  background: var(--bg-hover);
}

.note {
  @apply flex flex-none items-center m-0;
  gap: 0.5rem;
  color: var(--text-secondary);
}

.note-ok {
  @apply flex-none text-green;
}

.note-warn {
  @apply flex-none text-orange;
}

.list {
  @apply flex flex-col flex-none;
  gap: 0.25rem;
}

.list-head {
  @apply flex items-baseline;
  gap: 0.375rem;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.list-title {
  @apply font-semibold;
}
</style>
