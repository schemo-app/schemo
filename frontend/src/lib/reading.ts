import type { ReadingRow } from '../../bindings/schemo/internal/domain'
import { formatDate } from '../i18n'

export type InlineReadings = Readonly<Partial<Record<string, ReadingRow>>>

export function formatReadingTime(row: ReadingRow): string {
  return formatDate(Date.parse(row.value), {
    dateStyle: 'long',
    timeStyle: row.shape === 'date' ? undefined : 'short',
    timeZone: row.shape === 'time' ? undefined : 'UTC',
  })
}
