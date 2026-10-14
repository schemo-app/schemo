/** A stretch of a text, and whether it is what the query asked for. */
export interface MatchPart {
  text: string
  match: boolean
}

/** Cuts a text into the stretches that are the query, in any case, and the ones between them. */
export function matchParts(text: string, query: string): MatchPart[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return [{ text, match: false }]

  const haystack = text.toLowerCase()
  const parts: MatchPart[] = []
  let at = 0
  for (let found = haystack.indexOf(needle); found >= 0; found = haystack.indexOf(needle, at)) {
    if (found > at) parts.push({ text: text.slice(at, found), match: false })
    at = found + needle.length
    parts.push({ text: text.slice(found, at), match: true })
  }
  if (at < text.length) parts.push({ text: text.slice(at), match: false })
  return parts
}
