import { defineStore } from 'pinia'
import { ref } from 'vue'
import { t } from '../i18n'
import { useToast } from '../composables/useToast'
import { markStep } from '../composables/useGettingStarted'
import { copyToClipboard } from '../lib/clipboard'
import { TransferService } from '../../bindings/schemo/internal/transport/wails'
import type { Preview, Space } from '../../bindings/schemo/internal/usecase/transfer'
import { callOrToast } from './calls'
import { useCollectionsStore } from './collections'
import { useEnvironmentsStore } from './environments'
import { useWorkspacesStore } from './workspaces'

/** What a source is read as, and what an export is written as: the words Go knows the formats by.
 *  A published page is read only: a page is a place a collection stands at, and publishing it is
 *  what puts it there. */
export type TransferFormat = 'schemo' | 'postman' | 'openapi' | 'insomnia' | 'har' | 'curl' | 'page'

/** The two dialogs one modal at a time: what is open, what it is about, and what it wrote. */
export const useTransferStore = defineStore('transfer', () => {
  const collections = useCollectionsStore()
  const environments = useEnvironmentsStore()
  const workspaces = useWorkspacesStore()
  const toast = useToast()

  const importOpen = ref(false)
  // Where the import lands: '' is the top level, an id is the collection it was asked for.
  const importParent = ref('')
  // The page a link from outside named, waiting for the dialog to open on it. It is read once and
  // cleared: the address belongs to the link that was followed, not to the dialog.
  const linkedPage = ref('')
  const exportOpen = ref(false)
  // The collection the export is fixed to; '' lets the dialog pick one.
  const exportTarget = ref('')
  const spaces = ref<Space[]>([])
  // A preview or a write in flight: the dialogs keep their buttons still while it is.
  const busy = ref(false)

  // Every way in — the menu, the key, a row of the tree — comes here, and an import writes into the
  // space: where it may not, the answer is the sentence the refusal would have been.
  function askImport(parentId = '') {
    if (!workspaces.canWrite) {
      toast.show(t('errors.codes.workspaceReadOnly'))
      return
    }
    importParent.value = parentId
    exportOpen.value = false
    importOpen.value = true
  }

  // A link from outside is a person asking for one collection in particular, and it is the dialog
  // that reads it: the address is prefilled and read on the spot, so following the link and
  // importing is one move rather than two.
  function askImportPage(page: string) {
    if (!workspaces.canWrite) {
      toast.show(t('errors.codes.workspaceReadOnly'))
      return
    }
    linkedPage.value = page
    importParent.value = ''
    exportOpen.value = false
    importOpen.value = true
  }

  function askExport(id = '') {
    exportTarget.value = id
    importOpen.value = false
    exportOpen.value = true
  }

  function closeTransfer() {
    importOpen.value = false
    exportOpen.value = false
    linkedPage.value = ''
  }

  // A nil preview is a cancelled dialog, not a failure, and Go reads the file behind it.
  async function previewFile(format: TransferFormat, title: string): Promise<Preview | null> {
    busy.value = true
    try {
      const preview = await callOrToast(
        TransferService.PreviewFile(title, format),
        'transfer.readFailed'
      )
      return preview ?? null
    } finally {
      busy.value = false
    }
  }

  // A preview asked for while somebody is still typing: a paste that does not read as a command
  // yet is not news, and a toast per keystroke would be.
  async function quiet<T>(call: Promise<T | undefined>): Promise<T | null> {
    try {
      return (await call) ?? null
    } catch {
      return null
    }
  }

  async function previewText(
    format: TransferFormat,
    text: string,
    fileName: string,
    quietly = false
  ): Promise<Preview | null> {
    busy.value = true
    try {
      if (quietly) return await quiet(TransferService.PreviewText(format, text, fileName))
      const preview = await callOrToast(
        TransferService.PreviewText(format, text, fileName),
        'transfer.readFailed'
      )
      return preview ?? null
    } finally {
      busy.value = false
    }
  }

  async function previewUrl(
    format: TransferFormat,
    url: string,
    quietly = false
  ): Promise<Preview | null> {
    busy.value = true
    try {
      if (quietly) return await quiet(TransferService.PreviewUrl(format, url))
      const preview = await callOrToast(TransferService.PreviewUrl(format, url), 'transfer.readFailed')
      return preview ?? null
    } finally {
      busy.value = false
    }
  }

  // What a preview read is written where the person chose: Go answers with the tree to draw and
  // the count the toast says.
  async function apply(token: string, parentID: string, name: string): Promise<boolean> {
    busy.value = true
    try {
      const summary = await callOrToast(
        collections.takeTree(TransferService.Apply(token, parentID, name)),
        'transfer.importFailed'
      )
      if (!summary) return false
      markStep('import')
      if (summary.envState) await environments.applyState(summary.envState)
      // A file that held only environments wrote no collection: there is no tree to draw, and a
      // toast about requests would be counting something that never happened.
      if (summary.collections === 0) {
        toast.show(
          t('transfer.importedEnvs', { n: t('counts.environments', summary.environments) })
        )
        return true
      }
      toast.show(
        t('transfer.imported', {
          n: t('counts.requests', summary.requests),
          name: summary.name || name,
        })
      )
      return true
    } finally {
      busy.value = false
    }
  }

  async function loadSpaces(): Promise<Space[]> {
    const list = await callOrToast(TransferService.Spaces(), 'transfer.readFailed')
    spaces.value = list ?? []
    return spaces.value
  }

  async function importFromSpace(
    workspace: string,
    ids: string[],
    parentID: string,
    workspaceName: string
  ): Promise<boolean> {
    busy.value = true
    try {
      const summary = await callOrToast(
        collections.takeTree(TransferService.ImportFromSpace(workspace, ids, parentID)),
        'transfer.importFailed'
      )
      if (!summary) return false
      toast.show(
        t('transfer.copied', {
          n: t('counts.collections', summary.collections),
          space: workspaceName,
        })
      )
      return true
    } finally {
      busy.value = false
    }
  }

  // The name the file was written under; an empty one is a cancelled save dialog, not an error.
  async function exportSave(
    id: string,
    format: TransferFormat,
    secrets: boolean
  ): Promise<boolean> {
    const written = await callOrToast(
      TransferService.Export(t('files.exportCollection'), id, format, secrets),
      'transfer.exportFailed'
    )
    if (!written) return false
    toast.show(t(secrets ? 'transfer.savedSecrets' : 'transfer.saved', { file: written }))
    return true
  }

  async function exportText(
    id: string,
    format: TransferFormat,
    secrets: boolean
  ): Promise<string | null> {
    const text = await callOrToast(
      TransferService.ExportText(id, format, secrets),
      'transfer.exportFailed'
    )
    return text ?? null
  }

  async function curlText(id: string, secrets: boolean): Promise<string | null> {
    const text = await callOrToast(TransferService.Curl(id, secrets), 'transfer.exportFailed')
    return text ?? null
  }

  async function copy(text: string, message: string): Promise<boolean> {
    await copyToClipboard(text)
    toast.show(message)
    return true
  }

  return {
    importOpen,
    importParent,
    linkedPage,
    exportOpen,
    exportTarget,
    spaces,
    busy,
    askImport,
    askImportPage,
    askExport,
    closeTransfer,
    previewFile,
    previewText,
    previewUrl,
    apply,
    loadSpaces,
    importFromSpace,
    exportSave,
    exportText,
    curlText,
    copy,
  }
})
