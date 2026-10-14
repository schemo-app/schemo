import type {
  BodyRef,
  CookieWrite,
  HeaderPair,
  Record as RequestRecord,
} from '../../bindings/schemo/internal/domain'

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