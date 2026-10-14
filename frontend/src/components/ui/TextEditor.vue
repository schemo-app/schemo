<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap, placeholder as placeholderExt } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import { editorLook } from '../../lib/editorLook'
import { useSetting } from '../../composables/useSettings'

// A written body, painted as it will be read: the same face, the same palette and the same reader
// settings the answer is drawn with (lib/editorLook), so an example typed here looks like the
// answer it stands for. The grammar follows the text the caller hands over — nothing is guessed.
const { modelValue, language = null, placeholder = '' } = defineProps<{
  modelValue: string
  language?: ViewerLanguage
  placeholder?: string
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const host = useTemplateRef<HTMLElement>('host')
const view = shallowRef<EditorView | null>(null)

// Reader settings from Go; a missing value is the default. The window's editor settings are these,
// and both the reading and the writing obey them.
const wrap = useSetting('wrapLines')
const numbers = useSetting('lineNumbers')

function buildState(doc: string): EditorState {
  return EditorState.create({
    doc,
    extensions: [
      ...editorLook(language, { numbers: numbers.value, wrap: wrap.value }),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      ...(placeholder ? [placeholderExt(placeholder)] : []),
      EditorView.contentAttributes.of({ spellcheck: 'false' }),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
      }),
    ],
  })
}

onMounted(() => {
  if (!host.value) return
  view.value = new EditorView({ state: buildState(modelValue), parent: host.value })
})

onBeforeUnmount(() => {
  view.value?.destroy()
  view.value = null
})

// The settings change while the field is on screen; the editor is rebuilt, the caret's place is
// the text's own and the history is lost — a settings change is not a typing.
watch([wrap, numbers, () => language], () => {
  const v = view.value
  if (!v) return
  v.setState(buildState(v.state.doc.toString()))
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
