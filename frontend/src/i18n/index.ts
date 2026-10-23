import { ref, shallowRef } from 'vue'
import english from './locales/en.json'
import languages from './languages.json'

// The panel's messages: nested JSON catalogues, `{name}` parameters and plural forms separated by
// `|`, picked by Intl.PluralRules. English is bundled and is the fallback for every key; other
// catalogues load when their language is chosen.

export type Locale = keyof typeof languages
type Messages = typeof english
type Catalogue = { [key: string]: string | Catalogue }
export const locales = Object.keys(languages) as Locale[]
export { languages }

export type MessageKey = Paths<Messages>

type Paths<T, Prefix extends string = ''> = {
  [K in keyof T & string]:
    T[K] extends Record<string, unknown>
      ? Paths<T[K], `${Prefix}${K}.`>
      : `${Prefix}${K}`
}[keyof T & string]

/** Named parameters, or a bare count: `t('counts.requests', 3)` is `{ n: 3, count: 3 }`. */
type Params = number | Record<string, unknown>
type Failure = {
  code?: unknown
  args?: Record<string, string | undefined> | null
}

const DEFAULT_LOCALE: Locale = 'en'
const DAY_MS = 86_400_000

// Both are read by t(), so a template that translates re-renders when the language changes.
const current = ref<Locale>(DEFAULT_LOCALE)
const loaded = shallowRef<Partial<Record<Locale, Catalogue>>>({ en: english })

export function currentLocale(): Locale {
  return current.value
}

export function setLocale(locale: Locale): void {
  current.value = locale
}

/** Replaces the messages of one language: what loadCatalogue does, and what a test sets. */
export function setMessages(locale: Locale, messages: Catalogue | undefined): void {
  const next = { ...loaded.value }
  if (messages) next[locale] = messages
  else delete next[locale]
  loaded.value = next
}

const catalogues = import.meta.glob<Catalogue>(['./locales/*.json', '!./locales/en.json'], {
  import: 'default',
})

export async function loadCatalogue(locale: Locale): Promise<void> {
  if (loaded.value[locale]) return
  const load = catalogues[`./locales/${locale}.json`]
  if (!load) throw new Error(`Missing translation: ${locale}`)
  setMessages(locale, await load())
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

function lookup(catalogue: Catalogue | undefined, key: string): string | undefined {
  let node: string | Catalogue | undefined = catalogue
  for (const part of key.split('.')) {
    if (!node || typeof node === 'string') return undefined
    node = node[part]
  }
  return typeof node === 'string' ? node : undefined
}

const pluralRules = new Map<Locale, Intl.PluralRules>()

// Two forms are one|other in every language; more follow the order of the language's own
// categories (Russian: one|few|many).
function pluralForm(locale: Locale, count: number, forms: number): number {
  let rules = pluralRules.get(locale)
  if (!rules) pluralRules.set(locale, (rules = new Intl.PluralRules(locale)))
  const category = rules.select(count)
  if (forms === 2) return category === 'one' ? 0 : 1
  const order = ['zero', 'one', 'two', 'few', 'many', 'other']
    .filter(name => rules.resolvedOptions().pluralCategories.includes(name as Intl.LDMLPluralRule))
  return Math.max(0, Math.min(order.indexOf(category), forms - 1))
}

function render(locale: Locale, message: string, params?: Params): string {
  const named: Record<string, unknown> = typeof params === 'number' ? { n: params, count: params } : params ?? {}
  if (message.includes('|')) {
    const forms = message.split('|').map(form => form.trim())
    const count = [named.count, named.n].find(value => typeof value === 'number') as number | undefined
    message = forms[count === undefined ? 0 : pluralForm(locale, count, forms.length)]
  }
  return message.replace(/\{(\w+)\}/g, (_, name: string) => String(named[name] ?? ''))
}

function translate(key: string, params?: Params): string {
  const locale = current.value
  const own = lookup(loaded.value[locale], key)
  const message = own ?? lookup(english, key)
  return message === undefined ? key : render(own === undefined ? DEFAULT_LOCALE : locale, message, params)
}

export function t(key: MessageKey, params?: Params): string {
  return translate(key, params)
}

/** Whether the key names a message, in this language or in English. */
export function te(key: string): key is MessageKey {
  return lookup(english, key) !== undefined || lookup(loaded.value[current.value], key) !== undefined
}

/** A message for a key that came from data (Go names auth fields by key); anything else as is. */
export function textOf(key: string, params?: Params): string {
  return te(key) ? translate(key, params) : key
}

// Intl formatters are costly to build and are asked for on every render: one per locale and options.
const formatters = new Map<string, unknown>()

function formatter<T>(kind: string, options: object, make: (locale: Locale) => T): T {
  const key = `${kind}:${currentLocale()}:${JSON.stringify(options)}`
  let found = formatters.get(key) as T | undefined
  if (!found) formatters.set(key, (found = make(currentLocale())))
  return found
}

export function formatNumber(value: number, digits = 0): string {
  const options = { minimumFractionDigits: digits, maximumFractionDigits: digits }
  return formatter('number', options, locale => new Intl.NumberFormat(locale, options)).format(value)
}

export function formatDate(value: number | Date, options: Intl.DateTimeFormatOptions): string {
  return formatter('date', options, locale => new Intl.DateTimeFormat(locale, options)).format(value)
}

function relative(value: number, unit: Intl.RelativeTimeFormatUnit): string {
  const options = { numeric: 'always' } as const
  return formatter('relative', options, locale => new Intl.RelativeTimeFormat(locale, options)).format(value, unit)
}

const LONG_DAY: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'long' }

/** A refusal from Go in the person's words: `errors.codes.<code>`, or the server's own reason. */
export function failureText(failure: Failure | null | undefined): string | null {
  const code = failure?.code
  if (typeof code !== 'string') return null
  const args = failure?.args
  if (code === 'serverRefused' && args?.reason && te(`errors.reasons.${args.reason}`)) {
    return translate(`errors.reasons.${args.reason}`, words(args))
  }
  const key = `errors.codes.${code}`
  return te(key) ? translate(key, words(args)) : null
}

const WORDS = [
  ['what', 'errors.parts.'],
  ['feature', 'errors.features.'],
] as const

// Arguments arrive as strings: a count becomes a number again (it picks the plural form), and a
// part or feature name becomes its word.
function words(args: Record<string, string | undefined> | null | undefined): Record<string, unknown> {
  const result: Record<string, unknown> = { ...args }
  if (args?.n !== undefined && args.n !== '' && !Number.isNaN(Number(args.n))) result.n = Number(args.n)
  for (const [field, prefix] of WORDS) {
    const value = result[field]
    if (typeof value === 'string' && te(prefix + value)) result[field] = translate(prefix + value)
  }
  return result
}

export function describeFailure(error: unknown): string {
  const cause = error && typeof error === 'object' && 'cause' in error ? (error.cause as Failure | undefined) : undefined
  return failureText(cause) ?? String(error)
}

export function formatMicros(us: number): string {
  const ms = us / 1000
  const digits = us < 1000 ? 1 : 0
  return `${formatNumber(digits ? ms : Math.round(ms), digits)} ${t('units.ms')}`
}

export function formatDuration(us: number): string {
  return us > 0 ? formatMicros(us) : '—'
}

export function formatClock(at: number): string {
  return formatDate(at, { hour: '2-digit', minute: '2-digit' })
}

export function formatStatus(status: number, error?: string): string {
  if (error) return t('history.failed')
  return status > 0 ? String(status) : '—'
}

const BYTE_UNITS = ['units.b', 'units.kb', 'units.mb', 'units.gb'] as const satisfies readonly MessageKey[]

export function formatBytes(bytes: number): string {
  if (bytes === 0) return `0 ${t('units.b')}`
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit++
  }
  return `${formatNumber(value, value >= 100 || unit === 0 ? 0 : 1)} ${t(BYTE_UNITS[unit])}`
}

export function daysAgo(moment: number | Date, now = Date.now()): number {
  const day = (date: Date) => Date.UTC(date.getFullYear(), date.getMonth(), date.getDate())
  return Math.round((day(new Date(now)) - day(new Date(moment))) / DAY_MS)
}

export function formatDay(ms: number): string {
  const days = daysAgo(ms)
  if (days === 0) return t('history.today')
  if (days === 1) return t('history.yesterday')
  return formatDate(ms, LONG_DAY)
}

export function formatAgo(ms: number): string {
  if (!ms) return t('common.none')
  const elapsed = Date.now() - ms
  if (elapsed < 60_000) return t('time.justNow')
  const minutes = Math.floor(elapsed / 60_000)
  if (minutes < 60) return relative(-minutes, 'minute')
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return relative(-hours, 'hour')
  const days = Math.floor(hours / 24)
  if (days < 7) return relative(-days, 'day')
  return formatDate(ms, LONG_DAY)
}

export function formatIn(ms: number): string {
  if (!ms) return t('common.none')
  const seconds = Math.max(0, Math.ceil((ms - Date.now()) / 1000))
  if (seconds < 60) return relative(seconds, 'second')
  const minutes = Math.ceil(seconds / 60)
  if (minutes < 60) return relative(minutes, 'minute')
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return relative(hours, 'hour')
  return formatDate(ms, LONG_DAY)
}

export function formatCheckedAt(ms: number): string {
  if (!ms) return t('common.none')
  const time = formatClock(ms)
  const days = daysAgo(ms)
  if (days === 0) return t('time.todayAt', { time })
  if (days === 1) return t('time.yesterdayAt', { time })
  return `${formatDate(ms, LONG_DAY)}, ${time}`
}
