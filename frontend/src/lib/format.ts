import type { Protocol } from '../../bindings/schemo/internal/domain'
import { protocolOf } from './protocol'

/** Maps a response status to its badge color. */
export function statusBadgeClass(
  status: number,
  options: { failed?: boolean; protocol?: Protocol; answered?: boolean } = {},
): string {
  const { failed, protocol, answered } = options
  if (failed) return 'badge-status-5xx'
  if (protocolOf(protocol).status === 'grpc' && answered) {
    return status === 0 ? 'badge-status-2xx' : 'badge-status-5xx'
  }
  if (!status) return 'neutral'
  if (status === 101 || (status >= 200 && status < 300)) return 'badge-status-2xx'
  if (status >= 300 && status < 400) return 'badge-status-3xx'
  if (status >= 500) return 'badge-status-5xx'
  return 'badge-status-4xx'
}

export function hasResponseStatus(status: number, protocol?: Protocol, text?: string): boolean {
  return status !== 0 || (protocolOf(protocol).status === 'grpc' && !!text)
}

export function shortStatus(status: number, protocol?: Protocol, text?: string): number | string {
  return hasResponseStatus(status, protocol, text) ? status || 'OK' : '—'
}

/** The tone a record's status is drawn in: a failure or a refusal is bad, an answer is not. */
export function statusTone(record: { status: number; error?: string }): string {
  if (record.error) return 'is-bad'
  return record.status >= 200 && record.status < 400 ? 'is-ok' : 'is-bad'
}

/** Supported HTTP request methods list. */
export const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'] as const
export type HttpMethod = (typeof METHODS)[number]

const INK_CLASSES: Record<string, string> = {
  GQL: 'method-patch',
  WS: 'method-ws',
  SSE: 'method-post',
  GRPC: 'method-put',
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

/** The code with the server's own phrase after it. */
export function formatStatus(status: number, phrase?: string): string {
  const text = phrase?.trim() ?? ''
  return text.startsWith(String(status)) ? text : `${status} ${text}`.trim()
}

/** Short facts on one line, the empty ones left out. */
export function metaLine(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' · ')
}

/** The last segment of a file path, for a label with room for a name and not for a path. */
export function fileName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? path
}

export function isNumberText(value: string): boolean {
  return /^-?\d+(\.\d+)?$/.test(value.trim())
}

/** Formats a semver version string by stripping leading 'v' prefix. */
export function formatVersion(tag: string): string {
  return tag.replace(/^v/, '')
}
