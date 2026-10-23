import DOMPurify from 'dompurify'
import { marked } from 'marked'

// A description is markdown, and the window draws it. `marked` writes the HTML and the allow-list
// below keeps a description to what a description is: marked has no switch that turns raw HTML off,
// so the list is the switch, and it holds exactly what GFM itself writes.
const ALLOWED_TAGS = [
  'p',
  'br',
  'hr',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'ul',
  'ol',
  'li',
  'blockquote',
  'pre',
  'code',
  'strong',
  'em',
  'del',
  'a',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
]
// `img` is left out on purpose: a picture in a description is a request to a stranger. `class` is
// what marked puts `language-x` on a fence with; `align` is its table alignment.
const ALLOWED_ATTR = ['href', 'title', 'start', 'align', 'class']

/** renderMarkdown turns a description into the HTML the window draws for it. */
export function renderMarkdown(text: string): string {
  const html = marked.parse(text, { gfm: true, breaks: false, async: false })
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOW_DATA_ATTR: false,
  })
}

/** The headings of a description down to a level, with the marks of their text taken off. */
export function markdownHeadings(text: string, deepest = 3): { level: number; label: string }[] {
  return marked.lexer(text).flatMap((token) =>
    token.type === 'heading' && token.depth <= deepest
      ? [{ level: token.depth, label: token.text.replace(/[*`]/g, '').replace(/\[([^\]]+)\]\([^)]*\)/g, '$1') }]
      : []
  )
}
