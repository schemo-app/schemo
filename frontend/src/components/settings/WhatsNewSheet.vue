<script setup lang="ts">
import { IconButton } from '../ui/button'
import Icon from '../ui/Icon.vue'
import { useMessages } from '../../i18n'

// What changed in the release the app is on, or in the one it is being offered. A sheet over the
// settings rather than a window: it answers a question asked here.
defineProps<{
  app: string
  version: string
  line: string
  current: boolean
  notes: string[]
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useMessages()
</script>

<template>
  <div class="veil" @click="emit('close')">
    <div class="sheet" @click.stop>
      <header class="head">
        <span class="lines">
          <span class="title">{{ t('settings.whatsNewTitle', { app }) }}</span>
          <span class="sub">{{ line }}</span>
        </span>
        <IconButton variant="bare" size="tab" :hint="t('common.close')" @click="emit('close')">
          <Icon name="X" :size="12" :stroke-width="2.4" />
        </IconButton>
      </header>

      <div class="body">
        <div class="release">
          <span class="release-head">
            <span class="version">{{ version }}</span>
            <!-- The badge says which of the two releases this is: the one running, or the one on
                 offer. Only one of them is ever the case. -->
            <span class="badge" :class="{ offered: !current }">
              {{ current ? t('settings.installedBadge') : t('settings.offeredBadge') }}
            </span>
          </span>
          <div class="notes">
            <p v-for="(note, index) in notes" :key="index" class="note">
              <span class="dot"></span>
              <span>{{ note }}</span>
            </p>
            <p v-if="!notes.length" class="empty">{{ t('update.noNotes') }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.veil {
  @apply absolute inset-0 flex items-center justify-center z-10;
  background: rgba(20, 22, 30, 0.28);
  padding: 2rem;
}

.sheet {
  @apply flex flex-col w-full max-w-145 max-h-full rounded-xl overflow-hidden bg-bg-panel;
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.1), 0 1.5rem 3.75rem rgba(20, 24, 40, 0.28);
}

.head {
  @apply flex-none flex items-start justify-between gap-4 border-b border-border;
  padding: 1.125rem 1.25rem 0.875rem;
}

.lines {
  @apply flex flex-col;
  gap: 0.125rem;
}

.title {
  @apply text-17 font-semibold tracking-[-0.01em];
}

.sub {
  @apply text-12 text-text-tertiary;
}

.body {
  @apply flex-1 min-h-0 overflow-y-auto;
  padding: 1.125rem 1.25rem 1.375rem;
}

.release {
  @apply flex flex-col gap-2.5;
}

.release-head {
  @apply flex items-baseline gap-2.5;
}

.version {
  @apply text-15 font-semibold;
}

.badge {
  @apply inline-flex items-center h-5 px-1.5 rounded-[0.25rem] text-11;
  background: var(--green-soft);
  color: var(--green-text);
}

.badge.offered {
  background: var(--accent-soft);
  color: var(--accent);
}

.notes {
  @apply flex flex-col gap-2;
}

.note {
  @apply flex items-start gap-2.5 text-13;
  line-height: 1.55;
}

.dot {
  @apply flex-none rounded-full;
  width: 0.25rem;
  height: 0.25rem;
  margin-top: 0.5rem;
  background: var(--accent);
}

.empty {
  @apply text-13 text-text-tertiary;
}
</style>
