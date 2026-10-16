<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { findCollection, findNode } from '../../lib/collectionTree'
import { useCollectionsStore } from '../../stores/collections'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useMessages, type MessageKey } from '../../i18n'
import { missingDescriptionSections } from '../../lib/descriptionSections'
import DocEditor from '../ui/DocEditor.vue'

// What a collection or a request says about itself. The description is the tree row's own: the
// store keeps it, and the chip's dot, the overview's preview and the reader all read it from there.
// The caller keys it by the row, so a pending write is flushed to the row it was typed for.
const { id, emptyKey, startEditing = false, suggestSections = false } = defineProps<{
  id: string
  emptyKey: MessageKey
  startEditing?: boolean
  suggestSections?: boolean
}>()

const { t } = useMessages()
const collections = useCollectionsStore()

const stored = computed(
  () => (findCollection(collections.tree, id) ?? findNode(collections.tree, id))?.description ?? ''
)

const draft = ref('')
const suggestions = computed(() => suggestSections
  ? missingDescriptionSections(draft.value).map(section => ({ title: t(section.title), text: t(section.text) }))
  : [],
)
const field = useTemplateRef<{ setText(text: string): void }>('field')
const { dirty, touch, flush: commit, cancel } = useDeferredWrite(() =>
  collections.describe(id, draft.value.trim())
)

// What the tree answers while nobody is typing: the editor mid-keystroke holds its own text, so an
// answer older than the typing is left where it is.
watch(
  stored,
  (text) => {
    if (dirty.value) return
    draft.value = text
  },
  { immediate: true }
)

// Escape puts the text back to what the tree holds, and nothing travels — the editor owns its own
// body, so it is told what to draw again.
function discard() {
  cancel()
  draft.value = stored.value
  field.value?.setText(draft.value)
}
</script>

<template>
  <DocEditor
    ref="field"
    v-model="draft"
    :empty-text="t(emptyKey)"
    :hint="t('request.docs.hintSaved')"
    :start-editing="startEditing"
    :suggestions="suggestions"
    @update:model-value="touch()"
    @commit="commit"
    @cancel="discard"
  />
</template>
