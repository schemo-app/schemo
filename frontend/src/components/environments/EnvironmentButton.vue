<script setup lang="ts">
import { computed } from 'vue'
import EnvironmentMenu from './EnvironmentMenu.vue'
import Icon from '../ui/Icon.vue'
import { DropdownMenu, DropdownMenuTrigger } from '../ui/dropdown-menu'
import { useEnvironmentsStore } from '../../stores/environments'
import { dotOf } from './palette'
import { useMessages } from '../../i18n'

// Which environment the window sends with, and the way to another one. It is a component of its own
// because the titlebar draws it in two places: in the middle of the bar where the bar is ours, and
// among the controls on the right where the system draws the chrome and the middle belongs to the
// title. The two arrangements are one button and one menu either way.
const envStore = useEnvironmentsStore()

const { t } = useMessages()

const activeEnvName = computed(() => envStore.activeEnvironment?.name ?? t('titlebar.noEnvironment'))

// The colour is the palette's word, and which mark it turns into is the palette's business: the dot
// this chip draws and the avatar the sheet draws are one answer to one name.
const envDotStyle = computed(() => {
  const env = envStore.activeEnvironment
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
          <span>{{ activeEnvName }}</span>
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

.env-btn:hover {
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
