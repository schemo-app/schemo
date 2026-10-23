import { History } from '../../bindings/schemo/internal/request'
import { t } from '../i18n'
import { callOrToast } from '../lib/calls'
import { useToast } from './useToast'

// The history written out as a HAR file, all of it or one browser tab's. Cancelling the file dialog
// is an answer too: nothing was written, and nothing is said.
export function useHarExport(session: () => string) {
  const toast = useToast()

  async function exportHar() {
    const written = await callOrToast(
      History.ExportHar(t('files.exportHar'), session()),
      'transfer.exportFailed'
    )
    if (written) toast.show(t('browser.saved'))
  }

  return { exportHar }
}
