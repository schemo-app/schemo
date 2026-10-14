import { computed, type Ref } from 'vue'
import type { CollectionRun, CollectionRunResult, LevelRow } from '../../bindings/schemo/internal/domain'
import { useCollectionsStore } from '../stores/collections'
import { statusBadgeClass } from '../lib/format'
import { formatMicros, useMessages } from '../i18n'

// The page's reading of the last run of a level: how each request of it ended, and what a row of the
// table says about the answer. A row is broken when the request failed or asserted false.
export function useLevelRun(levelId: Ref<string>, requests: Ref<LevelRow[]>) {
  const { t } = useMessages()
  const store = useCollectionsStore()

  const run = computed<CollectionRun | null>(() => {
    const last = store.lastRun
    return last && last.collectionId === levelId.value ? last : null
  })

  const results = computed(() => run.value?.results ?? [])

  // The table asks once per request, by node id: the results are indexed rather than searched.
  const resultsByNode = computed(() => new Map(results.value.map((result) => [result.nodeId, result])))

  function failed(result: CollectionRunResult | undefined): boolean {
    if (!result || result.skipped) return false
    return !result.ok || result.assertionsPassed < result.assertionsTotal
  }

  // A request that never went out says so with a dash: the table's columns are narrow, and the
  // sentence is the row's tooltip rather than its text.
  function statusOf(result: CollectionRunResult | undefined): { text: string; cls: string; title: string } {
    if (!result) return { text: '—', cls: 'none', title: t('collections.notRun') }
    if (result.skipped) return { text: t('collections.skippedShort'), cls: 'none', title: '' }
    if (result.error) return { text: t('collections.failed'), cls: 'bad', title: result.error }
    const text = result.status ? String(result.status) : '—'
    return {
      text,
      cls: failed(result) ? 'bad' : statusBadgeClass(result.status ?? 0),
      title: result.durationUs > 0 ? formatMicros(result.durationUs) : '',
    }
  }

  const tableRows = computed(() =>
    requests.value.map((row) => {
      const result = resultsByNode.value.get(row.id)
      return { row, bad: failed(result), status: statusOf(result) }
    })
  )

  const resultOf = (nodeId: string) => resultsByNode.value.get(nodeId)

  return { run, tableRows, resultOf }
}
