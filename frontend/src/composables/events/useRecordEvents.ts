import { RecordSource } from '../../../bindings/schemo/internal/domain'
import { useRecordsStore } from '../../stores/records'
import { useCollectionsStore } from '../../stores/collections'
import { ownerOf } from '../../stores/content'
import { useToast } from '../useToast'
import { failureText, t as tr } from '../../i18n'
import { holdAnswer } from '../request/earlyAnswers'
import { useWailsEvent } from './useWailsEvent'

/** Subscribes to request execution, record ingestion, and collection run events emitted by the backend. */
export function useRecordEvents(
    store: ReturnType<typeof useRecordsStore>,
    collections: ReturnType<typeof useCollectionsStore>
): void {
  const toast = useToast()

  // Handles new ingested records from browser capture or manual sampling
  useWailsEvent('record:added', (ev) => {
    if (ev.data.source === RecordSource.SourceBrowser) {
      store.setCaptureState({ connected: true, recording: true, paused: false })
    }
    store.addIngested(ev.data)
  })

  // Routes completed request execution payloads to the tab that sent it, or to the early answer buffer
  useWailsEvent('request:finished', (ev) => {
    const { id, record } = ev.data
    const owner = ownerOf(id)
    if (owner) {
      void owner.finishSend(record)
      store.prepend(record)
    } else if (record.agent) {
      // An agent's send belongs to nobody in this window — it asked over the MCP server — so there
      // is no answer to hold for anybody: it goes straight into the history, which is where the
      // person watches what the agent does.
      store.addIngested(record)
    } else holdAnswer(id, { record })
  })

  // Handles request execution failures prior to record creation
  useWailsEvent('request:failed', (ev) => {
    const { id } = ev.data
    toast.show(tr('errors.requestFailed', { error: failureText(ev.data.failure) ?? ev.data.error }), 'error')
    const owner = ownerOf(id)
    if (owner) owner.failSend()
    else holdAnswer(id, { failed: true })
  })

  // Reloads history store when history is cleared for the current workspace
  useWailsEvent('history:cleared', (ev) => {
    if (store.workspaceId && ev.data.workspaceId && ev.data.workspaceId !== store.workspaceId) {
      return
    }
    store.reset()
    void store.load()
  })

  // Collection execution progress and completion updates
  useWailsEvent('collection:run-progress', (ev) => {
    collections.applyRunProgress(ev.data)
  })

  useWailsEvent('collection:run-finished', (ev) => {
    collections.applyRunFinished(ev.data)
  })
}
