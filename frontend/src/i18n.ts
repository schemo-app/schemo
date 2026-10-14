import { createI18n, useI18n } from 'vue-i18n'
import english from './locales/en.json'
import languages from './languages.json'

export type Locale = keyof typeof languages
type Messages = typeof english
export const locales = Object.keys(languages) as Locale[]
export { languages }

export type MessageKey = Paths<Messages>

type Params = number | Record<string, unknown> | unknown[]
type Failure = {
  code?: unknown
  args?: Record<string, string | undefined> | null
}

const DEFAULT_LOCALE: Locale = 'en'
const DAY_MS = 86_400_000

function pluralRule(locale: Locale) {
  const rules = new Intl.PluralRules(locale)
  const categories = ['zero', 'one', 'two', 'few', 'many', 'other']
    .filter(category => rules.resolvedOptions().pluralCategories.includes(category as Intl.LDMLPluralRule))
  return (choice: number, length: number): number => {
    const category = rules.select(choice)
    if (length === 2) return category === 'one' ? 0 : 1
    return Math.min(categories.indexOf(category), length - 1)
  }
}

export function resolveLocale(preferences: readonly string[]): Locale {
  for (const preference of preferences) {
    let requested: Intl.Locale
    try {
      requested = new Intl.Locale(preference)
    } catch {
      continue
    }
    const exact = locales.find(locale => locale.toLowerCase() === requested.baseName.toLowerCase())
    if (exact) return exact
    const related = locales.filter(locale => new Intl.Locale(locale).language === requested.language)
    const sameScript = related.find(locale => new Intl.Locale(locale).maximize().script === requested.maximize().script)
    const candidate = sameScript ?? related[0]
    if (candidate) return candidate
  }
  return DEFAULT_LOCALE
}

export function systemLocale(): Locale {
  return resolveLocale(navigator.languages?.length ? navigator.languages : [navigator.language])
}

export const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: DEFAULT_LOCALE,
  fallbackLocale: DEFAULT_LOCALE,
  fallbackWarn: false,
  missingWarn: false,
  messages: { en: english } as Record<string, Messages>,
  pluralRules: Object.fromEntries(locales.map(locale => [locale, pluralRule(locale)])),
})

export function currentLocale(): Locale {
  return i18n.global.locale.value as Locale
}

const catalogues = import.meta.glob<Messages>(['./locales/*.json', '!./locales/en.json'], {
  import: 'default',
})

export async function loadCatalogue(locale: Locale): Promise<void> {
  if (i18n.global.availableLocales.includes(locale)) return

  const load = catalogues[`./locales/${locale}.json`]
  if (!load) throw new Error(`Missing translation: ${locale}`)
  i18n.global.setLocaleMessage(locale, await load())
}

type Paths<T, Prefix extends string = ''> = {
  [K in keyof T & string]:
    T[K] extends Record<string, unknown>
      ? Paths<T[K], `${Prefix}${K}.`>
      : `${Prefix}${K}`
}[keyof T & string]

const translate = i18n.global.t as (
  key: string,
  params?: Params,
) => string

export function t(key: MessageKey, params?: Params): string {
  return translate(key, params)
}

export function te(key: string): key is MessageKey {
  return i18n.global.te(key) || i18n.global.te(key, DEFAULT_LOCALE)
}

export function textOf(key: string, params?: Params): string {
  return te(key) ? translate(key, params) : key
}

export function useMessages(): {
  t: typeof t
  te: typeof te
} {
  useI18n({ useScope: 'global' })
  return { t, te }
}

const numberFormatters = new Map<string, Intl.NumberFormat>()
const dateFormatters = new Map<string, Intl.DateTimeFormat>()
const relativeFormatters = new Map<string, Intl.RelativeTimeFormat>()

function formatterKey(options: object): string {
  return `${currentLocale()}:${JSON.stringify(options)}`
}

function numberFormatter(
  options: Intl.NumberFormatOptions = {},
): Intl.NumberFormat {
  const key = formatterKey(options)
  let found = numberFormatters.get(key)

  if (!found) {
    found = new Intl.NumberFormat(currentLocale(), options)
    numberFormatters.set(key, found)
  }

  return found
}

function dateFormatter(
  options: Intl.DateTimeFormatOptions = {},
): Intl.DateTimeFormat {
  const key = formatterKey(options)
  let found = dateFormatters.get(key)

  if (!found) {
    found = new Intl.DateTimeFormat(currentLocale(), options)
    dateFormatters.set(key, found)
  }

  return found
}

function relativeFormatter(
  options: Intl.RelativeTimeFormatOptions = {},
): Intl.RelativeTimeFormat {
  const key = formatterKey(options)
  let found = relativeFormatters.get(key)

  if (!found) {
    found = new Intl.RelativeTimeFormat(currentLocale(), options)
    relativeFormatters.set(key, found)
  }

  return found
}

export function formatNumber(value: number, digits = 0): string {
  return numberFormatter({
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value)
}

export function formatDate(
  value: number | Date,
  options: Intl.DateTimeFormatOptions,
): string {
  return dateFormatter(options).format(value)
}

function relative(
  value: number,
  unit: Intl.RelativeTimeFormatUnit,
): string {
  return relativeFormatter({ numeric: 'always' }).format(value, unit)
}

export function failureText(
  failure: Failure | null | undefined,
): string | null {
  const code = failure?.code
  if (typeof code !== 'string') return null

  const args = failure?.args
  const reason = args?.reason

  if (code === 'serverRefused' && reason) {
    const key = `errors.reasons.${reason}`

    if (te(key)) {
      return translate(key, words(args))
    }
  }

  const key = `errors.codes.${code}`
  return te(key) ? translate(key, words(args)) : null
}

const WORDS = [
  ['what', 'errors.parts.'],
  ['feature', 'errors.features.'],
] as const

function words(
  args: Record<string, string | undefined> | null | undefined,
): Record<string, unknown> {
  const result = countable(args)

  for (const [field, prefix] of WORDS) {
    const value = result[field]
    if (typeof value !== 'string') continue

    const key = `${prefix}${value}`

    if (te(key)) {
      result[field] = translate(key)
    }
  }

  return result
}

function countable(
  args: Record<string, string | undefined> | null | undefined,
): Record<string, unknown> {
  if (!args) return {}

  const count = args.n

  if (count === undefined || count === '') {
    return { ...args }
  }

  const value = Number(count)

  return Number.isNaN(value)
    ? { ...args }
    : { ...args, n: value }
}

export function describeFailure(error: unknown): string {
  const cause =
    error && typeof error === 'object' && 'cause' in error
      ? (error.cause as Failure | undefined)
      : undefined

  return failureText(cause) ?? String(error)
}

export function formatMicros(us: number): string {
  const ms = us / 1000
  const digits = us < 1000 ? 1 : 0

  return `${formatNumber(digits ? ms : Math.round(ms), digits)} ${t('units.ms')}`
}

const BYTE_UNITS = [
  'units.b',
  'units.kb',
  'units.mb',
  'units.gb',
] as const satisfies readonly MessageKey[]

export function formatDuration(us: number): string {
  return us > 0 ? formatMicros(us) : '—'
}

export function formatClock(at: number): string {
  return formatDate(at, {
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatStatus(status: number, error?: string): string {
  if (error) return t('history.failed')
  return status > 0 ? String(status) : '—'
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) {
    return `0 ${t('units.b')}`
  }

  let value = bytes
  let unit = 0

  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit++
  }

  const digits = value >= 100 || unit === 0 ? 0 : 1

  return `${formatNumber(value, digits)} ${t(BYTE_UNITS[unit])}`
}

export function daysAgo(moment: number | Date, now = Date.now()): number {
  return Math.round(
    (calendarDay(new Date(now)) - calendarDay(new Date(moment))) / DAY_MS,
  )
}

function calendarDay(date: Date): number {
  return Date.UTC(
    date.getFullYear(),
    date.getMonth(),
    date.getDate(),
  )
}

export function formatDay(ms: number): string {
  const days = daysAgo(ms)

  if (days === 0) return t('history.today')
  if (days === 1) return t('history.yesterday')

  return formatDate(ms, {
    day: 'numeric',
    month: 'long',
  })
}

export function formatAgo(ms: number): string {
  if (!ms) return t('common.none')

  const elapsed = Date.now() - ms

  if (elapsed < 60_000) {
    return t('time.justNow')
  }

  const minutes = Math.floor(elapsed / 60_000)
  if (minutes < 60) return relative(-minutes, 'minute')

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return relative(-hours, 'hour')

  const days = Math.floor(hours / 24)
  if (days < 7) return relative(-days, 'day')

  return formatDate(ms, {
    day: 'numeric',
    month: 'long',
  })
}

export function formatIn(ms: number): string {
  if (!ms) return t('common.none')

  const seconds = Math.max(
    0,
    Math.ceil((ms - Date.now()) / 1000),
  )

  if (seconds < 60) return relative(seconds, 'second')

  const minutes = Math.ceil(seconds / 60)
  if (minutes < 60) return relative(minutes, 'minute')

  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return relative(hours, 'hour')

  return formatDate(ms, {
    day: 'numeric',
    month: 'long',
  })
}

export function formatCheckedAt(ms: number): string {
  if (!ms) return t('common.none')

  const at = new Date(ms)
  const time = formatClock(ms)
  const days = daysAgo(at)

  if (days === 0) {
    return t('time.todayAt', { time })
  }

  if (days === 1) {
    return t('time.yesterdayAt', { time })
  }

  return `${formatDate(at, {
  day: 'numeric',
  month: 'long',
})}, ${time}`
}
