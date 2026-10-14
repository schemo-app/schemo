import { ref } from 'vue'
import { createEventHook } from '@vueuse/core'

// What a menu asks of the pane on screen. The sender names the act and the pane that can do it
// answers, because what comes back belongs to the tab that is open: a request sent from behind a
// menu would leave its answer to the wrong one.

/** The tree menu's «send» and the request's own Send button are one act. */
export const sendRequest = createEventHook<void>()

/** The tab menu's «save to collection…» opens the panel of the tab's own bookmark. */
export const saveRequest = createEventHook<void>()

/** The menu's «find in the response» asks the viewer for its search field. */
export const searchResponse = createEventHook<void>()

/** Whether the pane on screen has an answer its field can search — what the menu's row reads. */
export const responseSearchable = ref(false)

// A request for something that is drawn only while its chip is open waits here until it registers,
// instead of being lost.
function deferred<T = void>() {
  let handler: ((request: T) => void) | null = null
  let held: { request: T } | null = null

  function request(what?: T): void {
    if (handler) handler(what as T)
    else held = { request: what as T }
  }

  /** Registers the thing that can carry the request out, and answers with the undo. */
  function register(onRequest: (request: T) => void): () => void {
    handler = onRequest
    if (held) {
      const { request: waiting } = held
      held = null
      onRequest(waiting)
    }
    return () => {
      handler = null
    }
  }

  return { request, register }
}

/** A stretch of the request body, in the editor's UTF-16 units. */
export interface BodySpan {
  start: number
  end: number
}

const addressField = deferred()
const bodyEditor = deferred<BodySpan>()

/** Asks for the caret in the address field, or keeps the request until the field is drawn. */
export function focusUrlField(): void {
  addressField.request()
}

export const registerUrlField = addressField.register

/** Selects a stretch of the request body and brings it into view, once the editor is drawn. */
export function focusBodyRange(span: BodySpan): void {
  bodyEditor.request(span)
}

export const registerBodyEditor = bodyEditor.register
