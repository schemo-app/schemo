import { jsonLanguage } from '@codemirror/lang-json'
import { highlightTree, tagHighlighter, tags } from '@lezer/highlight'

/** Result of attempting to parse a JSON string. */
export interface ParsedJson {
  ok: boolean
  value: unknown
  error?: string
}

/** Safely parses a string into JSON, returning a result object with error details on failure. */
export function tryParseJson(text: string): ParsedJson {
  if (text == null || text.trim() === '') {
    return { ok: false, value: null, error: 'empty body' }
  }
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch (e) {
    return { ok: false, value: null, error: e instanceof Error ? e.message : String(e) }
  }
}

/** Whether text typed as JSON fails to parse. */
export function isBrokenJson(text: string): boolean {
  return !!text.trim() && !text.includes('{{') && !tryParseJson(text).ok
}

/** Formats a JS value as an indented JSON string, falling back to string conversion on failure.
 *  The unit is the editor's own answer where an editor asked; the views get two spaces. */
export function prettyJson(value: unknown, unit: string | number = 2): string {
  try {
    return JSON.stringify(value, null, unit) ?? ''
  } catch {
    return String(value)
  }
}

// Parse with source access and raw output are in the engine but not yet in TypeScript's lib.
type SourceReviver = (key: string, value: unknown, context: { source?: string }) => unknown
const sourced = JSON as unknown as {
  parse(text: string, reviver: SourceReviver): unknown
  rawJSON(text: string): unknown
}

/** Indents a JSON text like `prettyJson`, but an integer past 2^53 keeps the digits it was sent
 *  with: a round trip through a number would round it. Null when the text is not JSON. */
export function prettyJsonText(text: string, unit: string | number = 2): string | null {
  try {
    const value = sourced.parse(text, (_key, value, { source }) =>
      typeof value === 'number' && !Number.isSafeInteger(value) && source && /^-?\d+$/.test(source)
        ? sourced.rawJSON(source)
        : value
    )
    return JSON.stringify(value, null, unit) ?? ''
  } catch {
    return null
  }
}

/** A stretch of a text and the class of the token it is, empty for a stretch that is none. */
export interface JsonPart {
  text: string
  tone: string
}

const tones = tagHighlighter([
  { tag: [tags.string, tags.propertyName], class: 'tok-str' },
  { tag: tags.number, class: 'tok-num' },
  { tag: tags.bool, class: 'tok-bool' },
  { tag: tags.null, class: 'tok-null' },
])

/** Cuts a text into the stretches the JSON grammar colours and the ones between them. */
export function jsonParts(text: string): JsonPart[] {
  const parts: JsonPart[] = []
  let at = 0
  highlightTree(jsonLanguage.parser.parse(text), tones, (from, to, tone) => {
    if (from > at) parts.push({ text: text.slice(at, from), tone: '' })
    parts.push({ text: text.slice(from, to), tone })
    at = to
  })
  if (at < text.length) parts.push({ text: text.slice(at), tone: '' })
  return parts
}
