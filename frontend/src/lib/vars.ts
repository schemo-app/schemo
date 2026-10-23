import type { VariableKind } from '../../bindings/schemo/internal/domain'

export type VarKind = VariableKind

/** Resolution metadata for environment variables used in UI tooltips. */
export interface VarResolution {
  value: string
  source: 'env' | 'global'
  kind: VarKind
}

/** Token positional metadata and raw substring. */
export interface Token {
  start: number
  end: number
  name: string
  raw: string
}

/** Text slice metadata for UI token highlighting and caret offset tracking. */
export interface TokenSegment {
  text: string
  tokenName?: string
  start: number
}

/** Segment metadata for URL syntax highlighting and token overlay. */
export interface UrlPiece {
  text: string
  tokenName?: string
  kind?: 'query-key' | 'query-value' | 'path-param'
  start: number
}

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/

/** Validates whether a given string is a valid variable name. */
export function isVariableName(name: string): boolean {
  return NAME_RE.test(name)
}

/** Parses template tokens (`{{variable}}`) while skipping escaped sequences (`\{{`). */
export function parseTokens(text: string): Token[] {
  const out: Token[] = []
  let i = 0

  while (i < text.length) {
    if (text[i] === '\\' && text.startsWith('\\{{', i)) {
      i += 3
      continue
    }
    if (!text.startsWith('{{', i)) {
      i += 1
      continue
    }
    const close = text.indexOf('}}', i + 2)
    if (close === -1) {
      break
    }
    const name = text.slice(i + 2, close).trim()
    if (!NAME_RE.test(name)) {
      i += 2
      continue
    }
    out.push({ start: i, end: close + 2, name, raw: text.slice(i, close + 2) })
    i = close + 2
  }

  return out
}

const PATH_PARAM_RE = /(?<=\/):[A-Za-z_][A-Za-z0-9_-]*/g

/** Determines URL character syntax roles (path parameter, query key and value) for highlighting. */
function charKinds(text: string): (UrlPiece['kind'])[] {
  const kinds: (UrlPiece['kind'])[] = new Array(text.length).fill(undefined)
  const query = text.indexOf('?')
  const fragment = text.indexOf('#')
  const pathEnd = Math.min(...[query, fragment].filter((at) => at >= 0), text.length)

  for (const found of text.slice(0, pathEnd).matchAll(PATH_PARAM_RE)) {
    kinds.fill('path-param', found.index, found.index + found[0].length)
  }
  if (query < 0) return kinds

  let inValue = false
  for (let i = query; i < text.length; i++) {
    const char = text[i]
    if (char === '&') inValue = false
    kinds[i] = inValue ? 'query-value' : 'query-key'
    if (char === '=' && !inValue) inValue = true
  }

  return kinds
}

/** Decomposes a URL string into highlighted segments and variable tokens. */
export function urlPieces(text: string): UrlPiece[] {
  const kinds = charKinds(text)
  const out: UrlPiece[] = []

  function addPlain(from: number, to: number): void {
    let start = from
    for (let i = from; i <= to; i++) {
      if (i === to || kinds[i] !== kinds[start]) {
        if (i > start) out.push({ text: text.slice(start, i), kind: kinds[start], start })
        start = i
      }
    }
  }

  let last = 0
  for (const token of parseTokens(text)) {
    addPlain(last, token.start)
    out.push({ text: token.raw, tokenName: token.name, start: token.start })
    last = token.end
  }
  addPlain(last, text.length)

  return out
}

/** Splits text into contiguous segments of plain text and variable tokens with character offsets. */
export function tokenSegments(text: string): TokenSegment[] {
  const out: TokenSegment[] = []
  let last = 0

  for (const token of parseTokens(text)) {
    if (token.start > last) out.push({ text: text.slice(last, token.start), start: last })
    out.push({ text: token.raw, tokenName: token.name, start: token.start })
    last = token.end
  }
  if (last < text.length) out.push({ text: text.slice(last), start: last })

  return out
}