<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from 'vue'
import { EditorState, RangeSetBuilder, StateEffect, StateField, type Text } from '@codemirror/state'
import { Decoration, EditorView, keymap, type DecorationSet } from '@codemirror/view'
import { codeFolding, foldGutter, foldKeymap } from '@codemirror/language'
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import { editorLook } from '../../lib/editorLook'
import { useSetting } from '../../composables/useSettings'
import { t } from '../../i18n'

// CodeMirror renders the text itself, so the toolbar's search has to be decorations here rather
// than markup on HTML.
//
// A grammar is laid over the text only when the text is written in it: an HTML error page read as
// JSON comes out painted at random, every quoted run a string and every digit of `1rem` a number.
// The caller says which it is (lib/viewerLanguage); anything else is shown as plain text.
const props = defineProps<{
  text: string
  query: string
  language?: ViewerLanguage
}>()

const emit = defineEmits<{
  (e: 'stats', s: { count: number; index: number }): void
}>()

const host = useTemplateRef<HTMLElement>('host')
const view = shallowRef<EditorView | null>(null)

// Reader settings from Go; a missing value is the default.
const wrap = useSetting('wrapLines')
const numbers = useSetting('lineNumbers')

const readerTheme = EditorView.theme({
  '.cm-content': { padding: '0.625rem 0' },
  '.cm-line': { padding: '0 1rem' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--accent) 25%, transparent)' },
})

const folding = codeFolding({
  preparePlaceholder: (state, range) =>
    t('response.raw.foldedLines', state.doc.lineAt(range.to).number - state.doc.lineAt(range.from).number),
  placeholderDOM: (_view, onclick, prepared: string) => {
    const el = document.createElement('span')
    el.textContent = `⋯ ${prepared}`
    el.title = t('response.raw.expand')
    el.addEventListener('click', onclick)
    return el
  },
})

// Search is hand-written rather than @codemirror/search: that extension highlights
// only while its own panel is open, and the search UI here lives in the app toolbar.

interface Match {
  from: number
  to: number
}

// A 2 MB body can hold tens of thousands of matches, and decorating them all
// costs more than it tells anyone.
const MAX_MARKS = 2000

function findMatches(doc: Text, query: string): Match[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return []
  const hay = doc.toString().toLowerCase()
  const out: Match[] = []
  let i = hay.indexOf(needle)
  while (i !== -1 && out.length < MAX_MARKS) {
    out.push({ from: i, to: i + needle.length })
    i = hay.indexOf(needle, i + needle.length)
  }
  return out
}

const setQuery = StateEffect.define<string>()

const queryField = StateField.define<string>({
  create: () => '',
  update(value, tr) {
    for (const effect of tr.effects) if (effect.is(setQuery)) return effect.value
    return value
  },
})

const matchMark = Decoration.mark({ class: 'cm-searchMatch' })

const matchField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    // Recompute only on query/doc change — scrolling must not rebuild a set per match.
    const touched = tr.docChanged || tr.effects.some((effect) => effect.is(setQuery))
    if (!touched) return deco.map(tr.changes)
    const builder = new RangeSetBuilder<Decoration>()
    for (const match of findMatches(tr.state.doc, tr.state.field(queryField))) {
      builder.add(match.from, match.to, matchMark)
    }
    return builder.finish()
  },
  provide: (field) => EditorView.decorations.from(field),
})

function foundMatches(): Match[] {
  const editorView = view.value
  if (!editorView) return []
  return findMatches(editorView.state.doc, editorView.state.field(queryField))
}

function matchStats(): { count: number; index: number } {
  const editorView = view.value
  const all = foundMatches()
  if (!editorView || all.length === 0) return { count: 0, index: 0 }
  const pos = editorView.state.selection.main.from
  const at = all.findIndex((match) => match.from <= pos && pos <= match.to)
  return { count: all.length, index: at === -1 ? 0 : at }
}

function emitMatchStats() {
  emit('stats', matchStats())
}

// Selecting a match is what makes it visible — the caret sits on it.
function moveToMatch(direction: 'next' | 'prev') {
  const editorView = view.value
  if (!editorView) return
  const all = foundMatches()
  if (all.length === 0) {
    emitMatchStats()
    return
  }
  const pos = editorView.state.selection.main.from
  const after = all.findIndex((match) => match.from > pos)
  const target =
    direction === 'next'
      ? all[after === -1 ? 0 : after]
      : all[(after === -1 ? all.length : after) - 1] ?? all[all.length - 1]

  editorView.dispatch({
    selection: { anchor: target.from, head: target.to },
    scrollIntoView: true,
  })
  emitMatchStats()
}

function applyQuery(query: string) {
  const editorView = view.value
  if (!editorView) return
  editorView.dispatch({ effects: setQuery.of(query) })
  emitMatchStats()
}

function buildState(doc: string): EditorState {
  return EditorState.create({
    doc,
    extensions: [
      // Line numbers and wrapping are the reader's choice.
      ...editorLook(props.language, { numbers: numbers.value, wrap: wrap.value }),
      foldGutter({ openText: '' }),
      folding,
      keymap.of(foldKeymap),
      EditorState.phrases.of({
        'Fold line': t('response.raw.foldLine'),
        'Unfold line': t('response.raw.unfoldLine'),
      }),
      EditorState.readOnly.of(true),
      EditorView.editable.of(false),
      queryField,
      matchField,
      readerTheme,
      EditorView.updateListener.of((update) => {
        if (update.selectionSet || update.docChanged) emitMatchStats()
      }),
    ],
  })
}

onMounted(() => {
  if (!host.value) return
  view.value = new EditorView({ state: buildState(props.text), parent: host.value })
  applyQuery(props.query)
})

onBeforeUnmount(() => {
  view.value?.destroy()
  view.value = null
})

// Rebuilding the state (not patching the doc) also drops folding from the previous
// response, which would otherwise hide lines that no longer exist.
watch(
  () => [props.text, props.language] as const,
  ([text]) => {
    const editorView = view.value
    if (!editorView) return
    editorView.setState(buildState(text))
    applyQuery(props.query)
  }
)

// Wrapping and line numbers change from settings while on screen; the editor is rebuilt, folds lost.
watch([wrap, numbers], () => {
  const editorView = view.value
  if (!editorView) return
  editorView.setState(buildState(props.text))
  applyQuery(props.query)
})

watch(() => props.query, applyQuery)

defineExpose({
  next: () => moveToMatch('next'),
  prev: () => moveToMatch('prev'),
})
</script>

<template>
  <div ref="host" class="raw-viewer"></div>
</template>

<style scoped>
@reference "../../style.css";

.raw-viewer {
  @apply flex-1 min-h-0 overflow-hidden bg-bg;
}
</style>
