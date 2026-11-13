
import {
  computed,
  nextTick,
  ref,
  shallowRef,
  watch,
  type ComponentPublicInstance,
} from 'vue'

import type { TreeRow as Row } from '../lib/treeRows'
import { nextCollectionName } from '../lib/treeRows'
import { findCollection } from '../lib/collectionTree'
import type { useCollectionsStore } from '../stores/collections'
import { useTransferStore } from '../stores/transfer'
import { useWorkspacesStore } from '../stores/workspaces'
import { t } from '../i18n'

export const NEW_REQUEST_METHOD = 'GET'

type CollectionsStore = ReturnType<typeof useCollectionsStore>
type TreeEditsOptions = {
  rows: () => Row[]
}

export function useTreeEdits(
    collections: CollectionsStore,
    { rows }: TreeEditsOptions,
) {
  const transfer = useTransferStore()
  const workspaces = useWorkspacesStore()

  const canWrite = () => workspaces.canWrite
  const findRow = (id: string) => rows().find(row => row.id === id)

  const {
    target: renamingId,
    draft: nameDraft,
    invalid: renameInvalid,
    setInput: setNameInput,
    open: openRename,
    commit: commitRename,
    cancel: cancelRename,
    onKeydown: onRenameKeydown,
  } = useInlineName<string>(
      (id, name) => collections.rename(id, name),
  )

  const {
    target: creatingIn,
    draft: creatingName,
    invalid: creatingInvalid,
    setInput: setCreatingInput,
    open: openCreating,
    commit: commitCreating,
    cancel: cancelCreating,
    onKeydown: onCreatingKeydown,
  } = useInlineName<string>(
      (id, name) => collections.createNode(id, name, NEW_REQUEST_METHOD),
  )

  const confirming = shallowRef<Row | null>(null)

  const creatingAt = computed(() => {
    const id = creatingIn.value
    if (!id) return null

    const items = rows()
    const index = items.findIndex(row => row.id === id)
    if (index === -1) return null

    const depth = items[index].depth
    let end = index

    while (
        end + 1 < items.length &&
        items[end + 1].depth > depth
        ) {
      end++
    }

    return {
      after: items[end].id,
      depth: depth + 1,
    }
  })

  function startRename(row: Row): void {
    if (!canWrite()) return

    cancelCreating()
    openRename(row.id, row.name)
  }

  function commitRenameFrom(id: string): void {
    if (renamingId.value === id) {
      void commitRename()
    }
  }

  function startCreating(collectionId: string): void {
    if (!canWrite()) return

    cancelRename()
    collections.expanded[collectionId] = true
    openCreating(collectionId, t('collections.newRequest'))
  }

  async function addCollection(parentId = ''): Promise<void> {
    if (!canWrite()) return

    const siblings = parentId
        ? findCollection(collections.tree, parentId)?.children ?? []
        : collections.tree

    const name = nextCollectionName(
        siblings,
        t('collections.newCollection'),
        n => t('collections.newCollectionN', { n }),
    )

    await collections.createCollection(name, parentId)
  }

  async function runNode(row: Row): Promise<void> {
    await collections.select(row.id)
    await collections.run(collections.runNodeId, row.name)
  }

  function exportNode(row: Row): void {
    transfer.askExport(row.id)
  }

  function askRemove(row: Row): void {
    if (!canWrite()) return

    confirming.value = row
  }

  async function remove(row: Row): Promise<void> {
    if (!canWrite()) return

    await collections.remove(row.id)
    confirming.value = null
  }

  function cancelTop(): boolean {
    if (creatingIn.value !== null) {
      cancelCreating()
      return true
    }

    if (renamingId.value !== null) {
      cancelRename()
      return true
    }

    if (confirming.value !== null) {
      confirming.value = null
      return true
    }

    return false
  }

  // Handle actions requested by the store.
  watch(
      [() => collections.pendingRename, rows],
      ([id]) => {
        if (!id || !canWrite()) return

        const row = findRow(id)
        if (!row) return

        collections.pendingRename = null
        startRename(row)
      },
      { flush: 'post', immediate: true },
  )

  watch(
      () => collections.pendingCreate,
      id => {
        if (!id || !canWrite()) return

        collections.pendingCreate = null
        startCreating(id)
      },
      { flush: 'post', immediate: true },
  )

  watch(
      [() => collections.pendingRemove, rows],
      ([id]) => {
        if (!id || !canWrite()) return

        const row = findRow(id)
        if (!row) return

        collections.pendingRemove = null
        askRemove(row)
      },
      { flush: 'post', immediate: true },
  )

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

/**
 * Manages an inline name editor with validation,
 * keyboard shortcuts and automatic focus.
 */
function useInlineName<T>(
    write: (target: T, name: string) => Promise<unknown>,
) {
  const target = shallowRef<T | null>(null)
  const draft = ref('')
  const invalid = ref(false)
  const input = shallowRef<HTMLInputElement | null>(null)
  const saving = ref(false)

  function setInput(
      element: Element | ComponentPublicInstance | null,
  ): void {
    const node = element instanceof Element
        ? element
        : element?.$el

    input.value = node instanceof HTMLInputElement
        ? node
        : null
  }

  function open(at: T, initial: string): void {
    if (saving.value) return

    target.value = at
    draft.value = initial
    invalid.value = false

    void nextTick(() => {
      if (target.value !== at) return

      input.value?.focus()
      input.value?.select()
    })
  }

  async function commit(): Promise<void> {
    const at = target.value
    if (at === null || saving.value) return

    const name = draft.value.trim()

    if (!name) {
      invalid.value = true
      input.value?.focus()
      return
    }

    saving.value = true
    invalid.value = false

    try {
      await write(at, name)
      target.value = null
    } finally {
      saving.value = false
    }
  }

  function cancel(): void {
    if (saving.value) return

    target.value = null
    invalid.value = false
  }

  function onKeydown(event: KeyboardEvent): void {
    event.stopPropagation()

    if (event.key === 'Escape') {
      event.preventDefault()
      cancel()
      return
    }

    if (event.key === 'Enter' && !event.isComposing) {
      event.preventDefault()
      void commit().catch(console.error)
    }
  }

  return {
    target,
    draft,
    invalid,
    input,
    saving,
    setInput,
    open,
    commit,
    cancel,
    onKeydown,
  }
}