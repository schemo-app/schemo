<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useEnvironmentsStore } from './stores/environments'
import { useRecordsStore } from './stores/records'
import { useCollectionsStore } from './stores/collections'
import { answerUnsaved, leavePending } from './stores/content'
import { useSearchStore } from './stores/search'
import { useWorkspacesStore } from './stores/workspaces'
import { useCookiesStore } from './stores/cookies'
import { callOrToast } from './stores/calls'
import { useAppEvents } from './composables/events/useAppEvents'
import { useGlobalShortcuts } from './composables/useGlobalShortcuts'
import { useCollectionKeys } from './composables/tree/useCollectionKeys'
import { useSessionPersistence } from './composables/useSessionPersistence'
import { useUpdateCheck } from './composables/useUpdateCheck'
import { useNativeMenu } from './composables/useNativeMenu'
import { focusUrlField } from './composables/intents'
import { SystemService } from '../bindings/schemo/internal/transport/wails'
import type { StartupStatus } from '../bindings/schemo/internal/transport/desktop'
import { useMessages } from './i18n'
import { usePlan } from './composables/usePlan'
import { useSidebarToggle } from './composables/useSidebarToggle'
import Sidebar from './components/layout/Sidebar.vue'
import AppHeader from './components/layout/AppHeader.vue'
import Workspace from './components/layout/Workspace.vue'
import StatusBar from './components/layout/StatusBar.vue'
import EnvironmentsSheet from './components/environments/EnvironmentsSheet.vue'
import SearchPalette from './components/search/SearchPalette.vue'
import WorkspacesSheet from './components/workspaces/WorkspacesSheet.vue'
import CookieManagerSheet from './components/cookies/CookieManagerSheet.vue'
import UnsavedDialog from './components/ui/UnsavedDialog.vue'
import SignInModal from './components/settings/SignInModal.vue'
import SignOutSheet from './components/settings/SignOutSheet.vue'
import McpConfirmSheet from './components/mcp/McpConfirmSheet.vue'
import UnpublishDialog from './components/publications/UnpublishDialog.vue'
import ImportDialog from './components/transfer/ImportDialog.vue'
import ExportDialog from './components/transfer/ExportDialog.vue'
import { useAccount } from './composables/useAccount'
import Toast from './components/ui/Toast.vue'
import { Button } from './components/ui/button'

const { t, te } = useMessages()
const { loadPlan } = usePlan()

const store = useRecordsStore()
const collections = useCollectionsStore()
const envStore = useEnvironmentsStore()
const search = useSearchStore()
const workspaces = useWorkspacesStore()
const cookies = useCookiesStore()
const { phase: updatePhase, latest: updateVersion } = useUpdateCheck()
const availableVersion = computed(() => (updatePhase.value === 'available' ? updateVersion.value : ''))
const { hidden: sidebarHidden } = useSidebarToggle()
const {
  state: accountState,
  signInOpen,
  signOutOpen,
  beginSignIn,
  cancelSignIn,
  closeSignIn,
  closeSignOut,
  signOut,
  checkServer,
} = useAccount()
const account = computed(() => accountState.value?.account ?? null)
const startup = ref<StartupStatus | null>(null)
const retrying = ref(false)

async function loadStartup() {
  startup.value = (await callOrToast(SystemService.StartupStatus(), 'startup.readFailed')) ?? null
}

async function retryInit() {
  retrying.value = true
  try {
    startup.value = (await callOrToast(SystemService.RetryInit(), 'startup.readFailed')) ?? startup.value
    if (startup.value?.ready) await initializeSession()
  } finally {
    retrying.value = false
  }
}

const startupTitle = computed(() => {
  const kind = startup.value?.failure?.kind
  const key = `startup.failed.${kind}`
  return kind && te(key) ? t(key) : t('startup.title')
})

const folderRefused = computed(() => startup.value?.failure?.kind === 'data-dir')

async function initializeSession() {
  await Promise.all([
    callOrToast(loadSession(), 'startup.readFailed'),
    loadPlan(),
    checkServer(),
    collections.load(),
  ])
}

onMounted(async () => {
  await loadStartup()
  if (startup.value?.ready) await initializeSession()
})

function openAbout() {
  SystemService.ShowAbout()
}

const loadSession = useSessionPersistence(store, envStore)
useAppEvents(store, collections, () => startup.value?.ready === true)
useGlobalShortcuts(envStore)
useCollectionKeys()
useNativeMenu()

function closeSheet() {
  envStore.closeSheet()
  focusUrlField()
}
</script>

<template>
  <div class="app">
    <div v-if="startup && !startup.ready" class="startup-failure">
      <div class="startup-title">{{ startupTitle }}</div>
      <p v-if="!folderRefused" class="startup-hint">{{ t('startup.hint') }}</p>
      <pre v-if="startup.failure?.detail" class="startup-detail">{{ startup.failure.detail }}</pre>
      <div class="startup-actions">
        <Button variant="primary" :disabled="retrying" @click="retryInit">
          {{ retrying ? t('startup.retrying') : t('startup.retry') }}
        </Button>
        <Button v-if="!folderRefused" variant="outline" @click="SystemService.OpenDataFolder()">
          {{ t('startup.openDataFolder') }}
        </Button>
      </div>
      <div v-if="!folderRefused" class="startup-path">{{ startup.dbPath }}</div>
    </div>

    <template v-else-if="startup?.ready">
      <div class="body">
        <Sidebar v-if="!sidebarHidden" />
        <div class="main-col">
          <AppHeader />
          <Workspace />
        </div>
      </div>
      <StatusBar :update-version="availableVersion" @open-update="openAbout" />
    </template>
  </div>

  <EnvironmentsSheet v-if="startup?.ready && envStore.sheetOpen" @close="closeSheet" />
  <SearchPalette v-if="startup?.ready" :open="search.open" @close="search.close()" />
  <WorkspacesSheet v-if="startup?.ready && workspaces.sheetOpen" @close="workspaces.closeSheet()" />
  <CookieManagerSheet v-if="startup?.ready && cookies.sheetOpen" @close="cookies.closeSheet()" />

  <SignInModal
    v-if="startup?.ready && signInOpen"
    :server="account?.server ?? ''"
    @begin="beginSignIn"
    @cancel="cancelSignIn"
    @close="closeSignIn()"
  />
  <SignOutSheet v-if="startup?.ready && signOutOpen" @close="closeSignOut()" @confirm="signOut()" />
  <McpConfirmSheet v-if="startup?.ready" />
  <!-- A page is taken down from wherever it was asked about — a collection's own head or the list
       of pages — so the question stands outside both. -->
  <UnpublishDialog v-if="startup?.ready" />
  <ImportDialog v-if="startup?.ready" />
  <ExportDialog v-if="startup?.ready" />
  <!-- A card with unsaved edits is asked about when its tab is closed — a tab left open is not
       left behind, and the command line's own draft keeps itself. -->
  <UnsavedDialog
    v-if="startup?.ready"
    :open="leavePending !== null"
    :title="t('collections.unsavedTitle', { name: leavePending?.name })"
    :hint="t('collections.unsavedHint')"
    can-save
    @cancel="answerUnsaved('cancel')"
    @discard="answerUnsaved('discard')"
    @save="answerUnsaved('save')"
  />

  <Toast />
</template>

<style scoped>
@reference "./style.css";

.main-col {
  @apply flex-1 min-w-0 min-h-0 flex flex-col;
}

.startup-failure {
  @apply flex-1 flex flex-col items-center justify-center gap-3 px-8 min-h-0 bg-bg;
}

.startup-title {
  @apply text-14 font-semibold text-text;
}

.startup-hint {
  @apply text-13 text-text-secondary text-center max-w-105;
}

.startup-detail {
  @apply max-w-130 max-h-40 overflow-auto text-12 text-text-tertiary;
  font-family: var(--mono);
  background: var(--bg-sidebar);
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  padding: 0.5rem 0.625rem;
  white-space: pre-wrap;
}

.startup-actions {
  @apply flex items-center gap-2 mt-1;
}

.startup-path {
  @apply text-11 text-text-tertiary;
  font-family: var(--mono);
}
</style>
