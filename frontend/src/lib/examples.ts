import { t, te } from '../i18n'
import type { ResponseExample } from '../../bindings/schemo/internal/domain'
import type { RecordView } from './requestRecord'

/** What a new example of an answer is called by default: the catalogue's own name for the status,
 *  and nothing for a code it has no name for — a key shown as a name is not a name. */
export function exampleName(status: number): string {
  const key = `request.examples.names.${status}`
  return te(key) ? t(key) : ''
}

/** Whether an answer is worth keeping: a refusal or a timeout has no status and nothing to save. */
export function isKeepable(record: Pick<RecordView, 'status' | 'error'>): boolean {
  return record.status > 0 && !record.error
}

/** An answer as an example kept beside its request: the status it carried, its headers and body. */
export function exampleOf(record: RecordView, name: string): ResponseExample {
  return {
    name,
    status: record.status,
    body: record.responseBody,
    headers: (record.responseHeaders ?? []).map(({ name, value }) => ({ name, value })),
  }
}
