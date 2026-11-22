<script setup lang="ts">
import { nextTick, reactive, ref } from 'vue'

const { create, namePlaceholder, valuePlaceholder, validateName } = defineProps<{
  create: (name: string, value: string) => Promise<boolean>
  namePlaceholder: string
  valuePlaceholder: string
  validateName?: (name: string) => string
}>()
const emit = defineEmits<{ invalid: [message: string] }>()
const draft = reactive({ name: '', value: '' })
const active = ref<'name' | 'value' | null>(null)
const inputs = new Map<'name' | 'value', HTMLInputElement | null>()
const saving = ref(false)

function start(field: 'name' | 'value') {
  if (saving.value) return
  active.value = field
  emit('invalid', '')
  void nextTick(() => inputs.get(field)?.focus())
}

function cancel() {
  if (saving.value) return
  draft.name = ''
  draft.value = ''
  active.value = null
  emit('invalid', '')
}

async function submit() {
  if (saving.value) return
  const name = draft.name.trim()
  if (!name) {
    draft.name = ''
    active.value = null
    return
  }
  const error = validateName?.(name)
  if (error) {
    emit('invalid', error)
    return
  }
  saving.value = true
  try {
    if (await create(name, draft.value)) {
      draft.name = ''
      draft.value = ''
      active.value = null
    }
  } finally {
    saving.value = false
  }
}

function next(event: KeyboardEvent) {
  if (event.shiftKey) return
  event.preventDefault()
  if (!draft.name.trim()) start('name')
  else void submit()
}
</script>

<template>
  <div class="row-create">
    <slot name="before"><span></span></slot>
    <div v-for="field in (['name', 'value'] as const)" :key="field" class="cell">
      <input
        v-if="active === field"
        :ref="element => inputs.set(field, element as HTMLInputElement | null)"
        :value="draft[field]"
        :disabled="saving"
        :aria-label="field === 'name' ? namePlaceholder : valuePlaceholder"
        spellcheck="false"
        @input="draft[field] = ($event.target as HTMLInputElement).value"
        @blur="active === field && submit()"
        @keydown.enter="next"
        @keydown.tab="next"
        @keydown.esc.stop.prevent="cancel"
      />
      <button v-else type="button" :disabled="saving" @click="start(field)">
        {{ draft[field] || (field === 'name' ? namePlaceholder : valuePlaceholder) }}
      </button>
      <slot v-if="field === 'value'" name="value-suffix" />
    </div>
    <slot />
    <span></span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.row-create {
  display: grid;
  grid-template-columns: var(--row-columns, 28px minmax(0, 1fr) minmax(0, 1.4fr) 80px);
  align-items: center;
  height: var(--create-row-height, 2rem);
  color: var(--text-tertiary);
  font-size: var(--text-13);
}

.cell {
  display: flex;
  align-items: center;
  min-width: 0;
  height: 100%;
  padding: var(--create-cell-padding, 0 12px 0 0);
}

input,
button {
  min-width: 0;
  width: 100%;
  padding: 0;
  border: 0;
  outline: none;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: text;
}

input {
  height: 28px;
  font-family: var(--mono);
  color: var(--text);
}

button {
  height: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
