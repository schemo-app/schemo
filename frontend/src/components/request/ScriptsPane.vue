<script setup lang="ts">
import { watch } from 'vue'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useScriptHalves } from '../../composables/useScriptHalves'
import { t } from '../../i18n'
import type { RequestSource } from '../../lib/requestSource'
import CaptureList from './CaptureList.vue'
import ScriptsEditor from '../scripts/ScriptsEditor.vue'

// The two halves of code the level runs around its requests, as the request's own pane draws them:
// both at once, and written down on a pause, the way the command line's address is.
const { source } = defineProps<{ source: RequestSource }>()

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

// What the boxes show: the code of the level the window is on. Reading it is the level's own
// business (a collection card reads the node it opened, the command line reads its draft), so the
// level is asked for and the answer is applied only when it is about that level and nobody is
// typing in it.
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
    <ScriptsEditor
      v-model:pre="pre"
      v-model:post="post"
      :pre-placeholder="inherited('pre') || t('request.body.prePlaceholder')"
      :post-placeholder="inherited('post') || t('request.body.postPlaceholder')"
      @input="touch()"
      @blur="commit"
    />

    <CaptureList :source />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.fields {
  @apply flex flex-col gap-3;
}
</style>
