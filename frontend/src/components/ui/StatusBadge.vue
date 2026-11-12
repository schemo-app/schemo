<script setup lang="ts">
import { t } from '../../i18n'
import { statusBadgeClass } from '../../lib/format'

// A status as a badge: tinted by its series, and plain where there is none to speak of. What it says
// is the status unless the caller has more to say about it.
defineProps<{ status: number; failed?: boolean }>()
</script>

<template>
  <span class="badge" :class="failed ? 'badge-status-5xx' : status ? statusBadgeClass(status) : 'neutral'">
    <slot>{{ failed ? t('history.failed') : status || '—' }}</slot>
  </span>
</template>

<style scoped>
@reference "../../style.css";

.badge {
  @apply justify-center text-center font-semibold;
  min-width: 1.625rem;
  font-family: var(--mono);
  font-size: var(--text-11);
  padding: 1px 5px;
  border-radius: 0.3125rem;
  white-space: nowrap;
}

.neutral {
  background: var(--chip);
  color: var(--text-secondary);
}
</style>
