import type { Record as RequestRecord } from '../../bindings/schemo/internal/domain'

/** The result of an early execution attempt: either a record or an instant failure. */
export type EarlyAnswer = { record: RequestRecord } | { failed: true }

// How many answers are kept for a caller that has not asked yet: an attempt is one request, and a
// window that never claims an answer must not grow the buffer forever.
const LIMIT = 8

// An answer that lands before the call which started the attempt has returned: the events carry it
// here, and the caller that comes back for its id takes it out. A module singleton, like `urlFocus`
// and `bodyFocus`, because the two sides of the race live in different modules.
const held = new Map<string, EarlyAnswer>()

export function holdAnswer(id: string, answer: EarlyAnswer) {
  held.delete(id)
  held.set(id, answer)

  if (held.size > LIMIT) {
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
