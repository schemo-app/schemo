<script setup lang="ts">
import { computed } from 'vue'
import VarToken from './VarToken.vue'
import { tokenSegments } from '../../lib/vars'

const { value, multiline = false } = defineProps<{ value: string; multiline?: boolean }>()

const segments = computed(() => tokenSegments(value))
</script>

<template>
  <span class="highlight" :class="{ area: multiline }" aria-hidden="true">
    <template v-for="(seg, si) in segments" :key="si">
      <VarToken v-if="seg.tokenName" :name="seg.tokenName" :text="seg.text" />
      <span v-else>{{ seg.text }}</span>
    </template>
  </span>
</template>

<style scoped>
@reference "../../style.css";

.highlight {
  @apply absolute inset-0 flex items-center overflow-hidden;
  padding: var(--field-pad, 0 0.75rem);
  white-space: pre;
  pointer-events: none;
}

.highlight.area {
  @apply items-start;
}
</style>
