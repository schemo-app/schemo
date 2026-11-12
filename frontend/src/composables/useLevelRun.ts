import { computed, type Ref } from 'vue'
import type { CollectionRun, CollectionRunResult, LevelRow } from '../../bindings/schemo/internal/domain'
import { useCollectionsStore } from '../stores/collections'
import { t } from '../i18n'

// The page's reading of the last run of a level: how each request of it ended, and what a row of the
// table says about the answer. A row is broken when the request failed or asserted false.
export function useLevelRun(levelId: Ref<string>, requests: Ref<LevelRow[]>) {
  const collections = useCollectionsStore()

  const run = computed<CollectionRun | null>(() => {
    const last = collections.lastRun
    return last && last.collectionId === levelId.value && !last.nodeId ? last : null
  })

  const results = computed(() => run.value?.results ?? [])

  // The table asks once per request, by node id: the results are indexed rather than searched.
  const resultsByNode = computed(() => new Map(results.value.map((result) => [result.nodeId, result])))

  function failed(result: CollectionRunResult | undefined): boolean {
    if (!result || result.skipped) return false
    return !result.ok || result.assertionsPassed < result.assertionsTotal
  }

  function statusOf(result: CollectionRunResult | undefined): { code: number; text: string } {
    if (result?.skipped) return { code: 0, text: t('collections.skippedShort') }
    if (result?.error) return { code: 0, text: t('collections.failed') }
    const code = result?.status ?? 0
    return { code, text: code ? String(code) : '—' }
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
