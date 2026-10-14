<script setup lang="ts">
import { computed, watch } from 'vue'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useIndent } from '../../composables/useIndent'
import { useScriptHalves } from '../../composables/useScriptHalves'
import { useMessages } from '../../i18n'
import { ScriptingService } from '../../../bindings/schemo/internal/transport/wails'
import { callOrToast } from '../../stores/calls'
import type { Scripts } from '../../../bindings/schemo/internal/domain'

// The code the collection runs around every request of it: one half before the request and one after
// the answer. Both are written here at once — the page has the width for it — and a pause in the
// typing is what writes them down.
const { levelId, scripts } = defineProps<{ levelId: string; scripts: Scripts | null }>()

const emit = defineEmits<{ saved: [scripts: Scripts | null] }>()

const { t } = useMessages()
const { insert } = useIndent()

const { pre, post, off, hold } = useScriptHalves()

// Longer than the request's own boxes wait (FLUSH_MS) before they write.
const PAUSE_MS = 600
const { dirty, touch, flush: commit } = useDeferredWrite(save, PAUSE_MS)

// The boxes show what the level holds: a level the page has just moved to starts empty, and nothing
// that is being typed is overwritten by an answer about the level it was written for.
watch(
  [() => levelId, () => scripts],
  ([level]) => {
    if (!level || dirty.value) return
    hold(scripts)
  },
  { immediate: true }
)

watch(
  () => levelId,
  () => {
    dirty.value = false
    hold()
  }
)

async function save() {
  if (!levelId) return
  // A level with nothing in either half goes back to inheriting, which is not the same as a level
  // that has said "nothing runs here".
  const written = pre.value.trim() || post.value.trim()
    ? {
        pre: pre.value,
        post: post.value,
        preOff: off.value.pre && !!pre.value.trim(),
        postOff: off.value.post && !!post.value.trim(),
      }
    : null
  const saved = await callOrToast(ScriptingService.SaveScripts(levelId, written), 'collections.saveFailed')
  if (saved !== undefined) emit('saved', saved)
}

function lines(text: string): number[] {
  return Array.from({ length: Math.max(1, text.split('\n').length) }, (_, i) => i + 1)
}

const preLines = computed(() => lines(pre.value))
const postLines = computed(() => lines(post.value))
</script>

<template>
  <div class="scripts">
    <section class="half">
      <div class="half-head">
        <span class="half-title">{{ t('collections.beforeRequest') }}</span>
        <span class="half-hint">{{ t('collections.preNote') }}</span>
      </div>
      <div class="box">
        <div class="gutter">
          <div v-for="n in preLines" :key="n">{{ n }}</div>
        </div>
        <textarea
          v-model="pre"
          class="code"
          :style="{ height: `${preLines.length * 22}px` }"
          spellcheck="false"
          :placeholder="t('request.body.prePlaceholder')"
          :disabled="!levelId"
          @keydown.tab.prevent="insert"
          @input="touch()"
          @blur="commit"
        ></textarea>
      </div>
    </section>

    <section class="half">
      <div class="half-head">
        <span class="half-title">{{ t('collections.afterResponse') }}</span>
        <span class="half-hint">{{ t('collections.postNote') }}</span>
      </div>
      <div class="box">
        <div class="gutter">
          <div v-for="n in postLines" :key="n">{{ n }}</div>
        </div>
        <textarea
          v-model="post"
          class="code"
          :style="{ height: `${postLines.length * 22}px` }"
          spellcheck="false"
          :placeholder="t('request.body.postPlaceholder')"
          :disabled="!levelId"
          @keydown.tab.prevent="insert"
          @input="touch()"
          @blur="commit"
        ></textarea>
      </div>
    </section>

    <span class="foot">{{ t('collections.scriptsAvailable') }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.scripts {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 1.125rem 1.5rem;
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

.foot {
  @apply text-text-tertiary;
  font-size: var(--text-12-5);
}
</style>
