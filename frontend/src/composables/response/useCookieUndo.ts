import { computed, ref } from 'vue'
import { runOrToast } from '../../stores/calls'
import { useRecordsStore } from '../../stores/records'
import { touchedJar } from '../../lib/cookies'
import type { RecordView } from '../../lib/requestRecord'
import { RecordsService } from '../../../bindings/schemo/internal/transport/wails'

// Taking an answer's cookie writes back reverts the jar and takes the claim off the record, so what
// the tab draws after it is the answer the record now describes. There is nothing to take back for
// an answer that left the jar as it was.
export function useCookieUndo(record: () => RecordView) {
  const records = useRecordsStore()
  const undoing = ref(false)
  const touched = computed(() => touchedJar(record().cookieWrites))

  async function undo() {
    undoing.value = true
    const ok = await runOrToast(RecordsService.UndoCookieWrites(record().id), 'cookies.undoFailed')
    undoing.value = false
    if (ok) await records.refresh(record().id)
  }

  return { touched, undoing, undo }
}
