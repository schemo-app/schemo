<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Window } from '@wailsio/runtime'
import CaptionClose from '../components/ui/CaptionClose.vue'
import { SystemService } from '../../bindings/schemo/internal/transport/wails'
import { usePlatform } from '../composables/usePlatform'
import { useUpdateCheck } from '../composables/useUpdateCheck'
import { Button } from '../components/ui/button'
import { useMessages } from '../i18n'
import { formatVersion } from '../lib/format'
import logoUrl from '../assets/logo.svg'

const { t } = useMessages()
const { customTitlebar } = usePlatform()
const { phase, latest, notes, error, development, install, skip } = useUpdateCheck()

const appName = ref('')
const version = ref('')
const build = ref('')

const versionLabel = computed(() => {
  const v = formatVersion(version.value)
  return build.value ? `${v} (${build.value})` : v || '…'
})

const busy = computed(() => phase.value === 'installing')

onMounted(async () => {
  void Window.SetTitle(t('update.title', { app: appName.value || 'Schemo' }))
  try {
    appName.value = (await SystemService.Name()) ?? ''
  } catch {}
  try {
    version.value = (await SystemService.Version()) ?? ''
    build.value = (await SystemService.Build()) ?? ''
  } catch {}
})

async function onSkip() {
  await skip()
  if (phase.value === 'uptodate') Window.Close()
}
</script>

<template>
  <div class="update-window">
    <header v-if="customTitlebar" class="update-bar">
      <span class="update-bar-title">{{ t('update.title', { app: appName || 'Schemo' }) }}</span>
      <CaptionClose @close="Window.Close()" />
    </header>
    <div v-else class="update-bar-mac"></div>

    <div class="update-body">
      <div class="update-head">
        <img class="update-icon" :src="logoUrl" :alt="appName" draggable="false" />
        <div class="update-heading">
          <div class="update-title">
            {{ latest ? t('update.available', { version: formatVersion(latest) }) : t('update.none') }}
          </div>
          <div class="update-subtitle">
            {{ t('update.current', { app: appName || 'Schemo', version: versionLabel }) }}
          </div>
        </div>
      </div>

      <div class="update-whats-new">
        <div class="update-label">{{ t('update.whatsNew') }}</div>
        <div class="update-notes">
          <div v-for="(note, index) in notes" :key="index" class="update-note">
            <span class="update-note-dot"></span>
            <span>{{ note }}</span>
          </div>
          <p v-if="!notes.length" class="update-empty">{{ t('update.noNotes') }}</p>
        </div>
      </div>

      <div class="update-actions">
        <button v-if="!development" class="update-skip" :disabled="busy" @click="onSkip">
          {{ t('update.skip') }}
        </button>
        <span v-else></span>
        <div class="update-actions-right">
          <Button variant="outline" size="lg" :disabled="busy" @click="Window.Close()">
            {{ t('update.remindLater') }}
          </Button>
          <Button
            v-if="!development"
            variant="primary"
            size="lg"
            :disabled="busy || !latest"
            @click="install"
          >
            {{ busy ? t('update.installing') : t('update.installAndRestart') }}
          </Button>
        </div>
      </div>

      <p v-if="development" class="update-source-note">{{ t('update.devBuild') }}</p>

      <p v-if="error" class="update-error">{{ error }}</p>
    </div>
  </div>
</template>

<style scoped>
@reference "../style.css";

.update-window {
  @apply h-full flex flex-col bg-bg-panel text-text select-none;
}

.update-bar-mac {
  @apply flex-none;
  height: 3.125rem;
  --wails-draggable: drag;
}

.update-bar {
  @apply relative flex-none h-13 flex items-center justify-start px-3;
  border-bottom: 1px solid var(--glass-chrome-border);
  background: var(--glass-chrome);
  backdrop-filter: var(--blur-chrome);
  --wails-draggable: drag;
}

.update-bar-title {
  @apply text-13 font-medium text-text-secondary;
}

.update-bar .cap-btn {
  @apply absolute right-0;
}

.update-body {
  @apply flex-1 min-h-0 flex flex-col gap-4.5 px-7.5 pt-7 pb-6;
}

.update-head {
  @apply flex items-start gap-4 flex-none;
}

.update-icon {
  @apply w-14 h-14 flex-none;
  filter: drop-shadow(0 0.5rem 0.875rem rgba(106, 108, 255, 0.35));
  -webkit-user-drag: none;
  user-drag: none;
}

.update-heading {
  @apply flex flex-col gap-0.5 pt-0.5 min-w-0;
}

.update-title {
  @apply text-19 font-bold tracking-[-0.01em];
}

.update-subtitle {
  @apply text-13 text-text-secondary;
}

.update-whats-new {
  @apply flex flex-col gap-1.5 flex-1 min-h-0;
}

.update-label {
  @apply text-12 font-semibold uppercase tracking-[0.06em] text-text-tertiary flex-none;
}

.update-notes {
  @apply flex-1 min-h-30 overflow-y-auto rounded-lg border border-border bg-bg-sidebar px-3.5 py-3;
}

.update-note {
  @apply flex gap-2 py-1.5 text-13 leading-[1.5];
}

.update-note-dot {
  @apply w-1 h-1 rounded-full flex-none mt-1.5;
  background: var(--text-tertiary);
}

.update-empty {
  @apply m-0 text-13 text-text-tertiary;
}

.update-actions {
  @apply flex items-center justify-between gap-2.5 pt-1.5 flex-none;
}

.update-skip {
  @apply bg-transparent border-none p-0 mt-1.5 text-13 text-text-tertiary cursor-pointer;
  font: inherit;
  font-size: var(--text-13);
}

.update-skip:hover:not(:disabled) {
  @apply text-text;
  text-decoration: underline;
}

.update-skip:disabled {
  @apply opacity-50 cursor-default;
}

.update-actions-right {
  @apply flex items-center gap-2 mt-1.5;
}

.update-error {
  @apply m-0 text-13 text-red flex-none;
}

.update-source-note {
  @apply m-0 text-13 text-text-secondary leading-[1.5] flex-none pt-4;
  border-top: 1px solid var(--border);
}
</style>
