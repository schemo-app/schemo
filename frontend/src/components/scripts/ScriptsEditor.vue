<script setup lang="ts">
import { computed, useId, type Ref } from 'vue'
import { useIndent } from '../../composables/useIndent'
import { t, type MessageKey } from '../../i18n'

const { prePlaceholder, postPlaceholder, disabled = false } = defineProps<{
  prePlaceholder?: string
  postPlaceholder?: string
  disabled?: boolean
}>()

const pre = defineModel<string>('pre', { required: true })
const post = defineModel<string>('post', { required: true })
const emit = defineEmits<{ input: []; blur: [] }>()
const id = useId()
const { insert } = useIndent()

function lines(text: string): number[] {
  return Array.from({ length: Math.max(1, text.split('\n').length) }, (_, index) => index + 1)
}

const halves = computed(() => [
  half('pre', pre, 'collections.beforeRequest', 'collections.preNote', prePlaceholder ?? t('request.body.prePlaceholder')),
  half('post', post, 'collections.afterResponse', 'collections.postNote', postPlaceholder ?? t('request.body.postPlaceholder')),
])

function half(key: string, model: Ref<string>, title: MessageKey, hint: MessageKey, placeholder: string) {
  return { key, model, title, hint, placeholder, lines: lines(model.value) }
}
</script>

<template>
  <div class="scripts-editor">
    <section v-for="item in halves" :key="item.key" class="half">
      <div class="half-head">
        <label :for="`${id}-${item.key}`" class="half-title">{{ t(item.title) }}</label>
        <span class="half-hint">{{ t(item.hint) }}</span>
      </div>
      <div class="box" :style="{ '--lines': item.lines.length }">
        <div class="gutter" aria-hidden="true">
          <div v-for="line in item.lines" :key="line">{{ line }}</div>
        </div>
        <textarea
          :id="`${id}-${item.key}`"
          v-model="item.model.value"
          class="code"
          spellcheck="false"
          :placeholder="item.placeholder"
          :disabled
          @keydown.tab.prevent="insert"
          @input="emit('input')"
          @blur="emit('blur')"
        ></textarea>
      </div>
    </section>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.scripts-editor {
  @apply flex flex-col;
  gap: 1.375rem;
}

.half {
  @apply flex flex-col;
  gap: 0.5rem;
}

.half-head {
  @apply flex items-baseline;
  gap: 0.625rem;
}

.half-title {
  @apply font-semibold;
}

.half-hint {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.box {
  display: grid;
  grid-template-columns: 2.5rem minmax(0, 1fr);
  padding: 0.625rem 0;
  border-radius: 0.625rem;
  background: var(--raise);
  font-family: var(--mono);
  font-size: var(--text-13);
  --code-line: 22px;
  line-height: var(--code-line);
}

.gutter {
  text-align: right;
  padding-right: 0.875rem;
  color: var(--text-tertiary);
  user-select: none;
}

.code {
  @apply block w-full border-0 outline-none bg-transparent p-0;
  resize: none;
  color: var(--text);
  font: inherit;
  white-space: pre;
  overflow: hidden;
  height: calc(var(--lines) * var(--code-line));
}

.code::placeholder {
  color: var(--text-tertiary);
}
</style>
