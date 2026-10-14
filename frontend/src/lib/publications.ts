import { HiddenReason } from '../../bindings/schemo/internal/domain'
import type { PageItem } from '../../bindings/schemo/internal/domain'
import type { Problem } from '../../bindings/schemo/internal/usecase/publication'
import { t, te, type MessageKey } from '../i18n'
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

const HIDDEN_WORDS: Partial<Record<HiddenReason, MessageKey>> = {
  [HiddenReason.HiddenSecret]: 'publications.hiddenSecret',
  [HiddenReason.HiddenLinked]: 'publications.hiddenLinked',
  [HiddenReason.HiddenLooksSecret]: 'publications.hiddenLooksSecret',
}

/** Why a variable is not on the page, as the frame under the publish button says it. */
export function hiddenText(reason: HiddenReason): string {
  const key = HIDDEN_WORDS[reason]
  return key ? t(key) : ''
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

/**
 * The three ways a request moves at a page, with the sign, the tone and the words each one draws.
 * Which of the three happened is Go's to say — it is the same word the page's own record carries —
 * and these are the words this window puts beside it. A rename is one of the three: a device that
 * compares fingerprints can say a request is not what it was, not which of its parts moved.
 */
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
      what: held ? t(held.word) : '',
    }
  })
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
