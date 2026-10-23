// What is left here is the formatting that has no language in it. Everything that depends on the
// language — numbers, dates, units — lives in `../i18n`, because that is where the locale is.

/** Maps an HTTP status code to its CSS badge class name based on response status series. */
export function statusBadgeClass(status: number): string {
  if (status >= 200 && status < 300) return 'badge-status-2xx'
  if (status >= 300 && status < 400) return 'badge-status-3xx'
  if (status >= 500) return 'badge-status-5xx'
  return 'badge-status-4xx'
}

/** The tone a record's status is drawn in: a failure or a refusal is bad, an answer is not. */
export function statusTone(record: { status: number; error?: string }): string {
  if (record.error) return 'is-bad'
  return record.status >= 200 && record.status < 400 ? 'is-ok' : 'is-bad'
}

/** Supported HTTP request methods list. */
export const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'] as const
export type HttpMethod = (typeof METHODS)[number]

const INK_CLASSES: Record<HttpMethod, string> = {
  GET: 'method-get',
  POST: 'method-post',
  PUT: 'method-put',
  PATCH: 'method-patch',
  DELETE: 'method-delete',
  HEAD: 'method-other',
  OPTIONS: 'method-other',
}

const SHORT: Record<HttpMethod, string> = {
  GET: 'GET',
  POST: 'POST',
  PUT: 'PUT',
  PATCH: 'PATCH',
  DELETE: 'DEL',
  HEAD: 'HEAD',
  OPTIONS: 'OPT',
}

/** Resolves the CSS styling class name associated with an HTTP method. */
export function methodInkClass(method: string): string {
  const classes: { [verb: string]: string | undefined } = INK_CLASSES
  return classes[method.toUpperCase()] ?? 'method-other'
}

/** Returns a shortened display abbreviation for an HTTP method to fit narrow UI columns. */
export function shortMethod(method: string): string {
  const short: { [verb: string]: string | undefined } = SHORT
  return short[method.toUpperCase()] ?? method
}

/** The code with the server's own phrase after it, written once: what the server sent, as it sent
 *  it, or the number alone where it sent none. */
export function formatStatus(status: number, phrase?: string): string {
  const text = phrase?.trim() ?? ''
  return text.startsWith(String(status)) ? text : `${status} ${text}`.trim()
}

/** Whether a text reads as a number: the tint a value is given says exactly this much. */
export function isNumberText(value: string): boolean {
  return /^-?\d+(\.\d+)?$/.test(value.trim())
}

/** Formats a semver version string by stripping leading 'v' prefix. */
export function formatVersion(tag: string): string {
  return tag.replace(/^v/, '')
}