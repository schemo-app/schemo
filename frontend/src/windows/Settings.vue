<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { Window } from '@wailsio/runtime'
import CaptionClose from '../components/ui/CaptionClose.vue'
import { useSettings } from '../composables/useSettings'
import { useSettingsEvents } from '../composables/events/useSettingsEvents'
import { useAccount } from '../composables/useAccount'
import SignOutSheet from '../components/settings/SignOutSheet.vue'
import { usePlatform } from '../composables/usePlatform'
import { useSheetNotice } from '../composables/useSheetNotice'
import { useMessages } from '../i18n'
import SettingsRail from '../components/settings/SettingsRail.vue'
import SettingsHeader from '../components/settings/SettingsHeader.vue'
import SettingsResults from '../components/settings/SettingsResults.vue'
// The panes arrive as they are asked for: the window draws one at a time, and somebody who opened it
// to change the accent has no use for the certificates, the shortcut table and the MCP server being
// carried in with it.
const AccountPane = defineAsyncComponent(() => import('../components/settings/AccountPane.vue'))
const GeneralPane = defineAsyncComponent(() => import('../components/settings/GeneralPane.vue'))
const AppearancePane = defineAsyncComponent(() => import('../components/settings/AppearancePane.vue'))
const RequestsPane = defineAsyncComponent(() => import('../components/settings/RequestsPane.vue'))
const ProxyPane = defineAsyncComponent(() => import('../components/settings/ProxyPane.vue'))
const ExtensionPane = defineAsyncComponent(() => import('../components/settings/ExtensionPane.vue'))
const McpPane = defineAsyncComponent(() => import('../components/settings/McpPane.vue'))
const ShortcutsPane = defineAsyncComponent(() => import('../components/settings/ShortcutsPane.vue'))
const UpdatesPane = defineAsyncComponent(() => import('../components/settings/UpdatesPane.vue'))
import { asCategory, type CategoryId } from '../components/settings/categories'
import { settingsHits } from '../lib/settingsIndex'

const { t } = useMessages()
const { customTitlebar } = usePlatform()
const { loadSettings } = useSettings()
const { notice, clearNotice } = useSheetNotice()
// The sign-in is the account pane's own in this window — the mockup draws the code there — so the
// only dialog left here is the way out.
const { signOutOpen, closeSignOut, signOut } = useAccount()

const active = ref<CategoryId>(asCategory(new URLSearchParams(location.search).get('tab')))
const query = ref('')

// The search is the window's own reading of what it draws: the index is a list of rows and their
// search words, and where a word stands in a title is a fact about the two texts, nothing more.
const hits = computed(() => settingsHits(t, query.value))
const counts = computed(() => {
  const out: Partial<Record<CategoryId, number>> = {}
  for (const hit of hits.value) out[hit.tab] = (out[hit.tab] ?? 0) + 1
  return out
})

function select(id: CategoryId) {
  active.value = id
  query.value = ''
  clearNotice()
}

// Enter in the search field opens the first row the query found, wherever it lives.
function openFirst() {
  const first = hits.value[0]
  if (first) select(first.tab)
}

onMounted(() => {
  void loadSettings()
  void Window.SetTitle(t('settings.title'))
})

useSettingsEvents((category) => select(asCategory(category)))
</script>

<template>
  <div class="settings-window">
    <header v-if="customTitlebar" class="settings-bar">
      <span class="settings-bar-title">{{ t('settings.title') }}</span>
      <CaptionClose @close="Window.Close()" />
    </header>
    <!-- On macOS the window's own traffic lights stand at the left of this bar, and the name
         stands centred between them: the mockup's titlebar, with the system's buttons. -->
    <div v-else class="settings-bar-mac">
      <span class="settings-bar-title">{{ t('settings.title') }}</span>
    </div>

    <div class="settings-body">
      <SettingsRail
        :active
        :query
        :counts
        @select="select"
        @search="query = $event"
        @open-first="openFirst"
      />

      <section class="settings-pane">
        <div class="pane-scroll">
          <SettingsHeader :active :query :count="hits.length" />

          <SettingsResults v-if="query.trim()" :hits @pick="select($event)" />

          <template v-else>
            <AccountPane v-if="active === 'account'" />
            <GeneralPane v-else-if="active === 'general'" />
            <AppearancePane v-else-if="active === 'appearance'" />
            <ShortcutsPane v-else-if="active === 'shortcuts'" />
            <RequestsPane v-else-if="active === 'requests'" />
            <ProxyPane v-else-if="active === 'proxy'" />
            <ExtensionPane v-else-if="active === 'extension'" />
            <McpPane v-else-if="active === 'mcp'" />
            <UpdatesPane v-else-if="active === 'updates'" />
          </template>
        </div>

        <div v-if="notice" class="pane-notice">{{ notice }}</div>
      </section>
    </div>

    <SignOutSheet v-if="signOutOpen" @close="closeSignOut()" @confirm="signOut($event)" />
  </div>
</template>

<style scoped>
@reference "../style.css";

.settings-window {
  @apply h-full flex flex-col bg-bg text-text select-none;
  line-height: normal;
}

.settings-bar-mac {
  @apply flex-none flex items-center justify-center border-b;
  height: 3.125rem;
  background-image: linear-gradient(var(--glass-chrome), var(--glass-chrome));
  backdrop-filter: var(--blur-chrome);
  border-color: var(--glass-chrome-border);
  --wails-draggable: drag;
}

.settings-bar {
  @apply relative flex-none h-13 flex items-center justify-start px-3;
  border-bottom: 1px solid var(--glass-chrome-border);
  background: var(--glass-chrome);
  backdrop-filter: var(--blur-chrome);
  --wails-draggable: drag;
}

.settings-bar-title {
  @apply text-13 font-semibold text-text;
}

.settings-bar .cap-btn {
  @apply absolute right-0;
}

.settings-body {
  @apply relative flex-1 flex min-h-0;
}

.settings-pane {
  @apply flex-1 min-w-0 flex flex-col;
}

.pane-scroll {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col;
  padding: 1.625rem 2rem 2.5rem;
  gap: 1.625rem;
}

.pane-notice {
  @apply flex-none text-13;
  padding: 0.625rem 1.25rem;
  border-top: 1px solid var(--border);
  color: var(--red-text);
}
</style>
