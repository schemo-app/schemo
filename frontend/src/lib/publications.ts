import type { PageItem } from '../../bindings/schemo/internal/domain'
import type { Problem } from '../../bindings/schemo/internal/usecase/publication'
import { t, te, currentLocale, type MessageKey } from '../i18n'
import { PASSWORD } from './layout'
import type { Visibility } from '../stores/publications'

/**
 * The sentence a problem is shown in. A problem is not a refusal — the page goes out without the
 * part — so what it says is which request it was about, and this is where the name the page could
 * not carry is put back beside the code's own wording.
 */
export function problemText(problem: Problem): string {
  const key = `errors.codes.${problem.code}`
  if (!te(key)) return ''
  const args: Record<string, unknown> = { ...(problem.args ?? {}), name: problem.name }
  // A count names the part of the collection it counted, and the word for it is the catalogue's.
  const what = args.what
  if (typeof what === 'string') {
    const part = `errors.parts.${what}`
    args.what = te(part) ? t(part) : what
  }
  return t(key, args)
}

/** The three accesses a page may have, with the line that says what each one means. */
export function visibilityOptions(): { value: Visibility; label: string; hint: string }[] {
  return [
    {
      value: 'unlisted',
      label: t('publications.visUnlisted'),
      hint: t('publications.visUnlistedHint'),
    },
    {
      value: 'public',
      label: t('publications.visPublic'),
      hint: t('publications.visPublicHint'),
    },
    {
      value: 'password',
      label: t('publications.visPassword'),
      hint: t('publications.visPasswordHint'),
    },
  ]
}

/** Whether the text a server answered with is one of the accesses this window draws. */
export function isVisibility(held: string | undefined): held is Visibility {
  return held === 'unlisted' || held === 'public' || held === 'password'
}

/** Whether a password is one a page may stand behind: the same two numbers Go holds it to. */
export function passwordFits(password: string): boolean {
  return password.length >= PASSWORD.min && password.length <= PASSWORD.max
}

/** Localized labels and colors for the changes identified by Go. */
const CHANGE_SIGNS: Record<string, { glyph: string; tone: string; word: MessageKey; tally: MessageKey }> =
  {
    '+': {
      glyph: '+',
      tone: 'add',
      word: 'publications.changeNew',
      tally: 'publications.tallyNew',
    },
    '~': {
      glyph: '~',
      tone: 'edit',
      word: 'publications.changeEdited',
      tally: 'publications.tallyEdited',
    },
    '-': {
      glyph: '−',
      tone: 'gone',
      word: 'publications.changeGone',
      tally: 'publications.tallyGone',
    },
  }

/** One request a page is missing, in the sign, the tone and the words of the row that draws it. */
export interface ChangeNote {
  sign: string
  tone: string
  method: string
  name: string
  what: string
}

/** What a page is missing, row by row. A sign this window has no word for is drawn as it came. */
export function changeNotes(items: PageItem[]): ChangeNote[] {
  return items.map((item) => {
    const held = CHANGE_SIGNS[item.sign]
    return {
      sign: held?.glyph ?? item.sign,
      tone: held?.tone ?? '',
      method: item.method,
      name: item.name,
      what: changeText(item),
    }
  })
}

function changeText(item: PageItem): string {
  const fields = item.fields ?? []
  const restored = fields.find(field => field.startsWith('restored:'))
  if (restored) return t('publications.restoredFrom', { n: Number(restored.slice(9)) })
  const initial = fields.find(field => field.startsWith('initial:'))
  if (initial) return t('publications.changeInitial', { n: Number(initial.slice(8)) })
  if (item.sign !== '~') {
    if (!item.method) return t(item.sign === '+' ? 'publications.changeSectionNew' : 'publications.changeSectionGone')
    const sign = CHANGE_SIGNS[item.sign]
    return sign ? t(sign.word) : ''
  }
  const labels = fields.map(field => {
    const [kind, value, ...name] = field.split(':')
    if (kind === 'parameter') return t('publications.changeParameter', { location: value, name: name.join(':') })
    if (kind === 'response') return t('publications.changeResponse', { status: value, name: name.join(':') }).trim()
    const key = `publications.changeFields.${field}`
    return te(key) ? t(key) : t('publications.changeEdited')
  })
  return labels.length ? new Intl.ListFormat(currentLocale(), { style: 'long', type: 'conjunction' }).format(labels) : t('publications.changeEdited')
}

/** One count of the tally: a sign, its tone, and how many requests moved by it. */
export interface ChangeTally {
  sign: string
  tone: string
  label: string
}

/** The same changes counted by sign, which is what a list too long to draw whole says instead. */
export function changeTally(items: PageItem[]): ChangeTally[] {
  const counted = Object.entries(CHANGE_SIGNS).map(([sign, held]) => {
    const count = items.filter((item) => item.sign === sign).length
    return count ? { sign: held.glyph, tone: held.tone, label: t(held.tally, { n: count }) } : null
  })
  return counted.filter((held): held is ChangeTally => held !== null)
}
