import { computed, ref, watch, nextTick, shallowRef, type ComponentPublicInstance } from 'vue'
import type { TreeRow as Row } from '../lib/treeRows'
import { nextCollectionName } from '../lib/treeRows'
import { findCollection } from '../lib/collectionTree'
import type { useCollectionsStore } from '../stores/collections'
import { useTransferStore } from '../stores/transfer'
import { useWorkspacesStore } from '../stores/workspaces'
import { t } from '../i18n'

/** The method a request typed into the tree starts with. */
export const NEW_REQUEST_METHOD = 'GET'

// What the tree is writing: a rename, a new request, the confirmation before a collection goes. All
// three are also asked for from outside, so the store carries the request and this answers it.
export function useTreeEdits(
  collections: ReturnType<typeof useCollectionsStore>,
  opts: { rows: () => Row[] }
) {
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
  } = useInlineName<string>((id, name) => collections.rename(id, name))

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
  } = useInlineName<string>((collectionId, name) => collections.createNode(collectionId, name, NEW_REQUEST_METHOD))

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
    collections.expanded[collectionId] = true
    openCreating(collectionId, t('collections.newRequest'))
  }

  async function addCollection(parentId = '') {
    if (!workspaces.canWrite) return
    const level = parentId ? (findCollection(collections.tree, parentId)?.children ?? []) : collections.tree
    const name = nextCollectionName(level, t('collections.newCollection'), (n) =>
      t('collections.newCollectionN', { n })
    )
    await collections.createCollection(name, parentId)
  }

  async function runNode(row: Row) {
    await collections.select(row.id)
    await collections.run(collections.runNodeId, row.name)
  }

  function exportNode(row: Row) {
    transfer.askExport(row.id)
  }

  // Both arrive as a field on the store: the row they are about may not be on screen yet.
  watch(
    () => collections.pendingRename,
    (id) => {
      if (!id) return
      collections.pendingRename = null
      const row = opts.rows().find((row) => row.id === id)
      if (row) startRename(row)
    },
    { flush: 'post' }
  )

  watch(
    () => collections.pendingCreate,
    (collectionId) => {
      if (!collectionId) return
      collections.pendingCreate = null
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
    await collections.remove(row.id)
  }

  // The key that asks to remove a row has no menu to warn in, so it goes the long way: the count of
  // what goes with a collection is worth a dialog of its own.
  watch(
    () => collections.pendingRemove,
    (id) => {
      if (!id) return
      collections.pendingRemove = null
      const row = opts.rows().find((row) => row.id === id)
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
    commitRename,
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

/** Composable for managing inline text input fields (renaming, creation) with key bindings and focus handling. */
function useInlineName<T>(write: (target: T, name: string) => Promise<void>) {
  const target = shallowRef<T | null>(null)
  const draft = ref('')
  const invalid = ref(false)
  const input = ref<HTMLInputElement | null>(null)

  /** Template ref callback to bind the input element. */
  function setInput(el: Element | ComponentPublicInstance | null): void {
    input.value = (el as HTMLInputElement | null) ?? null
  }

  /** Opens the inline edit input for the given target and selects the initial text. */
  function open(at: T, initial: string): void {
    target.value = at
    draft.value = initial
    invalid.value = false

    nextTick(() => {
      requestAnimationFrame(() => input.value?.focus())
      input.value?.select()
    })
  }

  /** Validates and commits the entered name, invoking the write callback on success. */
  async function commit(): Promise<void> {
    const at = target.value
    if (at === null) return

    const name = draft.value.trim()
    if (!name) {
      invalid.value = true
      input.value?.focus()
      return
    }

    target.value = null
    invalid.value = false
    await write(at, name)
  }

  /** Cancels the edit operation and resets target state. */
  function cancel(): void {
    target.value = null
    invalid.value = false
  }

  /** Keydown handler for inline inputs handling Enter (commit) and Escape (cancel). */
  function onKeydown(e: KeyboardEvent): void {
    e.stopPropagation()
    if (e.key === 'Enter') {
      e.preventDefault()
      void commit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancel()
    }
  }

  return { target, draft, invalid, input, setInput, open, commit, cancel, onKeydown }
}
