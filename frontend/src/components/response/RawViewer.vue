<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from 'vue'
import { EditorState, RangeSetBuilder, StateEffect, StateField, type Text } from '@codemirror/state'
import { Decoration, EditorView, type DecorationSet } from '@codemirror/view'
import { codeFolding, foldGutter, foldKeymap } from '@codemirror/language'
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import { editorLook } from '../../lib/editorLook'
import { useSetting } from '../../composables/useSettings'
import { t } from '../../i18n'
import { keymap } from '@codemirror/view'

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
  /** The node the inspector is on: its key from `head`, its value from `start` to `end`. */
  mark?: { head: number; start: number; end: number } | null
  /** Whether a click is a question for the inspector: with it off a click is only a click. */
  picking?: boolean
}>()

const emit = defineEmits<{
  (e: 'stats', s: { count: number; index: number }): void
  /** A click that selected nothing: where in the text it landed. */
  (e: 'pick', offset: number): void
}>()

// The catalogue's `t`; lezer's tags are imported as `tags`.

const host = useTemplateRef<HTMLElement>('host')
const view = shallowRef<EditorView | null>(null)

// Reader settings from Go; a missing value is the default.
const wrap = useSetting('wrapLines')
const numbers = useSetting('lineNumbers')

// What only a reading has: the page's own padding, the search hits and the marked node. The face,
// the palette and the reader's settings are lib/editorLook's, and the editor of an example wears
// the same ones.
const readerTheme = EditorView.theme({
  '.cm-content': { padding: '0.625rem 0' },
  '.cm-line': { padding: '0 1rem' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--accent) 25%, transparent)' },
  // The selected node: a band over the lines it spans with a rule at its edge, its key a shade
  // lighter than its value, so that the eye finds the member and then the value in it.
  '.cm-nodeLine': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 6%, transparent)',
    boxShadow: 'inset 0.125rem 0 0 var(--accent)',
  },
  '.cm-nodeKey': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 12%, transparent)',
    borderRadius: '0.1875rem',
  },
  '.cm-nodeValue': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 22%, transparent)',
    borderRadius: '0.1875rem',
  },
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

interface NodeMark {
  head: number
  start: number
  end: number
}

const setMark = StateEffect.define<NodeMark | null>()

const lineMark = Decoration.line({ class: 'cm-nodeLine' })
const keyMark = Decoration.mark({ class: 'cm-nodeKey' })
const valueMark = Decoration.mark({ class: 'cm-nodeValue' })

function markDecorations(doc: Text, mark: NodeMark): DecorationSet {
  if (mark.start >= mark.end || mark.end > doc.length) return Decoration.none
  const ranges = []
  const last = doc.lineAt(mark.end).number
  for (let lineNumber = doc.lineAt(mark.head).number; lineNumber <= last; lineNumber++) {
    ranges.push(lineMark.range(doc.line(lineNumber).from))
  }
  if (mark.head < mark.start) ranges.push(keyMark.range(mark.head, mark.start))
  ranges.push(valueMark.range(mark.start, mark.end))
  return Decoration.set(ranges, true)
}

const markField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setMark)) return effect.value ? markDecorations(tr.state.doc, effect.value) : Decoration.none
    }
    return deco.map(tr.changes)
  },
  provide: (field) => EditorView.decorations.from(field),
})

// A mark set from outside is scrolled to; one made by a click here is not.
let pickedHere = false

function applyMark(mark: NodeMark | null | undefined) {
  const editorView = view.value
  if (!editorView) return
  const effects: StateEffect<unknown>[] = [setMark.of(mark ?? null)]
  const scroll = !pickedHere
  pickedHere = false
  if (scroll && mark && mark.end <= editorView.state.doc.length) {
    effects.push(EditorView.scrollIntoView(mark.head, { y: 'nearest', yMargin: 48 }))
  }
  editorView.dispatch({ effects })
}

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
      foldGutter(),
      folding,
      keymap.of(foldKeymap),
      EditorState.readOnly.of(true),
      EditorView.editable.of(false),
      queryField,
      matchField,
      markField,
      readerTheme,
      // A click with nothing selected is a place in the text; a drag is the reader copying.
      EditorView.domEventHandlers({
        click: (event, v) => {
          if (!props.picking || !v.state.selection.main.empty) return
          const at = v.posAtCoords({ x: event.clientX, y: event.clientY }, false)
          if (at === null) return
          pickedHere = true
          emit('pick', at)
        },
      }),
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
  applyMark(props.mark)
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
    applyMark(props.mark)
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
watch(() => props.mark, applyMark)

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
