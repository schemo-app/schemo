<script setup lang="ts">
import { WELCOME_STEPS } from '../../lib/welcomeCards'
import { useGettingStarted } from '../../composables/useGettingStarted'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ toggle: [] }>()

const { count, percent, dismiss } = useGettingStarted()
</script>

<template>
  <div class="gs-card">
    <div class="gs-head" @click="emit('toggle')">
      <span class="gs-title">{{ t('gettingStarted.title') }}</span>
      <span class="gs-count">
        {{ t('gettingStarted.of', { n: count, total: WELCOME_STEPS.length }) }}
      </span>
      <Icon
        name="ChevronDown"
        :size="10"
        :stroke-width="2.8"
        class="gs-chev"
        :class="{ up: !open }"
      />
      <IconButton :hint="t('gettingStarted.hide')" @click.stop="dismiss()">
        <Icon name="X" :size="9" :stroke-width="1.5" />
      </IconButton>
    </div>
    <div class="gs-bar">
      <div class="gs-fill" :style="{ width: `${percent}%` }"></div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.gs-card {
  @apply flex flex-col;
  border-radius: 0.625rem;
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--border);
}

.gs-head {
  @apply flex items-center flex-none;
  height: 2.25rem;
  padding: 0 0.25rem 0 0.75rem;
  gap: 0.3125rem;
}

.gs-title {
  @apply flex-1 min-w-0 truncate font-semibold;
}

.gs-count {
  @apply flex-none text-text-tertiary text-12;
}

.gs-chev {
  @apply flex-none text-text-tertiary;
  margin: 0 0.125rem;
  transition: transform 0.15s ease;
}

.gs-chev.up {
  transform: rotate(180deg);
}

.gs-bar {
  @apply flex-none;
  height: 0.1875rem;
  margin: 0 0.75rem 0.625rem;
  border-radius: 0.125rem;
  background: var(--chip);
}

.gs-fill {
  height: 100%;
  border-radius: 0.125rem;
  background: var(--accent);
  transition: width 0.3s ease;
}
</style>
