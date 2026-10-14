<script setup lang="ts">
import { computed } from 'vue'
import { Sheet } from '../ui/sheet'
import { useMcp } from '../../composables/useMcp'
import { useMessages } from '../../i18n'

// The question «Спрашивать перед отправкой» comes to: an agent asked for a request to go out, and
// it goes nowhere until somebody here says so. What it shows is what the app knows — the agent, the
// method and the masked address — because a confirmation with a secret in it would be one more
// place a secret is written down.

const { t } = useMessages()
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
