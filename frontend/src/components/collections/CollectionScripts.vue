<script setup lang="ts">
import { watch } from 'vue'
import { useDeferredWrite } from '../../composables/useDeferredWrite'
import { useScriptHalves } from '../../composables/useScriptHalves'
import { t } from '../../i18n'
import { Scripts as ScriptsService } from '../../../bindings/schemo/internal/script'
import { callOrToast } from '../../lib/calls'
import type { Scripts } from '../../../bindings/schemo/internal/domain'
import ScriptsEditor from '../scripts/ScriptsEditor.vue'

// The code the collection runs around every request of it: one half before the request and one
// after the answer. Both are written here at once, and a pause in the typing writes them down.
const { levelId, scripts } = defineProps<{ levelId: string; scripts: Scripts | null }>()

const emit = defineEmits<{ saved: [scripts: Scripts | null] }>()

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
  const saved = await callOrToast(ScriptsService.SaveScripts(levelId, written), 'collections.saveFailed')
  if (saved !== undefined) emit('saved', saved)
}
</script>

<template>
  <div class="scripts">
    <ScriptsEditor
      v-model:pre="pre"
      v-model:post="post"
      :disabled="!levelId"
      @input="touch()"
      @blur="commit"
    />

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

.foot {
  @apply text-text-tertiary;
  font-size: var(--text-12-5);
}
</style>
