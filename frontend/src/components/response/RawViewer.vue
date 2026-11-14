<script setup lang="ts">
import { useTemplateRef, watch } from 'vue'
import { EditorState, RangeSetBuilder, StateEffect, StateField, type Text } from '@codemirror/state'
import { Decoration, EditorView, keymap, type DecorationSet } from '@codemirror/view'
import { codeFolding, foldGutter, foldKeymap } from '@codemirror/language'
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import { editorLook } from '../../lib/editorLook'
import { useEditorView, type ReaderLook } from '../../composables/useEditorView'
import { t } from '../../i18n'

// CodeMirror renders the text, so search highlights are decorations. The grammar comes from the
// caller (lib/viewerLanguage); unknown text stays plain.
const props = defineProps<{
  text: string
  query: string
  language?: ViewerLanguage
}>()

const emit = defineEmits<{
  (e: 'stats', s: { count: number; index: number }): void
}>()

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

function findMatches(doc: Text, query: string): Match[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return []
  const hay = doc.toString().toLowerCase()
  const out: Match[] = []
  let i = hay.indexOf(needle)
  // A 2 MB body can hold tens of thousands of matches, and decorating them all costs more than it
  // tells anyone.
  while (i !== -1 && out.length < 2000) {
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

function buildState(doc: string, look: ReaderLook): EditorState {
  return EditorState.create({
    doc,
    extensions: [
      ...editorLook(props.language, look),
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

const host = useTemplateRef<HTMLElement>('host')
const { view, rebuild } = useEditorView({
  host,
  text: () => props.text,
  build: buildState,
  ready: () => applyQuery(props.query),
})

// Rebuilding the state (not patching the doc) also drops folding from the previous
// response, which would otherwise hide lines that no longer exist.
watch(() => [props.text, props.language] as const, ([text]) => rebuild(text))

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
