import { computed, ref } from 'vue'
import type { Record as RequestRecord } from '../../bindings/schemo/internal/domain'
import { isRecordError, isSlow, matchesRecord, repeatedRecordsByDay } from '../lib/requestRecord'

export interface RecordFilterOptions {
  records: () => RequestRecord[]
  query: () => string
  errorsOnly: () => boolean
  slowOnly: () => boolean
  /** Narrows the list before the counts, e.g. by source or browser tab. */
  keep?: (record: RequestRecord) => boolean
  /** Repeats are collapsed only within the same scope. */
  scopeOf?: (record: RequestRecord) => string
}

/** The searched, filtered and day-grouped records of a history list, with collapsible repeats. */
export function useRecordFilter(options: RecordFilterOptions) {
  const expandedRepeats = ref(new Set<string>())

  // The error and slow counts are of what the search left, so the quick filters say what they would show.
  const matching = computed(() => {
    const needle = options.query().trim().toLowerCase()
    return options.records().filter(record =>
      (options.keep?.(record) ?? true) && (!needle || matchesRecord(record, needle)),
    )
  })
  const errorCount = computed(() => matching.value.filter(isRecordError).length)
  const slowCount = computed(() => matching.value.filter(isSlow).length)
  const rows = computed(() => matching.value.filter(record =>
    (!options.errorsOnly() || isRecordError(record)) && (!options.slowOnly() || isSlow(record)),
  ))
  const groups = computed(() => repeatedRecordsByDay(rows.value, expandedRepeats.value, options.scopeOf))

  function toggleRepeat(id: string) {
    if (expandedRepeats.value.has(id)) expandedRepeats.value.delete(id)
    else expandedRepeats.value.add(id)
  }

  return { errorCount, slowCount, rows, groups, toggleRepeat, collapseRepeats: () => expandedRepeats.value.clear() }
}
