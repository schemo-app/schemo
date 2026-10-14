// Which grammar the text viewer paints a body with. It is a question of drawing, not of reading: the
// body is shown whatever the answer, and a wrong answer only colours it wrongly. So it is decided
// here, from what is on hand — whether Go read the body as JSON, the media type the server named,
// and, when the server named none, how the text begins.

/** The grammars the viewer knows; `null` is plain text. Markdown is only ever asked for, never guessed. */
export type ViewerLanguage = 'json' | 'html' | 'xml' | 'markdown' | null

/** Picks the grammar for a response body. */
export function viewerLanguage(contentType: string | undefined, text: string, isJson: boolean): ViewerLanguage {
  if (isJson) return 'json'

  // The media type without its parameters: `text/html; charset=utf-8` is `text/html`.
  const type = (contentType ?? '').split(';')[0].trim().toLowerCase()
  if (type === 'text/html' || type === 'application/xhtml+xml') return 'html'
  if (type === 'application/xml' || type === 'text/xml' || type.endsWith('+xml')) return 'xml'
  if (type) return null

  // No type named: the first thing in the text says what it is, and only a document that announces
  // itself is taken at its word — a body that merely starts with `<` could be anything.
  const head = text.trimStart().slice(0, 64).toLowerCase()
  if (head.startsWith('<!doctype html') || head.startsWith('<html')) return 'html'
  if (head.startsWith('<?xml')) return 'xml'
  return null
}
