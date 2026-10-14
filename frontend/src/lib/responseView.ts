/** The readings an answer leaves: what it is made of, the sections of a request, and the request
 *  itself when it travelled with a captured one. */
export type ResponseTab = 'body' | 'headers' | 'cookies' | 'timings' | 'scripts' | 'request'

/** The three ways a body is read: as a tree, as the text it is, and as the shape of it. */
export type BodyView = 'tree' | 'raw' | 'map'

/** What the viewer remembers per tab: which reading is on screen, and how far the tree is opened. */
export interface ResponseViewState {
  tab: ResponseTab
  bodyView: BodyView
  collapsed: Record<string, boolean>
  selPath: string
}

export function freshResponseView(): ResponseViewState {
  return { tab: 'body', bodyView: 'raw', collapsed: {}, selPath: '$' }
}
