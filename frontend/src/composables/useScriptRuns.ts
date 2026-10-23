import { ref, watch } from 'vue'
import type { ScriptRun } from '../../bindings/schemo/internal/domain'
import { Scripts } from '../../bindings/schemo/internal/script'

// The scripts that ran for the record on screen, read beside the viewer because the row counts the
// tests the report holds and the scripts tab is one reading of the same record.
export function useScriptRuns(recordId: () => string) {
  const runs = ref<ScriptRun[]>([])
  const isLoading = ref(true)

  watch(
    recordId,
    async (id, _previous, onCleanup) => {
      // A read that comes back after the record has changed belongs to a record no longer on screen.
      let isStale = false
      onCleanup(() => (isStale = true))

      isLoading.value = true
      let found: ScriptRun[] = []
      try {
        found = (await Scripts.Runs(id)) ?? []
      } catch {
        // A report that cannot be read is no report: the tab says there is nothing, not that it is
        // still looking.
      }
      if (isStale) return
      runs.value = found
      isLoading.value = false
    },
    { immediate: true }
  )

  return { runs, isLoading }
}
