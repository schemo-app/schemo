<script setup lang="ts">
import { computed } from 'vue'
import EnvironmentMenu from './EnvironmentMenu.vue'
import Icon from '../ui/Icon.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'
import { useEnvironmentsStore } from '../../stores/environments'
import { dotOf } from '../../lib/environmentPalette'
import { t } from '../../i18n'

// A component of its own because the titlebar draws it in two places.
const environments = useEnvironmentsStore()

const activeEnvName = computed(() => environments.activeEnvironment?.name ?? t('titlebar.noEnvironment'))

// The colour is the palette's word, and which mark it turns into is the palette's business: the dot
// this chip draws and the avatar the sheet draws are one answer to one name.
const envDotStyle = computed(() => {
  const env = environments.activeEnvironment
  if (!env) return { background: 'var(--text-tertiary)' }
  return { background: dotOf(env.color ?? '') }
})
</script>

<template>
  <div class="env-wrap">
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" class="env-btn" :title="t('titlebar.environment', { name: activeEnvName })">
          <span class="env-dot" :style="envDotStyle"></span>
          <span class="env-name">{{ activeEnvName }}</span>
          <Icon name="ChevronDown" :size="10" :stroke-width="2.6" class="env-caret" />
        </button>
      </DropdownMenuTrigger>
      <EnvironmentMenu />
    </DropdownMenu>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.env-btn {
  @apply flex items-center flex-none cursor-pointer border-0;
  height: 1.75rem;
  padding: 0 0.625rem;
  gap: 0.5rem;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: var(--text-13);
}

.env-btn:hover,
.env-btn[data-state='open'] {
  background: var(--bg-hover);
}

.env-name {
  @apply truncate;
  max-width: min(10rem, 20vw);
}

.env-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
}

.env-caret {
  @apply flex-none;
  color: var(--text-tertiary);
}
</style>
