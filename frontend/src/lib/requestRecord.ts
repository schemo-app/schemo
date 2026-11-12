import type {
  BodyRef,
  CookieWrite,
  HeaderPair,
  Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import { formatBytes, formatDay } from '../i18n'

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
