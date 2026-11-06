<script setup lang="ts">
import { computed, useId } from 'vue'
import { useIndent } from '../../composables/useIndent'
import { t } from '../../i18n'

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

const preLines = computed(() => lines(pre.value))
const postLines = computed(() => lines(post.value))
</script>

<template>
  <div class="scripts-editor">
    <section class="half">
      <div class="half-head">
        <label :for="`${id}-pre`" class="half-title">{{ t('collections.beforeRequest') }}</label>
        <span class="half-hint">{{ t('collections.preNote') }}</span>
      </div>
      <div class="box">
        <div class="gutter" aria-hidden="true">
          <div v-for="line in preLines" :key="line">{{ line }}</div>
        </div>
        <textarea
          :id="`${id}-pre`"
          v-model="pre"
          class="code"
          :style="{ height: `${preLines.length * 22}px` }"
          spellcheck="false"
          :placeholder="prePlaceholder ?? t('request.body.prePlaceholder')"
          :disabled
          @keydown.tab.prevent="insert"
          @input="emit('input')"
          @blur="emit('blur')"
        ></textarea>
      </div>
    </section>

    <section class="half">
      <div class="half-head">
        <label :for="`${id}-post`" class="half-title">{{ t('collections.afterResponse') }}</label>
        <span class="half-hint">{{ t('collections.postNote') }}</span>
      </div>
      <div class="box">
        <div class="gutter" aria-hidden="true">
          <div v-for="line in postLines" :key="line">{{ line }}</div>
        </div>
        <textarea
          :id="`${id}-post`"
          v-model="post"
          class="code"
          :style="{ height: `${postLines.length * 22}px` }"
          spellcheck="false"
          :placeholder="postPlaceholder ?? t('request.body.postPlaceholder')"
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
  font-size: var(--text-12-5);
  line-height: 22px;
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
}

.code::placeholder {
  color: var(--text-tertiary);
}
</style>
