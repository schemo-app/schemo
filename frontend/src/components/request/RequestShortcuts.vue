<script setup lang="ts">
import { computed } from 'vue'
import { useCommands } from '../../composables/useCommands'
import { usePlatform } from '../../composables/usePlatform'
import { t } from '../../i18n'
import { capsOf } from '../../lib/commands'

const { caps } = useCommands()
const { isMac } = usePlatform()

const shortcuts = computed(() => [
  { label: t('request.send'), keys: caps('send') },
  { label: t('request.pasteCurl'), keys: capsOf('mod+V', isMac.value) },
  { label: t('request.saveDraftTitle'), keys: caps('save') },
].filter(shortcut => shortcut.keys.length))
</script>

<template>
  <div class="request-shortcuts">
    <dl class="shortcut-list">
      <div v-for="shortcut in shortcuts" :key="shortcut.label" class="shortcut-row">
        <dt class="shortcut-keys">
          <kbd v-for="key in shortcut.keys" :key="key">{{ key }}</kbd>
        </dt>
        <dd>{{ shortcut.label }}</dd>
      </div>
    </dl>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.request-shortcuts {
  @apply flex justify-center overflow-auto;
  padding: 2.5rem 1.25rem;
  color: var(--text-secondary);
}

.shortcut-list {
  @apply flex flex-col;
  gap: 0.25rem;
}

.shortcut-row {
  @apply flex flex-none items-center;
  min-height: 1.875rem;
  gap: 0.875rem;
}

.shortcut-keys {
  @apply flex flex-none justify-end;
  width: 6.875rem;
  gap: 0.1875rem;
}

kbd {
  @apply flex items-center justify-center;
  min-width: 1.375rem;
  height: 1.375rem;
  padding: 0 0.375rem;
  border-radius: 0.375rem;
  background: var(--chip);
  box-shadow: inset 0 0 0 0.5px var(--border-strong), 0 0.5px 0 var(--border-strong);
  font: inherit;
  font-size: var(--text-12);
}
</style>
