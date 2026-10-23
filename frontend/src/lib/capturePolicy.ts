import { CaptureSource, type Capture } from '../../bindings/schemo/internal/domain'

/** A name as a variable name: `content-type` and `Content Type` are both `content_type`. */
export function variableNameFor(text: string): string {
  const base = text.replace(/[^A-Za-z0-9_]+/g, '_').replace(/^_+|_+$/g, '')
  if (!base) return 'value'
  return /^[0-9]/.test(base) ? `_${base}` : base
}

const HIDDEN = /token|secret|password|passwd|pwd|api[_-]?key|authorization|credential|signature|session/i

/** Whether a value called this is one a person would rather not see written out. */
export const looksSecret = (name: string): boolean => HIDDEN.test(name)

/** The rule that already keeps what is at this place of a response, if one does. */
export function capturedAt(
  rules: Capture[],
  source: CaptureSource,
  path: string
): Capture | undefined {
  return rules.find((rule) => rule.source === source && (rule.path ?? '') === path)
}
