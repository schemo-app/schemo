import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { Workspaces } from '../../bindings/schemo/internal/workspace'
import type {
  Workspace,
  WorkspaceCounts,
  WorkspaceState,
} from '../../bindings/schemo/internal/domain'
import type {
  WorkspaceInput,
  InviteOutcome,
  WorkspacePatch,
} from '../../bindings/schemo/internal/workspace'
import { t as tr } from '../i18n'
import { markStep } from '../composables/useGettingStarted'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'

export type CreateResult = { id: string; invites: InviteOutcome[] }

export const useWorkspacesStore = defineStore('workspaces', () => {
  const snapshot = useWorkspaceSnapshot<WorkspaceState | null>(null)
  const state = snapshot.state
  const sheetOpen = ref(false)
  const creating = ref(false)
  const editedId = ref<string | null>(null)

  const list = computed(() => state.value?.workspaces ?? [])
  const activeId = computed(() => state.value?.activeId ?? '')
  const personalId = computed(() => state.value?.personalId ?? '')
  const active = computed(() => list.value.find((one) => one.id === activeId.value) ?? null)
  const edited = computed(() => {
    const id = editedId.value ?? activeId.value
    return list.value.find((one) => one.id === id) ?? null
  })
  const counts = computed<Record<string, WorkspaceCounts | undefined>>(() => state.value?.counts ?? {})

  // Whether this person may change what the active workspace holds. Go refuses every write there, so this
  // is only what the window already knows: a control that would be answered with a refusal is drawn
  // off instead.
  const canWrite = computed(() => active.value?.access.write ?? true)

  async function load() {
    await snapshot.take(Workspaces.Snapshot())
  }

  async function switchTo(id: string) {
    await snapshot.take(Workspaces.Switch(id))
  }

  async function create(input: WorkspaceInput): Promise<CreateResult> {
    const before = new Set(list.value.map((one) => one.id))
    const answer = await snapshot.take(Workspaces.Create(input))
    const workspaces = answer.workspaces ?? list.value
    const made = workspaces.find((workspace) => !before.has(workspace.id))
    if (made) markStep('ws')
    if (state.value === answer) {
      editedId.value = made?.id ?? null
      creating.value = false
    }
    return { id: made?.id ?? '', invites: answer.invites ?? [] }
  }

  async function update(id: string, patch: WorkspacePatch) {
    await snapshot.take(Workspaces.Update(id, patch))
  }

  async function remove(id: string) {
    await snapshot.take(Workspaces.Delete(id))
  }

  // The exit of somebody who is only a member: the workspace stays for everybody else, and this device
  // stops holding it.
  async function leave(id: string) {
    await snapshot.take(Workspaces.Leave(id))
  }

  // A workspace Go has not counted yet reads as empty.
  function countsOf(id: string): WorkspaceCounts {
    return counts.value[id] ?? { collections: 0, environments: 0, runs: 0 }
  }

  function openSheet(id: string | null = null) {
    editedId.value = id ?? activeId.value
    creating.value = false
    sheetOpen.value = true
  }

  function openCreate() {
    creating.value = true
    editedId.value = activeId.value
    sheetOpen.value = true
  }

  function edit(id: string) {
    editedId.value = id
    creating.value = false
  }

  function cancelCreating() {
    creating.value = false
  }

  function closeSheet() {
    sheetOpen.value = false
    creating.value = false
  }

  return {
    state,
    sheetOpen,
    creating,
    editedId,
    list,
    activeId,
    personalId,
    canWrite,
    active,
    edited,
    counts,
    load,
    switch: switchTo,
    create,
    update,
    remove,
    leave,
    countsOf,
    openSheet,
    openCreate,
    edit,
    cancelCreating,
    closeSheet,
  }
})

// Returns workspace name or translated default if empty
export function workspaceName(workspace: Workspace): string {
  return workspace.name || tr('workspaces.personal')
}

// First character of the workspace name for UI avatar fallback
export function workspaceInitial(workspace: Workspace): string {
  return workspaceName(workspace).trim().charAt(0).toUpperCase() || '·'
}
