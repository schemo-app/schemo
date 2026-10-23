<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import type { RecordView } from '../../lib/requestRecord'
import { formatStatus, statusBadgeClass } from '../../lib/format'
import { formatBytes, formatMicros } from '../../i18n'

// The end of the answer's row: what came back, then what the reading on screen offers, handed in by
// the viewer. The time is the size's neighbour and both are the answer's own numbers.
const { record, searchable, searching, searchTitle, inspectable, inspectorOpen, inspectorTitle } =
  defineProps<{
    record: RecordView
    searchable: boolean
    searching: boolean
    searchTitle: string
    inspectable: boolean
    inspectorOpen: boolean
    inspectorTitle: string
  }>()

defineEmits<{ toggleSearch: []; toggleInspector: [] }>()

const bodySize = () => record.responseBytes ?? new Blob([record.responseBody]).size

const statusLine = computed(() => formatStatus(record.status, record.statusText))
</script>

<template>
  <div class="resp-meta">
    <span class="resp-status" :class="statusBadgeClass(record.status)">{{ statusLine }}</span>
    <span class="resp-num">{{ formatMicros(record.durationUs) }}</span>
    <span class="resp-num" :title="record.contentType || undefined">{{ formatBytes(bodySize()) }}</span>
  </div>

  <span class="resp-div"></span>

  <div class="resp-actions">
    <slot name="leading" />

    <IconButton
      v-if="searchable"
      variant="bare"
      size="bar"
      class="row-mark"
      :class="{ on: searching }"
      :hint="searchTitle"
      @click="$emit('toggleSearch')"
    >
      <Icon name="Search" :size="16" :stroke-width="1.8" />
    </IconButton>

    <slot />

    <IconButton
      v-if="inspectable"
      variant="bare"
      size="bar"
      class="row-mark inspector-toggle"
      :class="{ on: inspectorOpen }"
      :hint="inspectorTitle"
      @click="$emit('toggleInspector')"
    >
      <Icon name="PanelRight" :size="16" :stroke-width="1.8" />
    </IconButton>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.resp-meta {
  @apply flex flex-none items-center self-center;
  gap: 0.625rem;
  padding-left: 0.5rem;
}

.resp-num {
  @apply whitespace-nowrap text-text-secondary;
  line-height: 1rem;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.resp-status {
  @apply flex-none whitespace-nowrap font-semibold;
  padding: 0.125rem 0.4375rem;
  line-height: 1rem;
  border-radius: 0.3125rem;
  font-size: var(--text-11-5);
}

.resp-div {
  @apply flex-none self-center;
  width: 1px;
  height: 1.125rem;
  margin: 0 0.3rem 0 0.625rem;
  background: var(--border);
}

.resp-div:has(+ .resp-actions:empty) {
  display: none;
}

.resp-actions {
  @apply flex items-center self-center flex-none;
  gap: 2px;
}

.resp-actions :deep(.row-mark) {
  color: var(--text-secondary);
}

.resp-actions :deep(.row-mark.on) {
  background: var(--bg-hover);
  color: var(--text);
}

.resp-actions :deep(.inspector-toggle.on),
.resp-actions :deep(.inspector-toggle.on:hover) {
  background: var(--accent-soft);
  color: var(--accent);
}
</style>
