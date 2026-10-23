<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { System } from '../../../bindings/schemo/internal/app'
import { SIDEBAR } from '../../lib/layout'
import { useTabsStore } from '../../stores/tabs'
import { usePublicationsStore } from '../../stores/publications'
import { useCaptureStore } from '../../stores/capture'
import { useSidebarToggle } from '../../composables/useSidebarToggle'
import { useCommands } from '../../composables/useCommands'
import { usePlatform } from '../../composables/usePlatform'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useResizeDrag } from '../../composables/useResizeDrag'
import { rem } from '../../lib/units'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import AccountButton from '../cloud/AccountButton.vue'
import WorkspaceSwitcher from '../workspaces/WorkspaceSwitcher.vue'
import CollectionTree from '../collections/CollectionTree.vue'
import GettingStarted from '../onboarding/GettingStarted.vue'
import AppMenu from './AppMenu.vue'

// The sidebar: the workspace, the two pages, the collections, and who is signed in — the whole of what
// stands beside the work area. What it points at opens as a tab; nothing here switches a section.
const tabs = useTabsStore()
const publications = usePublicationsStore()
const capture = useCaptureStore()
const { hint } = useCommands()
const { customTitlebar } = usePlatform()
const { hidden: sidebarHidden, toggle: toggleSidebar } = useSidebarToggle()

const sidebarHint = computed(() => hint('sidebar'))
const { setLayout } = useSettings()
const stored = useSetting('sideWidth')
const width = ref(stored.value)

const { startDrag, dragging } = useResizeDrag(width, {
  axis: 'x',
  side: 'left',
  min: SIDEBAR.min,
  max: SIDEBAR.max,
  onEnd: () => setLayout({ sideWidth: Math.round(width.value) }),
})

watch(stored, (now) => {
  if (!dragging.value && now !== width.value) width.value = now
})

function resetWidth() {
  width.value = SIDEBAR.default
  setLayout({ sideWidth: SIDEBAR.default })
}

const live = computed(() => capture.recording)

const nav = computed(() => [
  {
    key: 'history',
    label: t('sidebar.history'),
    icon: 'Clock' as const,
    kbd: hint('history'),
    live: false,
    warn: false,
    active: tabs.activeKind === TabKind.TabKindHistory,
    on: () => tabs.openHistory(),
  },
  {
    key: 'capture',
    label: t('sidebar.capture'),
    icon: 'Globe' as const,
    kbd: '',
    live: live.value,
    warn: false,
    active: tabs.activeKind === TabKind.TabKindCapture,
    on: () => tabs.openCapture(),
  },
  {
    key: 'pubs',
    label: t('sidebar.publications'),
    icon: 'Radio' as const,
    kbd: '',
    live: false,
    warn: publications.hasChanges,
    active: tabs.activeKind === TabKind.TabKindPubs,
    on: () => tabs.openPublications(),
  },
])

function openSettings() {
  System.ShowSettings('')
}
</script>

<template>
  <aside class="sidebar" :style="{ width: rem(width) }">
    <div
      class="sidebar-grip"
      :title="t('sidebar.resize')"
      @mousedown.prevent="startDrag"
      @dblclick="resetWidth"
    ></div>

    <div class="sidebar-head" :class="{ 'sidebar-head-custom': customTitlebar }">
      <AppMenu v-if="customTitlebar" />
      <button
        v-if="!sidebarHidden"
        class="side-btn"
        :class="{ 'side-btn-mac': !customTitlebar }"
        :title="sidebarHint ? `${t('sidebar.hide')} · ${sidebarHint}` : t('sidebar.hide')"
        @click="toggleSidebar()"
      >
        <Icon name="PanelLeft" :size="16" />
      </button>
    </div>

    <div class="sidebar-ws">
      <WorkspaceSwitcher />
    </div>

    <nav class="sidebar-nav">
      <button
        v-for="item in nav"
        :key="item.key"
        class="nav-row"
        :class="{ active: item.active }"
        @click="item.on()"
      >
        <Icon
          :name="item.icon"
          :size="16"
          :stroke-width="1.7"
          class="nav-icon"
          :class="{ accent: item.active }"
        />
        <span class="nav-label cap-line">{{ item.label }}</span>
        <span v-if="item.live" class="nav-live-dot" :title="t('sidebar.live')"></span>
        <span v-else-if="item.warn" class="nav-warn-dot" :title="t('publications.changes')"></span>
        <span v-else-if="item.kbd" class="nav-kbd">{{ item.kbd }}</span>
      </button>
    </nav>

    <CollectionTree />

    <GettingStarted />

    <div class="sidebar-foot">
      <AccountButton />

      <button class="foot-btn" :title="t('settings.title')" @click="openSettings()">
        <Icon name="SlidersHorizontal" :size="16" :stroke-width="1.8" />
      </button>
    </div>
  </aside>
</template>

<style scoped>
@reference "../../style.css";

.sidebar {
  @apply relative flex flex-col flex-none min-h-0;
  background: var(--chrome);
  border-right: 1px solid var(--border);
  --wails-draggable: drag;
}

.sidebar-grip {
  @apply absolute top-0 bottom-0 w-1 z-10 cursor-col-resize;
  right: -1px;
  --wails-draggable: no-drag;
}

.sidebar-grip:hover {
  background: linear-gradient(
    90deg,
    transparent 0.125rem,
    var(--accent) 0.125rem,
    var(--accent) 0.25rem
  );
}

.sidebar-head {
  @apply flex items-center justify-end flex-none;
  height: var(--app-header-height);
  padding: 0 0.5rem;
}

.sidebar-head-custom {
  @apply justify-start gap-0.5;
  padding: 0 0.5rem 0 0.6rem;
}

.sidebar-head-custom .side-btn {
  width: 2rem;
  border-radius: 0.3rem;
}

.side-btn-mac {
  margin-top: 0;
}

.side-btn,
.foot-btn {
  @apply flex items-center justify-center flex-none;
  width: 1.875rem;
  height: 1.75rem;
  border: 0;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-secondary);
  cursor: default;
  --wails-draggable: no-drag;
}

.side-btn:hover,
.foot-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.sidebar-ws {
  @apply flex-none;
  padding: 0.125rem 0.5rem 0.5rem;
}

.sidebar-nav {
  @apply flex flex-col flex-none;
  gap: 1px;
  padding: 0 0.5rem;
}

.nav-row {
  @apply flex items-center relative;
  gap: 0.5625rem;
  height: 1.875rem;
  padding: 0 0.5rem;
  border: 0;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text);
  font-size: var(--text-13);
  cursor: default;
  text-align: left;
  --wails-draggable: no-drag;
}

.nav-row:hover {
  background: var(--bg-hover);
}

.nav-row.active {
  background: var(--bg-hover);
  font-weight: 500;
}

.nav-icon {
  @apply flex-none text-text-secondary;
}

.nav-icon.accent {
  color: var(--accent);
}

.nav-label {
  @apply flex-1 min-w-0 truncate;
}

.nav-kbd {
  @apply flex-none text-text-tertiary;
  font-size: var(--text-11-5);
}

.nav-live-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--red);
}

.nav-warn-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--orange);
}

.sidebar-foot {
  @apply flex items-center gap-0.5 flex-none;
  padding: 0.5rem;
  border-top: 1px solid var(--border);
}

</style>
