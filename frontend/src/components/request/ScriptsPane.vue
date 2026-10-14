<script setup lang="ts">
import { watch } from 'vue'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useIndent } from '../../composables/useIndent'
import { useScriptHalves } from '../../composables/useScriptHalves'
import { useMessages } from '../../i18n'
import type { RequestSource } from '../../lib/requestSource'
import CaptureList from './CaptureList.vue'

// The two halves of code the level runs around its requests, as the request's own pane draws them:
// both at once, and written down on a pause, the way the command line's address is.
const { source, note } = defineProps<{ source: RequestSource; note: string }>()

const { t } = useMessages()
const { insert } = useIndent()

// The two off flags travel with the code, so the level above cannot hand down a script it has
// switched off.
const { pre, post, off, hold } = useScriptHalves()
const { dirty, touch, flush: commit } = useDeferredWrite(() =>
  source.saveScripts(pre.value, post.value, off.value)
)

// What would run for one half while this level's own box stays empty. Greyed out in the box, because an
// empty box beside a chain of code is not a box that does nothing.
function inherited(which: 'pre' | 'post'): string {
  return source.inheritedScript(which)?.text ?? ''
}

// What the boxes show: the code of the level the window is on. Reading it is the level's own business —
// a collection card reads the node it opened, the command line reads its draft — so the level is asked
// for and the answer is applied only when it is about that level and nobody is typing in it.
watch(
  [() => source.scriptsLevel, () => source.scripts],
  ([level]) => {
    if (!level || source.scriptsFor !== level || dirty.value) return
    hold(source.scripts)
  },
  { immediate: true }
)

watch(
  () => source.scriptsLevel,
  (level) => {
    dirty.value = false
    // The level changed: what is in the boxes belongs to the one that was open before.
    hold()
    if (level) void source.loadScripts()
  },
  { immediate: true }
)
</script>

<template>
  <div class="fields">
    <label class="field">
      <span class="label">{{ t('collections.scriptPre') }}</span>
      <textarea
        v-model="pre"
        class="code"
        spellcheck="false"
        :placeholder="inherited('pre') || t('request.body.prePlaceholder')"
        @keydown.tab.prevent="insert"
        @input="touch()"
        @blur="commit"
      />
    </label>

    <label class="field">
      <span class="label">{{ t('collections.scriptPost') }}</span>
      <textarea
        v-model="post"
        class="code"
        spellcheck="false"
        :placeholder="inherited('post') || t('request.body.postPlaceholder')"
        @keydown.tab.prevent="insert"
        @input="touch()"
        @blur="commit"
      />
    </label>

    <CaptureList :source />

    <div class="note plain">
      <span class="note-text">{{ note }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.fields {
  @apply flex flex-col gap-3;
}

.field {
  @apply flex flex-col;
  gap: 0.375rem;
}

.label {
  @apply text-12 font-medium text-text-secondary;
}

.code {
  @apply h-26.5 resize-y rounded-[0.5rem] border border-border-strong bg-bg-inset
         px-3 py-2.5 font-mono text-13 leading-[1.6] text-text;
}

.code::placeholder {
  @apply text-text-tertiary;
}

.code:focus {
  @apply outline-none border-accent;
}

.note {
  @apply flex items-start gap-2 px-3 py-2.5 rounded-lg bg-accent-soft;
}

.note.plain {
  @apply bg-transparent p-0 text-13 text-text-tertiary;
}

.note-text {
  @apply text-12 leading-normal;
}

.note.plain .note-text {
  @apply text-12 text-text-tertiary;
}
</style>
