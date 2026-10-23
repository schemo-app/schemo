import type { ScriptRun } from '../../bindings/schemo/internal/domain'
import type { RecordView } from './requestRecord'

/** The readings an answer leaves: what it is made of, the sections of a request, and the request
 *  itself when it travelled with a captured one. */
export type ResponseTab = 'body' | 'headers' | 'cookies' | 'timings' | 'scripts' | 'request'

/** What stands beside a reading's label: how much the section holds, and whether a test failed. */
export interface TabBadge {
  count: string
  failed: boolean
}

/** The badge of a reading. Headers and cookies count their rows; scripts count the tests that
 *  passed out of those that ran. An empty section has no count rather than a zero. */
export function tabBadge(
  tab: ResponseTab,
  record: Pick<RecordView, 'responseHeaders' | 'cookieWrites'>,
  runs: readonly ScriptRun[]
): TabBadge {
  switch (tab) {
    case 'headers':
      return { count: String(record.responseHeaders.length || ''), failed: false }
    case 'cookies':
      return { count: String(record.cookieWrites.length || ''), failed: false }
    case 'scripts': {
      const tests = runs.flatMap((run) => run.tests ?? [])
      const passed = tests.filter((test) => test.passed).length
      return {
        count: tests.length ? `${passed}/${tests.length}` : '',
        failed: passed < tests.length,
      }
    }
    default:
      return { count: '', failed: false }
  }
}

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
