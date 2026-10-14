import type { Extension } from '@codemirror/state'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { EditorView, lineNumbers } from '@codemirror/view'
import { json } from '@codemirror/lang-json'
import { html } from '@codemirror/lang-html'
import { xml } from '@codemirror/lang-xml'
import { markdown } from '@codemirror/lang-markdown'
import { tags } from '@lezer/highlight'
import type { ViewerLanguage } from './viewerLanguage'

// How every editor of ours looks, said once: the answer is read and the example is written in the
// same palette and the same mono face, so the two are recognisably the same text. The colours are
// the app's own tokens — a grammar is a reading of the text, not a second theme.
const editorHighlight = HighlightStyle.define([
  { tag: [tags.propertyName, tags.tagName], color: 'var(--accent)' },
  { tag: [tags.attributeName, tags.keyword, tags.className], color: 'var(--purple)' },
  { tag: [tags.string, tags.attributeValue], color: 'var(--tok-str)' },
  { tag: [tags.number, tags.bool, tags.null, tags.unit, tags.color, tags.atom], color: 'var(--tok-num)' },
  { tag: [tags.punctuation, tags.separator], color: 'var(--text-tertiary)' },
  { tag: [tags.documentMeta, tags.processingInstruction], color: 'var(--text-tertiary)' },
  { tag: tags.comment, color: 'var(--text-tertiary)', fontStyle: 'italic' },
])

// The face and the size are the reader's own settings, read off the same tokens the rest of the
// window uses; a caller lays its own padding and its own marks over this.
const editorTheme = EditorView.theme({
  '&': { backgroundColor: 'transparent', height: '100%', fontSize: 'var(--mono-size)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '1.65' },
  // The app turns selection off globally and that inherits in here — a body is text and has to stay
  // selectable and copyable.
  '.cm-content': { userSelect: 'text', WebkitUserSelect: 'text' },
  '.cm-gutters': {
    backgroundColor: 'transparent',
    border: 'none',
    color: 'var(--text-tertiary)',
    paddingLeft: '0.375rem',
  },
  '.cm-foldGutter span': { color: 'var(--text-tertiary)', cursor: 'pointer' },
  '.cm-foldGutter span:hover': { color: 'var(--text)' },
  '.cm-foldPlaceholder': {
    backgroundColor: 'var(--bg-hover)',
    border: '1px solid var(--border)',
    borderRadius: '0.25rem',
    color: 'var(--text-secondary)',
    padding: '0 0.375rem',
    margin: '0 0.25rem',
    cursor: 'pointer',
  },
  '.cm-selectionBackground, ::selection': { backgroundColor: 'var(--accent-soft)' },
})

const GRAMMARS = { json, html, xml, markdown } as const

/**
 * What an editor wears: the reader's line numbers and wrapping, the grammar of the text it holds and
 * the palette. The caller lays its own marks and padding after it.
 */
export function editorLook(
  language: ViewerLanguage | undefined,
  { numbers, wrap }: { numbers: boolean; wrap: boolean }
): Extension[] {
  return [
    ...(numbers ? [lineNumbers()] : []),
    ...(language ? [GRAMMARS[language]()] : []),
    syntaxHighlighting(editorHighlight),
    ...(wrap ? [EditorView.lineWrapping] : []),
    editorTheme,
  ]
}
