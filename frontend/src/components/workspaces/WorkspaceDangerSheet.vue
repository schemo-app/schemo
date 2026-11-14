<script setup lang="ts">
import { computed } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useWorkspacesStore } from '../../stores/workspaces'
import { type MessageKey, t } from '../../i18n'

const { leaving = false } = defineProps<{ leaving?: boolean }>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const workspaces = useWorkspacesStore()
const { gone, kept } = useSheetLines()

const held = computed(() => (workspaces.edited ? workspaces.countsOf(workspaces.edited.id) : null))

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

// A cloud workspace disappears for everybody, unless this person is only leaving it.
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
