import { onMounted } from 'vue'
import { TransferService } from '../../../bindings/schemo/internal/transport/wails'
import { useTransferStore } from '../../stores/transfer'
import { useWailsEvent } from './useWailsEvent'

/** A link to a published page asks this window for one collection, so the import dialog opens on
 *  it. The page is taken from Go rather than carried by the event: a link is usually what started
 *  this app, and the window has nothing listening while it is still drawing. */
export function useTransferEvents(): void {
  const transfer = useTransferStore()

  // One caller opens the dialog, whichever of the two gets there first: the take empties the page.
  async function follow(): Promise<void> {
    try {
      const page = await TransferService.TakeImportLink()
      if (page) transfer.askImportPage(page)
    } catch {
      // No backend answer: the window keeps its own import button, which is where a link lands by
      // hand anyway.
    }
  }

  useWailsEvent('import-link', () => void follow())
  onMounted(() => void follow())
}
