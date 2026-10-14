import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { WorkspaceService } from '../../bindings/schemo/internal/transport/wails'
import type {
  Workspace,
  WorkspaceCounts,
  WorkspaceState,
} from '../../bindings/schemo/internal/domain'
import type {
  CreateInput,
  InviteOutcome,
  Patch,
} from '../../bindings/schemo/internal/usecase/workspace'
import { t as tr } from '../i18n'
import { markStep } from '../composables/useGettingStarted'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'

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
  const active = computed(() => list.value.find((w) => w.id === activeId.value) ?? null)
  const edited = computed(() => {
    const id = editedId.value ?? activeId.value
    return list.value.find((w) => w.id === id) ?? null
  })
  const counts = computed<Record<string, WorkspaceCounts | undefined>>(() => state.value?.counts ?? {})

  // Whether this person may change what the active space holds. Go refuses every write there, so this
  // is only what the window already knows: a control that would be answered with a refusal is drawn
  // off instead.
  const canWrite = computed(() => active.value?.access.write ?? true)

  async function load() {
    await snapshot.take(WorkspaceService.Snapshot())
  }

  async function switchTo(id: string) {
    await snapshot.take(WorkspaceService.Switch(id))
  }

  async function create(input: CreateInput): Promise<CreateResult> {
    const before = new Set(list.value.map((w) => w.id))
    const answer = await snapshot.take(WorkspaceService.Create(input))
    const workspaces = answer.workspaces ?? list.value
    const made = workspaces.find((w) => !before.has(w.id))
    if (made) markStep('ws')
    if (state.value === answer) {
      editedId.value = made?.id ?? null
      creating.value = false
    }
    return { id: made?.id ?? '', invites: answer.invites ?? [] }
  }

  async function update(id: string, patch: Patch) {
    await snapshot.take(WorkspaceService.Update(id, patch))
  }

  async function remove(id: string) {
    await snapshot.take(WorkspaceService.Delete(id))
  }

  // The exit of somebody who is only a member: the space stays for everybody else, and this device
  // stops holding it.
  async function leave(id: string) {
    await snapshot.take(WorkspaceService.Leave(id))
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
