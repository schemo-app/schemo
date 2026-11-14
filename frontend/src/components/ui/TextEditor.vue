<script setup lang="ts">
import { useTemplateRef, watch } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap, placeholder as placeholderExt } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import { editorLook } from '../../lib/editorLook'
import { useEditorView } from '../../composables/useEditorView'

// Same look as the response viewer (lib/editorLook); the grammar is the caller's, never guessed.
const { modelValue, language = null, placeholder = '', label, compact = false } = defineProps<{
  modelValue: string
  language?: ViewerLanguage
  placeholder?: string
  label?: string
  compact?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const host = useTemplateRef<HTMLElement>('host')

const { view } = useEditorView({
  host,
  text: () => modelValue,
  rebuildOn: [() => language, () => label, () => placeholder],
  build: (doc, look) => EditorState.create({
    doc,
    extensions: [
      ...editorLook(language, { ...look, numbers: !compact && look.numbers }),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      ...(placeholder ? [placeholderExt(placeholder)] : []),
      EditorView.contentAttributes.of({ spellcheck: 'false', ...(label ? { 'aria-label': label } : {}) }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) emit('update:modelValue', update.state.doc.toString())
      }),
    ],
  }),
})

// Text from outside — what a caller does when an edit was cancelled. What the editor itself typed
// comes back as the same string, so the guard is what keeps the caret where it stands.
watch(
  () => modelValue,
  (now) => {
    const v = view.value
    if (!v || now === v.state.doc.toString()) return
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: now } })
  }
)
</script>

<template>
  <div ref="host" class="text-editor"></div>
</template>

<style scoped>
@reference "../../style.css";

.text-editor {
  @apply flex-1 min-h-0 overflow-hidden;
}
</style>
