<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import { Application, Window } from '@wailsio/runtime'
import { usePlatform } from '../../composables/usePlatform'
import { useSidebarToggle } from '../../composables/useSidebarToggle'
import { useCommands } from '../../composables/useCommands'
import { useSearchStore } from '../../stores/search'
import { useMessages } from '../../i18n'
import EnvironmentButton from '../environments/EnvironmentButton.vue'
import AppMenu from './AppMenu.vue'
import CaptionClose from '../ui/CaptionClose.vue'
import TabStrip from './TabStrip.vue'
import Icon from '../ui/Icon.vue'

// The window's header: the strip of tabs and the two buttons that live beside it — the environment
// and the command palette. On macOS the traffic lights float over its left; with the sidebar
// closed they float over this bar instead, which is what the left inset makes room for. Where the
// app draws the chrome itself, its left leads with the menu and the panel instead.
const { t } = useMessages()
const search = useSearchStore()
const { customTitlebar } = usePlatform()
const { hidden: sidebarHidden, toggle: toggleSidebar } = useSidebarToggle()
const { hint } = useCommands()

const sidebarHint = computed(() => hint('sidebar'))
const paletteHint = computed(() => hint('palette'))

// The drawn window controls are the custom titlebar's — Windows and Linux — and the square turns
// into the restore sign when the window is as big as it goes.
const isMaximised = ref(false)

async function refreshMaximised() {
  try {
    isMaximised.value = await Window.IsMaximised()
  } catch {
    // Runtime not ready yet.
  }
}

useEventListener(() => (customTitlebar.value ? window : null), 'resize', refreshMaximised)
watch(customTitlebar, (custom) => custom && refreshMaximised(), { immediate: true })
</script>

<template>
  <header class="app-header" :class="{ 'app-header-custom': customTitlebar }" @dblclick="Window.ToggleMaximise()">
    <div v-if="sidebarHidden" class="header-side" :class="{ 'header-side-custom': customTitlebar }">
      <AppMenu v-if="customTitlebar" />
      <button
        class="header-btn"
        :title="sidebarHint ? `${t('sidebar.show')} · ${sidebarHint}` : t('sidebar.show')"
        @click="toggleSidebar()"
      >
        <Icon name="PanelLeft" :size="16" />
      </button>
    </div>

    <TabStrip />

    <div class="header-actions">
      <EnvironmentButton />
      <button
        class="header-btn header-search"
        :title="paletteHint ? `${t('shortcuts.cmd.palette')} · ${paletteHint}` : t('shortcuts.cmd.palette')"
        @click="search.openPalette()"
      >
        <Icon name="Search" :size="14" :stroke-width="1.9" />
        <span v-if="paletteHint" class="header-key">{{ paletteHint }}</span>
      </button>
    </div>

    <div v-if="customTitlebar" class="header-caps">
      <button class="cap-btn" :title="t('titlebar.minimise')" @click="Window.Minimise()">
        <span class="cap-icon cap-icon-minus"></span>
      </button>
      <button class="cap-btn" :title="t('titlebar.maximise')" @click="Window.ToggleMaximise()">
        <span v-if="!isMaximised" class="cap-icon cap-icon-square"></span>
        <span v-else class="cap-icon cap-icon-restore">
          <span class="cap-icon-restore-back"></span>
          <span class="cap-icon-restore-front"></span>
        </span>
      </button>
      <CaptionClose @close="Application.Quit()" />
    </div>
  </header>
</template>

<style scoped>
@reference "../../style.css";

.app-header {
  @apply flex-none flex items-stretch relative;
  height: 2.75rem;
  background: var(--chrome);
  box-shadow: inset 0 -1px 0 var(--border);
  padding: 0 0.75rem 0 0.5rem;
  z-index: 40;
  --wails-draggable: drag;
}

.app-header-custom {
  padding: 0 0 0 0.5rem;
}

.app-header-custom .header-actions {
  padding-right: 0.5rem;
}

.app-header-custom .header-caps {
  margin-right: 0;
}

.header-side {
  @apply flex items-center flex-none;
  padding: 0 0.5rem 0 4.375rem;
}

.header-side .header-btn {
  margin-left: 0.625rem;
}

.header-side:not(.header-side-custom) .header-btn {
  margin-top: 0.5625rem;
}

.header-side.header-side-custom {
  @apply gap-0.5;
  padding: 0 0.25rem 0 0.5rem;
}

.header-side.header-side-custom .header-btn {
  width: 2rem;
  margin-left: 0;
  border-radius: 0.375rem;
}

.header-btn {
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

.header-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.header-actions {
  @apply flex items-center flex-none;
  gap: 0.375rem;
  padding-left: 0.75rem;
  --wails-draggable: no-drag;
}

.header-search {
  width: auto;
  padding: 0 0.5625rem;
  gap: 0.375rem;
  background: var(--chip);
}

.header-key {
  @apply leading-none text-text-tertiary;
  font-size: var(--text-11);
}

.header-caps {
  @apply flex items-stretch flex-none;
  margin-right: -0.75rem;
}
</style>
