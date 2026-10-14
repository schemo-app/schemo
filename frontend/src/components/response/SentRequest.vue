<script setup lang="ts">
import HeaderTable from './HeaderTable.vue'
import type { RecordView } from '../../lib/requestRecord'
import { jsonParts } from '../../lib/json'
import { useMessages } from '../../i18n'

// The request a captured answer was the answer to, as it went out: the browser rail has no command
// line to show it in, so the viewer does.
defineProps<{ record: RecordView }>()

const { t } = useMessages()
</script>

<template>
  <div class="pair-row"><span class="pair-label">{{ t('response.method') }}</span><span class="mono">{{ record.method }}</span></div>
  <div class="pair-row"><span class="pair-label">URL</span><span class="mono break">{{ record.url }}</span></div>
  <div class="ja-section-title" style="padding-left: 0">{{ t('response.requestHeaders') }}</div>
  <HeaderTable :headers="record.requestHeaders" empty="—" />
  <div v-if="record.requestBody" class="ja-section-title" style="padding-left: 0">{{ t('response.requestBody') }}</div>
  <pre v-if="record.requestBody" class="code"><span v-for="(part, i) in jsonParts(record.requestBody)" :key="i" :class="part.tone">{{ part.text }}</span></pre>
</template>

<style scoped>
@reference "../../style.css";

.pair-row {
  @apply flex gap-3 py-0.5 px-0 text-12;
}

.pair-label {
  @apply text-text-secondary min-w-17.5 grow-0 shrink-0 basis-17.5;
}

.break {
  @apply break-all;
}
</style>
