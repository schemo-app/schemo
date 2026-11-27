import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { t } from '../i18n'
import { useToast } from '../composables/useToast'
import { markStep } from '../composables/useGettingStarted'
import { copyToClipboard } from '../lib/clipboard'
import {
  Specs,
  Transfer,
  type Preview,
  type SpecReview,
  type Workspace,
} from '../../bindings/schemo/internal/convert'
import type { Choices } from '../../bindings/schemo/internal/convert/merge'
import type { SpecSource } from '../../bindings/schemo/internal/domain'
import { callOrToast } from '../lib/calls'
import { requestIds } from '../lib/collectionTree'
import type { RequestInstance } from './content'
import { useCollectionsStore } from './collections'
import { useEnvironmentsStore } from './environments'
import { useWorkspacesStore } from './workspaces'

/** Format names as Go knows them; `page` is import-only, since publishing is how a page is made. */
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
  // The collection being updated from its description; '' while that dialog is closed.
  const specCollection = ref('')
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
    specCollection.value = ''
    importOpen.value = true
  }

  function askSpec(id: string) {
    if (!workspaces.canWrite) {
      toast.show(t('errors.codes.workspaceReadOnly'))
      return
    }
    closeTransfer()
    specCollection.value = id
  }

  // The dialog reads the link at once, so following it and importing is one step.
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
    specCollection.value = ''
    linkedPage.value = ''
  }

  // No toast while typing (a half-pasted command is not news); a nil preview means cancelled.
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

  function pickImport(format: TransferFormat, title: string): Promise<Preview | null> {
    return whileBusy(() => readPreview(Transfer.PickImport(format, title)))
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
        collections.applyTree(Transfer.Apply(token, parentID, name, t('transfer.importedSuffix'))),
        'transfer.importFailed'
      )
      if (!summary) return false
      markStep('import')
      if (summary.envState) await environments.applyState(summary.envState)
      // One toast: what was imported, then the environments it created.
      let said: string
      if (summary.collections === 0) {
        said = t('transfer.importedCount', { n: t('counts.environments', summary.environments) })
      } else if (summary.collections > 1) {
        said = t('transfer.importedCount', { n: t('counts.collections', summary.collections) })
      } else {
        said = t('transfer.imported', { n: t('counts.requests', summary.requests), name: summary.name || name })
      }
      const created = summary.createdEnvironments ?? []
      if (summary.collections > 0 && created.length > 0) {
        said += ' · ' + t('transfer.createdEnvironments', { names: created.join(', '), count: created.length })
      }
      toast.show(said)
      return true
    })
  }

  async function specSource(id: string): Promise<SpecSource | null> {
    return (await callOrToast(Specs.Linked(id), 'spec.readFailed')) ?? null
  }

  // A review is null when reading failed or the file dialog was closed.
  function specReview(read: () => Promise<SpecReview | null>): Promise<SpecReview | null> {
    return whileBusy(async () => (await callOrToast(read(), 'spec.readFailed')) ?? null)
  }

  // Open tabs of the collection are read again, so that saving one cannot bring back what the
  // update replaced. The dialog does not apply while one of them has unsaved changes.
  function specApply(token: string, choices: Choices, editors: RequestInstance[]): Promise<boolean> {
    return whileBusy(async () => {
      await Promise.all(editors.map((editor) => editor.flushExamples()))
      const summary = await callOrToast(
        collections.applyTree(Specs.Apply(token, choices)),
        'spec.applyFailed'
      )
      if (!summary) return false
      if (summary.envState) await environments.applyState(summary.envState)
      const left = requestIds(summary.tree ?? [])
      await Promise.all(
        editors
          .filter((editor) => !editor.unsaved.value && left.has(editor.node.value?.node.id ?? ''))
          .map((editor) => editor.discardNode())
      )
      if (summary.added + summary.updated + summary.removed) toast.show(t('spec.updated'))
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
    specCollection,
    sources,
    busy,
    askImport,
    askSpec,
    specSource,
    specReview,
    specApply,
    askImportPage,
    askExport,
    closeTransfer,
    pickImport,
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
