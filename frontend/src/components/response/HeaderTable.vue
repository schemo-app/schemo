<script setup lang="ts">
import { computed } from 'vue'
import CaptureButton from './CaptureButton.vue'
import MatchText from '../search/MatchText.vue'
import { CaptureSource } from '../../../bindings/schemo/internal/domain'
import { matchesQuery } from '../../lib/match'
import type { RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'

const { headers, cookies = [], query = '' } = defineProps<{
  headers: { name: string; value: string }[]
  empty: string
  host?: RequestSource | null
  cookies?: { name: string; value: string }[]
  query?: string
}>()


const shown = computed(() => headers.filter((one) => matchesQuery(query, one.name, one.value)))

defineExpose({ matches: computed(() => shown.value.length) })

function capture(header: { name: string; value: string }) {
  if (header.name.toLowerCase() === 'set-cookie') {
    const line = header.value.trimStart()
    const cookie = cookies.find((one) => line.startsWith(`${one.name}=`))
    if (cookie) return { source: CaptureSource.CaptureCookie, path: cookie.name, value: cookie.value }
  }
  return { source: CaptureSource.CaptureHeader, path: header.name, value: header.value }
}
</script>

<template>
  <div class="header-table">
    <div v-for="(h, i) in shown" :key="i" class="header-row" :class="{ plain: !host }">
      <span class="header-name mono"><MatchText :text="h.name" :query /></span>
      <span class="header-value mono"><MatchText :text="h.value" :query /></span>
      <span v-if="host" class="header-act">
        <CaptureButton :host v-bind="capture(h)" />
      </span>
    </div>

    <div v-if="headers.length === 0" class="header-empty">{{ empty }}</div>
    <div v-else-if="shown.length === 0" class="header-empty">{{ t('common.nothingFound') }}</div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.header-table {
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.header-row {
  display: grid;
  grid-template-columns: minmax(8.75rem, 13.75rem) minmax(0, 1fr) 1.75rem;
  align-items: start;
  min-height: 2.125rem;
  border-bottom: 1px solid var(--line2);
}

.header-row.plain {
  grid-template-columns: minmax(8.75rem, 13.75rem) minmax(0, 1fr);
}

.header-row:hover {
  background: var(--bg-hover);
}

.header-name,
.header-value {
  min-width: 0;
  padding: 0.4375rem 0 0.5rem;
  line-height: 1.125rem;
}

.header-name {
  @apply select-text;
  overflow-wrap: anywhere;
  padding-left: 0.5rem;
  padding-right: 0.75rem;
  color: var(--tok-key);
}

.header-value {
  @apply break-all select-text;
  color: var(--text-secondary);
}

.header-act {
  @apply flex items-center justify-center;
  width: 1.75rem;
  height: 1.75rem;
  margin-top: 0.125rem;
}

.header-row :deep(.capture-btn) {
  width: 1.5rem;
  height: 1.5rem;
  border-radius: 0.3rem;
  opacity: 0;
}

.header-row:hover :deep(.capture-btn) {
  opacity: 1;
}

.header-empty {
  padding: 0.75rem 0.5rem;
  color: var(--text-tertiary);
}
</style>
