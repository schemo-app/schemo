<script setup lang="ts">
import { computed, onMounted } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useAccount } from '../../composables/useAccount'
import { useWorkspacesStore } from '../../stores/workspaces'
import { t } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const { gone, kept } = useSheetLines()
const { deleteAccount, busy } = useAccount()
const workspaces = useWorkspacesStore()

// How much of the account this device is holding, which is what "everything of it" means here. A
// workspace that never reached the account is this device's own work and stays where it is.
const here = computed(() => workspaces.list.filter((one) => one.remoteId).length)

// The list is a reading of what is held, and nothing tells this window when a workspace is made: it is
// read again as the sheet opens, so the count it names is the one that is about to go.
onMounted(() => void workspaces.load())

const rows = computed(() => [
  gone(t('counts.workspaces', here.value), t('account.deleteSheetWorkspacesNote')),
  gone(t('account.deleteSheetProfile'), t('account.deleteSheetProfileNote')),
  kept(t('workspaces.deleteSheetSettings'), t('account.deleteSheetSettingsNote')),
])
</script>

<template>
  <ConfirmSheet
    :title="t('account.deleteTitle')"
    :sub="t('account.deleteSub')"
    :rows
    :action="t('account.deleteAction')"
    :action-disabled="busy"
    action-tone="danger"
    @close="emit('close')"
    @confirm="deleteAccount()"
  />
</template>
