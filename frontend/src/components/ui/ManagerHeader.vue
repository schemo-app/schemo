<script setup lang="ts">
import Icon from './Icon.vue'
import IconButton from './IconButton.vue'
import { t } from '../../i18n'

defineProps<{ title?: string }>()
defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <div class="manager-head">
    <slot><span class="title">{{ title }}</span></slot>
    <span class="spacer"></span>
    <slot name="actions" />
    <IconButton size="lg" :hint="t('common.close') + ' · Esc'" :aria-label="t('common.close')" @click="$emit('close')">
      <Icon name="X" :size="14" :stroke-width="2" />
    </IconButton>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.manager-head {
  @apply flex-none flex items-center;
  height: 52px;
  padding: 0 12px 0 24px;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}

.title {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-semibold text-14;
}

.spacer {
  @apply flex-1;
}
</style>
