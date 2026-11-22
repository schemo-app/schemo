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

const envDotStyle = computed(() => {
  const env = environments.activeEnvironment
  if (!env) return { boxShadow: 'inset 0 0 0 1.5px var(--text-tertiary)' }
  return { background: dotOf(env.color ?? '') }
})
</script>

<template>
  <div class="env-wrap">
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" class="env-btn" :title="t('titlebar.environment', { name: activeEnvName })">
          <span class="env-dot" :style="envDotStyle"></span>
          <span class="cap-line">{{ activeEnvName }}</span>
          <Icon name="ChevronDown" :size="9" :stroke-width="2.8" class="env-caret" />
        </button>
      </DropdownMenuTrigger>
      <EnvironmentMenu
        :selected="environments.activeId"
        :none-label="t('titlebar.noEnvironment')"
        @select="environments.setActive($event)"
      />
    </DropdownMenu>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.env-btn {
  @apply flex items-center flex-none cursor-pointer border-0;
  height: 28px;
  max-width: 200px;
  padding: 0 8px 0 10px;
  gap: 7px;
  border-radius: 7px;
  background: transparent;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text);
  font: inherit;
  font-size: var(--text-13);
}

.env-btn:hover,
.env-btn[data-state='open'] {
  background: var(--bg-hover);
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
