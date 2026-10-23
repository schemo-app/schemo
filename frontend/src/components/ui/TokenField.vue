<script setup lang="ts">
import { computed } from 'vue'
import VarToken from './VarToken.vue'
import { vVarPeek } from '../../lib/varPeek'
import { parseTokens, tokenSegments } from '../../lib/vars'
import { isNumberText } from '../../lib/format'

const { value, variant = 'plain' } = defineProps<{
  value: string
  placeholder?: string
  variant?: 'plain' | 'cell'
}>()

const emit = defineEmits<{ (e: 'change', value: string): void }>()

const segments = computed(() => tokenSegments(value))
const hasTokens = computed(() => parseTokens(value).length > 0)
const valueClass = computed(() => (isNumberText(value) ? 'num' : 'str'))

function syncScroll(e: Event) {
  const input = e.target as HTMLInputElement
  const display = input.parentElement?.querySelector<HTMLElement>('.row-display')
  if (display) display.scrollLeft = input.scrollLeft
}
</script>

<template>
  <div class="row-cell" :class="variant">
    <input
      v-var-peek
      :value
      class="row-input mono"
      :class="[valueClass, { 'row-input-veiled': hasTokens }]"
      :placeholder
      spellcheck="false"
      @input="emit('change', ($event.target as HTMLInputElement).value); syncScroll($event)"
      @scroll="syncScroll"
    />
    <span
      v-if="hasTokens"
      class="row-input row-display mono"
      :class="valueClass"
      aria-hidden="true"
    >
      <template v-for="(seg, si) in segments" :key="si">
        <VarToken v-if="seg.tokenName" :name="seg.tokenName" :text="seg.text" />
        <span v-else>{{ seg.text }}</span>
      </template>
    </span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.row-cell {
  @apply relative flex min-w-0;
}

.row-input {
  @apply min-w-0 bg-transparent border-0 outline-none text-13 p-0 rounded-sm;
  font-family: var(--mono);
  color: var(--text);
}

.row-input:focus {
  background: var(--bg-inset);
}

.row-input.row-input-veiled {
  color: transparent;
  caret-color: var(--text);
}

.row-display {
  @apply absolute inset-0 flex items-center overflow-hidden pointer-events-none;
  white-space: pre;
}

.row-input.str {
  color: var(--tok-str);
}

.row-input.num {
  color: var(--tok-num);
}

.row-cell.cell {
  @apply flex-1 w-full;
}

.cell .row-input {
  @apply w-full;
  height: 1.625rem;
  padding: 0 0.5rem;
  border-radius: 0.3rem;
  font-size: var(--text-12-5);
}

.cell .row-display {
  height: 1.625rem;
}

.cell .row-input:not(.row-display):hover {
  background: var(--bg-hover);
}

.cell .row-input:not(.row-display):focus {
  background: var(--bg-inset);
  box-shadow: 0 0 0 1px var(--accent);
}

.cell .row-input.str:not(.row-input-veiled) {
  color: var(--text);
}

.cell .row-input::placeholder {
  color: var(--text-tertiary);
}
</style>
