<script setup lang="ts">
import { computed } from 'vue'
import {
  DropdownMenuItem, DropdownMenuItemIndicator, DropdownMenuRadioGroup,
  DropdownMenuRadioItem, DropdownMenuRoot, DropdownMenuSeparator, DropdownMenuTrigger,
} from 'reka-ui'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import Icon from '../ui/Icon.vue'
import { useRequestEnvironment } from '../../composables/useRequestEnvironment'
import { dotOf } from '../../lib/environmentPalette'
import type { RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'

const { source } = defineProps<{ source: RequestSource }>()

const { pinned, selection, active, hasPreviousSelection, label, variants, choose } = useRequestEnvironment(source)
const neutral = 'var(--text-tertiary)'
const color = computed(() => active.value && selection.value !== 'base' ? dotOf(active.value.color ?? '') : neutral)
const title = computed(() => t(pinned.value || hasPreviousSelection.value ? 'request.envPinnedTitle' : 'request.envCollectionTitle', {
  name: label.value,
}))
const choices = computed(() => [
  { id: 'base', name: t('request.envBase'), dot: neutral },
  ...variants.value.map(env => ({ id: env.id, name: env.name, dot: dotOf(env.color ?? '') })),
])
</script>

<template>
  <DropdownMenuRoot v-if="pinned || hasPreviousSelection || variants.length">
    <DropdownMenuTrigger as-child>
      <button type="button" class="bar-env bar-env-trigger" :title :aria-label="title">
        <span class="env-row-dot" :style="{ background: color }"></span>
        <span class="bar-env-name">{{ label }}</span>
        <Icon name="ChevronDown" :size="10" :stroke-width="2.4" class="bar-env-caret" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="request-env-menu" align="end" :side-offset="8">
      <DropdownMenuRadioGroup v-if="!pinned && (!hasPreviousSelection || variants.length)" class="env-options" :model-value="selection">
        <DropdownMenuRadioItem
          v-for="option in choices"
          :key="option.id"
          :value="option.id"
          class="env-row"
          @select="choose(option.id)"
        >
          <span class="env-row-dot" :style="{ background: option.dot }"></span>
          <span class="env-row-name">{{ option.name }}</span>
          <DropdownMenuItemIndicator class="env-row-check">
            <Icon name="Check" :size="12" :stroke-width="2.6" />
          </DropdownMenuItemIndicator>
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
      <DropdownMenuSeparator v-if="!pinned && hasPreviousSelection && variants.length" class="env-divider" />
      <DropdownMenuItem v-if="pinned || hasPreviousSelection" class="env-row" @select="choose('')">
        <span class="env-row-name">{{ t(pinned ? 'request.envReset' : 'request.envDefault') }}</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenuRoot>
  <span v-else-if="selection" class="bar-env" :title>
    <span class="env-row-dot" :style="{ background: color }"></span>
    <span class="bar-env-name">{{ label }}</span>
  </span>
</template>

<style scoped>
@reference "../../style.css";

.bar-env {
  @apply inline-flex flex-none items-center text-text-secondary;
  gap: 0.4375rem;
  height: 1.625rem;
  margin-right: 0.375rem;
  padding: 0 0.4375rem 0 0.5625rem;
  border-radius: 0.375rem;
  font-size: var(--text-12);
  font-weight: 500;
  --wails-draggable: no-drag;
}

.bar-env-trigger {
  @apply border-0 cursor-pointer bg-transparent text-text;
}

.bar-env-trigger:hover,
.bar-env-trigger[data-state='open'] {
  background: var(--bg-hover);
}

.bar-env-caret {
  @apply flex-none text-text-tertiary;
}

.bar-env-name {
  @apply truncate;
  max-width: min(8.125rem, 18vw);
}
</style>
