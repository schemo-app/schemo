<script setup lang="ts">
import { computed } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useWorkspacesStore } from '../../stores/workspaces'
import { type MessageKey, t } from '../../i18n'

// What is about to go, before it goes. A workspace takes everything in it, and what it does *not*
// take is worth saying in the same breath: the theme, the language, the order of the panels and how
// long history is kept belong to the installation, not to a workspace, and the list says so.
const { leaving = false } = defineProps<{ leaving?: boolean }>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const workspaces = useWorkspacesStore()
const { gone, kept } = useSheetLines()

const held = computed(() => (workspaces.edited ? workspaces.countsOf(workspaces.edited.id) : null))

// The rows are the same either way (what leaves is what this device showed) and the note is where
// the two part: one promises it is gone, the other that it is gone from here and left standing for
// everybody who stayed.
const goneNote = (deletedKey: MessageKey) =>
  t(leaving ? 'workspaces.leaveSheetNote' : deletedKey)

const rows = computed(() => [
  gone(
    t('counts.collections', held.value?.collections ?? 0),
    goneNote('workspaces.deleteSheetCollectionsNote')
  ),
  gone(
    t('counts.environments', held.value?.environments ?? 0),
    goneNote('workspaces.deleteSheetEnvironmentsNote')
  ),
  gone(t('counts.runs', held.value?.runs ?? 0), goneNote('workspaces.deleteSheetRunsNote')),
  kept(t('workspaces.deleteSheetSettings'), t('workspaces.deleteSheetSettingsNote')),
])

// A workspace that is in the account goes further than this machine, and the sheet says the promise
// that will be kept: "it disappears from here" and "it disappears for everybody" are two different
// things to agree to, and only one is true of a workspace somebody made in the panel.
const sub = computed(() => {
  if (leaving) return t('workspaces.leaveSub')
  return t(workspaces.edited?.remoteId ? 'workspaces.deleteSubSynced' : 'workspaces.deleteSub')
})
</script>

<template>
  <ConfirmSheet
    :title="t(leaving ? 'workspaces.leaveTitle' : 'workspaces.deleteTitle')"
    :sub
    :rows
    :action="t(leaving ? 'workspaces.leaveAction' : 'workspaces.removeAction')"
    @close="emit('close')"
    @confirm="emit('confirm')"
  />
</template>
