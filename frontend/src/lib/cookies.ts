import { t, type MessageKey } from '../i18n'
import { CookieState } from '../../bindings/schemo/internal/domain'
import type { CookieWrite, JarCookie, JarDomain } from '../../bindings/schemo/internal/domain'

const STATE_TEXTS = {
  [CookieState.CookieAdded]: 'cookies.state.added',
  [CookieState.CookieReplaced]: 'cookies.state.replaced',
  [CookieState.CookieDeleted]: 'cookies.state.deleted',
  [CookieState.CookieNotSaved]: 'cookies.state.notSaved',
} satisfies Record<Exclude<`${CookieState}`, ''>, MessageKey>

/** Returns localized status label for a cookie state. */
export function stateLabel(state: CookieState): string {
  const texts: Record<string, MessageKey | undefined> = STATE_TEXTS
  const key = texts[state]
  return key ? t(key) : t('cookies.state.notSaved')
}

const STATE_CLASSES = {
  [CookieState.CookieAdded]: 'cookie-added',
  [CookieState.CookieReplaced]: 'cookie-replaced',
  [CookieState.CookieDeleted]: 'cookie-deleted',
  [CookieState.CookieNotSaved]: 'cookie-kept-out',
} satisfies Record<Exclude<`${CookieState}`, ''>, string>

/** Returns CSS class associated with a cookie state. */
export function stateClass(state: CookieState): string {
  const classes: Record<string, string | undefined> = STATE_CLASSES
  return classes[state] ?? 'cookie-kept-out'
}

/** Formats domain display text (prefixes with '.' for wildcard domains). */
export function domainLabel(domain: string | undefined, hostOnly: boolean | undefined): string {
  if (!domain) return ''
  return hostOnly ? domain : `.${domain}`
}

/** Formats a jar domain as the manager draws it: dotted when one of its cookies reaches below. */
export function reachLabel(level: Pick<JarDomain, 'domain' | 'cookies'> | null): string {
  const dotted = (level?.cookies ?? []).some((cookie) => !cookie.hostOnly)
  return domainLabel(level?.domain, !dotted)
}

/** One flag as a table draws it: the short form, and what it says in full for the tooltip. */
export interface FlagBadge {
  text: string
  hint: string
}

/** Lists the flags a cookie carries in the short forms a cookie table writes them in. */
export function flagBadges(cookie: Pick<JarCookie, 'secure' | 'httpOnly' | 'sameSite'>): FlagBadge[] {
  const sameSite = cookie.sameSite || 'Lax'
  return [
    ...(cookie.secure ? [{ text: 'Sec', hint: t('cookies.flagSecure') }] : []),
    ...(cookie.httpOnly ? [{ text: 'HTTP', hint: t('cookies.flagHttpOnly') }] : []),
    { text: sameSite, hint: t('cookies.flagSameSite', { value: sameSite }) },
  ]
}

/** Formats cookie security flags (Secure, HttpOnly, SameSite) into a single line. */
export function flagLabel(cookie: Pick<JarCookie, 'secure' | 'httpOnly' | 'sameSite'>): string {
  return [
    cookie.secure ? 'Secure' : '',
    cookie.httpOnly ? 'HttpOnly' : '',
    cookie.sameSite || 'Lax',
  ]
      .filter(Boolean)
      .join(' · ')
}

const DAY = /^(\d{4})-(\d{1,2})-(\d{1,2})$/

/** Formats timestamp as zero-padded YYYY-MM-DD parts. */
function parts(at: number): string[] {
  const when = new Date(at)
  return [when.getFullYear(), when.getMonth() + 1, when.getDate()].map((n, i) =>
      String(n).padStart(i === 0 ? 4 : 2, '0'),
  )
}

/** Returns formatted expiry date (YYYY-MM-DD) or 'session' string. */
export function expiryLabel(expiresAt: number | null | undefined): string {
  if (!expiresAt) return t('cookies.session')
  return parts(expiresAt).join('-')
}

/** Formats expiry timestamp for date-time input fields (YYYY-MM-DD HH:mm). */
export function expiryInput(expiresAt: number | null | undefined): string {
  if (!expiresAt) return ''
  const when = new Date(expiresAt)
  const clock = [when.getHours(), when.getMinutes()].map((n) => String(n).padStart(2, '0'))
  return `${parts(expiresAt).join('-')} ${clock.join(':')}`
}

/**
 * Parses a date/time string into a timestamp.
 * Returns `null` for empty string (session), or `undefined` for invalid dates/format.
 */
export function expiryValue(text: string): number | null | undefined {
  const written = text.trim()
  if (written === '') return null

  const clock = written.includes(' ') ? written.split(' ') : written.split('T')
  if (clock.length > 2) return undefined
  const day = DAY.exec(clock[0].trim())
  if (!day) return undefined

  let hours = 0
  let minutes = 0
  if (clock.length === 2) {
    const time = /^(\d{1,2}):(\d{1,2})$/.exec(clock[1].trim())
    if (!time) return undefined
    hours = Number(time[1])
    minutes = Number(time[2])
  }

  const year = Number(day[1])
  const month = Number(day[2])
  const date = Number(day[3])
  const when = new Date(year, month - 1, date, hours, minutes)
  const kept =
      when.getFullYear() === year &&
      when.getMonth() === month - 1 &&
      when.getDate() === date &&
      when.getHours() === hours &&
      when.getMinutes() === minutes
  return kept ? when.getTime() : undefined
}

/** Checks if any cookies were actually modified/added in the cookie jar. */
export function touchedJar(writes: CookieWrite[]): boolean {
  return writes.some((write) => write.state !== CookieState.CookieNotSaved)
}