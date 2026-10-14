<script setup lang="ts">
import { computed } from 'vue'
import { Sheet } from '../ui/sheet'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useMessages } from '../../i18n'

// What is about to go, before it goes. A workspace takes everything in it — and what it does *not*
// take is worth saying in the same breath: the theme, the language, the order of the panels and how
// long history is kept belong to the installation, not to a space, and the list says so rather than
// leaving the reader to wonder.
const { leaving = false } = defineProps<{ leaving?: boolean }>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const store = useWorkspacesStore()
const { t } = useMessages()

const held = computed(() => (store.edited ? store.countsOf(store.edited.id) : null))

// The rows are the same either way — what leaves is what this device showed — and the note is where
// the two part: one promises it is gone, the other that it is gone from here and left standing for
// everybody who stayed.
const rows = computed(() => [
  {
    label: t('counts.collections', held.value?.collections ?? 0),
    note: t(leaving ? 'workspaces.leaveSheetNote' : 'workspaces.deleteSheetCollectionsNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  {
    label: t('counts.environments', held.value?.environments ?? 0),
    note: t(leaving ? 'workspaces.leaveSheetNote' : 'workspaces.deleteSheetEnvironmentsNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  {
    label: t('counts.runs', held.value?.runs ?? 0),
    note: t(leaving ? 'workspaces.leaveSheetNote' : 'workspaces.deleteSheetRunsNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  {
    label: t('workspaces.deleteSheetSettings'),
    note: t('workspaces.deleteSheetSettingsNote'),
    tag: t('common.kept'),
    tone: 'keep' as const,
  },
])

// A space that is in the account goes further than this machine, and the sheet says the promise that
// will be kept: "it disappears from here" and "it disappears for everybody" are two different things
// to agree to, and only one of them is true of a space somebody made in the panel.
const sub = computed(() => {
  if (leaving) return t('workspaces.leaveSub')
  return t(store.edited?.remoteId ? 'workspaces.deleteSubSynced' : 'workspaces.deleteSub')
})
</script>

<template>
  <Sheet
    :title="t(leaving ? 'workspaces.leaveTitle' : 'workspaces.deleteTitle')"
    :sub
    :rows
    :cancel="t('common.cancel')"
    :action="t(leaving ? 'workspaces.leaveAction' : 'workspaces.removeAction')"
    @close="emit('close')"
    @cancel="emit('close')"
    @action="emit('confirm')"
  />
</template>
