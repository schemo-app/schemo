import { computed, ref, watch } from 'vue'
import type { TreeRow as Row } from '../../lib/treeRows'
import { nextCollectionName } from '../../lib/treeRows'
import { findCollection } from '../../lib/collectionTree'
import { useInlineName } from '../useInlineName'
import type { useCollectionsStore } from '../../stores/collections'
import { useTransferStore } from '../../stores/transfer'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useMessages } from '../../i18n'

/** The method a request typed into the tree starts with. */
export const NEW_REQUEST_METHOD = 'GET'

// What the tree is writing: a rename, a new request, the confirmation before a collection goes. All
// three are also asked for from outside, so the store carries the request and this answers it.
export function useTreeEdits(
  store: ReturnType<typeof useCollectionsStore>,
  opts: { rows: () => Row[] }
) {
  const { t } = useMessages()
  const transfer = useTransferStore()
  const workspaces = useWorkspacesStore()

  const {
    target: renamingId,
    draft: nameDraft,
    invalid: renameInvalid,
    setInput: setNameInput,
    open: openRename,
    commit: commitRename,
    cancel: cancelRename,
    onKeydown: onRenameKeydown,
  } = useInlineName<string>((id, name) => store.rename(id, name))

  function startRename(row: Row) {
    if (!workspaces.canWrite) return
    openRename(row.id, row.name)
  }

  function commitRenameFrom(id: string) {
    if (renamingId.value === id) void commitRename()
  }

  const {
    target: creatingIn,
    draft: creatingName,
    invalid: creatingInvalid,
    setInput: setCreatingInput,
    open: openCreating,
    commit: commitCreating,
    cancel: cancelCreating,
    onKeydown: onCreatingKeydown,
  } = useInlineName<string>((collectionId, name) => store.createNode(collectionId, name, NEW_REQUEST_METHOD))

  // Where the line for a new request stands: under the whole of its collection, indented one step.
  const creatingAt = computed<{ after: string; depth: number } | null>(() => {
    const target = creatingIn.value
    if (!target) return null

    const rows = opts.rows()
    const at = rows.findIndex((row) => row.id === target)
    if (at < 0) return null

    let end = at
    while (end + 1 < rows.length && rows[end + 1].depth > rows[at].depth) end += 1
    return { after: rows[end].id, depth: rows[at].depth + 1 }
  })

  function startCreating(collectionId: string) {
    if (!workspaces.canWrite) return
    store.expanded[collectionId] = true
    openCreating(collectionId, t('collections.newRequest'))
  }

  async function addCollection(parentId = '') {
    if (!workspaces.canWrite) return
    const level = parentId ? (findCollection(store.tree, parentId)?.children ?? []) : store.tree
    const name = nextCollectionName(level, t('collections.newCollection'), (n) =>
      t('collections.newCollectionN', { n })
    )
    await store.createCollection(name, parentId)
  }

  async function runNode(row: Row) {
    await store.select(row.id)
    await store.run(store.runNodeId, row.name)
  }

  function exportNode(row: Row) {
    transfer.askExport(row.id)
  }

  // Both arrive as a field on the store: the row they are about may not be on screen yet.
  watch(
    () => store.pendingRename,
    (id) => {
      if (!id) return
      store.pendingRename = null
      const row = opts.rows().find((r) => r.id === id)
      if (row) startRename(row)
    },
    { flush: 'post' }
  )

  watch(
    () => store.pendingCreate,
    (collectionId) => {
      if (!collectionId) return
      store.pendingCreate = null
      startCreating(collectionId)
    },
    { flush: 'post' }
  )

  const confirming = ref<Row | null>(null)

  async function askRemove(row: Row) {
    if (!workspaces.canWrite) return
    if (row.kind === 'collection' && row.count > 1) {
      confirming.value = row
      return
    }
    await remove(row)
  }

  async function remove(row: Row) {
    confirming.value = null
    await store.remove(row.id)
  }

  // The key that asks to remove a row has no menu to warn in, so it goes the long way: the count of
  // what goes with a collection is worth a dialog of its own.
  watch(
    () => store.pendingRemove,
    (id) => {
      if (!id) return
      store.pendingRemove = null
      const row = opts.rows().find((r) => r.id === id)
      if (row) void askRemove(row)
    },
    { flush: 'post' }
  )

  // Escape belongs to the name being written, then the confirmation, then the window.
  function cancelTop(): boolean {
    if (creatingIn.value) {
      cancelCreating()
      return true
    }
    if (renamingId.value) {
      cancelRename()
      return true
    }
    if (confirming.value) {
      confirming.value = null
      return true
    }
    return false
  }

  return {
    renamingId,
    nameDraft,
    renameInvalid,
    setNameInput,
    onRenameKeydown,
    startRename,
    commitRenameFrom,
    creatingIn,
    creatingName,
    creatingInvalid,
    setCreatingInput,
    onCreatingKeydown,
    commitCreating,
    creatingAt,
    startCreating,
    addCollection,
    runNode,
    exportNode,
    remove,
    confirming,
    askRemove,
    cancelTop,
  }
}
