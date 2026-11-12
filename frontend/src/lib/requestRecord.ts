import type {
  BodyRef,
  CookieWrite,
  HeaderPair,
  Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import { formatBytes, formatDay } from '../i18n'
import { splitAddress } from './address'

/** How big a record's answer is, or a dash for one that carried nothing. */
export function responseSize(record: Pick<RequestRecord, 'responseBody'>): string {
  const size = record.responseBody?.size ?? 0
  return size > 0 ? formatBytes(size) : '—'
}

/** Groups adjacent history records by display date, preserving their order. */
export function recordsByDay(records: RequestRecord[]): { day: number; label: string; items: RequestRecord[] }[] {
  const groups: { day: number; label: string; items: RequestRecord[] }[] = []
  for (const record of records) {
    const day = new Date(record.startedAt).setHours(0, 0, 0, 0)
    const last = groups.at(-1)
    if (last?.day === day) {
      last.items.push(record)
    } else {
      groups.push({ day, label: formatDay(record.startedAt), items: [record] })
    }
  }
  return groups
}

export function matchesRecord(record: RequestRecord, query: string): boolean {
  return `${record.method} ${record.url} ${record.status}`.toLowerCase().includes(query) ||
    (record.agent ?? '').toLowerCase().includes(query)
}

export function isHttpError(record: RequestRecord): boolean {
  return record.status >= 400 && record.status < 600
}

export function isSlow(record: RequestRecord): boolean {
  return record.durationUs >= 1_000_000
}

function recordEntry(record: RequestRecord, scope: string) {
  const { host, path } = splitAddress(record.url)
  const normalizedPath = path.split(/[?#]/, 1)[0]
    .replace(/\/(?:\d+|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})(?=\/|$)/gi, '/{id}') || '/'
  const origin = record.url.slice(0, record.url.length - path.length)
  const key = JSON.stringify([scope, record.method, origin, normalizedPath, record.status, Boolean(record.error)])
  return { record, host, path, normalizedPath, key }
}

export function repeatedRecordsByDay(
  records: RequestRecord[],
  expandedIds: ReadonlySet<string>,
  scopeOf: (record: RequestRecord) => string = () => '',
) {
  return recordsByDay(records).map(({ day, label, items }) => {
    const repeats: ReturnType<typeof recordEntry>[][] = []
    for (const record of items) {
      const entry = recordEntry(record, scopeOf(record))
      const previous = repeats.at(-1)
      if (previous?.[0].key === entry.key) previous.push(entry)
      else repeats.push([entry])
    }
    const rows = repeats.flatMap(repeat => {
      const first = repeat[0]
      const expanded = expandedIds.has(first.record.id)
      const repeated = repeat.length >= 3
      const entries = () => repeat.map(entry => ({ ...entry, repeatCount: 0, indented: repeated, expanded: false }))
      if (!repeated) return entries()
      const summary = { ...first, path: first.normalizedPath, repeatCount: repeat.length, indented: false, expanded }
      return expanded ? [summary, ...entries()] : [summary]
    })
    return { day, label, count: items.length, rows }
  })
}

/** Flattened view model of a backend record with normalized collections and extracted body content. */
export type RecordView = Omit<
    RequestRecord,
    | 'requestBody'
    | 'responseBody'
    | 'requestHeaders'
    | 'responseHeaders'
    | 'cookieWrites'
> & {
  requestHeaders: HeaderPair[]
  responseHeaders: HeaderPair[]
  cookieWrites: CookieWrite[]
  requestBody: string
  responseBody: string
}

/** Container for fetched request and response body contents. */
export interface RecordBodies {
  request?: string
  response?: string
}

/** Transforms a raw record domain entity into a UI-ready RecordView representation. */
export function recordView(record: RequestRecord, bodies?: RecordBodies): RecordView {
  return {
    ...record,
    requestHeaders: record.requestHeaders ?? [],
    responseHeaders: record.responseHeaders ?? [],
    cookieWrites: record.cookieWrites ?? [],
    requestBody: bodies?.request ?? record.requestBody?.inline ?? '',
    responseBody: bodies?.response ?? record.responseBody?.inline ?? '',
  }
}

/** Checks if a non-empty body reference requires an asynchronous fetch to retrieve full content. */
export function bodyMissing(ref: BodyRef | null | undefined): boolean {
  return !!ref && ref.size > 0 && !ref.inline
}
