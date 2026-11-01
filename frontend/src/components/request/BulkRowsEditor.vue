<script setup lang="ts">
import { computed, onMounted, ref, useId, useTemplateRef } from 'vue'
import Button from '../ui/Button.vue'
import { mergeRows, parseRows, rowsToText } from '../../lib/bulkRows'
import type { Row } from '../../../bindings/schemo/internal/domain'
import { t } from '../../i18n'

const { rows } = defineProps<{ rows: Row[] }>()
const emit = defineEmits<{ apply: [rows: Row[]]; cancel: [] }>()
const hintId = useId()
const editor = useTemplateRef<HTMLTextAreaElement>('editor')
const text = ref(rowsToText(rows))
const parsed = computed(() => parseRows(text.value))
const invalid = computed(() => parsed.value.errorLine !== null)
const error = computed(() => {
    const line = parsed.value.errorLine
    return line === null ? '' : t('request.pane.bulkError', { line })
})

onMounted(() => editor.value?.focus())

function apply() {
    if (invalid.value) return
    emit('apply', mergeRows(parsed.value.rows, rows))
}
</script>

<template>
  <div class="bulk-editor">
    <textarea
      ref="editor"
      v-model="text"
      class="bulk-text"
      :aria-label="t('request.pane.bulkEdit')"
      :aria-invalid="invalid"
      :aria-describedby="hintId"
      spellcheck="false"
      autocapitalize="off"
      @keydown.ctrl.enter.stop.prevent="apply"
      @keydown.meta.enter.stop.prevent="apply"
    ></textarea>
    <div
      :id="hintId"
      class="bulk-hint"
      :class="{ error: invalid }"
      :role="invalid ? 'alert' : undefined"
    >
      {{ invalid ? error : t('request.pane.bulkHint') }}
    </div>
    <div class="bulk-actions">
      <Button variant="ghost" @click="emit('cancel')">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :disabled="invalid" @click="apply">
        {{ t('request.docs.apply') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.bulk-editor {
  @apply flex flex-col;
  gap: 0.5rem;
  padding-top: 0.375rem;
}

.bulk-text {
  @apply w-full text-text outline-none;
  min-height: 12rem;
  padding: 0.625rem 0.75rem;
  border: 1px solid var(--border-strong);
  border-radius: 0.5rem;
  background: var(--bg-inset);
  font-family: var(--mono);
  font-size: var(--text-13);
  line-height: 1.65;
  resize: vertical;
  white-space: pre;
  overflow-wrap: normal;
}

.bulk-text:focus-visible {
  border-color: var(--accent);
}

.bulk-hint {
  @apply text-12 text-text-tertiary;
  line-height: 1.5;
}

.bulk-hint.error {
  color: var(--red-text);
}

.bulk-actions {
  @apply flex justify-end;
  gap: 0.5rem;
}
</style>
