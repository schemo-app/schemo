import type { Record as RequestRecord } from '../../bindings/schemo/internal/domain'

/** The result of an early execution attempt: either a record or an instant failure. */
export type EarlyAnswer = { record: RequestRecord } | { failed: true }

// An answer that arrives before its call returns; a module singleton because the two sides of the
// race live in different modules.
const held = new Map<string, EarlyAnswer>()

export function holdAnswer(id: string, answer: EarlyAnswer) {
  held.delete(id)
  held.set(id, answer)

  // A window that never claims its answers must not grow the buffer forever.
  if (held.size > 8) {
    const oldest = held.keys().next().value
    if (oldest !== undefined) {
      held.delete(oldest)
    }
  }
}

/** Consumes and removes a buffered answer by ID, if present. */
export function takeAnswer(id: string): EarlyAnswer | undefined {
  const answer = held.get(id)
  held.delete(id)
  return answer
}
