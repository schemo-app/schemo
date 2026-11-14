<script setup lang="ts">
import { computed } from 'vue'
import Sheet from '../ui/Sheet.vue'
import { useMcp } from '../../composables/useMcp'
import { t } from '../../i18n'

// Only the masked address is shown, so the prompt never carries a secret.

const { question, answerSend } = useMcp()

const note = computed(() => {
  const ask = question.value
  if (!ask) return ''
  return t('mcp.confirmNote', { agent: ask.agent, method: ask.method, url: ask.url })
})
</script>

<template>
  <Sheet
    v-if="question"
    :title="t('mcp.confirmTitle')"
    :cancel="t('mcp.confirmDeny')"
    :action="t('mcp.confirmAllow')"
    @close="answerSend(false)"
    @cancel="answerSend(false)"
    @action="answerSend(true)"
  >
    <p class="note">{{ note }}</p>
  </Sheet>
</template>

<style scoped>
@reference "../../style.css";

.note {
  @apply m-0 text-13 text-text-secondary;
  line-height: 1.55;
  word-break: break-all;
}
</style>
