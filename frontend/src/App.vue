<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useEnvironmentsStore } from './stores/environments'
import { useRecordsStore } from './stores/records'
import { useCollectionsStore } from './stores/collections'
import { answerUnsaved, leavePending } from './stores/content'
import { useSearchStore } from './stores/search'
import { useWorkspacesStore } from './stores/workspaces'
import { useCookiesStore } from './stores/cookies'
import { callOrToast } from './lib/calls'
import { useAppEvents } from './composables/useAppEvents'
import { useSessionPersistence } from './composables/useSessionPersistence'
import { useUpdateCheck } from './composables/useUpdateCheck'
import { focusUrlField } from './lib/intents'
import { System } from '../bindings/schemo/internal/app'
import type { StartupStatus, MenuGroup as NativeMenu } from '../bindings/schemo/internal/desktop'
import { t, te } from './i18n'
import { usePlan } from './composables/usePlan'
import { useSidebarToggle } from './composables/useSidebarToggle'
import AppSidebar from './components/layout/AppSidebar.vue'
import AppSidebarRail from './components/layout/AppSidebarRail.vue'
import AppHeader from './components/layout/AppHeader.vue'
import AppContent from './components/layout/AppContent.vue'
import AppStatusBar from './components/layout/AppStatusBar.vue'
import EnvironmentsSheet from './components/environments/EnvironmentsSheet.vue'
import SearchPalette from './components/search/SearchPalette.vue'
import WorkspacesSheet from './components/workspaces/WorkspacesSheet.vue'
import CookiesSheet from './components/cookies/CookiesSheet.vue'
import UnsavedDialog from './components/ui/UnsavedDialog.vue'
import SignInModal from './components/cloud/SignInModal.vue'
import SignOutSheet from './components/cloud/SignOutSheet.vue'
import McpConfirmSheet from './components/settings/McpConfirmSheet.vue'
import UnpublishDialog from './components/cloud/UnpublishDialog.vue'
import ImportDialog from './components/transfer/ImportDialog.vue'
import ExportDialog from './components/transfer/ExportDialog.vue'
import { useAccount } from './composables/useAccount'
import Toast from './components/ui/Toast.vue'
import Button from './components/ui/Button.vue'
import { useEventListener } from '@vueuse/core'
import { Updates } from '../bindings/schemo/internal/desktop'
import { useTabsStore } from './stores/tabs'
import { useTransferStore } from './stores/transfer'
import { useCommands } from './composables/useCommands'
import { usePlatform } from './composables/usePlatform'
import { isRemoveEvent, isRenameEvent } from './lib/treeKeys'
import { useWailsEvent } from './composables/useWailsEvent'
import { useAppMenu } from './composables/useAppMenu'

const { loadPlan } = usePlan()

const records = useRecordsStore()
const collections = useCollectionsStore()
const environments = useEnvironmentsStore()
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
  startup.value = (await callOrToast(System.StartupStatus(), 'startup.readFailed')) ?? null
}

async function retryInit() {
  retrying.value = true
  try {
    startup.value = (await callOrToast(System.RetryInit(), 'startup.readFailed')) ?? startup.value
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

const loadSession = useSessionPersistence(records, environments)

// Rows the system's own menus already carry on macOS: the Edit menu's items, which work wherever
// text is, and the ones the application menu owns. The window's drawn menu has no such menu to lean
// on and draws them itself; the system's bar would show two of each.
const SYSTEM_ROWS = new Set([
  'undo',
  'redo',
  'cut',
  'copy',
  'paste',
  'settings',
  'quit',
  'checkUpdates',
  'about',
])

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
  System.ShowAbout()
}

useAppEvents(records, collections, () => startup.value?.ready === true)
useGlobalShortcuts(environments)
useCollectionKeys()
useNativeMenu()

function closeSheet() {
  environments.closeSheet()
  focusUrlField()
}

/** The commands the whole window carries out, wherever the focus is: the palette, the settings
 *  window, the history, the list's side, the update check the system menu also offers, and the
 *  environments sheet. What each wears comes from the registry, so rebinding one in the settings
 *  rebinds it here. */
function useGlobalShortcuts(environments: ReturnType<typeof useEnvironmentsStore>): void {
  const search = useSearchStore()
  const tabs = useTabsStore()
  const { matches } = useCommands()
  const { toggle: toggleSidebar } = useSidebarToggle()

  const actions: [command: string, run: () => void][] = [
    ['palette', () => search.openPalette()],
    ['history', () => void tabs.openHistory()],
    ['newTab', () => void tabs.newTab()],
    ['closeTab', () => tabs.activeId && void tabs.close(tabs.activeId)],
    ['closeOthers', () => tabs.activeId && void tabs.closeOthers(tabs.activeId)],
    ['settings', () => System.ShowSettings('')],
    ['sidebar', toggleSidebar],
    ['updates', () => Updates.RequestCheck()],
    ['environments', () => environments.sheetOpen || environments.openSheet()],
    // The key that the menu row wears does what the row does: a new collection, not the level the
    // window happens to be showing.
    ['import', () => useTransferStore().askImport()],
    ['export', () => useTransferStore().askExport(useCollectionsStore().collectionId ?? '')],
  ]

  useEventListener(window, 'keydown', (event: KeyboardEvent) => {
    const action = actions.find(([command]) => matches(command, event))
    if (!action) return
    event.preventDefault()
    action[1]()
  })
}

/** Checks whether the keyboard event originated from an active editable text input. */
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el?.tagName) return false
  return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName)
}

/** Composable managing keyboard shortcuts for selected items within the collections panel. */
function useCollectionKeys(): void {
  const collections = useCollectionsStore()
  const { matches, offered } = useCommands()
  const { isMac } = usePlatform()

  function onKeydown(e: KeyboardEvent) {
    if (!collections.inCollections || isTyping(e.target)) return
    if (!collections.selectedId) return

    if (matches('newRequest', e)) {
      e.preventDefault()
      collections.pendingCreate = collections.selectedLevel
      return
    }
    if (matches('duplicate', e)) {
      e.preventDefault()
      void collections.duplicate(collections.selectedId)
      return
    }
    if (offered('run') && matches('run', e)) {
      e.preventDefault()
      void collections.run(collections.runNodeId, collections.levelName)
      return
    }
    if (isRenameEvent(e)) {
      e.preventDefault()
      collections.pendingRename = collections.selectedId
      return
    }
    if (isRemoveEvent(e, isMac.value)) {
      e.preventDefault()
      collections.pendingRemove = collections.selectedId
    }
  }

  useEventListener(window, 'keydown', onKeydown)
}

/** The window's own menu, sent to the system's bar on the platforms where the system draws one: the
 *  same rows, the same keys, and the same enabled and checked state the drawn menu shows. A row is
 *  not run here — the system hands its id back and the row of the drawn menu runs it. */
function useNativeMenu() {
  const { customTitlebar } = usePlatform()
  // The platforms that draw their own titlebar draw their own menu too — `AppMenu` owns it there,
  // and there is no system bar to send anything to.
  if (customTitlebar.value) return

  const { groups } = useAppMenu()

  // Looked up when the row is picked: a row's action is built with the state of the moment, and the
  // ones kept from the first build would toggle against what the window showed then.
  const rowOf = (id: string) =>
    groups.value.flatMap((group) => group.sections.flat()).find((row) => row.id === id)

  const tree = computed<NativeMenu[]>(() =>
    groups.value
      .map((group) => ({
        id: group.id,
        label: t(group.label),
        sections: group.sections
          .map((section) => section.filter((row) => !SYSTEM_ROWS.has(row.id)))
          .filter((section) => section.length > 0)
          .map((section) =>
            section.map((row) => ({
              id: row.id,
              label: t(row.label),
              accel: row.accel,
              checked: row.checked,
              disabled: row.disabled,
            }))
          ),
      }))
      .filter((group) => group.sections.length > 0)
  )

  // The bar is rebuilt whenever what it shows moves; the window's stores move far more often than
  // the rows do, so the rows are compared and identical ones are not sent at all.
  let sent = ''
  watch(
    tree,
    () => {
      const payload = JSON.stringify(tree.value)
      if (payload === sent) return
      sent = payload
      void System.ApplyWindowMenu(tree.value).catch(() => {
        // Not delivered: the same rows go again the next time the tree moves.
        sent = ''
      })
    },
    { immediate: true }
  )

  useWailsEvent('menu-command', (event) => rowOf(String(event.data ?? ''))?.run?.())
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
        <Button v-if="!folderRefused" variant="outline" @click="System.OpenDataFolder()">
          {{ t('startup.openDataFolder') }}
        </Button>
      </div>
      <div v-if="!folderRefused" class="startup-path">{{ startup.dbPath }}</div>
    </div>

    <template v-else-if="startup?.ready">
      <div class="body">
        <AppSidebar v-if="!sidebarHidden" />
        <div class="main-col">
          <AppHeader />
          <div class="workspace-row">
            <AppSidebarRail v-if="sidebarHidden" />
            <AppContent />
          </div>
        </div>
      </div>
      <AppStatusBar :update-version="availableVersion" @open-update="openAbout" />
    </template>
  </div>

  <EnvironmentsSheet v-if="startup?.ready && environments.sheetOpen" @close="closeSheet" />
  <SearchPalette v-if="startup?.ready" :open="search.open" @close="search.close()" />
  <WorkspacesSheet v-if="startup?.ready && workspaces.sheetOpen" @close="workspaces.closeSheet()" />
  <CookiesSheet v-if="startup?.ready && cookies.sheetOpen" @close="cookies.closeSheet()" />

  <SignInModal
    v-if="startup?.ready && signInOpen"
    :server="account?.server ?? ''"
    @begin="beginSignIn"
    @cancel="cancelSignIn"
    @close="closeSignIn()"
  />
  <SignOutSheet v-if="startup?.ready && signOutOpen" @close="closeSignOut()" @confirm="signOut()" />
  <McpConfirmSheet v-if="startup?.ready" />
  <UnpublishDialog v-if="startup?.ready" />
  <ImportDialog v-if="startup?.ready" />
  <ExportDialog v-if="startup?.ready" />
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

.workspace-row {
  @apply flex flex-1 min-w-0 min-h-0;
}

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
