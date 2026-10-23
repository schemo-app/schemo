<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef } from 'vue'
import WorkspaceCreatePane from './WorkspaceCreatePane.vue'
import WorkspaceDetailPane from './WorkspaceDetailPane.vue'
import WorkspaceDangerSheet from './WorkspaceDangerSheet.vue'
import WorkspaceList from './WorkspaceList.vue'
import TwoPaneSheet from '../ui/TwoPaneSheet.vue'
import { useWorkspacesStore } from '../../stores/workspaces'
import { enterWorkspace } from '../../lib/switchWorkspace'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { usePlan } from '../../composables/usePlan'
import { describeFailure, t } from '../../i18n'

const { refreshPlan } = usePlan()

// The manager window: the workspaces on the left and, on the right, either the form that makes a new
// one or the workspace being looked at. It holds what neither side can hold alone: which pane the
// right-hand side shows, and the confirmation that stands over everything before a workspace is taken
// away. It is also where deleting the workspace *on screen* stops the panels below showing a workspace
// that no longer exists.
const emit = defineEmits<{ (e: 'close'): void }>()

const workspaces = useWorkspacesStore()
const { setNotice, clearNotice } = useSheetNotice()

const detailRef = useTemplateRef<InstanceType<typeof WorkspaceDetailPane>>('detailRef')
// What the confirmation is about: ending the workspace for everybody, or stepping out of somebody
// else's. The two are different promises, and the sheet words the one that is being made.
const confirming = ref<'end' | 'leave' | null>(null)

const creating = computed(() => workspaces.creating)
const workspace = computed(() => workspaces.edited)

// While a workspace is being made the window is about that and nothing else — the list steps aside —
// so the window's own title says so rather than a heading inside the form repeating it.
const title = computed(() => t(creating.value ? 'workspaces.newTitle' : 'workspaces.title'))

function select(id: string) {
  workspaces.edit(id)
  clearNotice()
}

// The counts are a reading of what other features hold, and nothing tells this window when a
// collection is made, so the window reads them again when it opens rather than drawing what the
// session started with.
onMounted(() => {
  clearNotice()
  void workspaces.load()
  // What the plan carries is asked again as this window opens, which is when the answer is looked
  // at: the choice of what a workspace is meant to be is drawn here, and a tariff bought in the panel
  // since the app started should already be offered.
  void refreshPlan()
})

async function confirmDanger() {
  const doing = confirming.value
  confirming.value = null
  const target = workspace.value
  if (!target) return

  const wasActive = target.id === workspaces.activeId
  try {
    if (doing === 'leave') await workspaces.leave(target.id)
    else await workspaces.remove(target.id)
  } catch (error) {
    setNotice(describeFailure(error))
    return
  }

  // The workspace on screen went with it: everything below this window belonged to it, so it is emptied and
  // read again — the same gesture as a switch, and the same function.
  if (wasActive) await enterWorkspace()

  // The pane moves to what is left rather than pointing at a row that is gone.
  workspaces.edit(workspaces.activeId)
}

// Escape: the form behind it first, then the field being edited. The confirmation is a dialog of
// its own and the primitive gives the key to the top layer first.
function cancelTop(): boolean {
  if (creating.value) {
    workspaces.cancelCreating()
    return true
  }
  return detailRef.value?.cancelTop() ?? false
}
</script>

<template>
  <TwoPaneSheet :title="title" :cancel-top="cancelTop" @close="emit('close')">
    <template #default>
      <!-- The list steps aside while a new workspace is being made: the form is the only thing to do at that
      moment, and a row to click beside it would only take the room the name and the colour want. Cancel
      and Escape are the way back, and they are both on screen. -->
      <WorkspaceList v-if="!creating" @select="select" @create="workspaces.openCreate()" />

      <WorkspaceCreatePane
        v-if="creating"
        @cancel="workspaces.cancelCreating()"
        @created="clearNotice()"
      />
      <WorkspaceDetailPane
        v-else
        ref="detailRef"
        @danger="confirming = 'end'"
        @leave="confirming = 'leave'"
      />
    </template>

    <template #over>
      <WorkspaceDangerSheet
        v-if="confirming"
        :leaving="confirming === 'leave'"
        @close="confirming = null"
        @confirm="confirmDanger"
      />
    </template>
  </TwoPaneSheet>
</template>
