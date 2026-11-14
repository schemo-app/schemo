<script setup lang="ts">
import type { ViewerLanguage } from '../../lib/viewerLanguage'
import TextEditor from '../ui/TextEditor.vue'

// The body of a GraphQL, gRPC or WebSocket request: a row of tools over an editor.
const { modelValue, language = null, placeholder = '', label, error = '', grows = false } = defineProps<{
  modelValue: string
  language?: ViewerLanguage
  placeholder?: string
  label: string
  error?: string
  // A short message grows with its lines instead of filling the pane.
  grows?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="editor-pane" :class="{ grows }">
    <div class="editor-pane-tools">
      <slot name="tools" />
      <span class="editor-pane-spacer"></span>
      <span v-if="error" class="editor-pane-error" role="status">{{ error }}</span>
      <slot name="actions" />
    </div>
    <TextEditor
      class="editor-pane-editor" compact :model-value="modelValue" :language :placeholder :label
      @update:model-value="emit('update:modelValue', $event)"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.editor-pane {
  @apply flex flex-1 flex-col min-h-0 min-w-0;
}

.editor-pane.grows {
  flex: none;
}

.editor-pane-tools {
  @apply flex flex-none items-center min-w-0;
  height: 2.5rem;
  gap: 0.5rem;
  padding: 0.25rem 1.25rem 0;
}

.editor-pane-spacer {
  @apply flex-1;
}

.editor-pane-error {
  @apply truncate;
  font-size: var(--text-12);
  color: var(--red-text);
}

.editor-pane-editor {
  min-height: 9.375rem;
}

.grows .editor-pane-editor {
  flex: none;
  min-height: 6.875rem;
}

.editor-pane-editor :deep(.cm-content) {
  padding: 0.75rem 0;
}

.editor-pane-editor :deep(.cm-line) {
  padding-inline: 1.25rem;
}
</style>
