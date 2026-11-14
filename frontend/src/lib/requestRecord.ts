import type {
  BodyRef,
  CookieWrite,
  HeaderPair,
  Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import { formatBytes, formatDay, formatDuration, formatNumber, t } from '../i18n'
import { splitAddress } from './address'
import { protocolOf, requestLabel } from './protocol'

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

export function recordSize(record: RequestRecord): string {
  const stream = protocolOf(record.protocol).stream
  if (stream) return t(stream.count, record.messageCount ?? 0)
  return record.responseBody?.size ? responseSize(record) : ''
}

export function matchesRecord(record: RequestRecord, query: string): boolean {
  return `${requestLabel(record)} ${record.url} ${record.operation ?? ''} ${record.status}`.toLowerCase().includes(query) ||
    (record.agent ?? '').toLowerCase().includes(query)
}

export function isRecordError(record: RequestRecord): boolean {
  if (record.error) return true
  if (protocolOf(record.protocol).status === 'grpc') return record.status !== 0
  return record.status >= 400 && record.status < 600
}

const MICROS_PER_SECOND = 1_000_000

/** A request is slow from one second up. */
export function isSlow(record: Pick<RequestRecord, 'durationUs'>): boolean {
  return record.durationUs >= MICROS_PER_SECOND
}

/** A list row's duration: milliseconds, seconds once slow, or a note while still running. */
export function durationLabel(record: Pick<RequestRecord, 'active' | 'durationUs'>): string {
  if (record.active) return t('request.protocol.inProgress')
  if (!isSlow(record)) return formatDuration(record.durationUs)
  return `${formatNumber(record.durationUs / MICROS_PER_SECOND, 1)} ${t('units.s')}`
}

function recordEntry(record: RequestRecord, scope: string) {
  const { host, path } = splitAddress(record.url)
  const normalizedPath = path.split(/[?#]/, 1)[0]
    .replace(/\/(?:\d+|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})(?=\/|$)/gi, '/{id}') || '/'
  const origin = record.url.slice(0, record.url.length - path.length)
  const key = JSON.stringify([
    scope, requestLabel(record), origin, normalizedPath, record.operation, record.status, Boolean(record.error), record.active,
  ])
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
  /** The answer is bytes: Go keeps them, and the window offers to save them instead of reading them. */
  responseBinary: boolean
}

/** A record's bodies as read: the references it came with, filled in where they had to be fetched. */
export interface RecordBodies {
  request?: BodyRef
  response?: BodyRef
}

/** Transforms a raw record domain entity into a UI-ready RecordView representation. */
export function recordView(record: RequestRecord, bodies?: RecordBodies): RecordView {
  return {
    ...record,
    requestHeaders: record.requestHeaders ?? [],
    responseHeaders: record.responseHeaders ?? [],
    cookieWrites: record.cookieWrites ?? [],
    requestBody: (bodies?.request ?? record.requestBody)?.inline ?? '',
    responseBody: (bodies?.response ?? record.responseBody)?.inline ?? '',
    responseBinary: !!(bodies?.response ?? record.responseBody)?.binary,
  }
}

/** Checks if a non-empty body reference requires an asynchronous fetch to retrieve full content. */
export function bodyMissing(ref: BodyRef | null | undefined): boolean {
  return !!ref && ref.size > 0 && !ref.inline && !ref.binary
}
