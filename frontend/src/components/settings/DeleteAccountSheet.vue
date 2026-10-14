<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { Sheet } from '../ui/sheet'
import { useAccount } from '../../composables/useAccount'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useMessages } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useMessages()
const { deleteAccount, busy } = useAccount()
const workspaces = useWorkspacesStore()

// How much of the account this device is holding, which is what "everything of it" means here. A
// space that never reached the account is this device's own work and stays where it is.
const here = computed(() => workspaces.list.filter((one) => one.remoteId).length)

// The list is a reading of what is held, and nothing tells this window when a space is made: it is
// read again as the sheet opens, so the count it names is the one that is about to go.
onMounted(() => void workspaces.load())

const rows = computed(() => [
  {
    label: t('counts.spaces', here.value),
    note: t('account.deleteSheetSpacesNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  {
    label: t('account.deleteSheetProfile'),
    note: t('account.deleteSheetProfileNote'),
    tag: t('common.deleted'),
    tone: 'drop' as const,
  },
  {
    label: t('workspaces.deleteSheetSettings'),
    note: t('account.deleteSheetSettingsNote'),
    tag: t('common.kept'),
    tone: 'keep' as const,
  },
])
</script>

<template>
  <Sheet
    :title="t('account.deleteTitle')"
    :sub="t('account.deleteSub')"
    :rows
    :cancel="t('common.cancel')"
    :action="t('account.deleteAction')"
    :action-disabled="busy"
    action-tone="danger"
    @close="emit('close')"
    @cancel="emit('close')"
    @action="deleteAccount()"
  />
</template>
