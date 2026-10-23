import type { MessageKey } from '../i18n'
import type { RequestSource } from './requestSource'
import { parseTokens } from './vars'

type Say = (key: MessageKey, said?: Record<string, unknown>) => string

interface PlacedText {
  text: string
  key: MessageKey
  field?: string
}

function placedTexts(source: RequestSource): PlacedText[] {
  const cut = source.url.indexOf('?')
  const out: PlacedText[] = [
    { text: cut < 0 ? source.url : source.url.slice(0, cut), key: 'request.missingWhere.urlPath' },
    { text: cut < 0 ? '' : source.url.slice(cut + 1), key: 'request.missingWhere.urlQuery' },
    { text: source.body, key: 'request.missingWhere.body' },
  ]

  for (const row of source.form) {
    out.push(
      { text: row.name, key: 'request.missingWhere.body' },
      { text: row.value, key: 'request.missingWhere.body' },
      { text: row.src ?? '', key: 'request.missingWhere.body' }
    )
  }

  for (const row of source.headers) {
    if (!row.enabled || !row.name.trim()) continue
    out.push(
      { text: row.name, key: 'request.missingWhere.headers', field: row.name },
      { text: row.value, key: 'request.missingWhere.headers', field: row.name }
    )
  }

  for (const value of Object.values(source.auth.fields ?? {})) {
    if (value) out.push({ text: value, key: 'request.missingWhere.auth' })
  }

  return out
}

export function missingPlace(source: RequestSource, name: string, t: Say): string | null {
  for (const one of placedTexts(source)) {
    if (parseTokens(one.text).some((token) => token.name === name)) {
      return t(one.key, { field: one.field ?? '' })
    }
  }
  return null
}
