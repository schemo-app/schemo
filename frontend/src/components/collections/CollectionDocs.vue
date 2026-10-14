<script setup lang="ts">
import { computed, reactive, useTemplateRef, watch } from 'vue'
import { findCollection } from '../../lib/collectionTree'
import { useCollectionsStore } from '../../stores/collections'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useMessages, type MessageKey } from '../../i18n'
import DocEditor from '../ui/DocEditor.vue'
import DescriptionEditor from './DescriptionEditor.vue'
import type { Docs } from '../../../bindings/schemo/internal/domain'

// What a collection says about itself: the line the overview previews, and the three notes the
// published page gives headings of their own. The four are one document — whoever publishes reads
// them together — so a note that is left writes the whole set, and the ones nobody typed in carry
// what the tree already holds.
const { id, startEditing = false } = defineProps<{
  id: string
  startEditing?: boolean
}>()

const { t } = useMessages()
const collections = useCollectionsStore()

type NoteKey = 'authorization' | 'errors' | 'limits'

const notes: { key: NoteKey; label: MessageKey; empty: MessageKey }[] = [
  { key: 'authorization', label: 'collections.docsAuthorization', empty: 'collections.docsAuthorizationEmpty' },
  { key: 'errors', label: 'collections.docsErrors', empty: 'collections.docsErrorsEmpty' },
  { key: 'limits', label: 'collections.docsLimits', empty: 'collections.docsLimitsEmpty' },
]

const stored = computed<Docs>(() => findCollection(collections.tree, id)?.docs ?? {})
const drafts = reactive<Record<NoteKey, string>>({ authorization: '', errors: '', limits: '' })
const fields = useTemplateRef<{ setText(text: string): void }[]>('fields')

function write() {
  return collections.saveDocs(id, { ...drafts })
}

// An edit of any note is a write of the set; each note holds its own pause, so a keystroke in one
// of them is not a reason to rewrite the other two.
const writes: Record<NoteKey, ReturnType<typeof useDeferredWrite>> = {
  authorization: useDeferredWrite(write),
  errors: useDeferredWrite(write),
  limits: useDeferredWrite(write),
}

// What the tree answers while nobody is typing: a note mid-keystroke holds its own text, so an
// answer older than the typing is left where it is.
watch(
  stored,
  (docs) => {
    for (const note of notes) {
      if (writes[note.key].dirty.value) continue
      drafts[note.key] = docs[note.key] ?? ''
    }
  },
  { immediate: true }
)

// Escape is a note left as it was found: the pending write is dropped, the text goes back to what
// the tree holds, and the editor is told to draw it again.
function discard(index: number) {
  const note = notes[index]
  writes[note.key].cancel()
  drafts[note.key] = stored.value[note.key] ?? ''
  fields.value?.[index]?.setText(drafts[note.key])
}
</script>

<template>
  <div class="docs">
    <section class="block">
      <span class="label">{{ t('collections.docsLead') }}</span>
      <DescriptionEditor
        :key="id"
        :id="id"
        empty-key="request.docs.emptyCollection"
        :start-editing="startEditing"
      />
    </section>

    <section v-for="(note, index) in notes" :key="note.key" class="block">
      <span class="label">{{ t(note.label) }}</span>
      <DocEditor
        ref="fields"
        v-model="drafts[note.key]"
        :empty-text="t(note.empty)"
        :hint="t('request.docs.hintSaved')"
        :start-editing="startEditing && index === 0"
        @update:model-value="writes[note.key].touch()"
        @commit="writes[note.key].flush()"
        @cancel="discard(index)"
      />
    </section>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.docs {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 1rem 1.5rem;
  gap: 1.75rem;
}

.block {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.label {
  font-size: var(--text-11-5);
  color: var(--text-tertiary);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
</style>
