<script setup lang="ts">
import { ref } from 'vue'
import Icon from '../ui/Icon.vue'
import { useMessages } from '../../i18n'

// Short words written one at a time: Enter or a comma makes a chip, Backspace on an empty field
// takes the last one back, and leaving the field keeps what was written.
const { values, placeholder } = defineProps<{ values: string[]; placeholder?: string }>()
const emit = defineEmits<{ (e: 'change', values: string[]): void }>()

const { t } = useMessages()
const draft = ref('')

function add() {
  const wanted = draft.value.trim().replace(/,$/, '')
  draft.value = ''
  if (!wanted || values.includes(wanted)) return
  emit('change', [...values, wanted])
}

function remove(value: string) {
  emit('change', values.filter((one) => one !== value))
}

function onKey(event: KeyboardEvent) {
  if (event.key === 'Enter' || event.key === ',') {
    event.preventDefault()
    add()
  } else if (event.key === 'Backspace' && !draft.value && values.length) {
    remove(values[values.length - 1])
  }
}
</script>

<template>
  <div class="chips">
    <span v-for="value in values" :key="value" class="chip">
      {{ value }}
      <button type="button" :title="t('settings.chipRemove')" @click="remove(value)">
        <Icon name="X" :size="8" :stroke-width="3" />
      </button>
    </span>
    <input
      v-model="draft"
      type="text"
      spellcheck="false"
      :placeholder="placeholder"
      @keydown="onKey"
      @blur="add"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.chips {
  @apply flex flex-wrap items-center gap-1.5 w-full rounded-[0.375rem];
  min-height: 2.125rem;
  padding: 0.25rem 0.375rem;
  border: 1px solid var(--border-strong);
  background: var(--bg-inset);
}

.chip {
  @apply inline-flex items-center gap-1 h-6 rounded-[0.25rem] text-12 flex-none;
  padding: 0 0.25rem 0 0.5rem;
  background: var(--bg-hover);
  font-family: var(--mono);
}

.chip button {
  @apply inline-flex items-center justify-center rounded-sm cursor-pointer;
  width: 1.125rem;
  height: 1.125rem;
  border: 0;
  background: transparent;
  color: var(--text-tertiary);
}

.chip button:hover {
  background: var(--bg-active);
  color: var(--text);
}

.chips input {
  @apply flex-1 border-0 outline-none bg-transparent text-12 text-text;
  min-width: 7.5rem;
  height: 1.5rem;
  padding: 0 0.25rem;
  font-family: var(--mono);
}
</style>
