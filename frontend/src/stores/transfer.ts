import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { t } from '../i18n'
import { useToast } from '../composables/useToast'
import { markStep } from '../composables/useGettingStarted'
import { copyToClipboard } from '../lib/clipboard'
import {
  Transfer,
  type Preview,
  type Workspace,
} from '../../bindings/schemo/internal/convert'
import { callOrToast } from '../lib/calls'
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
  const sources = ref<Workspace[]>([])
  // Previews and writes in flight: the dialogs keep their buttons still while any is. A count rather
  // than a flag, since the first to come back must not release the buttons of one still running.
  const inFlight = ref(0)
  const busy = computed(() => inFlight.value > 0)

  async function whileBusy<T>(job: () => Promise<T>): Promise<T> {
    inFlight.value++
    try {
      return await job()
    } finally {
      inFlight.value--
    }
  }

  // Every way in — the menu, the key, a row of the tree — comes here, and an import writes into the
  // workspace: where it may not, the answer is the sentence the refusal would have been.
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

  // A failed read is a toast, except while somebody is still typing: a paste that does not read as
  // a command yet is not news, and a toast per keystroke would be. A nil preview is a cancelled
  // dialog, not a failure.
  async function readPreview(
    read: Promise<Preview | null>,
    quietly = false
  ): Promise<Preview | null> {
    if (!quietly) return (await callOrToast(read, 'transfer.readFailed')) ?? null
    try {
      return await read
    } catch {
      return null
    }
  }

  // Go reads the file behind the dialog.
  function previewFile(format: TransferFormat, title: string): Promise<Preview | null> {
    return whileBusy(() => readPreview(Transfer.PreviewFile(title, format)))
  }

  function previewPath(format: TransferFormat, path: string): Promise<Preview | null> {
    return whileBusy(() => readPreview(Transfer.PreviewPath(format, path)))
  }

  function previewText(
    format: TransferFormat,
    text: string,
    fileName: string,
    quietly = false
  ): Promise<Preview | null> {
    return whileBusy(() =>
      readPreview(Transfer.PreviewText(format, text, fileName), quietly)
    )
  }

  function previewUrl(
    format: TransferFormat,
    url: string,
    quietly = false
  ): Promise<Preview | null> {
    return whileBusy(() => readPreview(Transfer.PreviewUrl(format, url), quietly))
  }

  // What a preview read is written where the person chose: Go answers with the tree to draw and
  // the count the toast says.
  function apply(token: string, parentID: string, name: string): Promise<boolean> {
    return whileBusy(async () => {
      const summary = await callOrToast(
        collections.applyTree(Transfer.Apply(token, parentID, name)),
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
    })
  }

  async function loadWorkspaces(): Promise<Workspace[]> {
    const list = await callOrToast(Transfer.Workspaces(), 'transfer.readFailed')
    sources.value = list ?? []
    return sources.value
  }

  function importFromWorkspace(
    workspace: string,
    ids: string[],
    parentID: string,
    workspaceName: string
  ): Promise<boolean> {
    return whileBusy(async () => {
      const summary = await callOrToast(
        collections.applyTree(Transfer.ImportFromWorkspace(workspace, ids, parentID)),
        'transfer.importFailed'
      )
      if (!summary) return false
      toast.show(
        t('transfer.copied', {
          n: t('counts.collections', summary.collections),
          workspace: workspaceName,
        })
      )
      return true
    })
  }

  // The name the file was written under; an empty one is a cancelled save dialog, not an error.
  async function exportSave(
    id: string,
    format: TransferFormat,
    secrets: boolean
  ): Promise<boolean> {
    const written = await callOrToast(
      Transfer.Export(t('files.exportCollection'), id, format, secrets),
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
      Transfer.ExportText(id, format, secrets),
      'transfer.exportFailed'
    )
    return text ?? null
  }

  async function curlText(id: string, secrets: boolean): Promise<string | null> {
    const text = await callOrToast(Transfer.Curl(id, secrets), 'transfer.exportFailed')
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
    sources,
    busy,
    askImport,
    askImportPage,
    askExport,
    closeTransfer,
    previewFile,
    previewPath,
    previewText,
    previewUrl,
    apply,
    loadWorkspaces,
    importFromWorkspace,
    exportSave,
    exportText,
    curlText,
    copy,
  }
})
