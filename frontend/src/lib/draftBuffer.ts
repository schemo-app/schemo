import { TextField, type TextResult } from '../../bindings/schemo/internal/usecase/draft'

/** Text typeInto but not yet written, and the revision tags that keep a stale answer out. */
export interface DraftBuffer {
  urlText: string
  bodyText: string
  urlRev: number
  bodyRev: number
  bufferedUrl: boolean
  bufferedBody: boolean
}

/** Debounce delay before flushing pending text changes to backend. */
export const FLUSH_MS = 400

/** Pending text change with revision counter for stale response matching. */
export interface PendingText {
  field: TextField
  text: string
  rev: number
}

/** Updates the buffer value and bumps its revision. */
export function typeInto(buffer: DraftBuffer, field: TextField, text: string): void {
  if (field === TextField.FieldURL) {
    buffer.urlText = text
    buffer.urlRev += 1
    buffer.bufferedUrl = true
  } else {
    buffer.bodyText = text
    buffer.bodyRev += 1
    buffer.bufferedBody = true
  }
}

/** Returns all uncommitted buffered inputs. */
export function drain(buffer: DraftBuffer): PendingText[] {
  const out: PendingText[] = []
  if (buffer.bufferedUrl) {
    out.push({ field: TextField.FieldURL, text: buffer.urlText, rev: buffer.urlRev })
  }
  if (buffer.bufferedBody) {
    out.push({ field: TextField.FieldBody, text: buffer.bodyText, rev: buffer.bodyRev })
  }
  return out
}

/**
 * Validates whether an incoming backend response matches the current buffer revision.
 * Clears the buffered flag if revisions match.
 */
export function settle(buffer: DraftBuffer, result: TextResult): boolean {
  if (result.field === TextField.FieldURL) {
    if (result.rev !== buffer.urlRev) return false
    buffer.bufferedUrl = false
    return true
  }
  if (result.rev !== buffer.bodyRev) return false
  buffer.bufferedBody = false
  return true
}

/** Drops all pending changes without flushing. */
export function discard(buffer: DraftBuffer): void {
  buffer.bufferedUrl = false
  buffer.bufferedBody = false
}