import type {
  BodyRef,
  CookieWrite,
  HeaderPair,
  Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'
import { formatDay } from '../i18n'

/** Groups adjacent history records by display date, preserving their order. */
export function recordsByDay(records: RequestRecord[]): { label: string; items: RequestRecord[] }[] {
  const groups: { label: string; items: RequestRecord[] }[] = []
  for (const record of records) {
    const label = formatDay(record.startedAt)
    const last = groups.at(-1)
    if (last?.label === label) {
      last.items.push(record)
    } else {
      groups.push({ label, items: [record] })
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
